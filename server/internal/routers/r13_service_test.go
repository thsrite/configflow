package routers

import (
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/repo"
	"configflow/internal/routers/routerfake"
)

type r13Lab struct {
	db            *repo.Repository
	source        r12Source
	first, second *routerfake.Fake
	one, two      *httptest.Server
	router        *Router
}

func r13Fixture(t *testing.T) *r13Lab {
	t.Helper()
	Wire()
	makeFake := func(pending bool) (*routerfake.Fake, *httptest.Server) {
		s := r08State()
		if pending {
			s.Tables[rosRoutes] = s.Tables[rosRoutes][:1]
		}
		f := routerfake.New(nil)
		f.Reset(s)
		server := httptest.NewServer(f)
		t.Cleanup(server.Close)
		return f, server
	}
	f, one := makeFake(true)
	g, two := makeFake(false)
	r := r10Router()
	r.Name = "synthetic main"
	r.Endpoint = one.URL
	r.Username = "synthetic-user"
	r.Password = "synthetic-password"
	r.VerifyTLS = false
	r.ProxyGatewayAgentID = "gateway"
	db := r12Repository(t, r, "{}")
	if _, e := db.CreateProfileRecord(t.Context(), repo.ProfileCreation{ID: "other", Name: "Synthetic other"}, ""); e != nil {
		t.Fatal(e)
	}
	second := *r
	second.Name = "synthetic branch"
	second.Endpoint = two.URL
	if _, _, e := db.SaveProfileFieldsJSON(t.Context(), "other", device.JSON(map[string]any{"mihomo": map[string]string{"custom_config": "{}"}, "routers": map[string]any{"items": []*Router{&second}}}), nil); e != nil {
		t.Fatal(e)
	}
	gateway := r12Gateway()
	gateway.Host = r08Gateway
	return &r13Lab{db: db, source: r12Source{gateway: gateway}, first: f, second: g, one: one, two: two, router: r}
}
func (l *r13Lab) request(t *testing.T) PendingRequest {
	t.Helper()
	return PendingRequest{AgentID: "gateway", Repo: l.db, Agents: l.source, Cache: NewPendingCache(r13Runtime(t), nil)}
}
func TestR13AgentRoutersScopeCacheAndNotifications(t *testing.T) {
	l := r13Fixture(t)
	req := l.request(t)
	result, e := AgentRouters(t.Context(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Routers) != 2 || result.Routers[0].ProfileID != "default" || result.Routers[1].ProfileID != "other" || result.Routers[0].State != "pending" || result.Routers[1].State != "synced" {
		t.Fatalf("scoped ordering/states %+v", result)
	}
	if result.Routers[0].ID != result.Routers[1].ID {
		t.Fatal("fixture must exercise cloned IDs")
	}
	reads := len(l.first.Calls()) + len(l.second.Calls())
	cached, e := AgentRouters(t.Context(), req)
	if e != nil || !cached.Cached || cached.CheckedAt != result.CheckedAt || reads != len(l.first.Calls())+len(l.second.Calls()) {
		t.Fatal("signature cache missed", cached, e)
	}
	req.ProfileID = pointer("other")
	scoped, e := AgentRouters(t.Context(), req)
	if e != nil || scoped.Cached || len(scoped.Routers) != 1 || scoped.Routers[0].ProfileID != "other" {
		t.Fatal(scoped, e)
	}
	if len(l.first.Calls())+len(l.second.Calls()) <= reads {
		t.Fatal("scope change reused unscoped cache")
	}
	req.ProfileID = pointer("")
	if _, e = AgentRouters(t.Context(), req); !errors.Is(e, repo.ErrValidation) {
		t.Fatal("invalid scope fell back", e)
	}
	req.ProfileID = pointer("missing")
	if _, e = AgentRouters(t.Context(), req); !errors.Is(e, repo.ErrNotFound) {
		t.Fatal("missing scope fell back", e)
	}
	data, e := os.ReadFile(filepath.Join(l.db.DataDir(), "notifications.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), "router:default:synthetic:changes") || strings.Contains(string(data), l.router.Password) {
		t.Fatalf("C07 receipt missing or unsafe %s", data)
	}
	if strings.Contains(string(device.JSON(result)), l.router.Password) {
		t.Fatal("public reminder exposed credentials")
	}
	for _, f := range []*routerfake.Fake{l.first, l.second} {
		for _, call := range f.Calls() {
			if call.Method != "GET" {
				t.Fatalf("reminder wrote device %+v", call)
			}
		}
	}
	req.ProfileID = nil
	req.Refresh = true
	if out, e := AgentRouters(t.Context(), req); e != nil || out.Cached {
		t.Fatal(out, e)
	}
	req.Refresh = false
	if _, _, e = dbDraft(l.db, t); e != nil {
		t.Fatal(e)
	}
	if out, e := AgentRouters(t.Context(), req); e != nil || out.Cached {
		t.Fatal("draft revision did not invalidate", out, e)
	}
	l.source.gateway.Version = "synthetic-new"
	if out, e := AgentRouters(t.Context(), req); e != nil || out.Cached {
		t.Fatal("Agent change did not invalidate", out, e)
	}
	req.AgentID = "unlinked"
	empty, e := AgentRouters(t.Context(), req)
	if e != nil || len(empty.Routers) != 0 || empty.Cached {
		t.Fatal(empty, e)
	}
}
func dbDraft(db *repo.Repository, t *testing.T) (json.RawMessage, int64, error) {
	t.Helper()
	v, n, e := db.SaveProfileFieldsJSON(t.Context(), "default", device.JSON(map[string]any{"mihomo": map[string]string{"custom_config": "# synthetic draft\n{}"}}), nil)
	if e != nil {
		return nil, 0, e
	}
	b, e := json.Marshal(v)
	return b, n, e
}

type r13ReadDriver struct {
	Driver
	read func(context.Context) (*RouterState, error)
}

func (d *r13ReadDriver) ReadState(ctx context.Context) (*RouterState, error) { return d.read(ctx) }
func TestR13Fanout25SecondsAndEightWorkers(t *testing.T) {
	l := r13Fixture(t)
	routers := []*Router{}
	for i := 0; i < 12; i++ {
		r := *l.router
		r.ID = string(rune('a' + i))
		routers = append(routers, &r)
	}
	// New records in a new profile need no mutation of an existing connection
	// (the pre-O3-6b fail-closed JobLock behaviour remains intact).
	if _, e := l.db.CreateProfileRecord(t.Context(), repo.ProfileCreation{ID: "many"}, ""); e != nil {
		t.Fatal(e)
	}
	if _, _, e := l.db.SaveProfileFieldsJSON(t.Context(), "many", device.JSON(map[string]any{"mihomo": map[string]string{"custom_config": "{}"}, "routers": map[string]any{"items": routers}}), nil); e != nil {
		t.Fatal(e)
	}
	rt := r13Runtime(t)
	clock := lifecycletest.NewClock(time.Unix(1700000000, 0))
	req := l.request(t)
	req.Cache = NewPendingCache(rt, clock)
	req.ProfileID = pointer("many")
	var reads atomic.Int32
	req.NewDriver = func(r *Router) Driver {
		d := NewDriver(r)
		return &r13ReadDriver{Driver: d, read: func(ctx context.Context) (*RouterState, error) { reads.Add(1); <-ctx.Done(); return nil, ctx.Err() }}
	}
	type answer struct {
		out *PendingResponse
		err error
	}
	done := make(chan answer, 1)
	go func() { out, e := AgentRouters(t.Context(), req); done <- answer{out, e} }()
	r13Eventually(t, func() bool { return reads.Load() == PendingWorkers && clock.Pending() >= 2 })
	clock.Advance(PendingRouterTimeout)
	var result answer
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadline did not return")
	}
	if result.err != nil || len(result.out.Routers) != 12 {
		t.Fatal(result.err, result.out)
	}
	for _, r := range result.out.Routers {
		if r.State != "unknown" || r.Message != "读取超时（25 秒），请检查路由器负载和网络后重试。" {
			t.Fatalf("timeout became another state %+v", r)
		}
	}
	if e := rt.WaitIdle(t.Context()); e != nil {
		t.Fatal(e)
	}
	if reads.Load() != PendingWorkers {
		t.Fatal("queued read started after budget", reads.Load())
	}
	if _, e := os.Stat(filepath.Join(l.db.DataDir(), "notifications.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("timed-out read recorded a receipt", e)
	}
}
func TestR13DisconnectFinishesButShutdownDoesNotCommit(t *testing.T) {
	for _, mode := range []string{"disconnect", "disconnect-before-entry", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			l := r13Fixture(t)
			rt := r13Runtime(t)
			req := l.request(t)
			req.Cache = NewPendingCache(rt, nil)
			req.ProfileID = pointer("default")
			started := make(chan struct{})
			gate := make(chan struct{})
			req.NewDriver = func(r *Router) Driver {
				d := NewDriver(r)
				return &r13ReadDriver{Driver: d, read: func(ctx context.Context) (*RouterState, error) {
					close(started)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-gate:
						return d.ReadState(ctx)
					}
				}}
			}
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := rt.BindRequest(request)
			if mode == "disconnect-before-entry" {
				cancel()
			}
			done := make(chan error, 1)
			go func() { _, e := AgentRouters(ctx, req); done <- e }()
			<-started
			if mode != "shutdown" {
				cancel()
				if e := <-done; !errors.Is(e, context.Canceled) {
					t.Fatal(e)
				}
				close(gate)
				if e := rt.WaitIdle(t.Context()); e != nil {
					t.Fatal(e)
				}
				if _, e := os.Stat(filepath.Join(l.db.DataDir(), "notifications.json")); e != nil {
					t.Fatal("detached record did not finish", e)
				}
				hit, e := AgentRouters(t.Context(), req)
				if e != nil || !hit.Cached {
					t.Fatal("detached owner did not cache", hit, e)
				}
			} else {
				rt.Shutdown(t.Context())
				if e := <-done; e == nil {
					t.Fatal("shutdown lost cancellation")
				}
				if _, e := os.Stat(filepath.Join(l.db.DataDir(), "notifications.json")); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("record after shutdown", e)
				}
				req.Cache.mu.Lock()
				for _, entry := range req.Cache.entries {
					if entry.value != nil {
						t.Error("cache committed after shutdown")
					}
				}
				req.Cache.mu.Unlock()
			}
		})
	}
}

