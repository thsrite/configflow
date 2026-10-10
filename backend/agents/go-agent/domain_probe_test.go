package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func validProbeRequest() probeRequest {
	return probeRequest{
		Targets:   []probeTarget{{Host: "www.example.com", URL: "https://www.example.com/"}},
		Paths:     []string{"DIRECT", "PROXY"},
		Attempts:  3,
		TimeoutMS: 2000,
	}
}

func TestValidateProbeRequest(t *testing.T) {
	ok := validProbeRequest()
	if err := validateProbeRequest(&ok); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := map[string]func(*probeRequest){
		"no targets":     func(r *probeRequest) { r.Targets = nil },
		"too many paths": func(r *probeRequest) { r.Paths = []string{"a", "b", "c", "d"} },
		"empty path":     func(r *probeRequest) { r.Paths = []string{""} },
		"attempts":       func(r *probeRequest) { r.Attempts = 6 },
		"timeout":        func(r *probeRequest) { r.TimeoutMS = 100 },
		"ip host":        func(r *probeRequest) { r.Targets[0] = probeTarget{Host: "1.2.3.4", URL: "https://1.2.3.4/"} },
		"upper host":     func(r *probeRequest) { r.Targets[0].Host = "WWW.example.com" },
		"scheme":         func(r *probeRequest) { r.Targets[0].URL = "file:///etc/passwd" },
		"host mismatch":  func(r *probeRequest) { r.Targets[0].URL = "https://evil.example.net/" },
		"bad port":       func(r *probeRequest) { r.Targets[0].URL = "https://www.example.com:0/" },
	}
	for name, mutate := range cases {
		req := validProbeRequest()
		req.Targets = append([]probeTarget(nil), req.Targets...)
		mutate(&req)
		if err := validateProbeRequest(&req); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

// fakeDelayController 模拟 Mihomo 的 /proxies/{name}/delay
func fakeDelayController(_ *testing.T, behave func(path, target string) (int, string)) (*httptest.Server, *int64, *int64) {
	var calls, inflight, peak int64
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		current := atomic.AddInt64(&inflight, 1)
		mu.Lock()
		if current > peak {
			peak = current
		}
		mu.Unlock()
		defer atomic.AddInt64(&inflight, -1)
		time.Sleep(10 * time.Millisecond)
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(r.URL.EscapedPath(), "/proxies/")
		escaped := strings.TrimSuffix(rest, "/delay")
		path, _ := url.PathUnescape(escaped)
		status, body := behave(path, r.URL.Query().Get("url"))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	return server, &calls, &peak
}

func probeController(server *httptest.Server) *mihomoController {
	return &mihomoController{BaseURL: server.URL, Secret: "s3cret"}
}

func TestRunProbesClassifiesAndStopsEarly(t *testing.T) {
	server, calls, _ := fakeDelayController(t, func(path, target string) (int, string) {
		switch {
		case path == "DIRECT" && strings.Contains(target, "blocked"):
			return http.StatusGatewayTimeout, `{"message":"Timeout"}`
		case path == "DIRECT":
			return http.StatusOK, `{"delay":120}`
		case path == "PROXY":
			return http.StatusOK, `{"delay":300}`
		}
		return http.StatusNotFound, `{"message":"Resource not found"}`
	})
	defer server.Close()

	req := probeRequest{
		Targets: []probeTarget{
			{Host: "blocked.example.com", URL: "https://blocked.example.com/"},
			{Host: "ok.example.com", URL: "http://ok.example.com/"},
		},
		Paths: []string{"DIRECT", "PROXY", "MISSING"}, Attempts: 3, TimeoutMS: 1000,
	}
	results := runProbes(context.Background(), probeController(server), req)

	blocked := results[0].Paths
	if blocked["DIRECT"].OK != 0 || blocked["DIRECT"].Total != 2 || blocked["DIRECT"].Errors["timeout"] != 2 {
		t.Fatalf("blocked direct: %+v", blocked["DIRECT"])
	}
	if blocked["PROXY"].OK != 2 || len(blocked["PROXY"].Delays) != 2 || blocked["PROXY"].Delays[0] != 300 {
		t.Fatalf("blocked proxy: %+v", blocked["PROXY"])
	}
	if blocked["MISSING"].Total != 1 || blocked["MISSING"].Errors["no_path"] != 1 {
		t.Fatalf("missing path should stop after one attempt: %+v", blocked["MISSING"])
	}
	if results[1].Paths["DIRECT"].OK != 2 {
		t.Fatalf("ok direct: %+v", results[1].Paths["DIRECT"])
	}
	// 2 个目标 × (2 + 2 + 1) 次
	if got := atomic.LoadInt64(calls); got != 10 {
		t.Fatalf("expected 10 controller calls, got %d", got)
	}
}

func TestRunProbesUsesThirdAttemptWhenFirstTwoDisagree(t *testing.T) {
	var n int64
	server, _, _ := fakeDelayController(t, func(path, target string) (int, string) {
		if atomic.AddInt64(&n, 1) == 1 {
			return http.StatusGatewayTimeout, `{"message":"Timeout"}`
		}
		return http.StatusOK, `{"delay":50}`
	})
	defer server.Close()
	req := probeRequest{Targets: []probeTarget{{Host: "a.example.com", URL: "https://a.example.com/"}},
		Paths: []string{"DIRECT"}, Attempts: 3, TimeoutMS: 1000}
	stats := runProbes(context.Background(), probeController(server), req)[0].Paths["DIRECT"]
	if stats.Total != 3 || stats.OK != 2 {
		t.Fatalf("expected 3 attempts with 2 ok, got %+v", stats)
	}
}

func TestRunProbesLimitsConcurrency(t *testing.T) {
	server, _, peak := fakeDelayController(t, func(path, target string) (int, string) {
		return http.StatusOK, `{"delay":10}`
	})
	defer server.Close()
	req := probeRequest{Paths: []string{"DIRECT", "PROXY"}, Attempts: 2, TimeoutMS: 1000}
	for i := 0; i < probeMaxTargets; i++ {
		host := fmt.Sprintf("h%d.example.com", i)
		req.Targets = append(req.Targets, probeTarget{Host: host, URL: "https://" + host + "/"})
	}
	runProbes(context.Background(), probeController(server), req)
	if *peak > probeMaxConcurrency {
		t.Fatalf("peak concurrency %d exceeds %d", *peak, probeMaxConcurrency)
	}
}

func TestDomainProbeHandler(t *testing.T) {
	server, _, _ := fakeDelayController(t, func(path, target string) (int, string) {
		return http.StatusOK, `{"delay":42}`
	})
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(configPath, []byte("external-controller: "+strings.TrimPrefix(server.URL, "http://")+"\nsecret: s3cret\n"), 0600)
	handler := domainProbeHandler(&Config{ServiceType: "mihomo", ConfigPath: configPath})

	body, _ := json.Marshal(validProbeRequest())
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodPost, "/api/domain-probe", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var response struct {
		Results []probeTargetResult `json:"results"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &response)
	if len(response.Results) != 1 || response.Results[0].Paths["PROXY"].Delays[0] != 42 {
		t.Fatalf("unexpected response %s", recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodPost, "/api/domain-probe", strings.NewReader(`{"targets":[]}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}

	mosdns := domainProbeHandler(&Config{ServiceType: "mosdns", ConfigPath: configPath})
	recorder = httptest.NewRecorder()
	mosdns(recorder, httptest.NewRequest(http.MethodPost, "/api/domain-probe", bytes.NewReader(body)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("mosdns agent must reject probes, got %d", recorder.Code)
	}
}

func TestRunProbesEscapesPolicyNames(t *testing.T) {
	seen := make(chan string, 4)
	server, _, _ := fakeDelayController(nil, func(path, target string) (int, string) {
		seen <- path
		return http.StatusOK, `{"delay":10}`
	})
	defer server.Close()
	req := probeRequest{Targets: []probeTarget{{Host: "a.example.com", URL: "https://a.example.com/"}},
		Paths: []string{"🚀 节点/选择"}, Attempts: 1, TimeoutMS: 1000}
	stats := runProbes(context.Background(), probeController(server), req)[0].Paths["🚀 节点/选择"]
	if stats.OK != 1 || <-seen != "🚀 节点/选择" {
		t.Fatalf("policy name not round-tripped: %+v", stats)
	}
}
