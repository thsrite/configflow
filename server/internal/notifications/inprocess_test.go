package notifications

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

func TestLeafTransactionsShareMutexAndLeaveLegacyLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.json")
	_ = path
	lockPath := filepath.Join(dir, "notifications.lock")
	if err := os.WriteFile(lockPath, []byte("python sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(lockPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 5 {
				if err := UpdateItems(context.Background(), dir, func(items *Items) error {
					var count int
					if entry, ok := items.Get("counter"); ok && entry != nil {
						if raw, ok := entry.Extra.Get("count"); ok {
							_ = json.Unmarshal(raw, &count)
						}
					}
					next := &Item{State: "pending"}
					rawCount, _ := json.Marshal(count + 1)
					next.Extra.Set("count", rawCount)
					items.Set("counter", next)
					return nil
				}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if err := UpdateItems(context.Background(), dir, func(items *Items) error {
		entry, ok := items.Get("counter")
		var count int
		if ok && entry != nil {
			if raw, exists := entry.Extra.Get("count"); exists {
				_ = json.Unmarshal(raw, &count)
			}
		}
		if count != 100 {
			t.Errorf("updates lost: count=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || string(data) != "python sentinel" {
		t.Fatal("legacy lock changed")
	}
}

func TestNoNewNotificationLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := UpdateItems(context.Background(), dir, func(*Items) error { return nil }); err != nil {
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
			previous := currentInputs()
			t.Cleanup(func() { ResetInputs(); SetInputs(previous) })
			task := func(message string) []*tasks.Task {
				return []*tasks.Task{{
					Key:      "sample",
					Name:     "sample",
					Category: "subscription",
					Enabled:  true,
					HistoryData: &tasks.HistoryData{
						LastStatus: new("failed"),
						LastError:  new(message),
						RunCount:   "1",
					},
				}}
			}
			SetInputs(Inputs{
				Agents: func(context.Context) ([]model.Agent, error) { return nil, nil },
				Rows:   func(context.Context) ([]*tasks.Task, error) { return task("current"), nil },
			})
			initial, err := SnapshotItems(ctx, "")
			if err != nil || len(initial) != 1 {
				t.Fatal(initial, err)
			}
			version := stringValue(initial[0].Version)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			var calls atomic.Int32
			SetInputs(Inputs{Rows: func(context.Context) ([]*tasks.Task, error) {
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
			go func() { _, err := SnapshotItems(ctx, ""); old <- err }()
			notificationResult(t, entered)
			changed := make(chan error, 1)
			go func() {
				var err error
				switch name {
				case "newer snapshot", "unchanged snapshot":
					_, err = SnapshotItems(ctx, "")
				case "notice":
					err = SaveNotice(ctx, repository, "task:sample", "new notice", "new notice", nil)
				case "read":
					err = Dismiss(ctx, repository.DataDir(), map[string]string{"task:sample": version})
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
			if err := UpdateItems(ctx, repository.DataDir(), func(items *Items) error {
				item, _ := items.Get("task:sample")
				if item == nil {
					t.Fatal("missing task:sample")
				}
				switch name {
				case "read":
					if item.State != "dismissed" {
						t.Error("stale observation undid read", item)
					}
				case "notice":
					if stringValue(item.Message) != "new notice" {
						t.Error("stale observation overwrote notice", item)
					}
				default:
					want := "new"
					if name == "unchanged snapshot" {
						want = "current"
					}
					if stringValue(item.Message) != want {
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
