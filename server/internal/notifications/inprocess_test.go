package notifications

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func TestLeafTransactionsShareMutexAndLeaveLegacyLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.lock")
	if err := os.WriteFile(path, []byte("python sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 5 {
				if err := Transaction(dir, func(items *jsonx.Object) error {
					entry := items.GetObject("counter")
					count := entry.GetInt("count")
					items.Set("counter", jsonx.Obj("state", "pending", "count", jsonx.Int(count+1)))
					return nil
				}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if err := Transaction(dir, func(items *jsonx.Object) error {
		if items.GetObject("counter").GetInt("count") != 100 {
			t.Error("updates lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || string(data) != "python sentinel" {
		t.Fatal("legacy lock changed")
	}
}

func TestNoNewNotificationLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := Transaction(dir, func(*jsonx.Object) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notifications.lock")); !os.IsNotExist(err) {
		t.Fatal("created resource lock", err)
	}
}

func notificationResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("notification operation blocked")
		var zero T
		return zero
	}
}

func TestObservationCollectionDoesNotHoldLeafAndCannotOverwriteNewerState(t *testing.T) {
	tests := []string{"newer snapshot", "unchanged snapshot", "notice", "read"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			repository, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := repo.WithRepository(context.Background(), repository)
			previous := currentSources()
			t.Cleanup(func() { ResetSources(); SetSources(previous) })
			task := func(message string) []*jsonx.Object {
				return []*jsonx.Object{jsonx.Obj("key", "sample", "name", "sample", "category", "subscription", "enabled", true,
					"last_status", "failed", "last_error", message, "run_count", 1)}
			}
			SetSources(Sources{
				Agents:    func(context.Context) ([]*jsonx.Object, error) { return nil, nil },
				ListTasks: func(context.Context) ([]*jsonx.Object, error) { return task("current"), nil },
			})
			initial, err := Snapshot(ctx, "")
			if err != nil || len(initial) != 1 {
				t.Fatal(initial, err)
			}
			version := initial[0].(*jsonx.Object).Value("version")
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			var calls atomic.Int32
			SetSources(Sources{ListTasks: func(context.Context) ([]*jsonx.Object, error) {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
					return task("stale"), nil
				}
				if name == "unchanged snapshot" {
					return task("current"), nil
				}
				return task("new"), nil
			}})
			old := make(chan error, 1)
			go func() { _, err := Snapshot(ctx, ""); old <- err }()
			notificationResult(t, entered)
			changed := make(chan error, 1)
			go func() {
				var err error
				switch name {
				case "newer snapshot", "unchanged snapshot":
					_, err = Snapshot(ctx, "")
				case "notice":
					err = RecordNotice(ctx, repository, "task:sample", "new notice", "new notice", nil)
				case "read":
					err = MarkRead(repository.DataDir(), jsonx.Obj("task:sample", version))
				}
				changed <- err
			}()
			// This operation finishes while the first source is still blocked. Both
			// repository reads and notification writes must be outside its leaf lock.
			if err := notificationResult(t, changed); err != nil {
				t.Fatal(err)
			}
			once.Do(func() { close(release) })
			if err := notificationResult(t, old); err != nil {
				t.Fatal(err)
			}
			if err := Transaction(repository.DataDir(), func(items *jsonx.Object) error {
				item := items.GetObject("task:sample")
				switch name {
				case "read":
					if item.GetString("state") != "dismissed" {
						t.Error("stale observation undid read", item)
					}
				case "notice":
					if item.GetString("message") != "new notice" {
						t.Error("stale observation overwrote notice", item)
					}
				default:
					want := "new"
					if name == "unchanged snapshot" {
						want = "current"
					}
					if item.GetString("message") != want {
						t.Error("stale observation overwrote snapshot", item)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
