package taskstatus

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func statusRuntime(t *testing.T) (*lifecycle.Runtime, context.Context) {
	t.Helper()
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	return runtime, runtime.BindRequest(context.Background())
}

func statusBytes(t *testing.T, s *Store) []byte {
	t.Helper()
	data, err := os.ReadFile(s.path())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestO36cStatusLockWaitRuntimeBoundary(t *testing.T) {
	for _, record := range []bool{true, false} {
		for _, shutdown := range []bool{true, false} {
			name := "forget"
			if record {
				name = "record"
			}
			if shutdown {
				name += "/shutdown"
			} else {
				name += "/disconnect"
			}
			t.Run(name, func(t *testing.T) {
				s := New(t.TempDir())
				if _, err := s.Record("existing", Result{OK: true}); err != nil {
					t.Fatal(err)
				}
				before := statusBytes(t, s)
				callbacks := 0
				s.OnRecord = func(string) { callbacks++ }
				runtime, request := statusRuntime(t)
				ctx, disconnect := context.WithCancel(request)
				defer disconnect()
				guard.Lock()
				held := true
				defer func() {
					if held {
						guard.Unlock()
					}
				}()
				done := make(chan error, 1)
				go func() {
					if record {
						_, err := s.RecordContext(ctx, "existing", Result{OK: true})
						done <- err
					} else {
						done <- s.ForgetContext(ctx, "existing")
					}
				}()
				select {
				case err := <-done:
					t.Fatalf("writer escaped held leaf lock: %v", err)
				case <-time.After(50 * time.Millisecond):
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				guard.Unlock()
				held = false
				var err error
				select {
				case err = <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("status writer did not finish")
				}
				after := statusBytes(t, s)
				if shutdown {
					if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) || callbacks != 0 {
						t.Fatalf("status committed after Runtime shutdown: err=%v callbacks=%d", err, callbacks)
					}
				} else if err != nil || bytes.Equal(before, after) || (record && callbacks != 1) {
					t.Fatalf("disconnected status writer did not finish: %v", err)
				}
			})
		}
	}
}

func TestO36cStatusShutdownDuringRecord(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Record("existing", Result{OK: true}); err != nil {
		t.Fatal(err)
	}
	before := statusBytes(t, s)
	runtime, ctx := statusRuntime(t)
	s.Now = func() time.Time {
		runtime.Shutdown(context.Background())
		return time.Now()
	}
	_, err := s.RecordContext(ctx, "existing", Result{OK: true})
	if !errors.Is(err, context.Canceled) || !bytes.Equal(before, statusBytes(t, s)) {
		t.Fatalf("mid-record shutdown committed: %v", err)
	}
	files, err := os.ReadDir(s.DataDir)
	if err != nil || len(files) != 1 || files[0].Name() != Filename {
		t.Fatal("status temporary file remained", files, err)
	}
}

func TestRecordContextPreservesNativeStorageErrors(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		s := New(t.TempDir())
		if err := os.Mkdir(s.path(), 0700); err != nil {
			t.Fatal(err)
		}
		_, ctx := statusRuntime(t)
		_, err := s.RecordContext(ctx, "native", Result{OK: true})
		var link *os.LinkError
		if !errors.As(err, &link) || link.Op != "rename" || link.New != s.path() {
			t.Fatalf("rename cause was replaced: %T %v", err, err)
		}
		files, readErr := os.ReadDir(s.DataDir)
		if readErr != nil || len(files) != 1 || files[0].Name() != Filename || !files[0].IsDir() {
			t.Fatal("failed replacement left temporary files", files, readErr)
		}
	})
	t.Run("parent-path", func(t *testing.T) {
		root := t.TempDir() + "/blocked"
		if err := os.WriteFile(root, []byte("directory is a file"), 0600); err != nil {
			t.Fatal(err)
		}
		_, ctx := statusRuntime(t)
		_, err := New(root).RecordContext(ctx, "native", Result{OK: true})
		var path *os.PathError
		if !errors.As(err, &path) || errors.Is(err, context.Canceled) {
			t.Fatalf("path cause was replaced: %T %v", err, err)
		}
	})
}
