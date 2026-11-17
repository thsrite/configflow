package notifications

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
)

func TestLeafCommitChecksOriginalRuntimeAfterCallback(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown during update"}[stop], func(t *testing.T) {
			dir := t.TempDir()
			if err := Transaction(dir, func(items *jsonx.Object) error {
				items.Set("notice", jsonx.Obj("state", "pending", "version", "one"))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "notifications.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(runtime.BindRequest(context.Background()))
			defer cancel()
			cancel()
			err = TransactionContext(request, dir, func(items *jsonx.Object) error {
				items.Set("notice", jsonx.Obj("state", "pending", "version", "two"))
				if stop {
					runtime.Shutdown(context.Background())
				}
				return nil
			})
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if stop {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) {
					t.Fatal("stopped transaction committed", err, string(after))
				}
				if err := MarkReadContext(request, dir, jsonx.Obj("notice", "one")); !errors.Is(err, context.Canceled) {
					t.Fatal("stopped mark-read admitted", err)
				}
			} else if err != nil || bytes.Equal(before, after) {
				t.Fatal("disconnect aborted transaction", err)
			}
		})
	}
}

func TestWaitingLeafTransactionHonoursShutdown(t *testing.T) {
	dir := t.TempDir()
	_, release, err := guard.Lock(context.Background(), "notifications", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
	runtime.Start(context.Background())
	request := runtime.BindRequest(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- TransactionContext(request, dir, func(*jsonx.Object) error { t.Error("cancelled waiter entered"); return nil })
	}()
	runtime.Shutdown(context.Background())
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("leaf wait ignored shutdown")
	}
}
