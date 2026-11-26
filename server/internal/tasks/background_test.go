package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

func taskRuntime(t *testing.T) *lifecycle.Runtime {
	t.Helper()
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	r.Start(context.Background())
	t.Cleanup(func() {
		r.Shutdown(context.Background())
		if lockx.Violations() != 0 {
			t.Error("lock order violation")
		}
	})
	return r
}

func taskIdle(t *testing.T, r *lifecycle.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundSurvivesDisconnectWithFreshScope(t *testing.T) {
	r := taskRuntime(t)
	store, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(repo.WithScope(context.Background()))
	defer cancel()
	original := repo.ScopeOf(request)
	entered, gate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	key := t.Name()
	if err := startBackground(request, key, func(ctx context.Context) error {
		if repo.ScopeOf(ctx) == original {
			t.Error("request scope retained")
		}
		close(entered)
		<-gate
		_, err := store.WriteSharedJSON(ctx, "completed.json", map[string]bool{"done": true})
		if err != nil {
			t.Error(err)
		}
		return err
	}, r); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	cancel()
	if !IsBackground(key) {
		t.Fatal("missing held key")
	}
	var busy *Busy
	if err := startBackground(request, key, func(context.Context) error { t.Error("duplicate ran"); return nil }, r); !errors.As(err, &busy) || busy.Message != msgBusy {
		t.Fatal(err)
	}
	release()
	taskIdle(t, r)
	if IsBackground(key) {
		t.Fatal("key leaked")
	}
	raw, err := store.ReadSharedBytes(context.Background(), "completed.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Done bool `json:"done"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil || !saved.Done {
		t.Fatal(string(raw), err)
	}
}

type refusingSpawner struct{}

func (refusingSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}

func TestBackgroundReleasesOnRefusalAndPanic(t *testing.T) {
	key := t.Name()
	if err := startBackground(context.Background(), key, func(context.Context) error { t.Error("refused job ran"); return nil }, refusingSpawner{}); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	if IsBackground(key) {
		t.Fatal("refused job held key")
	}
	r := taskRuntime(t)
	if err := startBackground(context.Background(), key, func(context.Context) error { panic("synthetic") }, r); err != nil {
		t.Fatal(err)
	}
	taskIdle(t, r)
	if IsBackground(key) {
		t.Fatal("panic held key")
	}
	if err := startBackground(context.Background(), key, func(context.Context) error { return errors.New("owner-recorded failure") }, r); err != nil {
		t.Fatal(err)
	}
	taskIdle(t, r)
	if IsBackground(key) {
		t.Fatal("failure held key")
	}
}

func TestBackgroundIOReceivesRuntimeCancellation(t *testing.T) {
	r := taskRuntime(t)
	entered, exited := make(chan struct{}), make(chan struct{})
	key := t.Name()
	if err := startBackground(context.Background(), key, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	}, r); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	r.Shutdown(context.Background())
	select {
	case <-exited:
	default:
		t.Fatal("shutdown did not join I/O")
	}
	if IsBackground(key) {
		t.Fatal("cancelled job held key")
	}
}
