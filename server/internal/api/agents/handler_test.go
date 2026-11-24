package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/dnsha"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/rulesync"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/agents/upgrades"
	"configflow/internal/deploy"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

func TestHandlerUsesInjectedDepsWithoutGlobalLocators(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	// Point the global repository and DATA_DIR at a completely separate empty
	// directory so any accidental fallback to repo.GetRepository, agents.Get,
	// or repo.DataDir reads/writes the wrong store and fails the assertions.
	otherDir := t.TempDir()
	otherRepo, err := repo.OpenDefault(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherRepo.Close)
	t.Setenv("DATA_DIR", otherDir)
	repo.SetRepository(otherRepo)
	t.Cleanup(func() { repo.SetRepository(nil) })

	ctx := context.Background()
	ts := trafficstore.NewService(dir)
	mt := metrics.New(metrics.Options{Repo: r, TrafficStore: ts})
	mon := monitor.New(dir)
	m, err := core.New(ctx, r, core.Options{
		Metrics:     mt,
		Publication: mon,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	ha := dnsha.New(m, dnsha.Options{})
	if err := m.SetPairPublisher(ha); err != nil {
		t.Fatal(err)
	}
	if err := mon.SetHARules(ha.PairRulesJSON); err != nil {
		t.Fatal(err)
	}
	mon.MarkStarted()
	m.MarkStarted()

	up := upgrades.New(m)
	follow := dnsfollow.New(m, dnsfollow.Options{})
	rsync := rulesync.New(m, nil, rulesync.Options{})
	dp := deploy.New(deploy.Options{})

	app := httpx.New()
	app.Repo = r
	NewHandler(Deps{
		Repo:      r,
		Agents:    m,
		Traffic:   ts,
		Metrics:   mt,
		Monitor:   mon,
		Upgrades:  up,
		DNSHA:     ha,
		DNSFollow: follow,
		RuleSync:  rsync,
		Deploy:    dp,
	}).Register(app.Router)

	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var reqBody *bytes.Reader
		if body != "" {
			reqBody = bytes.NewReader([]byte(body))
		} else {
			reqBody = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reqBody)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	// Create an agent via registryHandler.
	rec := call(http.MethodPost, "/api/agents", `{"name":"Injected-Mihomo","host":"192.0.2.50","port":8080,"service_type":"mihomo"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/agents = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Success bool `json:"success"`
		Data    struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || !created.Success || created.Data.ID == "" {
		t.Fatalf("unexpected create response: %s", rec.Body.String())
	}
	id := created.Data.ID

	// Verify the agent was written to the injected repo, not otherRepo.
	otherMgr, err := core.New(ctx, otherRepo, core.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherMgr.Close)
	if otherAgents, err := otherMgr.ReadAgents(ctx, false, nil); err != nil || len(otherAgents) != 0 {
		t.Fatalf("global repo was written: len=%d err=%v", len(otherAgents), err)
	}

	// Exercise sub-handlers with explicit injected dependencies.
	for _, tc := range []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"list", http.MethodGet, "/api/agents?summary=1", http.StatusOK},
		{"item", http.MethodGet, "/api/agents/" + id, http.StatusOK},
		{"status", http.MethodGet, "/api/agents/" + id + "/status", http.StatusOK},
		{"metrics", http.MethodGet, "/api/agents/" + id + "/metrics", http.StatusOK},
		{"metrics-history", http.MethodGet, "/api/agents/" + id + "/metrics/history", http.StatusOK},
		{"traffic-stats", http.MethodGet, "/api/agents/" + id + "/traffic/stats", http.StatusOK},
		{"traffic-history", http.MethodGet, "/api/agents/" + id + "/traffic-history", http.StatusOK},
		{"rule-sync", http.MethodGet, "/api/agents/" + id + "/rule-sync", http.StatusOK},
		{"dns-follow", http.MethodGet, "/api/agents/" + id + "/dns-follow", http.StatusConflict},
		{"client-notes", http.MethodGet, "/api/agents/" + id + "/client-notes", http.StatusOK},
		{"gateway-installations", http.MethodGet, "/api/agents/gateway-installations?profile_id=default", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := call(tc.method, tc.path, "")
			if resp.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d: %s", tc.method, tc.path, resp.Code, tc.want, resp.Body.String())
			}
		})
	}

	// Verify installerHandler writes to injected Repo.DataDir(), not global DATA_DIR.
	rec = call(http.MethodPost, "/api/agents/core-install-command", `{"target":"mihomo"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/agents/core-install-command = %d: %s", rec.Code, rec.Body.String())
	}
	var cmdResp struct {
		Success      bool   `json:"success"`
		DownloadPath string `json:"download_path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cmdResp); err != nil || !cmdResp.Success || cmdResp.DownloadPath == "" {
		t.Fatalf("unexpected core-install-command response: %s", rec.Body.String())
	}
	dl := call(http.MethodGet, cmdResp.DownloadPath, "")
	if dl.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", cmdResp.DownloadPath, dl.Code, dl.Body.String())
	}
	if entries, err := os.ReadDir(filepath.Join(otherDir, "install_scripts")); err == nil && len(entries) > 0 {
		t.Fatalf("global DATA_DIR/install_scripts was written: %d entries", len(entries))
	}
}
