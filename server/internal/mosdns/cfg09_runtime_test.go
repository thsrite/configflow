package mosdns

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/taskstatus"
)

func TestCFG09RuntimeReceiptBoundary(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			store := taskstatus.New(t.TempDir())
			if _, err := store.SaveResult(context.Background(), "baseline", taskstatus.Result{OK: true}); err != nil {
				t.Fatal(err)
			}
			receipt := filepath.Join(store.DataDir, taskstatus.Filename)
			before, err := os.ReadFile(receipt)
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			request, disconnect := context.WithCancel(runtime.BindRequest(context.Background()))
			defer disconnect()
			if shutdown {
				// Stop a real Runtime after leaf admission but before the final file commit.
				store.Now = func() time.Time { runtime.Shutdown(context.Background()); return time.Now() }
			} else {
				disconnect()
			}
			_, err = store.SaveResult(request, "new", taskstatus.Result{OK: true})
			after, readErr := os.ReadFile(receipt)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || isBadRequest(err) || !bytes.Equal(before, after) {
					t.Fatalf("shutdown receipt/cause/HTTP classification: %v", err)
				}
			} else if err != nil || bytes.Equal(before, after) {
				t.Fatalf("request disconnect aborted receipt: %v", err)
			}
			temps, err := filepath.Glob(filepath.Join(store.DataDir, ".task-status-*"))
			if err != nil || len(temps) != 0 {
				t.Fatal("receipt temporary file leak", temps, err)
			}
		})
	}
}

func TestCFG09RuntimeNativeStorageCauses(t *testing.T) {
	for _, kind := range []string{"rename", "parent-path"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if kind == "rename" {
				if err := os.Mkdir(filepath.Join(dir, taskstatus.Filename), 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				dir = filepath.Join(dir, "blocked")
				if err := os.WriteFile(dir, []byte("file, not directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			store := taskstatus.New(dir)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			_, err := store.SaveResult(runtime.BindRequest(context.Background()), "native", taskstatus.Result{OK: true})
			if err == nil || isBadRequest(err) || errors.Is(err, context.Canceled) {
				t.Fatalf("native storage failure lost: %v", err)
			}
			if kind == "rename" {
				var cause *os.LinkError
				if !errors.As(err, &cause) {
					t.Fatalf("rename cause lost: %v", err)
				}
			} else {
				var cause *os.PathError
				if !errors.As(err, &cause) {
					t.Fatalf("path cause lost: %v", err)
				}
			}
		})
	}
}