func TestR13OneReadErrorDoesNotHideOtherRouters(t *testing.T) {
	l := r13Fixture(t)
	req := l.request(t)
	var mode atomic.Int32
	req.NewDriver = func(r *Router) Driver {
		d := NewDriver(r)
		if r.Endpoint != l.one.URL {
			return d
		}
		return &r13ReadDriver{Driver: d, read: func(ctx context.Context) (*RouterState, error) {
			switch mode.Load() {
			case 0:
				return nil, device.Error("synthetic-password read rejected")
			case 1:
				panic("synthetic-password private panic")
			default:
				return d.ReadState(ctx)
			}
		}}
	}
	for _, kind := range []string{"router error", "panic", "recovered"} {
		out, e := AgentRouters(t.Context(), req)
		if e != nil || len(out.Routers) != 2 || out.Cached {
			t.Fatal(kind, out, e)
		}
		if out.Routers[1].State != "synced" {
			t.Fatal("bad router hid good router", out)
		}
		if mode.Load() < 2 && out.Routers[0].State != "unknown" {
			t.Fatal("error became known state", out)
		}
		if strings.Contains(string(device.JSON(out)), "synthetic-password") {
			t.Fatal("private diagnostic leaked", out)
		}
		mode.Add(1)
	}
	if out, e := AgentRouters(t.Context(), req); e != nil || !out.Cached {
		t.Fatal("recovered read not reusable", out, e)
	}
}
