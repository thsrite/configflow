package routers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/notifications"
)

// TestR13ReviewMalformedRouterIsolated: Python reads routers_of(config) as raw
// dicts, so a stored router whose fields have unexpected types only affects
// that router (and only when it is linked). One such record must not turn the
// reminder of every Agent into an error.
func TestR13ReviewMalformedRouterIsolated(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprintf("linked=%v", linked), func(t *testing.T) {
			l := r13Fixture(t)
			agent := "another-gateway"
			if linked {
				agent = "gateway"
			}
			bad := map[string]any{"id": "bad", "name": "synthetic malformed", "type": "routeros", "endpoint": "https://192.0.2.9",
				"username": "synthetic-user", "password": "synthetic-password", "rule_sync_interval": "sixty",
				"proxy_gateway_agent_id": agent}
			if _, e := l.db.CreateProfile(t.Context(), r12Record(t, map[string]string{"id": "legacy", "name": "Synthetic legacy"}), ""); e != nil {
				t.Fatal(e)
			}
			if _, _, e := l.db.SaveProfile(t.Context(), "legacy", r12Record(t, map[string]any{"mihomo": map[string]string{"custom_config": "{}"}, "routers": map[string]any{"items": []any{bad}}}), nil); e != nil {
				t.Skip("repository rejects the malformed record:", e)
			}
			out, e := AgentRouters(t.Context(), l.request(t))
			if e != nil {
				t.Fatal("one malformed router record hid every reminder:", e)
			}
			want := 2
			if linked {
				want = 3
			}
			if len(out.Routers) != want || out.Routers[0].State != "pending" || out.Routers[1].State != "synced" {
				t.Fatalf("good routers lost %+v", out.Routers)
			}
			if linked {
				got := out.Routers[2]
				if got.ID != "bad" || got.Name != "synthetic malformed" || got.Type != "routeros" || got.ProfileID != "legacy" || got.State != "unknown" || got.Message == "" {
					t.Fatalf("malformed linked router %+v", got)
				}
				if strings.Contains(string(mustPolicyJSON(out)), "synthetic-password") {
					t.Fatal("credential leaked")
				}
			}
		})
	}
}

// TestR13ReviewNoticeMatchesReminder: the C07 receipt is produced through the
// legacy Summarize hook from the marshalled real report and plan; it must say
// exactly what the native reminder says.
func TestR13ReviewNoticeMatchesReminder(t *testing.T) {
	l := r13Fixture(t)
	out, e := AgentRouters(t.Context(), l.request(t))
	if e != nil {
		t.Fatal(e)
	}
	first := out.Routers[0]
	if first.State != "pending" || first.Pending == 0 || first.Message != "" {
		t.Fatalf("fixture must be pending %+v", first)
	}
	data, e := os.ReadFile(filepath.Join(l.db.DataDir(), "notifications.json"))
	if e != nil {
		t.Fatal(e)
	}
	want := fmt.Sprintf("有 %d 项配置差异待发布。", first.Pending)
	if !strings.Contains(string(data), want) {
		t.Fatalf("receipt summary differs from reminder; want %q in %s", want, data)
	}
}

// TestR13ReviewEmptyAgentMatchesNothing: an empty Agent ID is a missing
// identity, never a match against routers that have no proxy gateway.
func TestR13ReviewEmptyAgentMatchesNothing(t *testing.T) {
	l := r13Fixture(t)
	if _, e := l.db.CreateProfile(t.Context(), r12Record(t, map[string]string{"id": "plain", "name": "Synthetic plain"}), ""); e != nil {
		t.Fatal(e)
	}
	plain := *l.router
	plain.ProxyGatewayAgentID = ""
	if _, _, e := l.db.SaveProfile(t.Context(), "plain", r12Record(t, map[string]any{"mihomo": map[string]string{"custom_config": "{}"}, "routers": map[string]any{"items": []*Router{&plain}}}), nil); e != nil {
		t.Fatal(e)
	}
	req := l.request(t)
	req.AgentID = ""
	out, e := AgentRouters(t.Context(), req)
	if e != nil || len(out.Routers) != 0 {
		t.Fatalf("empty Agent ID read routers without a gateway: %+v %v", out, e)
	}
	if n := len(l.first.Calls()) + len(l.second.Calls()); n != 0 {
		t.Fatalf("device read for an empty Agent ID: %d calls", n)
	}
}

type r13CloseDriver struct {
	Driver
	closed *atomic.Int32
}

func (d r13CloseDriver) Close() { d.Driver.Close(); d.closed.Add(1) }

// TestR13ReviewNoticeWaitingForLeafLock (P1, after C07 batch-1): the owner has
// read every router and waits for the notification leaf lock. Shutdown while
// it waits must not write a receipt; a request disconnect must still write it.
func TestR13ReviewNoticeWaitingForLeafLock(t *testing.T) {
	for _, mode := range []string{"shutdown", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			l := r13Fixture(t)
			rt := r13Runtime(t)
			req := l.request(t)
			req.Cache = NewPendingCache(rt, nil)
			req.ProfileID = pointer("default")
			var closed atomic.Int32
			req.NewDriver = func(r *Router) Driver { return r13CloseDriver{NewDriver(r), &closed} }
			held, release := make(chan struct{}), make(chan struct{})
			blocker := make(chan error, 1)
			go func() {
				blocker <- notifications.TransactionContext(context.Background(), l.db.DataDir(), func(*jsonx.Object) error {
					close(held)
					<-release
					return nil
				})
			}()
			<-held
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := AgentRouters(rt.BindRequest(request), req); done <- e }()
			r13Eventually(t, func() bool { return closed.Load() == 1 })
			// The read is finished; the owner now only marshals the receipt and
			// waits for the leaf lock (held well below its 5 s limit).
			time.Sleep(300 * time.Millisecond)
			path := filepath.Join(l.db.DataDir(), "notifications.json")
			if mode == "shutdown" {
				rt.Shutdown(t.Context())
				close(release)
				if e := <-blocker; e != nil {
					t.Fatal(e)
				}
				if e := <-done; e == nil {
					t.Fatal("shutdown lost cancellation")
				}
				if _, e := os.Stat(path); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("receipt written after shutdown", e)
				}
				return
			}
			cancel()
			if e := <-done; !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			close(release)
			if e := <-blocker; e != nil {
				t.Fatal(e)
			}
			if e := rt.WaitIdle(t.Context()); e != nil {
				t.Fatal(e)
			}
			data, e := os.ReadFile(path)
			if e != nil || !strings.Contains(string(data), "router:default:synthetic:changes") {
				t.Fatal("detached owner did not write its receipt", e)
			}
		})
	}
}
