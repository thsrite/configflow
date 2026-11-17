package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

func TestIntervalOwnerKeepsDisconnectButRejectsStoppedGeneration(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
			_, err = r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
				shared.Set("subscriptions", []any{jsonx.Obj("id", "s", "interval", 86400)})
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(runtime.BindRequest(ctx))
			defer cancel()
			t.Cleanup(tasks.SetRows(tasks.SlotSubscriptions, func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }))
			before, err := r.ExportAll(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				cancel()
			}
			var body intervalRequest
			if err := body.UnmarshalJSON([]byte(`{"key":"subscription:s","interval_seconds":60}`)); err != nil {
				t.Fatal(err)
			}
			_, err = updateInterval(request, body)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				after, err := r.ExportAll(ctx, false)
				if err != nil || !jsonx.Equal(before, after) {
					t.Fatal("shutdown changed stored document", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				shared, err := r.GetShared(ctx)
				if err != nil || shared.GetList("subscriptions")[0].(*jsonx.Object).GetInt("interval") != 60 {
					t.Fatal("disconnect aborted save", err)
				}
			}
		})
	}
}
