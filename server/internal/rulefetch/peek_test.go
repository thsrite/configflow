package rulefetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func TestPeekCacheSharesValidationWithoutMigration(t *testing.T) {
	for _, kind := range []string{"valid", "receipt", "native receipt", "symlink", "format"} {
		t.Run(kind, func(t *testing.T) {
			ctx, _, legacy, old, native := migrationFixture(t)
			rule := legacyRule(legacy)
			switch kind {
			case "receipt":
				if err := os.WriteFile(old+".invalid", []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			case "native receipt":
				if err := os.MkdirAll(filepath.Dir(native), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(native+".invalid", []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.list")
				if err := os.Rename(old, outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, old); err != nil {
					t.Fatal(err)
				}
			case "format":
				if err := os.WriteFile(old, []byte("not: a payload\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Stat(old)
			if err != nil {
				t.Fatal(err)
			}
			data, err := PeekCacheWithFormat(ctx, rule, &SourceConfig{}, "yaml")
			if err != nil {
				t.Fatal(err)
			}
			if kind == "valid" && string(data) != "payload: [example.com]\n" || kind != "valid" && data != nil {
				t.Fatalf("peek %s: %q", kind, data)
			}
			after, err := os.Stat(old)
			if err != nil || !after.ModTime().Equal(before.ModTime()) {
				t.Fatal("peek touched legacy mtime", err)
			}
			if _, err := os.Stat(native); !os.IsNotExist(err) {
				t.Fatal("peek migrated bytes", err)
			}
		})
	}
}

func TestRuntimeCacheReadMigratesOnlyWhileActive(t *testing.T) {
	for _, kind := range []string{"disconnect", "shutdown", "lock shutdown"} {
		t.Run(kind, func(t *testing.T) {
			ctx, r, legacy, _, native := migrationFixture(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(ctx)
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(ctx))
			disconnect()
			if kind == "shutdown" {
				runtime.Shutdown(context.Background())
			}
			var unlock func()
			if kind == "lock shutdown" {
				held, release, released := make(chan struct{}), make(chan struct{}), make(chan error, 1)
				go func() {
					released <- r.UpdateShared(ctx, func(context.Context, *model.Shared) error { close(held); <-release; return nil })
				}()
				<-held
				unlock = func() {
					close(release)
					if err := <-released; err != nil {
						t.Error(err)
					}
				}
				defer unlock()
			}
			done := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				data, err := ReadCacheWithFormat(request, legacyRule(legacy), nil, "yaml")
				if err == nil && string(data) != "payload: [example.com]\n" {
					err = errors.New("lost offline bytes")
				}
				done <- err
			}()
			<-started
			if kind == "lock shutdown" {
				runtime.Shutdown(context.Background())
			}
			err := <-done
			if kind == "disconnect" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(native); err != nil {
					t.Fatal("active source context did not migrate", err)
				}
			} else {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown read: %v", err)
				}
				if _, err := os.Stat(native); !os.IsNotExist(err) {
					t.Fatal("shutdown migrated cache", err)
				}
			}
		})
	}
}
