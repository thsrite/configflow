package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/routerfake"
)

type r12Source struct {
	gateway *GatewayAgent
	client  ReturnAgent
}

func (s r12Source) Agent(_ context.Context, id string) (*GatewayAgent, error) {
	if s.gateway != nil && s.gateway.ID == id {
		return s.gateway, nil
	}
	return nil, nil
}
func (s r12Source) ReturnAgent(*GatewayAgent) ReturnAgent { return s.client }
func r12Repository(t *testing.T, r *Router, yaml string) *repo.Repository {
	t.Helper()
	raw := device.JSON(map[string]any{"mihomo": map[string]any{"custom_config": yaml}, "routers": map[string]any{"items": []*Router{r}}})
	db, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.SaveProfileFieldsJSON(t.Context(), "default", raw, nil); err != nil {
		t.Fatal(err)
	}
	return db
}
func r12Config(t *testing.T, db *repo.Repository) (json.RawMessage, *Router, model.Profile) {
	t.Helper()
	cfg, e := db.Config(t.Context(), "default", repo.ResourceRoots{})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	items := spec.RoutersOf(raw)
	if len(items) != 1 {
		t.Fatal(string(raw))
	}
	var profile model.Profile
	if e = json.Unmarshal(raw, &profile); e != nil {
		t.Fatal(e)
	}
	return raw, items[0], profile
}
func r12Gateway() *GatewayAgent {
	agent := r10Agent()
	agent.ProfileID = "default"
	agent.Status = "offline"
	agent.Port = 9100
	return &GatewayAgent{Agent: agent}
}
func TestR12RuntimePublicationBoundaries(t *testing.T) {
	originalHost := IPv6HostRequest
	IPv6HostRequest = func(context.Context, *GatewayAgent) (IPv6Host, error) { return r10Host(), nil }
	defer func() { IPv6HostRequest = originalHost }()
	for _, mode := range []string{"disconnect", "shutdown_before_write", "shutdown_after_write", "shutdown_in_commit", "disconnect_in_commit"} {
		t.Run(mode, func(t *testing.T) {
			f := routerfake.New(nil)
			f.Reset(r11State())
			server := httptest.NewServer(f)
			defer server.Close()
			r := r10Router()
			r.Endpoint = server.URL
			r.Username = "synthetic-user"
			r.Password = "synthetic-password"
			r.ProxyGatewayAgentID = "gateway"
			r.VerifyTLS = false
			db := r12Repository(t, r, r10YAML)
			cfg, r, before := r12Config(t, db)
			source := r12Source{gateway: r12Gateway()}
			previewDriver := NewRouterOS(server.URL, r.Username, r.Password, false)
			inspection, e := Inspect(t.Context(), InspectRequest{ProfileID: "default", Config: cfg, Router: r, Driver: previewDriver, Agents: source, Repo: db})
			if e != nil {
				t.Fatal(e)
			}
			plan, e := BuildPlan(t.Context(), r, inspection, previewDriver)
			previewDriver.Close()
			if e != nil || len(plan.Blockers) > 0 || plan.Empty {
				t.Fatalf("preview %v %+v", e, plan)
			}
			if len(r08Comparable(t, r08Writes(t, f.Calls())).([]any)) != 0 {
				t.Fatal("preview wrote device")
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := runtime.BindRequest(request)
			triggered := false
			callsAtShutdown := 0
			var publicationDriver *RouterOS
			req := PublishRequest{ProfileID: "default", RouterID: r.ID, Expected: plan.Fingerprint, Router: r, Repo: db, Agents: source, NewDriver: func(r *Router) Driver {
				d := NewRouterOS(server.URL, r.Username, r.Password, false)
				publicationDriver = d
				d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, url string) {
					if triggered {
						return
					}
					if mode == "disconnect" && method == "PUT" {
						triggered = true
						cancel()
					}
					// The feature owner has finished and published its in-memory proof; the
					// next readback boundary cancels the real Runtime, before any next write.
					if mode == "shutdown_after_write" && d.IPv6FakeIPManifestSet() && effectful(d.IPv6FakeIPManifest()) && strings.Contains(string(d.IPv6FakeIPManifest()), "resources") && strings.HasSuffix(url, ros.PathIPv6Routes) {
						triggered = true
						runtime.Shutdown(t.Context())
						callsAtShutdown = len(f.Calls())
					}
					if mode == "shutdown_before_write" && method == "GET" {
						triggered = true
						runtime.Shutdown(t.Context())
						callsAtShutdown = len(f.Calls())
					}
				}})
				return d
			}}
			previous := repo.SetHooks(repo.Hooks{})
			defer repo.SetHooks(previous)
			Wire()
			held, unlock, e := oplock.RouterJobLock(repo.WithRepository(ctx, db), "default", r.ID, r)
			if e != nil {
				t.Fatal(e)
			}
			defer unlock()
			commits := 0
			db.Store().BeforeCommit = func() error {
				commits++
				if !triggered && (mode == "shutdown_in_commit" || mode == "disconnect_in_commit") {
					triggered = true
					if mode == "shutdown_in_commit" {
						runtime.Shutdown(t.Context())
						callsAtShutdown = len(f.Calls())
					} else {
						cancel()
					}
				}
				return nil
			}
			defer func() { db.Store().BeforeCommit = nil }()
			outcome := Publish(held, req)
			unlock()
			_, stored, after := r12Config(t, db)
			if !triggered {
				t.Fatal("cancellation boundary not exercised")
			}
			switch mode {
			case "disconnect", "disconnect_in_commit":
				if outcome.Err != nil || outcome.Result == nil || !outcome.Result.OK || after.Revision != before.Revision+1 {
					t.Fatalf("disconnect %+v revision %d -> %d", outcome, before.Revision, after.Revision)
				}
			case "shutdown_before_write":
				if !errors.Is(outcome.Err, context.Canceled) || after.Revision != before.Revision || len(stored.Extra["last_release"]) > 0 {
					t.Fatalf("write-before-side-effect %+v revision %d -> %d", outcome, before.Revision, after.Revision)
				}
			case "shutdown_after_write", "shutdown_in_commit":
				if outcome.Err != nil || outcome.Result == nil || !outcome.Result.Aborted || !outcome.Result.Wrote || outcome.Status != PublishFailed || len(outcome.Result.Rollback) > 0 {
					t.Fatalf("abort lost result %+v result %+v", outcome, outcome.Result)
				}
				if after.Revision != before.Revision+1 || !effectful(stored.State("ipv6_fakeip_state")) {
					t.Fatalf("missing atomic failure/proof revision %d -> %d stored %+v", before.Revision, after.Revision, stored)
				}
				var last LastRelease
				if e = json.Unmarshal(stored.Extra["last_release"], &last); e != nil || last.OK || last.Fingerprint != plan.Fingerprint {
					t.Fatalf("failure record %+v %v", last, e)
				}
				if len(f.Calls()) != callsAtShutdown {
					t.Fatal("device I/O after shutdown")
				}
				// Restart inspection must recognize the response-proven owned route.
				fresh := NewRouterOS(server.URL, r.Username, r.Password, false)
				defer fresh.Close()
				p := ipv6.InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), stored, r10Agent(), r10Host(), fresh.Client)
				if len(p.Blockers) > 0 || len(p.Ops) > 0 || p.Status != "synced" {
					t.Fatalf("restart lost ownership %+v", p)
				}
			}
			if after.Revision == before.Revision+1 {
				manifests := publicationDriver.Manifests()
				for name, slot := range map[string]Manifest{"ros_policy_state": manifests.Policy, "dns_guard_state": manifests.DNSGuard, "ipv6_fakeip_state": manifests.IPv6FakeIP, "ipv6_routes_state": manifests.IPv6Routes, "ipv6_pd_guard_state": manifests.IPv6PDGuard} {
					expected := r.State(name)
					if slot.Set {
						expected = slot.Value
					}
					actual := stored.State(name)
					if len(actual) == 0 && len(expected) == 0 {
						continue
					}
					if !device.JSONEqual(actual, expected) {
						t.Fatalf("manifest %s: got %s want %s", name, actual, expected)
					}
				}
			}
			wantAttempts := 1
			if mode == "shutdown_before_write" {
				wantAttempts = 0
			}
			if mode == "shutdown_in_commit" {
				wantAttempts = 2
			} // First COMMIT rejected; one local exception lands.
			if commits != wantAttempts {
				t.Fatalf("commit attempts=%d, want %d", commits, wantAttempts)
			}
			if lockx.Violations() != 0 {
				t.Fatalf("lock order violations: %d", lockx.Violations())
			}
			_, free, e := oplock.RouterJobLock(t.Context(), "alias-profile", "alias-router", r)
			if e != nil {
				t.Fatal("L1 leaked", e)
			}
			free()
			held, release, e := oplock.RouterOperationLock(t.Context(), *r, false)
			_ = held
			if e != nil {
				t.Fatal("L2 leaked", e)
			}
			release()
		})
	}
}
