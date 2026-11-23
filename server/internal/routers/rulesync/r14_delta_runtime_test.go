package rulesync

import (
	"configflow/internal/routers"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/routers/routerfake"
)

func TestR14RuntimeDestinationWriteBoundaries(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			f := routerfake.New(nil)
			f.Reset(r08State())
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := runtime.BindRequest(request)
			var triggered atomic.Bool
			var callsAtStop atomic.Int64
			stopped := make(chan struct{})
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capture := httptest.NewRecorder()
				f.ServeHTTP(capture, r)
				if r.Method == "PUT" && triggered.CompareAndSwap(false, true) {
					if mode == "disconnect" {
						cancel()
					} else {
						runtime.Shutdown(t.Context())
						callsAtStop.Store(int64(len(f.Calls())))
					}
					close(stopped)
				}
				for key, values := range capture.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(capture.Code)
				_, _ = w.Write(capture.Body.Bytes())
			}))
			defer httpServer.Close()
			d := routers.NewRouterOS(httpServer.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			old := r08Desired(false)
			old.ProxyCIDRs = old.ProxyCIDRs[:1]
			static, err := syncStaticResources(t.Context(), d, old)
			if err != nil {
				t.Fatal(err)
			}
			next := r08Desired(false)
			plan := r14Plan(t, t.Context(), d, r10Router(), next)
			result, err := ApplyRuleSyncDelta(ctx, d, next, plan, &RuleSyncBaseline{Desired: old, StaticResources: static}, nil)
			if !triggered.Load() {
				t.Fatal("device write cancellation boundary not exercised")
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("write-boundary callback did not finish")
			}
			if mode == "disconnect" {
				if err != nil || result.Added != 1 {
					t.Fatalf("disconnect abandoned accepted destination %+v %v", result, err)
				}
			} else {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown continued %v", err)
				}
				if int64(len(f.Calls())) != callsAtStop.Load() {
					t.Fatal("device readback/compensation continued after shutdown")
				}
			}
		})
	}
}
