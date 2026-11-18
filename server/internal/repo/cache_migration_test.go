package repo_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestSharedCacheMigrationPreservesTimestampAndExistingFile(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[stop], func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			ctx, cancel := context.WithCancel(runtime.BindRequest(base))
			cancel()
			if stop {
				runtime.Shutdown(context.Background())
			}
			stamp := time.Unix(1700000000, 123456789)
			installed, err := r.WriteSharedTextIfMissing(ctx, "rules/migrate.list", []byte("old-valid"), stamp)
			path, _ := r.SharedPath("rules/migrate.list")
			if stop {
				if installed || !errors.Is(err, context.Canceled) {
					t.Fatal(installed, err)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("shutdown installed migration", err)
				}
				return
			}
			if !installed || err != nil {
				t.Fatal(installed, err)
			}
			info, err := os.Stat(path)
			if err != nil || !info.ModTime().Equal(stamp) {
				t.Fatal("migration refreshed timestamp", err)
			}
			installed, err = r.WriteSharedTextIfMissing(ctx, "rules/migrate.list", []byte("overwrite"), stamp.Add(time.Hour))
			if installed || err != nil {
				t.Fatal(installed, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "old-valid" {
				t.Fatal("migration replaced existing cache", err)
			}
			info, _ = os.Stat(path)
			if !info.ModTime().Equal(stamp) {
				t.Fatal("skipped migration touched timestamp")
			}
		})
	}
}
