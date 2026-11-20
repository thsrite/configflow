package routers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/routerfake"
)

const r14Old = "IP-CIDR,198.51.100.0/24\nIP-CIDR6,2001:db8:1::/48\nDOMAIN-SUFFIX,example.com"
const r14New = "IP-CIDR,203.0.113.0/24\nIP-CIDR6,2001:db8:2::/48\nDOMAIN-SUFFIX,new.example.com"

func r14Hook(t *testing.T) {
	t.Helper()
	old := repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{
		JobLockRecord: func(ctx context.Context, profile, id string, raw model.Router) (func(), bool) {
			var r Router
			_ = json.Unmarshal(mustPolicyJSON(raw), &r)
			_, release, lockErr := RouterJobLock(ctx, profile, id, &r)
			return release, lockErr == nil
		},
	}})
	t.Cleanup(func() { repo.SetHooks(old) })
}
func r14Fixture(t *testing.T) (*RuleSyncService, *routerfake.Fake, *Router) {
	t.Helper()
	r14Hook(t)
	f := routerfake.New(nil)
	f.Reset(r08State())
	http := httptest.NewServer(f)
	t.Cleanup(http.Close)
	var r Router
	if err := json.Unmarshal([]byte(`{"id":"r-sync","name":"Synthetic Router","type":"routeros","username":"synthetic-user","password":"synthetic-password","verify_tls":false,"auto_rule_sync":true,"proxy_gateway_agent_id":"gateway","ip_rule_sets":["remote"],"ros_policy_rule_sets":[],"dns_agent_id":"","manage_return_routing":false}`), &r); err != nil {
		t.Fatal(err)
	}
	r.Endpoint = http.URL
	r.source = nil
	raw := mustPolicyJSON(map[string]any{"routers": map[string]any{"items": []*Router{&r}}, "rule_library": []any{map[string]any{"id": "lib", "name": "Synthetic", "source_type": "url", "url": "https://example.com/ips", "behavior": "classical", "enabled": true}}, "rule_configs": []any{map[string]any{"id": "remote", "itemType": "ruleset", "library_rule_id": "lib", "enabled": true, "policy": "PROXY"}}})
	db, err := repo.Open(t.TempDir(), repo.Options{InitialSettings: func() *model.Config {
		cfg, err := model.DecodeConfig(raw)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}})
	if err != nil {
		t.Fatal(err)
	}
	service := &RuleSyncService{Repo: db, Agents: r12Source{gateway: &GatewayAgent{Agent: &model.Agent{ID: "gateway", Host: r08Gateway, ServiceType: "mihomo", ProfileID: "default"}}}, Download: func(context.Context, json.RawMessage, json.RawMessage) (string, error) { return r14New, nil }}
	cfg, err := syncConfig(repo.WithRepository(t.Context(), db), db, "default")
	if err != nil {
		t.Fatal(err)
	}
	router := findSyncRouter(cfg, r.ID)
	sources, err := selectedSyncSources(cfg, router)
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), db)
	if err = syncCacheWrite(ctx, sources[0], r14Old, cfg); err != nil {
		t.Fatal(err)
	}
	_, records, err := loadSyncSources(ctx, cfg, router, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := service.driver(router)
	defer d.Close()
	inspection, err := Inspect(ctx, InspectRequest{Loader: syncIPLoader(records), ProfileID: "default", Config: cfg, Router: router, Driver: d, Agents: service.Agents, Repo: db})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(ctx, router, inspection, d)
	if err != nil || len(plan.Blockers) > 0 {
		t.Fatalf("plan %v %+v", err, plan)
	}
	if result := Execute(ctx, plan, d, nil); !result.OK {
		t.Fatalf("initial publication %+v", result)
	}
	if err = service.Capture(ctx, "default", cfg, router, d, inspection.Desired); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.store().Read(ctx, "default", router.ID)
	if err != nil || receipt.Baseline == nil {
		t.Fatalf("baseline %+v %v", receipt, err)
	}
	f.Reset(f.Snapshot())
	return service, f, router
}
func TestR14CaptureAndSync(t *testing.T) {
	service, f, r := r14Fixture(t)
	before, err := service.Repo.ConfigRevision(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Sync(t.Context(), "default", r.ID, "manual")
	if err != nil || result.Status != "synced" || !result.Ready || result.Added != 0 || result.Removed != 0 {
		t.Fatalf("sync %+v %v", result, err)
	}
	if len(r08Writes(t, f.Calls()).([]any)) != 0 {
		t.Fatal("domain-only/test-net update wrote device")
	}
	after, _ := service.Repo.ConfigRevision(t.Context(), "default")
	if !jsonEqual(before, after) {
		t.Fatal("no-op bumped config revision")
	}
	if len(result.Rules) != 1 || result.Rules[0].Version != result.Rules[0].SyncedVersion {
		t.Fatalf("versions %+v", result.Rules)
	}
	public := string(mustPolicyJSON(result))
	if strings.Contains(public, r.Password) || strings.Contains(public, "baseline") || strings.Contains(public, "static_resources") {
		t.Fatal("public receipt leaked private data")
	}
	if _, err = os.Stat(service.store().path("default", r.ID)); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.Calls() {
		if call.Method == "GET" || call.Method == "DELETE" {
			if call.Body != nil {
				t.Fatalf("unexpected raw body %s %q", call.Method, call.Body)
			}
		}
	}
}
func TestR14FailuresBeforeWrites(t *testing.T) {
	for _, mode := range []string{"download", "invalid", "controls", "running", "missing", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, f, r := r14Fixture(t)
			ctx := repo.WithRepository(t.Context(), s.Repo)
			receipt, _ := s.store().Read(ctx, "default", r.ID)
			switch mode {
			case "download":
				s.Download = func(context.Context, json.RawMessage, json.RawMessage) (string, error) {
					return "", &syncDownloadError{routerError(messageSyncDownloadError + "ConnectionError")}
				}
			case "invalid":
				s.Download = func(context.Context, json.RawMessage, json.RawMessage) (string, error) {
					return "<html>bad</html>", nil
				}
			case "controls":
				receipt.Baseline.Control = "changed"
			case "running":
				receipt.Status = "running"
			case "missing":
				receipt.Baseline = nil
			case "disabled":
				_, err := s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, current *model.Profile) error {
					current.Routers.Items[0].AutoRuleSync = new(false)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := s.store().Write(ctx, "default", r.ID, receipt); err != nil {
				t.Fatal(err)
			}
			result, err := s.Sync(ctx, "default", r.ID, "manual")
			if mode == "missing" || mode == "disabled" || mode == "running" {
				if err == nil {
					t.Fatalf("accepted %+v", result)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := "paused"
				if mode == "download" {
					want = "failed"
				}
				if result.Status != want {
					t.Fatalf("%s %+v", want, result)
				}
			}
			if len(r08Writes(t, f.Calls()).([]any)) != 0 {
				t.Fatal("unsafe update wrote")
			}
		})
	}
}
func TestR14RuntimeBoundaries(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown", "shutdown_before_entry"} {
		t.Run(mode, func(t *testing.T) {
			s, f, r := r14Fixture(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := runtime.BindRequest(request)
			before, _ := s.store().Read(t.Context(), "default", r.ID)
			if mode == "shutdown_before_entry" {
				runtime.Shutdown(t.Context())
			} else {
				s.Download = func(ctx context.Context, _ json.RawMessage, _ json.RawMessage) (string, error) {
					if mode == "disconnect" {
						cancel()
					} else {
						runtime.Shutdown(t.Context())
					}
					return r14New, ctx.Err()
				}
			}
			result, err := s.Sync(ctx, "default", r.ID, "manual")
			after, _ := s.store().Read(t.Context(), "default", r.ID)
			if mode == "disconnect" {
				if err != nil || result.Status != "synced" {
					t.Fatalf("disconnect %+v %v", result, err)
				}
			} else {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown %+v %v", result, err)
				}
				if mode == "shutdown_before_entry" {
					if !jsonEqual(before, after) {
						t.Fatal("committed receipt after shutdown")
					}
				} else if after.Status != "running" {
					t.Fatalf("post-cancel receipt %+v", after)
				}
			}
			if len(r08Writes(t, f.Calls()).([]any)) > 0 {
				t.Fatal("unexpected device writes")
			}
		})
	}
}
func TestR14ReceiptLeafAndPrivacy(t *testing.T) {
	store := RuleSyncStore{DataDir: t.TempDir()}
	ctx := t.Context()
	receipt := RuleSyncReceipt{Status: "synced", Message: messageSyncDone}
	if err := store.Write(ctx, "plain", "r", receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path("plain", "r"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := store.Read(ctx, "plain", "r")
	if err != nil || got.Status != "paused" || got.Message != messageSyncUnreadable {
		t.Fatalf("corrupt %+v %v", got, err)
	}
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(ctx)
	cancelCtx := runtime.BindRequest(ctx)
	runtime.Shutdown(ctx)
	if err = store.Write(cancelCtx, "plain", "r", receipt); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.path("plain", "r"))
	if string(data) != "broken" {
		t.Fatal("cancelled replacement landed")
	}
}
func FuzzR14Classify(f *testing.F) {
	for _, seed := range []string{r14Old, "payload:\n - DOMAIN,example.com", "<html>", "IP-CIDR,192.0.2.1/24", "payload: &x [*x]"} {
		f.Add(seed, "classical")
	}
	f.Fuzz(func(t *testing.T, text, behavior string) {
		if len(text) > syncMaxBytes+1 {
			return
		}
		record, err := ClassifySyncRules(text, behavior)
		if err == nil && (record.IPCount != len(record.IPv4) || record.IP6Count != len(record.IPv6)) {
			t.Fatal("inconsistent counts")
		}
	})
}
