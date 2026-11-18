package check_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"configflow/internal/health/check"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// Err pauses after capturing the first entry check, modelling preemption before
// lock acquisition. Cancellation still comes from the real Runtime generation.
type reviewEntryContext struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *reviewEntryContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.entered); <-c.release })
	return err
}

func TestReviewN89StoreCommitLifetime(t *testing.T) {
	for _, operation := range []string{"probe-save", "region-update", "probe-delete", "region-delete"} {
		for _, shutdown := range []bool{true, false} {
			name := "disconnect"
			if shutdown {
				name = "shutdown"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				request, disconnect := context.WithCancel(runtime.BindRequest(context.Background()))
				defer disconnect()
				ctx := &reviewEntryContext{Context: lifecycle.Detach(request), entered: make(chan struct{}), release: make(chan struct{})}
				dir := t.TempDir()
				probe, region := check.NewProbeStore(dir), check.NewRegionStore(dir)
				path := probe.Path("synthetic")
				const before = `{"services":{"keep":true},"hosts":{},"domains":{},"websites":{}}`
				if err := os.WriteFile(path, []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
				var results model.Map[check.TimedRecord]
				results.Set("example.test", check.TimedRecord{CheckedAt: "2030-01-02T00:00:00"})
				release := sync.OnceFunc(func() { close(ctx.release) })
				defer release()
				done := make(chan error, 1)
				go func() {
					switch operation {
					case "probe-save":
						done <- probe.SaveEntriesContext(ctx, "synthetic", results, now)
					case "region-update":
						done <- region.UpdateDataContext(ctx, "synthetic", func(data *check.RegionData) error { data.Services = json.RawMessage(`{"new":true}`); return nil }, now)
					case "probe-delete":
						done <- probe.DeleteContext(ctx, "synthetic")
					case "region-delete":
						done <- region.DeleteContext(ctx, "synthetic")
					}
				}()
				select {
				case <-ctx.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("store entry not reached")
				}
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				release()
				select {
				case err := <-done:
					if shutdown && !errors.Is(err, context.Canceled) {
						t.Fatalf("stopped store returned %v", err)
					}
					if !shutdown && err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("store did not finish")
				}
				raw, err := os.ReadFile(path)
				if shutdown {
					if err != nil || string(raw) != before {
						t.Fatalf("stopped store changed file: %s %v", raw, err)
					}
					return
				}
				if operation == "probe-delete" || operation == "region-delete" {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("disconnect prevented delete: %v", err)
					}
					return
				}
				if err != nil || string(raw) == before {
					t.Fatalf("disconnect prevented write: %s %v", raw, err)
				}
			})
		}
	}
}

func TestReviewN89RegionTimestampRetainsSiblings(t *testing.T) {
	store := check.NewRegionStore(t.TempDir())
	const source = `{"services":{"keep":true},"domains":{"bad":{"checked_at":true},"valid":{"checked_at":"2030-01-01T00:00:00","extra":1.0}},"websites":{"site":{"checked_at":"2030-01-01T00:00:00"}},"extra":"kept"}`
	if err := os.WriteFile(store.Path("synthetic"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	data := store.LoadData("synthetic")
	valid, ok := data.Domains.Get("valid")
	if !ok || valid.CheckedAt != "2030-01-01T00:00:00" {
		t.Fatal("valid domain lost")
	}
	if _, ok := data.Websites.Get("site"); !ok {
		t.Fatal("website sibling lost")
	}
	if string(data.Services) != `{"keep":true}` {
		t.Fatal("services sibling lost")
	}
}

func TestReviewN89RegionUpdateChecksLifetimeAfterMutator(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(context.Background()))
			defer disconnect()
			store := check.NewRegionStore(t.TempDir())
			const before = `{"services":{"keep":true},"domains":{},"websites":{}}`
			if err := os.WriteFile(store.Path("synthetic"), []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			err := store.UpdateDataContext(lifecycle.Detach(request), "synthetic", func(data *check.RegionData) error {
				data.Services = json.RawMessage(`{"new":true}`)
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				return nil
			}, time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC))
			if shutdown && !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown error: %v", err)
			}
			if !shutdown && err != nil {
				t.Fatal(err)
			}
			raw, readErr := os.ReadFile(store.Path("synthetic"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if shutdown && string(raw) != before {
				t.Fatalf("late region publication: %s", raw)
			}
			if !shutdown && string(raw) == before {
				t.Fatal("disconnect prevented publication")
			}
		})
	}
}

// reviewCommitContext simulates preemption after the health owner's final check
// has captured nil, before it calls jsonstore. Only the leaf sees cancellation.
type reviewCommitContext struct {
	context.Context
	checks int
	at     int
	pause  func()
}

func (c *reviewCommitContext) Err() error {
	err := c.Context.Err()
	c.checks++
	if c.checks == c.at {
		c.pause()
	}
	return err
}

func TestReviewN89LeafCommitLifetime(t *testing.T) {
	for _, operation := range []string{"probe-save", "region-update", "probe-delete", "region-delete"} {
		for _, shutdown := range []bool{true, false} {
			name := "disconnect"
			if shutdown {
				name = "shutdown"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				r.Start(context.Background())
				t.Cleanup(func() { r.Shutdown(context.Background()) })
				request, disconnect := context.WithCancel(r.BindRequest(context.Background()))
				t.Cleanup(disconnect)
				probe, region := check.NewProbeStore(t.TempDir()), check.NewRegionStore(t.TempDir())
				path := probe.Path("synthetic")
				if operation == "region-update" || operation == "region-delete" {
					path = region.Path("synthetic")
				}
				const before = `{"services":null,"hosts":{},"domains":{},"websites":{}}`
				if err := os.WriteFile(path, []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				at := 3
				if operation == "probe-delete" || operation == "region-delete" {
					at = 2
				}
				paused := false
				ctx := &reviewCommitContext{Context: lifecycle.Detach(request), at: at, pause: func() {
					paused = true
					if shutdown {
						r.Shutdown(context.Background())
					} else {
						disconnect()
					}
				}}
				now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
				var err error
				switch operation {
				case "probe-save":
					var entries model.Map[check.TimedRecord]
					entries.Set("example.com", check.TimedRecord{CheckedAt: "2030-01-02T00:00:00"})
					err = probe.SaveEntriesContext(ctx, "synthetic", entries, now)
				case "region-update":
					err = region.UpdateDataContext(ctx, "synthetic", func(data *check.RegionData) error { data.Services = json.RawMessage(`{"new":true}`); return nil }, now)
				case "probe-delete":
					err = probe.DeleteContext(ctx, "synthetic")
				case "region-delete":
					err = region.DeleteContext(ctx, "synthetic")
				}
				if !paused {
					t.Fatal("final health check was not reached")
				}
				if shutdown && !errors.Is(err, context.Canceled) {
					t.Errorf("shutdown error: %v", err)
				}
				if !shutdown && err != nil {
					t.Fatal(err)
				}
				raw, readErr := os.ReadFile(path)
				if shutdown {
					if readErr != nil || string(raw) != before {
						t.Errorf("late publication: %s %v", raw, readErr)
					}
				} else if operation == "probe-delete" || operation == "region-delete" {
					if !errors.Is(readErr, os.ErrNotExist) {
						t.Errorf("disconnect prevented delete: %v", readErr)
					}
				} else if readErr != nil || string(raw) == before {
					t.Errorf("disconnect prevented write: %s %v", raw, readErr)
				}
				if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("temp remains: %v", err)
				}
			})
		}
	}
}
