package main

import (
	"bytes"
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
)

// fakeRegionMihomo 同时扮演 Mihomo 控制接口和探测入口（HTTP 代理）：
// 入口按探测组当前选中的目标返回不同内容，模拟经不同节点访问。
type fakeRegionMihomo struct {
	mu       sync.Mutex
	now      string
	all      []string
	selected []string
	headers  http.Header
}

func (f *fakeRegionMihomo) controller() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/proxies":
			proxies := map[string]interface{}{regionProbeGroup: map[string]interface{}{"type": "Selector", "all": f.all, "now": f.now}}
			proxies["DIRECT"] = map[string]interface{}{"type": "Direct", "alive": true}
			proxies["🇺🇸 美国 01"] = map[string]interface{}{"type": "Socks5", "alive": true}
			proxies["🇭🇰 香港 01"] = map[string]interface{}{"type": "Socks5", "alive": false}
			json.NewEncoder(w).Encode(map[string]interface{}{"proxies": proxies})
		case r.URL.Path == "/proxies/"+regionProbeGroup && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{"now": f.now, "all": f.all})
		case r.URL.Path == "/proxies/"+regionProbeGroup && r.Method == http.MethodPut:
			var body struct{ Name string }
			json.NewDecoder(r.Body).Decode(&body)
			found := false
			for _, name := range f.all {
				found = found || name == body.Name
			}
			if !found {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.now = body.Name
			f.selected = append(f.selected, body.Name)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeRegionMihomo) listener() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		target := f.now
		f.headers = r.Header.Clone()
		f.mu.Unlock()
		if !r.URL.IsAbs() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch {
		case r.URL.Path == "/trace":
			loc := map[string]string{"🇺🇸 美国 01": "US", "🇭🇰 香港 01": "HK", "DIRECT": "CN"}[target]
			fmt.Fprintf(w, "ip=1.2.3.4\nloc=%s\n", loc)
		case r.URL.Path == "/app" && target == "🇭🇰 香港 01":
			http.Redirect(w, r, "http://svc.test/app-unavailable-in-region", http.StatusFound)
		case r.URL.Path == "/app-unavailable-in-region":
			w.Write([]byte("<h1>App unavailable</h1> not available in your country"))
		case r.URL.Path == "/app":
			w.Write([]byte("welcome"))
		case r.URL.Path == "/headers":
			fmt.Fprintf(w, "auth=%s evil=%s", r.Header.Get("Authorization"), r.Header.Get("X-Evil"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func setupRegionFake(t *testing.T) (*fakeRegionMihomo, *mihomoController, string, string) {
	fake := &fakeRegionMihomo{now: "DIRECT", all: []string{"DIRECT", "🇺🇸 美国 01", "🇭🇰 香港 01", "🚀 节点选择"}}
	controller := httptest.NewServer(fake.controller())
	listener := httptest.NewServer(fake.listener())
	t.Cleanup(controller.Close)
	t.Cleanup(listener.Close)
	listenAddr := strings.TrimPrefix(listener.URL, "http://")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	host, port, _ := strings.Cut(listenAddr, ":")
	os.WriteFile(configPath, []byte(fmt.Sprintf(
		"external-controller: %s\nsecret: s3cret\nlisteners:\n  - {name: other, type: mixed, port: 1}\n  - {name: %s, type: mixed, listen: %s, port: %s, proxy: %s}\n",
		strings.TrimPrefix(controller.URL, "http://"), regionProbeListener, host, port, regionProbeGroup)), 0600)
	return fake, &mihomoController{BaseURL: controller.URL, Secret: "s3cret"}, listenAddr, configPath
}

func appRequests() []regionRequest {
	return []regionRequest{
		{ID: "trace", URL: "http://svc.test/trace", Markers: map[string]string{"loc": `loc=([A-Z]{2})`}},
		{ID: "app", URL: "http://svc.test/app", Markers: map[string]string{"blocked": `not available in your (country|region)`, "ok": "welcome"}},
	}
}

func TestResolveRegionProbeListener(t *testing.T) {
	_, _, listener, configPath := setupRegionFake(t)
	got, err := resolveRegionProbeListener(configPath)
	if err != nil || got != listener {
		t.Fatalf("got %q %v, want %q", got, err, listener)
	}
	missing := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(missing, []byte("mixed-port: 7890\n"), 0600)
	if _, err := resolveRegionProbeListener(missing); err != errRegionProbeMissing {
		t.Fatalf("expected missing listener error, got %v", err)
	}
}

func TestValidateRegionCheck(t *testing.T) {
	valid := regionCheckRequest{Target: "DIRECT", TimeoutMS: 3000, Requests: appRequests()}
	if _, err := validateRegionCheck(&valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := map[string]func(*regionCheckRequest){
		"no target":   func(r *regionCheckRequest) { r.Target = "" },
		"timeout":     func(r *regionCheckRequest) { r.TimeoutMS = 50 },
		"no requests": func(r *regionCheckRequest) { r.Requests = nil },
		"scheme":      func(r *regionCheckRequest) { r.Requests[0].URL = "file:///etc/passwd" },
		"dup id":      func(r *regionCheckRequest) { r.Requests[1].ID = r.Requests[0].ID },
		"bad regex":   func(r *regionCheckRequest) { r.Requests[0].Markers = map[string]string{"x": "("} },
		"too many": func(r *regionCheckRequest) {
			for i := 0; i <= regionMaxRequests; i++ {
				r.Requests = append(r.Requests, regionRequest{ID: fmt.Sprint("r", i), URL: "http://a.test/"})
			}
		},
	}
	for name, mutate := range cases {
		req := regionCheckRequest{Target: "DIRECT", TimeoutMS: 3000, Requests: appRequests()}
		mutate(&req)
		if _, err := validateRegionCheck(&req); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestRunRegionCheckPerTargetAndRestoresSelection(t *testing.T) {
	fake, controller, listener, _ := setupRegionFake(t)
	run := func(target string) map[string]regionResult {
		req := regionCheckRequest{Target: target, TimeoutMS: 3000, Requests: appRequests()}
		patterns, err := validateRegionCheck(&req)
		if err != nil {
			t.Fatal(err)
		}
		results, err := runRegionCheck(context.Background(), controller, listener, req, patterns)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		byID := map[string]regionResult{}
		for _, result := range results {
			byID[result.ID] = result
		}
		return byID
	}

	us := run("🇺🇸 美国 01")
	if us["trace"].Markers["loc"] != "US" || us["app"].Markers["ok"] != "welcome" || us["app"].Markers["blocked"] != "" {
		t.Fatalf("US results: %+v", us)
	}
	hk := run("🇭🇰 香港 01")
	app := hk["app"]
	if hk["trace"].Markers["loc"] != "HK" || app.Markers["blocked"] != "country" || app.Status != 200 ||
		!strings.HasSuffix(app.FinalURL, "/app-unavailable-in-region") ||
		len(app.Redirects) != 1 || app.Redirects[0] != "http://svc.test/app" {
		t.Fatalf("HK results: %+v", app)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.now != "DIRECT" {
		t.Fatalf("selection not restored, now=%q", fake.now)
	}
	if strings.Join(fake.selected, ",") != "🇺🇸 美国 01,DIRECT,🇭🇰 香港 01,DIRECT" {
		t.Fatalf("unexpected selection history %v", fake.selected)
	}
}

func TestRunRegionCheckRejectsUnknownTarget(t *testing.T) {
	_, controller, listener, _ := setupRegionFake(t)
	req := regionCheckRequest{Target: "nope", TimeoutMS: 3000, Requests: appRequests()}
	patterns, _ := validateRegionCheck(&req)
	if _, err := runRegionCheck(context.Background(), controller, listener, req, patterns); err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestRegionCheckHeadersAreWhitelisted(t *testing.T) {
	_, controller, listener, _ := setupRegionFake(t)
	req := regionCheckRequest{Target: "DIRECT", TimeoutMS: 3000, Requests: []regionRequest{{
		ID: "h", URL: "http://svc.test/headers",
		Headers: map[string]string{"authorization": "Bearer null", "X-Evil": "1"},
		Markers: map[string]string{"auth": `auth=([^ ]*)`, "evil": `evil=(\S+)`},
	}}}
	patterns, _ := validateRegionCheck(&req)
	results, err := runRegionCheck(context.Background(), controller, listener, req, patterns)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Markers["auth"] != "Bearer" || results[0].Markers["evil"] != "" {
		t.Fatalf("header filtering failed: %+v", results[0].Markers)
	}
}

func TestRegionHandlers(t *testing.T) {
	_, _, _, configPath := setupRegionFake(t)
	cfg := &Config{ServiceType: "mihomo", ConfigPath: configPath}

	recorder := httptest.NewRecorder()
	regionTargetsHandler(cfg)(recorder, httptest.NewRequest(http.MethodGet, "/api/region-check/targets", nil))
	var listing struct {
		Targets []regionTarget `json:"targets"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &listing)
	if recorder.Code != 200 || len(listing.Targets) != 4 || listing.Targets[2].Alive || listing.Targets[1].Type != "Socks5" {
		t.Fatalf("targets: %d %s", recorder.Code, recorder.Body)
	}

	body, _ := json.Marshal(regionCheckRequest{Target: "🇺🇸 美国 01", TimeoutMS: 3000, Requests: appRequests()})
	recorder = httptest.NewRecorder()
	regionCheckHandler(cfg)(recorder, httptest.NewRequest(http.MethodPost, "/api/region-check", bytes.NewReader(body)))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"loc":"US"`) {
		t.Fatalf("check: %d %s", recorder.Code, recorder.Body)
	}

	missing := filepath.Join(t.TempDir(), "c.yaml")
	raw, _ := os.ReadFile(configPath)
	os.WriteFile(missing, []byte(strings.Split(string(raw), "listeners:")[0]), 0600)
	recorder = httptest.NewRecorder()
	regionCheckHandler(&Config{ServiceType: "mihomo", ConfigPath: missing})(recorder,
		httptest.NewRequest(http.MethodPost, "/api/region-check", bytes.NewReader(body)))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409 without listener, got %d", recorder.Code)
	}
}
