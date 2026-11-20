package rulelibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/refresh"
	"configflow/internal/taskstatus"
)

func TestO36cForgetStatusWhileWaiting(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			app := fixture(t)
			status := taskstatus.New(app.DataDir)
			if _, err := status.SaveResult(app.Context(), refresh.RuleTaskKey("manual"), taskstatus.Result{OK: true}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(app.DataDir, taskstatus.Filename)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatal(err)
			}
			// A malformed counter makes the holder roll back after Now,
			// giving a real leaf-lock barrier without changing the status file.
			data["holder"] = json.RawMessage(`{"run_count":["invalid counter"]}`)
			before, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			beforeDoc := doc(t, app)
			runtime, request, disconnect := runtimeRequest(t, app.Context())
			ctx := lifecycle.Detach(request)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			holder := taskstatus.New(app.DataDir)
			holder.Now = func() time.Time {
				close(entered)
				<-release
				return time.Now()
			}
			holderDone := make(chan error, 1)
			go func() {
				_, err := holder.SaveResult(context.Background(), "holder", taskstatus.Result{OK: true})
				holderDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("status holder did not acquire the leaf lock")
			}
			done := make(chan error, 1)
			go func() { done <- forget(ctx, "manual") }()
			select {
			case err := <-done:
				t.Fatalf("forget escaped held status lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			unblock()
			select {
			case err := <-holderDone:
				if err == nil {
					t.Fatal("invalid-counter holder unexpectedly committed")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("status holder did not finish")
			}
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("forget did not finish")
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || doc(t, app) != beforeDoc {
				t.Fatal("forget changed the database or could not read status", readErr)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) {
					t.Fatalf("forget committed after shutdown while queued: %v", err)
				}
			} else if err != nil || bytes.Equal(before, after) {
				t.Fatalf("disconnected forget did not complete: %v", err)
			}
		})
	}
}
