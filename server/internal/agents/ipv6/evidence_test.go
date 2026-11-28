package ipv6

import (
	"configflow/internal/jsonresp"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/routers/ros"
)

func TestTupleEvidence(t *testing.T) {
	request := Document{"source_ip": jsonresp.Value("192.0.2.8"), "source_port": jsonresp.Value(34567), "address": jsonresp.Value("198.51.100.9"), "status": jsonresp.Value("ok"), "family": jsonresp.Value("ipv4")}
	match := Document{"source_ip": jsonresp.Value("192.0.2.8"), "source_port": jsonresp.Value(34567), "host": jsonresp.Value("site.example"), "network": jsonresp.Value("tcp"), "destination_port": jsonresp.Value(443), "proxy_name": jsonresp.Value("node"), "proxy_type": jsonresp.Value("ss"), "chains": jsonresp.Value([]string{"node"}), "ipv6_transport_verified": jsonresp.Value(true)}
	fixtures := []Document{}
	for _, change := range []Document{nil, {"source_port": jsonresp.Value(1)}, {"host": jsonresp.Value("other.example")}, {"chains": jsonresp.Value([]string{"DIRECT"})}, {"ipv6_transport_verified": jsonresp.Value(false)}} {
		m := object(jsonresp.Value(match))
		for k, v := range change {
			m[k] = v
		}
		report := Document{"target_host": jsonresp.Value("site.example"), "requests": jsonresp.Value([]Document{request})}
		trace := Document{"target_host": jsonresp.Value("site.example"), "matches": jsonresp.Value([]Document{m})}
		fixtures = append(fixtures, Document{"report": jsonresp.Value(report), "trace": jsonresp.Value(trace), "url": jsonresp.Value("https://site.example/")})
	}
	fixtures = append(fixtures, Document{"report": jsonresp.Value(Document{"requests": jsonresp.Value([]any{request, nil})}), "trace": jsonresp.Value(Document{}), "url": jsonresp.Value("https://site.example/")})
	for i, f := range fixtures {
		checks := evaluate(context.Background(), nil, nil, object(f["report"]), object(f["trace"]), text(f["url"]), nil)
		if i == 0 && (checks[0].Status != "ok" || checks[1].Status != "ok") {
			t.Fatal("positive control lacks evidence")
		}
		if i > 0 && checks[1].Status == "ok" {
			t.Fatal("invalid tuple proved IPv6")
		}
	}
}
func TestTransportCheckNamesTheNode(t *testing.T) {
	request := Document{"source_ip": jsonresp.Value("192.0.2.8"), "source_port": jsonresp.Value(34567), "address": jsonresp.Value("198.51.100.9"), "status": jsonresp.Value("ok"), "family": jsonresp.Value("ipv4")}
	tests := []struct {
		name       string
		match      Document
		wantStatus string
		wantText   string
	}{
		{"ipv6 node", Document{"transport": jsonresp.Value("ipv6"), "ipv6_transport_verified": jsonresp.Value(true)}, "ok", fmt.Sprintf(msgClientTransportVia, "IPv6 组 → v6-node")},
		{"ipv4 node", Document{"transport": jsonresp.Value("ipv4")}, "unknown", fmt.Sprintf(msgClientTransportIPv4, "IPv6 组 → v6-node")},
		{"unreliable remote", Document{"transport": jsonresp.Value("unknown")}, "unknown", fmt.Sprintf(msgClientTransportUnknown, "IPv6 组 → v6-node")},
		{"agent before 1.9.5-go", Document{}, "unknown", msgClientTransportMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := Document{"source_ip": jsonresp.Value("192.0.2.8"), "source_port": jsonresp.Value(34567), "host": jsonresp.Value("site.example"), "network": jsonresp.Value("tcp"), "destination_port": jsonresp.Value(443), "proxy_name": jsonresp.Value("v6-node"), "proxy_type": jsonresp.Value("AnyTLS"), "chains": jsonresp.Value([]string{"v6-node", "IPv6 组"})}
			for k, v := range test.match {
				m[k] = v
			}
			report := Document{"target_host": jsonresp.Value("site.example"), "requests": jsonresp.Value([]Document{request})}
			trace := Document{"target_host": jsonresp.Value("site.example"), "matches": jsonresp.Value([]Document{m})}
			checks := evaluate(context.Background(), nil, nil, report, trace, "https://site.example/", nil)
			if checks[0].Status != "ok" || checks[1].Status != test.wantStatus || checks[1].Message != test.wantText {
				t.Fatalf("got %s %q", checks[1].Status, checks[1].Message)
			}
		})
	}
}
func TestConnectionTupleCountersAndLegacy(t *testing.T) {
	rows := []ros.Row{{"protocol": "tcp", "src-address": "192.0.2.8", "src-port": "34567", "dst-address": "198.51.100.9", "dst-port": "443", "orig-packets": "1"}, {"protocol": "tcp", "src-address": "[2001:db8::8]:34567", "dst-address": "[2001:db8::9]:443", "orig-fasttrack-packets": "2"}}
	for i, row := range rows {
		source, destination := "192.0.2.8", "198.51.100.9"
		if i == 1 {
			source, destination = "2001:db8::8", "2001:db8::9"
		}
		if !rowMatches(row, source, 34567, destination, 443) {
			t.Fatal("valid tuple rejected", row)
		}
		if rowMatches(row, source, 34567, "2001:db8::10", 443) {
			t.Fatal("unrelated flow accepted")
		}
		row["orig-packets"] = "0"
		row["orig-fasttrack-packets"] = "0"
		if rowMatches(row, source, 34567, destination, 443) {
			t.Fatal("zero packets proves path")
		}
	}
}
func TestObserveIncompleteBaselineNeverCreatesFreshEvidence(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "bad-baseline"}[incomplete], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/rest/") || r.URL.Query().Get("protocol") != "tcp" || r.URL.Query().Get("dst-port") != "443" || r.URL.Query().Get(".proplist") == "" {
					t.Error("unrestricted request", r.Method, r.URL.String())
				}
				n := calls.Add(1)
				if n <= 2 {
					if incomplete {
						_, _ = w.Write([]byte(`[false,{".id":"old"}]`))
					} else {
						_, _ = w.Write([]byte(`[{".id":"old"}]`))
					}
					return
				}
				_, _ = w.Write([]byte(`[{".id":"fresh","protocol":"tcp","src-address":"192.0.2.8","src-port":"34567","dst-address":"198.51.100.9","dst-port":"443","orig-packets":"1"}]`))
			}))
			defer server.Close()
			config := Document{"routers": jsonresp.Value(map[string]any{"items": []any{map[string]any{"id": "r1", "type": "routeros", "endpoint": server.URL, "username": "api", "password": "synthetic-router-pass", "proxy_gateway_agent_id": "source"}}})}
			ctx, cancel := context.WithCancel(context.Background())
			ready := make(chan struct{}, 1)
			done := make(chan observed, 1)
			go func() {
				done <- observe(ctx, config, Document{"id": jsonresp.Value("source")}, "https://site.example", ready)
			}()
			<-ready
			if !incomplete {
				deadline := time.Now().Add(3 * time.Second)
				for calls.Load() < 4 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
			}
			cancel()
			result := <-done
			if incomplete && len(result["r1"]) != 0 {
				t.Fatal("bad baseline proved freshness")
			}
			if !incomplete && len(result["r1"]) == 0 {
				t.Fatal("fresh connection discarded")
			}
		})
	}
}

func TestPacketCountersFailClosed(t *testing.T) {
	// Device null and true arrive as "" and "true" (Row decoding).
	for _, value := range []string{"", "bad", "-1", "18446744073709551616", "true"} {
		if hasPackets(ros.Row{"orig-packets": value, "orig-fasttrack-packets": "1"}) {
			t.Fatal("invalid counter proved packet", value)
		}
	}
	if !hasPackets(ros.Row{"orig-packets": "18446744073709551615", "orig-fasttrack-packets": "18446744073709551615"}) {
		t.Fatal("uint64 counters overflowed")
	}
}
