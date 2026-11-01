package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func historyConn(id, source, host string, chains []string, up, down int64) mihomoConn {
	conn := mihomoConn{ID: id, Upload: up, Download: down, Chains: chains}
	conn.Metadata.SourceIP = source
	conn.Metadata.Host = host
	conn.Metadata.DestinationIP = "203.0.113.9"
	return conn
}

func snapshotAt(at time.Time, up, down int64, conns ...mihomoConn) mihomoConnSnapshot {
	return mihomoConnSnapshot{At: at, Conns: conns, UploadTotal: up, DownloadTotal: down, Controller: &mihomoController{}}
}

// chain 用 > 连接，从入口策略组到出口节点
func usageOf(report trafficHistoryReport, source, chain string) trafficUsageItem {
	for _, item := range report.Usage {
		if item.Source == source && strings.Join(item.Chain, ">") == chain {
			return item
		}
	}
	return trafficUsageItem{}
}

func TestTrafficHistoryCountsDeltasNewConnectionsAndUnattributed(t *testing.T) {
	collector := newTrafficHistoryCollector(&Config{}, nil)
	at := time.Date(2026, 11, 1, 10, 15, 0, 0, time.UTC)
	// mihomo 的 chains 出口节点在前：Proxy 选了 Media，Media 选了 HK
	proxied := []string{"HK", "Media", "Proxy"}
	// 基线：建立前的累计量不计入
	collector.observe(snapshotAt(at, 1000, 5000, historyConn("a", "10.0.0.2", "www.example.com", proxied, 100, 4000)))
	if report := collector.drain(); len(report.Usage) != 0 {
		t.Fatalf("baseline must not record traffic: %+v", report)
	}
	collector.observe(snapshotAt(at.Add(2*time.Second), 1300, 7000,
		historyConn("a", "10.0.0.2", "www.example.com", proxied, 150, 5000),
		historyConn("b", "", "", nil, 10, 20)))
	report := collector.drain()
	if got := usageOf(report, "10.0.0.2", "Proxy>Media>HK"); got.Up != 50 || got.Down != 1000 || got.Conns != 0 {
		t.Fatalf("existing connection delta: %+v", got)
	}
	// 来源为空记为 Inner；新连接的字节全部计入并计一个连接
	if got := usageOf(report, "Inner", ""); got.Up != 10 || got.Down != 20 || got.Conns != 1 {
		t.Fatalf("new inner connection: %+v", got)
	}
	// 内核总量增量减去各连接增量：上行 300-60，下行 2000-1020
	if got := usageOf(report, "", ""); got.Up != 240 || got.Down != 980 {
		t.Fatalf("unattributed: %+v", got)
	}
	if got := usageOf(report, "10.0.0.2", "Proxy>Media>HK"); got.Hour != at.Truncate(time.Hour).Unix() {
		t.Fatalf("hour bucket: %d", got.Hour)
	}
	domains := map[string]trafficDomainItem{}
	for _, item := range report.Domains {
		domains[item.Source+"|"+item.Domain+"|"+strings.Join(item.Chain, ">")] = item
	}
	// 目标同样按链路分开，没有链路的连接链路为空
	if domains["10.0.0.2|www.example.com|Proxy>Media>HK"].Down != 1000 || domains["Inner|203.0.113.9|"].Conns != 1 {
		t.Fatalf("targets: %+v", report.Domains)
	}
}

func TestTrafficHistoryRebaselinesAfterRestartOrError(t *testing.T) {
	collector := newTrafficHistoryCollector(&Config{}, nil)
	at := time.Now()
	conn := historyConn("a", "10.0.0.2", "example.com", []string{"DIRECT"}, 100, 100)
	collector.observe(snapshotAt(at, 500, 500, conn))
	// 内核重启后总量变小：只重新建立基线
	collector.observe(snapshotAt(at.Add(time.Second), 10, 10, historyConn("c", "10.0.0.2", "example.com", []string{"DIRECT"}, 5, 5)))
	if report := collector.drain(); len(report.Usage) != 0 {
		t.Fatalf("restart must rebaseline: %+v", report)
	}
	collector.observe(mihomoConnSnapshot{Err: fmt.Errorf("unreachable")})
	collector.observe(snapshotAt(at.Add(3*time.Second), 900, 900, historyConn("c", "10.0.0.2", "example.com", []string{"DIRECT"}, 400, 400)))
	if report := collector.drain(); len(report.Usage) != 0 {
		t.Fatalf("a failed read must rebaseline: %+v", report)
	}
}

