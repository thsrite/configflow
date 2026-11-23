package notifications_test

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/rulesync"
	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/discovery"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/refresh"
	"configflow/internal/routers"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func seedO10T3Server(t *testing.T, deployments int) (*httpxtest.Server, *agents.Manager) {
	t.Helper()
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apinotifications.RegisterAlways)})
	ctx := srv.Context()

	deps := make(map[string]any, deployments)
	for i := range deployments {
		id := fmt.Sprintf("dep-%04d", i)
		deps[id] = map[string]any{
			"success":       true,
			"deployment_id": id,
			"status":        "succeeded",
			"profile_id":    "default",
			"service_type":  "mihomo",
			"created_at":    "2026-01-01T00:00:00Z",
			"message":       fmt.Sprintf("synthetic deployment %04d padding-0123456789abcdef0123456789abcdef", i),
			// A credential stored only in the history: task rows must still
			// redact it (they collected full records before O10).
			"password": "synthetic-history-secret",
			"events": []map[string]any{
				{"at": "2026-01-01T00:00:00Z", "status": "succeeded", "message": "synthetic event 0123456789abcdef0123456789abcdef"},
			},
		}
	}

	mihomoRaw, err := json.Marshal(map[string]any{
		"id":           "agent-mihomo",
		"name":         "alice-mihomo",
		"host":         "192.0.2.11",
		"port":         18080,
		"token":        "synthetic-mihomo-token",
		"service_type": "mihomo",
		"profile_id":   "default",
		"enabled":      true,
		"rule_sync": map[string]any{
			"settings": map[string]any{"library": true, "rules": false},
			"state": map[string]any{
				"status":  "failed",
				"message": "alice failed with synthetic-mihomo-token synthetic-history-secret and http://alice:synthetic-proxy-pass@proxy.example.com:8080",
			},
		},
		"deployments": deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	oxidnsRaw, err := json.Marshal(map[string]any{
		"id":           "agent-oxidns",
		"name":         "alice-oxidns",
		"host":         "192.0.2.12",
		"port":         18080,
		"token":        "synthetic-oxidns-token",
		"service_type": "oxidns",
		"profile_id":   "default",
		"enabled":      true,
		"dns_follow":   map[string]any{"source_agent_id": "agent-mihomo", "upstream_tag": "main"},
		"dns_follow_state": map[string]any{
			"status":  "failed",
			"message": "alice follow error synthetic-mihomo-token synthetic-job-secret synthetic-history-secret",
		},
		"dns_follow_job": map[string]any{
			"binding_id": "b1",
			"password":   "synthetic-job-secret",
		},
		"deployments": deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	var mihomoAgent, oxidnsAgent model.Agent
	if err := json.Unmarshal(mihomoRaw, &mihomoAgent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(oxidnsRaw, &oxidnsAgent); err != nil {
		t.Fatal(err)
	}

	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.RuleFetchProxy = "http://alice:synthetic-proxy-pass@proxy.example.com:8080"
		s.Agents = []model.Agent{mihomoAgent, oxidnsAgent}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Repo.UpdateShared(ctx, func(_ context.Context, sh *model.Shared) error {
		return sh.SetJSON("subscriptions", json.RawMessage(`[{"id":"sub1","name":"Sub One","url":"https://sub.example.com/feed","enabled":true,"interval":3600}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		if err := p.SetJSON("domain_discovery", json.RawMessage(`{"auto_probe":true}`)); err != nil {
			return err
		}
		var r model.Router
		if err := json.Unmarshal([]byte(`{"id":"r1","name":"Edge","type":"routeros","endpoint":"http://192.0.2.1","username":"alice","password":"synthetic-router-pass","ip_rule_sets":[]}`), &r); err != nil {
			return err
		}
		p.Routers.Items = []model.Router{r}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Seed a subscription node cache so LoadCacheSummary is exercised.
	subNodes := []model.Node{}
	for i := range 20 {
		var n model.Node
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"name":"node-%d","type":"ss","server":"192.0.2.50","port":443}`, i)), &n); err != nil {
			t.Fatal(err)
		}
		subNodes = append(subNodes, n)
	}
	if _, err := substore.SaveCache(ctx, "sub1", subNodes, &substore.CacheMetadata{
		SubscriptionName: "Sub One",
		URL:              new("https://sub.example.com/feed"),
	}); err != nil {
		t.Fatal(err)
	}

	m, err := agents.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return srv, m
}

func wireAllO10T3Slots(t *testing.T, agentReads *atomic.Int32) {
	t.Helper()
	agents.Wire()
	rulesync.Wire()
	dnsfollow.Register()
	metrics.Wire()
	discovery.Wire()
	refresh.Wire()
	routers.Wire()
	t.Cleanup(notifications.SetInputs(notifications.Inputs{
		Rows:         tasks.Available,
		MissingTasks: tasks.Missing,
		Agents: func(ctx context.Context) ([]model.Agent, error) {
			if agentReads != nil {
				agentReads.Add(1)
			}
			return agents.NotificationAgents(ctx)
		},
	}))
}

// TestO10T3NotificationSnapshotSkipsHistoryRedactsAndInvalidatesRouterCache
// verifies that full-stack notification snapshots:
//  1. Redact Agent tokens, system proxy secrets, and nested job credentials
//     without hiding usernames.
//  2. Do not scale allocations with Agent deployment history (with a negative
//     control proving full Agent listing does scale).
//  3. Invalidate cached router signatures immediately when the repository
//     Head advances.
func TestO10T3NotificationSnapshotSkipsHistoryRedactsAndInvalidatesRouterCache(t *testing.T) {
	var reads atomic.Int32
	wireAllO10T3Slots(t, &reads)

	smallSrv, smallMgr := seedO10T3Server(t, 1)
	largeSrv, largeMgr := seedO10T3Server(t, 300)

	// Record a failed subscription receipt on both so SnapshotItems exercises
	// task, agent, rulesync, dnsfollow, discovery, and router paths.
	for _, srv := range []*httpxtest.Server{smallSrv, largeSrv} {
		if _, err := taskstatus.New(srv.DataDir).SaveResult(srv.Context(), "subscription:sub1", taskstatus.Result{
			Error: "synthetic timeout",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Verify redaction and username preservation on the large fixture.
	readsBefore := reads.Load()
	items, err := notifications.SnapshotItems(largeSrv.Context(), "")
	if err != nil {
		t.Fatalf("SnapshotItems: %v", err)
	}
	if got := reads.Load() - readsBefore; got != 1 {
		t.Fatalf("SnapshotItems called Agent source %d times, want 1", got)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	// Members the records do not declare ("password" in the history and the
	// job) are dropped on load (O18 T1b), so they are no credentials any more.
	for _, secret := range []string{"synthetic-mihomo-token", "synthetic-oxidns-token", "synthetic-proxy-pass", "synthetic-router-pass"} {
		if strings.Contains(text, secret) {
			t.Fatalf("notification snapshot leaked secret %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "alice") {
		t.Fatalf("notification snapshot hid username alice: %s", text)
	}
	// Negative control: the history-only credential is not in a history-free
	// collection, so the check above depends on the full-record read.
	listed, err := largeMgr.ListAgents(largeSrv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range listed {
		if slices.Contains(agents.CollectAgentCredentials(&listed[i], nil), "synthetic-history-secret") {
			t.Fatal("control: history-free collection found the history-only credential")
		}
	}
	// Verify both agent task incidents and subscription incident are present.
	byID := map[string]*notifications.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, wantID := range []string{"task:agent_rule_sync:agent-mihomo", "task:dns_follow:agent-oxidns", "task:subscription:sub1"} {
		if byID[wantID] == nil {
			t.Fatalf("missing expected incident %s in %v", wantID, byID)
		}
	}

	// Measure per-snapshot allocations BEFORE adding router checks to largeSrv
	// alone, so small and large have identical incidents and differ only in
	// Agent deployment history (1 vs 300 deployments).
	measure := func(fn func() error) uint64 {
		if err := fn(); err != nil {
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range 10 {
			if err := fn(); err != nil {
				t.Fatal(err)
			}
		}
		runtime.ReadMemStats(&after)
		return (after.TotalAlloc - before.TotalAlloc) / 10
	}

	// // Allocation measurements exclude the test-only view guard's encodings.
	smallSrv.Repo.SetViewGuard(nil)
	largeSrv.Repo.SetViewGuard(nil)
	smallBytes := measure(func() error {
		_, err := notifications.SnapshotItems(smallSrv.Context(), "")
		return err
	})
	largeBytes := measure(func() error {
		_, err := notifications.SnapshotItems(largeSrv.Context(), "")
		return err
	})
	// 2 Agents × 300 deployments is >1 MB raw; copying history even once blows past 256 KB.
	if largeBytes > smallBytes+256*1024 {
		t.Fatalf("SnapshotItems allocation scaled with Agent history: small=%d bytes, large=%d bytes", smallBytes, largeBytes)
	}

	// Negative control: deep-copying the listing with histories (the pre-R1
	// ListAgents(ctx, true)) must exceed small by >=512 KB, proving our bound
	// catches regressions.
	fullCopy := func(srv *httpxtest.Server, m *agents.Manager) func() error {
		return func() error {
			rows, err := m.ListAgents(srv.Context(), true)
			for i := range rows {
				if err == nil {
					_, err = agents.CloneAgent(&rows[i])
				}
			}
			return err
		}
	}
	ctrlSmall := measure(fullCopy(smallSrv, smallMgr))
	ctrlLarge := measure(fullCopy(largeSrv, largeMgr))
	if ctrlLarge < ctrlSmall+512*1024 {
		t.Fatalf("control: deep copy did not scale with history: small=%d large=%d", ctrlSmall, ctrlLarge)
	}

	// Verify router signature cache invalidates on profile edit (stale flips false -> true).
	cfg, err := largeSrv.Repo.ConfigWithOxiDNSAgents(largeSrv.Context(), "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	routerRec := cfg.Profile.Routers.Items[0]
	if err := notifications.SaveRouter(largeSrv.Context(), "default", cfgRaw, &routerRec, forwardReport(), routePlan(), ""); err != nil {
		t.Fatal(err)
	}
	beforeEdit, err := notifications.SnapshotItems(largeSrv.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	findCheck := func(list []*notifications.Item) *notifications.Item {
		for _, it := range list {
			if it.ID == "router:default:r1:firewall" {
				return it
			}
		}
		return nil
	}
	if item := findCheck(beforeEdit); item == nil || boolVal(item.Stale) {
		t.Fatalf("router check should not be stale before edit: %+v", item)
	}
	if _, err := largeSrv.Repo.UpdateProfile(largeSrv.Context(), "default", func(_ context.Context, p *model.Profile) error {
		return markerSet(&p.Loon.CustomConfig, json.RawMessage(`"changed"`))
	}); err != nil {
		t.Fatal(err)
	}
	afterEdit, err := notifications.SnapshotItems(largeSrv.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if item := findCheck(afterEdit); item == nil || !boolVal(item.Stale) {
		t.Fatalf("router check did not turn stale after edit: %+v", item)
	}
}
