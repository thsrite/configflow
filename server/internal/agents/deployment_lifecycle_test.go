package agents

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/store"
)

func TestTypedRollbackDisconnectAndShutdownCommitBoundary(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown-during-reply"}[shutdown], func(t *testing.T) {
			e := newEnv(t)
			id := successfulDeployment(e, "native-rollback")
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(e.ctx)
			defer runtime.Shutdown(context.Background())
			bound := runtime.BindRequest(e.ctx)
			request, disconnect := context.WithCancel(bound)
			disconnect()
			var reserved store.Head
			calls := 0
			e.fake.set(func(Request) (*Response, error) {
				calls++
				var err error
				reserved, err = e.repo.Head(e.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				}
				return reply(map[string]any{"success": true, "status": "rolled_back", "manual_rollback": true}, 200), nil
			})
			state, err := e.m.RollbackDeploymentModel(request, e.id(), id)
			if calls != 1 {
				t.Fatalf("requests=%d", calls)
			}
			saved, _ := e.stored(e.id()).Deployments.Get(id)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("shutdown accepted receipt", state, err)
				}
				if saved.Status != "rolling_back" {
					t.Fatal("shutdown overwrote reservation", saved)
				}
				if after, err := e.repo.Head(e.ctx); err != nil || after != reserved {
					t.Fatal("receipt committed after shutdown", err)
				}
				if _, err := e.m.RecoverDeploymentModel(bound, e.id(), id); !errors.Is(err, context.Canceled) {
					t.Fatal("shutdown admitted recovery", err)
				}
			} else if err != nil || state.Status != "rolled_back" || saved.Status != "rolled_back" {
				t.Fatal("disconnect canceled durable action", state, err)
			}
		})
	}
}
