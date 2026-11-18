package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

func TestR14ManifestRollbackPersistsLatestState(t *testing.T) {
	s, _, r := r14Fixture(t)
	ctx := repo.WithRepository(t.Context(), s.Repo)
	held, unlock, ok := JobLock(ctx, "default", r.ID, r)
	if !ok {
		t.Fatal("busy")
	}
	defer unlock()
	d := s.driver(r)
	defer d.Close()
	desired := r08Desired(true)
	plan, err := r08Plan(held, d, desired)
	if err != nil || len(plan.Ops) != 1 {
		t.Fatal(err, plan)
	}
	if _, err = d.Apply(held, mustPolicyJSON(plan.Ops[0])); err != nil {
		t.Fatal(err)
	}
	if err = s.saveManifest(held, "default", r, d); err != nil {
		t.Fatal(err)
	}
	cfg, err := syncConfig(held, s.Repo, "default")
	if err != nil {
		t.Fatal(err)
	}
	if !effectful(findSyncRouter(cfg, r.ID).State("ros_policy_state")) {
		t.Fatal("new ownership not persisted")
	}
	d.SetPolicyManifest(nil)
	if err = s.saveManifest(held, "default", r, d); err != nil {
		t.Fatal(err)
	}
	cfg, err = syncConfig(held, s.Repo, "default")
	if err != nil {
		t.Fatal(err)
	}
	if effectful(findSyncRouter(cfg, r.ID).State("ros_policy_state")) {
		t.Fatal("rollback to original null was compared to stale initial record")
	}
}
func TestR14ReceiptCompatibilityAndRuleOrder(t *testing.T) {
	store := RuleSyncStore{DataDir: t.TempDir()}
	raw := []byte(`{"status":"synced","future":{"keep":[1,2]},"rules":{"z":{"version":"123456789012abc","ipv4":[],"ip_count":0,"future_rule":true},"a":{"version":"short","ipv4":[],"ip_count":0}},"baseline":{"control":"synthetic","rules":{},"desired":{"proxy_cidrs":[]},"static_resources":[],"policy":null,"nat_list":null,"future_base":42}}`)
	var receipt RuleSyncReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(t.Context(), "plain", "one", receipt); err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Read(t.Context(), "plain", "one")
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(mustPolicyJSON(receipt))
	for _, key := range []string{"future", "future_rule", "future_base"} {
		if !strings.Contains(encoded, key) {
			t.Fatalf("lost %s", key)
		}
	}
	view := receipt.Public(true)
	if len(view.Rules) != 2 || view.Rules[0].ID != "z" || view.Rules[1].ID != "a" {
		t.Fatalf("legacy insertion order changed %+v", view.Rules)
	}
	if err = store.Write(t.Context(), "clone", "one", RuleSyncReceipt{Status: "waiting"}); err != nil {
		t.Fatal(err)
	}
	clone, _ := store.Read(t.Context(), "clone", "one")
	if clone.Baseline != nil {
		t.Fatal("cloned profile reused receipt")
	}
}
func TestR14UpdateGuards(t *testing.T) {
	for _, mode := range []string{"missing", "local", "ipv4_empty", "ipv6_empty", "two_sources"} {
		t.Run(mode, func(t *testing.T) {
			old := &RuleSyncRecord{Version: "old", Name: "Synthetic", SourceType: "url", IPCount: 1, IP6Count: 1}
			current := *old
			current.Version = "new"
			previous := map[string]*RuleSyncRecord{"remote": old}
			next := map[string]*RuleSyncRecord{"remote": &current}
			switch mode {
			case "missing":
				delete(previous, "remote")
			case "local":
				current.SourceType = "content"
			case "ipv4_empty":
				current.IPCount = 0
			case "ipv6_empty":
				current.IP6Count = 0
			case "two_sources":
				second := *old
				previous["other"] = &second
				empty := second
				empty.IPCount = 0
				next["other"] = &empty
			}
			if err := checkSyncUpdates(previous, next); err == nil {
				t.Fatal("unsafe refresh accepted")
			}
		})
	}
}
func TestR14CancelAndUnpublishedDraftDuringDownload(t *testing.T) {
	for _, mode := range []string{"disable", "gateway", "local_content"} {
		t.Run(mode, func(t *testing.T) {
			s, f, r := r14Fixture(t)
			s.Download = func(ctx context.Context, _ json.RawMessage, _ json.RawMessage) (string, error) {
				_, _, err := s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
					router := doc.GetObject("routers").GetList("items")[0].(*jsonx.Object)
					if mode == "disable" {
						router.Set("auto_rule_sync", false)
					} else if mode == "gateway" {
						router.Set("proxy_gateway_agent_id", "other")
					}
					return nil, nil
				})
				if err != nil {
					return "", err
				}
				if mode == "local_content" {
					_, err = s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
						rule := doc.GetList("rule_library")[0].(*jsonx.Object)
						rule.Set("source_type", "content")
						rule.Set("content", r14Old)
						return nil, nil
					})
				}
				return r14New, err
			}
			result, err := s.Sync(t.Context(), "default", r.ID, "manual")
			if err != nil || result.Status != "paused" {
				t.Fatalf("draft %+v %v", result, err)
			}
			if len(r08Writes(t, f.Calls()).([]any)) > 0 {
				t.Fatal("draft update reached device")
			}
		})
	}
}
func TestR14SpawnRejectionReleasesAdmission(t *testing.T) {
	s, _, r := r14Fixture(t)
	s.Spawner = r13RejectSpawner{}
	before, _ := s.store().Read(t.Context(), "default", r.ID)
	if err := s.StartJob(t.Context(), "default", r.ID); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	after, _ := s.store().Read(t.Context(), "default", r.ID)
	if !jsonEqual(before, after) {
		t.Fatal("rejected spawn left queued receipt")
	}
	_, unlock, ok := syncWorkers.TryLock(t.Context(), s.Repo.DataDir())
	if !ok {
		t.Fatal("worker admission leaked")
	}
	unlock()
}
func TestR14TasksRows(t *testing.T) {
	s, _, r := r14Fixture(t)
	ctx := repo.WithRepository(t.Context(), s.Repo)
	for _, status := range []string{"synced", "running", "queued", "paused", "failed", "waiting"} {
		t.Run(status, func(t *testing.T) {
			receipt, err := s.store().Read(ctx, "default", r.ID)
			if err != nil {
				t.Fatal(err)
			}
			receipt.Status = status
			if status == "waiting" {
				receipt.Baseline = nil
			}
			if err = s.store().Write(ctx, "default", r.ID, receipt); err != nil {
				t.Fatal(err)
			}
			rows, err := ruleSyncRows(ctx, jsonx.NewObject())
			if err != nil || len(rows) != 1 {
				t.Fatal(err, rows)
			}
			row := rows[0]
			if row.GetString("key") != syncTaskKey("default", r.ID) || row.GetInt("interval_seconds") != 3600 {
				t.Fatalf("task identity %s", mustPolicyJSON(row))
			}
			running := status == "running" || status == "queued"
			if row.GetBool("running") != running {
				t.Fatal("running status")
			}
			if row.GetBool("can_run") != (status == "synced" || status == "failed") {
				t.Fatalf("run eligibility %s", mustPolicyJSON(row))
			}
		})
	}
}
func TestR14SourceSelectionAndCacheIdentity(t *testing.T) {
	s, _, r := r14Fixture(t)
	ctx := repo.WithRepository(t.Context(), s.Repo)
	_, err := s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		doc.Set("rule_library", append(doc.GetList("rule_library"), jsonx.Obj("id", "collision", "name", "Synthetic.", "source_type", "url", "url", "https://example.com/other", "behavior", "classical", "enabled", true)))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		doc.Set("rule_configs", append(doc.GetList("rule_configs"), jsonx.Obj("id", "second", "itemType", "ruleset", "library_rule_id", "collision", "enabled", true, "policy", "PROXY")))
		doc.GetObject("routers").GetList("items")[0].(*jsonx.Object).Set("ip_rule_sets", []any{"remote", "second", "remote"})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := syncConfig(ctx, s.Repo, "default")
	if err != nil {
		t.Fatal(err)
	}
	r = findSyncRouter(config, r.ID)
	sources, err := selectedSyncSources(config, r)
	if err != nil || len(sources) != 2 {
		t.Fatal(err, sources)
	}
	if err = syncCacheWrite(ctx, sources[1], "DOMAIN,other.example.com", config); err != nil {
		t.Fatal(err)
	}
	contents, records, err := loadSyncSources(ctx, config, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if contents["remote"] != r14Old || contents["second"] != "DOMAIN,other.example.com" || len(records) != 2 {
		t.Fatalf("cache collision %v", contents)
	}
	ids := []string{}
	for i := 0; i < 129; i++ {
		ids = append(ids, fmt.Sprintf("set-%d", i))
	}
	r.IPRuleSets = ids
	if _, err = selectedSyncSources(config, r); err == nil || err.Error() != messageSyncSelectionCap {
		t.Fatalf("selection cap %v", err)
	}
}

func TestR14ClassifierBounds(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{{"bytes", strings.Repeat("x", syncMaxBytes+1), messageSyncLarge}, {"lines", strings.Repeat("DOMAIN,example.com\n", 100001), messageSyncLines}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ClassifySyncRules(tc.text, "classical"); err == nil || err.Error() != tc.want {
				t.Fatalf("bound %v", err)
			}
		})
	}
}
