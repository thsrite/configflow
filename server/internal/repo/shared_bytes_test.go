package repo_test

import (
	"configflow/internal/repo"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestN89NativeSharedDeleteCommitLifetime(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			path, err := r.WriteSharedText(base, "n89/cache.json", []byte("synthetic-cache"))
			if err != nil {
				t.Fatal(err)
			}
			ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			type result struct {
				removed bool
				err     error
			}
			done := make(chan result, 1)
			go func() {
				removed, err := repo.RemoveSharedUsingForTest(r, ctx, "n89/cache.json", func(held context.Context, path string, fn func() error) error {
					return repo.WithFileForTest(held, path, func() error { close(entered); <-release; return fn() })
				})
				done <- result{removed, err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("file lease not acquired")
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			unblock()
			select {
			case got := <-done:
				if shutdown {
					if got.removed || !errors.Is(got.err, context.Canceled) {
						t.Fatalf("stopped delete: %+v", got)
					}
					data, err := os.ReadFile(path)
					if err != nil || string(data) != "synthetic-cache" {
						t.Fatalf("cancelled deletion changed file: %s %v", data, err)
					}
				} else {
					if !got.removed || got.err != nil {
						t.Fatalf("disconnected delete: %+v", got)
					}
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("file survived deletion: %v", err)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("delete did not finish")
			}
		})
	}
}
