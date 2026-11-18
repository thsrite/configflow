package repo_test

import (
	"configflow/internal/repo"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewN89MigrationLeafLockAndGeneration(t *testing.T) {
	for _, boundary := range []string{"disconnect", "shutdown", "new-target"} {
		t.Run(boundary, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			path, err := r.SharedPath("rules/migrate.list")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			_, release, err := repo.AcquireFileForTest(base, path)
			if err != nil {
				t.Fatal(err)
			}
			held := true
			defer func() {
				if held {
					release()
				}
			}()
			request, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			stamp := time.Unix(1700000000, 123456789)
			content := []byte(strings.Repeat("old-valid\n", 8192))
			type result struct {
				installed bool
				err       error
			}
			done := make(chan result, 1)
			go func() {
				installed, err := r.WriteSharedTextIfMissing(request, "rules/migrate.list", content, stamp)
				done <- result{installed, err}
			}()
			select {
			case result := <-done:
				t.Fatalf("migration escaped held leaf lock: %+v", result)
			case <-time.After(50 * time.Millisecond):
			}
			disconnect()
			switch boundary {
			case "shutdown":
				runtime.Shutdown(context.Background())
			case "new-target":
				// The writer already holding the lock publishes a newer file.
				if err := os.WriteFile(path, []byte("new-valid\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, stamp.Add(time.Hour), stamp.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			release()
			held = false
			var got result
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("migration waiter stranded")
			}
			data, readErr := os.ReadFile(path)
			switch boundary {
			case "shutdown":
				if got.installed || !errors.Is(got.err, context.Canceled) || !os.IsNotExist(readErr) {
					t.Fatalf("stopped migration committed: %+v %v", got, readErr)
				}
			case "disconnect":
				if !got.installed || got.err != nil || readErr != nil || string(data) != string(content) {
					t.Fatalf("admitted migration incomplete: %+v %v", got, readErr)
				}
			case "new-target":
				if got.installed || got.err != nil || readErr != nil || string(data) != "new-valid\n" {
					t.Fatalf("migration replaced new target: %+v %q %v", got, data, readErr)
				}
			}
			if boundary != "shutdown" {
				want := stamp
				if boundary == "new-target" {
					want = stamp.Add(time.Hour)
				}
				info, err := os.Stat(path)
				if err != nil || !info.ModTime().Equal(want) {
					t.Fatal("migration changed freshness", err)
				}
			}
			files, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.Name() != "migrate.list" {
					t.Fatal("migration left staging/backup file", file.Name())
				}
			}
		})
	}
}
