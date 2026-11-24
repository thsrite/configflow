package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	core "configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/testjson"
)

func contentBundle(t *testing.T, main, rules string) *deploy.Bundle {
	t.Helper()
	m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "previous", AgentID: "content-agent", ProfileID: "default", ServiceType: "mihomo", ConfigPath: "config.yaml"}
	files := map[string]deploy.Content{"config.yaml": {Data: []byte(main), Role: deploy.RoleConfig}, "rules/example.list": {Data: []byte(rules), Role: deploy.RoleRule}}
	for _, p := range []string{"config.yaml", "rules/example.list"} {
		v := files[p]
		sum := sha256.Sum256(v.Data)
		m.Files = append(m.Files, deploy.File{Path: p, Role: v.Role, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(v.Data))})
	}
	b, err := deploy.Pack(m, files, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type contentHTTP struct {
	s         *httpxtest.Server
	sub       *substore.Service
	manager   *core.Manager
	admin     httpxtest.Option
	candidate atomic.Pointer[deploy.Bundle]
	calls     atomic.Int32
	gate      chan struct{}
}

func newContentHTTP(t *testing.T, baseline bool, authenticated bool) *contentHTTP {
	t.Helper()
	// Earlier API tests can activate O3's lazy shared workers. End that
	// generation before installing this fixture's isolated registry.
	lifecycle.Stop(8 * time.Second)
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	lifecycle.Start(context.Background())
	f := &contentHTTP{s: httpxtest.New(t, httpxtest.Options{DefaultFactories: true, Auth: authenticated})}
	f.sub = substore.NewService(substore.NewEnvFor(f.s.Repo))
	WireContentSyncWith(lifecycle.Current(), f.sub)
	old := contentBundle(t, "mode: rule\n", "DOMAIN,old.example.test\n")
	f.candidate.Store(old)
	fp, err := deploy.Fingerprint(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	digests, err := deploy.Digests(old.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	for k, v := range digests {
		files[k] = v
	}
	record := map[string]any{
		"id": "content-agent", "name": "Synthetic", "host": "192.0.2.10", "port": 8080,
		"token": "stored-agent-token", "service_type": "mihomo", "profile_id": "default",
		"version": "1.0.0", "enabled": true,
		"deployments":        map[string]any{"old": map[string]any{"status": "failed", "prepared_content": "private-prepared"}},
		"ipv6_verifications": []any{map[string]any{"id": "private-run"}},
		"dns_follow_job":     map[string]any{"secret": "private-job"},
	}
	if baseline {
		record["config_version"] = digests["config.yaml"]
		record["pushed_at"] = "2026-01-01T00:00:00Z"
		record["config_revision"] = 1
		record["applied_content"] = map[string]any{
			"fingerprint": fp, "files": files, "profile_id": "default", "service_type": "mihomo",
			"config_version": digests["config.yaml"], "deployment_id": "previous", "config_path": "config.yaml",
			"base_url": "https://original.example.test/prefix", "extra": "private-baseline",
		}
	}
	if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", mustRawJSON(t, []any{record}))
	}); err != nil {
		t.Fatal(err)
	}
	f.manager, err = core.New(f.s.Context(), f.s.Repo, core.Options{ReceiptMatches: deploy.ReceiptMatcher(f.s.Repo)})
	if err != nil {
		t.Fatal(err)
	}
	NewHandler(Deps{Repo: f.s.Repo, Agents: f.manager}).Register(f.s.App.Router)
	if baseline {
		cfg, err := f.s.Repo.Config(f.s.Context(), "default", nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := model.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		source, err := deploy.SourceFromJSON(raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = deploy.NewReceipts(f.s.Repo).Capture(f.s.Context(), "content-agent", old, source, "https://original.example.test/prefix", nil); err != nil {
			t.Fatal(err)
		}
	}
	restore := SetContentPrepare(func(ctx context.Context, target deploy.Target, source *deploy.Source, origin, id string) (*deploy.Bundle, error) {
		f.calls.Add(1)
		if origin != "https://original.example.test/prefix" && baseline {
			t.Error("UI origin replaced publishing origin")
		}
		if !strings.HasPrefix(id, "check-") || source.ProfileID() != "default" {
			t.Error("invalid preparation snapshot")
		}
		if f.gate != nil {
			select {
			case <-f.gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return f.candidate.Load(), nil
	})
	t.Cleanup(func() {
		f.manager.Close()
		lifecycle.Stop(8 * time.Second)
		restore()
	})
	if authenticated {
		f.admin = f.s.Login()
	}
	return f
}
func (f *contentHTTP) get(path string) *httpxtest.Result {
	if f.admin != nil {
		return f.s.Get(path, f.admin)
	}
	return f.s.Get(path)
}
func (f *contentHTTP) settle(t *testing.T) *httpxtest.Result {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r := f.get("/api/agents/content-agent/push-diff").WantStatus(200)
		if mapStr(parseBodyMap(t, r), "status") != "checking" {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatal("comparison did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestContentHTTPAuthAndUnavailableStates(t *testing.T) {
	f := newContentHTTP(t, false, true)
	for _, path := range []string{"/api/agents", "/api/agents/content-agent", "/api/agents/content-agent/push-diff", "/api/agents/content-agent/push-diff/entries?version=x&file=x&change=added"} {
		f.s.Get(path).WantStatus(401)
	}
	wantBodyJSON(t, f.get("/api/agents/content-agent/push-diff").WantStatus(200), map[string]any{"status": "never-pushed", "service_type": "mihomo"})
	if f.calls.Load() != 0 {
		t.Fatal("prepared missing baseline")
	}
	f.get("/api/agents/missing").WantStatus(404)
	f.get("/api/agents/missing/push-diff").WantStatus(404)
	f.get("/api/agents/missing/push-diff/entries?version=x&file=x&change=added").WantStatus(404)
	f.get("/api/agents/missing/push-diff/entries?limit=-1").WantStatus(400)
}
func TestContentHTTPHistorySummaryAndPending(t *testing.T) {
	f := newContentHTTP(t, true, false)
	f.gate = make(chan struct{})
	defer close(f.gate)
	summary := f.get("/api/agents?summary=1").WantStatus(200)
	if f.calls.Load() != 0 || summary.Contains("content_sync") || summary.Contains("deployments") || summary.Contains("private-") || summary.Contains("stored-agent-token") {
		t.Fatal(string(summary.Body))
	}
	full := f.get("/api/agents?history=0").WantStatus(200)
	if !full.Contains("content_sync") || full.Contains("deployments") || full.Contains("ipv6_verifications") {
		t.Fatal(string(full.Body))
	}
	single := f.get("/api/agents/content-agent").WantStatus(200)
	if !single.Contains("deployments") || single.Contains("private-prepared") || single.Contains("private-baseline") || single.Contains("private-job") {
		t.Fatal(string(single.Body))
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	f.get("/api/agents/content-agent/push-diff").WantStatus(200)
	if f.calls.Load() != 1 {
		t.Fatal("status and diff did not share worker")
	}
	if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
		a := &system.Agents[0]
		raw, _, err := model.EncodeMember(&a, "deployments")
		if err != nil {
			return err
		}
		deps := parseJSONMap(t, raw)
		deps["next"] = map[string]any{"status": "ready"}
		return model.SetStoredMember(&a, "deployments", mustRawJSON(t, deps))
	}); err != nil {
		t.Fatal(err)
	}
	pending := f.get("/api/agents?history=0").WantStatus(200)
	if !pending.Contains("deployment_pending") || pending.Contains("deployments") {
		t.Fatal(string(pending.Body))
	}
}
func TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation(t *testing.T) {
	f := newContentHTTP(t, true, false)
	f.settle(t)
	if f.calls.Load() != 1 {
		t.Fatal("initial generation")
	}
	var lines strings.Builder
	lines.WriteString("DOMAIN,stored-agent-token.example.test\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&lines, "DOMAIN,fresh-%03d.example.test\n", i)
	}
	f.candidate.Store(contentBundle(t, "mode: global\nsecret: external-user-secret\n", lines.String()))
	_, err := fixtureSaveSubscriptionNodes(f.s.Context(), f.s.Repo, f.sub, "synthetic-feed", []any{}, map[string]any{"hosts": map[string]any{"entries": map[string]any{"host.example.test": "192.0.2.4"}}})
	if err != nil {
		t.Fatal(err)
	}
	result := f.settle(t).WantStatus(200)
	body := parseBodyMap(t, result)
	if mapStr(body, "status") != "changed" || mapStr(body, "content") != "full" || result.Header.Get("Cache-Control") != "no-store" || result.Contains("stored-agent-token") || !result.Contains("[REDACTED]") || !result.Contains("external-user-secret") {
		t.Fatal(string(result.Body))
	}
	version := mapStr(body, "version")
	query := "/api/agents/content-agent/push-diff/entries?version=" + version + "&file=rules%2Fexample.list&change=added"
	page := f.get(query + "&offset=20&limit=50").WantStatus(200)
	pageObj := parseBodyMap(t, page)
	if page.Header.Get("Cache-Control") != "no-store" || len(mapList(pageObj, "entries")) != 50 || mapInt(pageObj, "total") != 81 {
		t.Fatal(string(page.Body))
	}
	searched := f.get(query + "&q=" + url.QueryEscape("stored-agent-token")).WantStatus(200)
	if mapInt(parseBodyMap(t, searched), "total") != 0 {
		t.Fatal("stored credential searchable")
	}
	searched = f.get(query + "&q=fresh-001").WantStatus(200)
	if mapInt(parseBodyMap(t, searched), "total") != 1 {
		t.Fatal(string(searched.Body))
	}
	f.get(strings.Replace(query, "change=added", "change=modified", 1)).WantStatus(404)
	wantBodyJSON(t, f.get(strings.Replace(query, "version="+version, "version=old", 1)).WantStatus(200), map[string]any{"status": "stale", "reason": "replaced"})
	before := f.calls.Load()
	cached := f.get("/api/agents/content-agent/push-diff").WantStatus(200)
	if f.calls.Load() != before || !testjson.Equal(json.RawMessage(cached.Body), body) {
		t.Fatal("list was not cached")
	}
	contentChecks.dispatcher.Invalidate()
	wantBodyJSON(t, f.get(query).WantStatus(200), map[string]any{"status": "stale", "reason": "expired"})
}
func TestContentHTTPPageValidation(t *testing.T) {
	f := newContentHTTP(t, false, false)
	valid := "/api/agents/content-agent/push-diff/entries?version=x&file=x&change=added"
	for _, suffix := range []string{"&offset=-1", "&offset=%2B1", "&offset=1_0", "&offset=１００", "&offset=10000001", "&limit=0", "&limit=201", "&limit=bad", "&q=" + strings.Repeat("中", 201)} {
		f.get(valid + suffix).WantStatus(400)
	}
	for _, suffix := range []string{"&offset=000", "&limit=200", "&q=" + strings.Repeat("中", 200)} {
		wantBodyJSON(t, f.get(valid+suffix).WantStatus(200), map[string]any{"status": "stale", "reason": "expired"})
	}
}

func TestContentHTTPUnavailableProfileLegacyAndPreparation(t *testing.T) {
	f := newContentHTTP(t, true, false)
	restore := SetContentPrepare(func(context.Context, deploy.Target, *deploy.Source, string, string) (*deploy.Bundle, error) {
		return nil, errors.New("private preparation failure: stored-agent-token")
	})
	wantBodyJSON(t, f.settle(t), map[string]any{
		"status": "unknown", "reason": "check_failed", "service_type": "mihomo",
		"baseline": map[string]any{"pushed_at": "2026-01-01T00:00:00Z", "config_revision": 1},
	})
	restore()
	mutate := func(fn func(*model.Agent) error) {
		t.Helper()
		if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
			return fn(&system.Agents[0])
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Repository transactions reject broken bindings. Exercise a stale read at
	// the route adapter boundary, as Python's test does with a stale Manager.
	stale, err := f.manager.ReadAgent(f.s.Context(), "content-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.SetStoredMember(&stale, "profile_id", json.RawMessage(`"missing-profile"`)); err != nil {
		t.Fatal(err)
	}
	public, err := publicWithContent(f.s.Context(), f.manager, stale, -1, nil, true)
	var sync contentsync.Result
	if decodeErr := json.Unmarshal(public["content_sync"], &sync); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if err != nil || sync != (contentsync.Result{Status: "unknown", Reason: "profile_unavailable"}) {
		t.Fatalf("stale profile: %v %v", public, err)
	}
	f.get("/api/agents?history=0").WantStatus(200)
	mutate(func(a *model.Agent) error {
		a.ProfileID = "default"
		return model.ZeroMember(&a, "applied_content")
	})
	wantBodyJSON(t, f.get("/api/agents/content-agent/push-diff").WantStatus(200), map[string]any{"status": "unknown", "reason": "baseline_missing", "service_type": "mihomo"})
}

func TestContentHTTPMissingReceiptUsesDigests(t *testing.T) {
	f := newContentHTTP(t, true, false)
	if err := os.RemoveAll(filepath.Join(f.s.DataDir, ".agent-rule-sync")); err != nil {
		t.Fatal(err)
	}
	f.candidate.Store(contentBundle(t, "mode: global\n", "DOMAIN,new.example.test\n"))
	r := f.settle(t).WantStatus(200)
	obj := parseBodyMap(t, r)
	if mapStr(obj, "status") != "changed" || mapStr(obj, "content") != "digests" || mapStr(obj, "content_reason") != "bundle_missing" {
		t.Fatal(string(r.Body))
	}
}

func fixtureSaveSubscriptionNodes(ctx context.Context, r *repo.Repository, sub *substore.Service, id string, value any, metadata map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var nodes []model.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	var receipt *substore.CacheMetadata
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		receipt = &substore.CacheMetadata{}
		if err := json.Unmarshal(raw, receipt); err != nil {
			return nil, err
		}
	}
	cache, err := sub.SaveCache(ctx, r, id, nodes, receipt)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return nil, err
	}
	parsed, err := decodeJSONAny(data)
	if err != nil {
		return nil, err
	}
	out, _ := parsed.(map[string]any)
	return out, nil
}