func TestTrafficHistoryFoldsExtraTargetsIntoOther(t *testing.T) {
	collector := newTrafficHistoryCollector(&Config{}, nil)
	collector.mu.Lock()
	for i := 0; i < trafficHistoryMaxDomains+3; i++ {
		collector.addDomain(1, "10.0.0.2", fmt.Sprintf("site%d.example", i), "全球直连"+trafficChainSeparator+"DIRECT", trafficCounters{Down: 1})
	}
	collector.mu.Unlock()
	report := collector.drain()
	if len(report.Domains) != trafficHistoryMaxDomains+1 {
		t.Fatalf("expected %d rows, got %d", trafficHistoryMaxDomains+1, len(report.Domains))
	}
	for _, item := range report.Domains {
		if item.Domain == "" && (item.Down != 3 || strings.Join(item.Chain, ">") != "全球直连>DIRECT") {
			t.Fatalf("other bucket: %+v", item)
		}
	}
}

func TestTrafficHistoryReportOutcomes(t *testing.T) {
	var mu sync.Mutex
	status := http.StatusInternalServerError
	var received trafficHistoryReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents/agent-1/traffic-history" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(status)
	}))
	defer server.Close()
	disabled := false
	collector := newTrafficHistoryCollector(&Config{ServerURL: server.URL, AgentID: "agent-1", Token: "tok"}, func() { disabled = true })
	add := func() {
		collector.mu.Lock()
		collector.addUsage(trafficUsageKey{Hour: 3600, Source: "10.0.0.2", Chain: "Proxy" + trafficChainSeparator + "HK"}, trafficCounters{Down: 10})
		collector.mu.Unlock()
	}
	add()
	// 服务端出错：数据放回去，下次一起上报
	collector.reportOnce(context.Background())
	add()
	mu.Lock()
	status = http.StatusOK
	mu.Unlock()
	collector.reportOnce(context.Background())
	if received.Schema != trafficHistorySchema || len(received.Usage) != 1 || received.Usage[0].Down != 20 {
		t.Fatalf("retried report: %+v", received)
	}
	if len(collector.drain().Usage) != 0 {
		t.Fatal("a delivered report must be cleared")
	}
	add()
	mu.Lock()
	status = http.StatusConflict
	mu.Unlock()
	collector.reportOnce(context.Background())
	if !disabled || len(collector.drain().Usage) != 0 {
		t.Fatal("409 means the server turned recording off: stop and drop the data")
	}
}

func TestConnPollSlowsDownWithManyConnections(t *testing.T) {
	for conns, want := range map[int]time.Duration{0: time.Second, 2000: time.Second, 2001: 2 * time.Second, 5000: 2 * time.Second, 5001: 5 * time.Second} {
		if got := connPollInterval(time.Second, conns); got != want {
			t.Fatalf("%d connections: got %v, want %v", conns, got, want)
		}
	}
}

// 域名发现与流量记录同时开启时只轮询一次 /connections
func TestConnFeedSharesOnePollBetweenSubscribers(t *testing.T) {
	discoveryPollInterval = 40 * time.Millisecond
	defer func() { discoveryPollInterval = time.Second }()
	var mu sync.Mutex
	polls := 0
	mihomo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polls++
		mu.Unlock()
		w.Write([]byte(`{"uploadTotal":7,"downloadTotal":9,"connections":[]}`))
	}))
	defer mihomo.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(configPath, []byte("external-controller: "+strings.TrimPrefix(mihomo.URL, "http://")+"\n"), 0600)
	cfg := &Config{ConfigPath: configPath}
	first, stopFirst := mihomoConns.subscribe(cfg)
	second, stopSecond := mihomoConns.subscribe(cfg)
	for _, ch := range []<-chan mihomoConnSnapshot{first, second} {
		select {
		case snapshot := <-ch:
			if snapshot.Err != nil || snapshot.UploadTotal != 7 || snapshot.DownloadTotal != 9 {
				t.Fatalf("unexpected snapshot %+v", snapshot)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no snapshot")
		}
	}
	time.Sleep(200 * time.Millisecond)
	stopFirst()
	stopSecond()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	count := polls
	mu.Unlock()
	// 200ms 内约 5 次；两个订阅者各自轮询会翻倍
	if count < 3 || count > 8 {
		t.Fatalf("expected one shared poll loop, got %d polls", count)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if polls != count {
		t.Fatal("polling must stop when nobody subscribes")
	}
}
