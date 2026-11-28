package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"configflow/internal/api/stats"
	"configflow/internal/deploy"
	"configflow/internal/health"
	"configflow/internal/model"
	"configflow/internal/substore"
)

func installPollingReceipt(t testing.TB, f *o10Fixture) {
	t.Helper()
	db := f.srv.Repo
	ctx := f.srv.Context()
	id := f.agents[0].id
	manifest := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "poll-receipt", AgentID: id, ProfileID: "default", ServiceType: "mihomo", ConfigPath: "config.yaml"}
	files := map[string]deploy.Content{}
	for i := range 40 {
		path := fmt.Sprintf("rules/%d.txt", i)
		role := deploy.RoleRule
		if i == 0 {
			path, role = manifest.ConfigPath, deploy.RoleConfig
		}
		data := []byte("example.com\n")
		files[path] = deploy.Content{Data: data, Role: role}
		manifest.Files = append(manifest.Files, deploy.File{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data)), Role: role})
	}
	version := manifest.Files[0].SHA256[:8]
	bundle, err := deploy.Pack(manifest, files, version, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := db.Config(ctx, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := deploy.SourceFromConfig(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := deploy.NewReceipts(db).Capture(ctx, id, bundle, source, "https://example.com", nil); err != nil {
		t.Fatal(err)
	}
	err = db.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			a := &sys.Agents[i]
			if a.ID == id {
				a.ConfigVersion = version
				a.AppliedContent = &model.AppliedContent{DeploymentID: manifest.DeploymentID, ProfileID: a.ProfileID, ServiceType: a.ServiceType, ConfigVersion: version}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response := f.do(http.MethodGet, "/api/agents/"+id+"/rule-sync", "", nil)
	var result struct {
		Ready bool `json:"baseline_ready"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.Ready {
		t.Fatalf("receipt not ready: %d %s", response.Code, response.Body)
	}
}

func pollingViewFixture(t testing.TB) (*o10Fixture, []string) {
	t.Helper()
	f := newO10Fixture(t)
	db := f.srv.Repo
	db.SetViewGuard(nil)
	ctx := f.srv.Context()
	for _, feed := range []struct {
		id    string
		count int
	}{{o10SubA, 90}, {o10SubB, 212}} {
		proxies, err := substore.ParseProxyNodes(o10ProxiesYAML(feed.id, feed.count))
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := substore.ProxyNodes(proxies)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = substore.SaveCache(ctx, db, feed.id, nodes, &substore.CacheMetadata{URL: new(o10FeedURL(feed.id)), FetchedAt: new("2026-01-01T00:00:00Z")}); err != nil {
			t.Fatal(err)
		}
	}
	entries := map[string]any{}
	for i := range 302 {
		entries[fmt.Sprintf("node-%03d", i)] = map[string]any{"method": health.Method, "latency": i, "tested_at": "2026-01-01T00:00:00Z", "url": "https://example.com/", "history": []int{i, i + 1}}
	}
	latency, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.WriteSharedText(ctx, health.LatencyFile, latency); err != nil {
		t.Fatal(err)
	}
	samples := []any{}
	for i := range stats.HistorySize {
		samples = append(samples, map[string]any{"at": time.Now().UTC().Add(time.Duration(i+1-stats.HistorySize) * stats.SampleInterval).Format(time.RFC3339), "subscriptions": i, "nodes": i, "proxyGroups": i, "rules": i})
	}
	history, err := json.Marshal(map[string]any{"samples": samples})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.WriteProfileText(ctx, "default", stats.HistoryFile, history); err != nil {
		t.Fatal(err)
	}
	installPollingReceipt(t, f)
	return f, []string{"/api/subscriptions", "/api/nodes/latency", "/api/proxy-groups", "/api/rule-sets", "/api/rules", "/api/agents/" + f.agents[0].id + "/rule-sync", "/api/client-devices", "/api/stats/overview"}
}

func BenchmarkPollingViews(b *testing.B) {
	f, paths := pollingViewFixture(b)
	for _, path := range paths {
		benchmarkPollingRoute(b, f, path)
	}
}

func TestPolledViewAllocations(t *testing.T) {
	f, paths := pollingViewFixture(t)
	for _, path := range paths {
		measurePollingRoute(t, f, path)
	}
	response := f.do(http.MethodGet, "/api/subscriptions", "", nil)
	var rows []struct {
		ID    string `json:"id"`
		Count int    `json:"cached_node_count"`
		Total int    `json:"cached_total_count"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("subscriptions %d: %v", len(rows), err)
	}
	total := 0
	for _, row := range rows {
		if row.Count != row.Total {
			t.Fatalf("unfiltered count: %+v", row)
		}
		total += row.Count
	}
	if total != 302 {
		t.Fatalf("expected 302 cached nodes, got %d", total)
	}
	path, err := f.srv.Repo.SharedPath(health.LatencyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(`{"new":{"latency":42,"method":"proxy_http_v2"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	res := f.do(http.MethodGet, "/api/nodes/latency", "", nil)
	var current struct {
		Results map[string]health.LatencyEntry `json:"results"`
	}
	if err = json.Unmarshal(res.Body.Bytes(), &current); err != nil || len(current.Results) != 1 || string(current.Results["new"].Latency) != "42" {
		t.Fatalf("stale latency results: %v", err)
	}
}
