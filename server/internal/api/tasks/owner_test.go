package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func TestIntervalOwnerKeepsDisconnectButRejectsStoppedGeneration(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := repo.WithScope(context.Background())
			err = r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
				raw, err := json.Marshal([]any{map[string]any{"id": "s", "interval": 86400}})
				if err != nil {
					return err
				}
				return shared.SetJSON("subscriptions", raw)
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(runtime.BindRequest(ctx))
			defer cancel()
			reg := tasks.NewRegistry(r, runtime)
			reg.SetRowBuilder(tasks.SlotSubscriptions, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil })
			before, err := r.ExportAllJSON(ctx, false)
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
			_, err = updateIntervalOn(request, reg, r, body)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				after, err := r.ExportAllJSON(ctx, false)
				if err != nil || !testjson.Equal(before, after) {
					t.Fatal("shutdown changed stored document", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				shared, err := r.Shared(ctx)
				if err != nil || len(shared.Subscriptions) == 0 || !testjson.Equal(shared.Subscriptions[0].Interval, 60) {
					t.Fatal("disconnect aborted save", err)
				}
			}
		})
	}
}
