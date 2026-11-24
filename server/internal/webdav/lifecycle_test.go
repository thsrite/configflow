package webdav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestCloseDropsPendingButWaitCancellationDoesNotAbortUpload(t *testing.T) {
	source := &memorySource{}
	source.set(`{}`, true)
	var uploads atomic.Int32
	pending := testWorker(t, time.Hour, func(context.Context, Snapshot) error { uploads.Add(1); return nil })
	pending.Schedule(source)
	if err := pending.Close(context.Background()); err != nil || pending.Schedule(source) || uploads.Load() != 0 {
		t.Fatal("closed worker accepted or uploaded pending work")
	}
	started, release := make(chan struct{}), make(chan struct{})
	worker := testWorker(t, 0, func(ctx context.Context, _ Snapshot) error {
		close(started)
		<-release
		if ctx.Err() != nil {
			return ctx.Err()
		}
		uploads.Add(1)
		return nil
	})
	worker.Schedule(source)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload not started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(worker.Close(ctx), context.Canceled) {
		t.Fatal("close should respect its waiting context")
	}
	close(release)
	waitWorker(t, worker)
	if uploads.Load() != 1 {
		t.Fatal("waiting cancellation aborted committed upload")
	}
}

func TestSnapshotFailureIsNotRetriedAndLaterScheduleWorks(t *testing.T) {
	source := &memorySource{err: errors.New("synthetic snapshot failure")}
	source.set(`{}`, true)
	uploads := 0
	worker := testWorker(t, 0, func(context.Context, Snapshot) error { uploads++; return nil })
	worker.Schedule(source)
	waitWorker(t, worker)
	if uploads != 0 || source.reads != 1 {
		t.Fatal("failed snapshot was uploaded or retried")
	}
	source.mu.Lock()
	source.err = nil
	source.mu.Unlock()
	worker.Schedule(source)
	waitWorker(t, worker)
	if uploads != 1 {
		t.Fatal("later commit did not recover")
	}
}

func TestChangeDetectionIncludesAgentLifecycleAndLargeIntegers(t *testing.T) {
	base := []byte(`{"shared":{"sequence":9007199254740992},"profiles":{},"system":{"system_config":{},"backup":{},"agents":[{"id":"a","host":"example.com"}]}}`)
	tests := []struct {
		name     string
		from, to string
		changed  bool
	}{
		{"large integer", "9007199254740992", "9007199254740993", true},
		{"agent removed", `[{"id":"a","host":"example.com"}]`, `[]`, true},
		{"agent added", `[{"id":"a","host":"example.com"}]`, `[{"id":"a","host":"example.com"},{"id":"b"}]`, true},
		{"agent configuration", `"host":"example.com"`, `"host":"example.com","logging_config":{"enabled":true}`, true},
		{"runtime", `"host":"example.com"`, `"host":"example.com","status":"online","applied_content":{"digest":"new"}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			after := bytes.Replace(base, []byte(test.from), []byte(test.to), 1)
			changed, err := configurationChanged(base, after)
			if err != nil || changed != test.changed {
				t.Fatalf("changed=%t err=%v", changed, err)
			}
		})
	}
}

func TestReadsDisabledAndFailedCommitsDoNotSchedule(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	uploads := 0
	worker := testWorker(t, 0, func(context.Context, Snapshot) error { uploads++; return nil })
	r.OnConfigurationCommitted(commitObserver(worker))
	t.Cleanup(func() { _ = worker.Close(context.Background()) })
	ctx := context.Background()
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		return profile.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mode: rule"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorker(t, worker)
	if uploads != 0 {
		t.Fatal("disabled backup scheduled")
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", json.RawMessage(`{"auto_backup":true}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWorker(t, worker)
	count := uploads
	if _, err := r.ExportAllJSON(ctx, false); err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		_ = profile.SetJSON("mihomo", json.RawMessage(`{"custom_config":"not committed"}`))
		return errors.New("synthetic save failure")
	})
	if err == nil {
		t.Fatal("failed commit succeeded")
	}
	waitWorker(t, worker)
	if uploads != count || count != 1 {
		t.Fatal("read or failed commit scheduled backup")
	}
}

func TestInvalidSnapshotNeverUploads(t *testing.T) {
	fake, server := newDAV(t)
	_, err := NewService(server.Client()).Upload(context.Background(), json.RawMessage(`{"invalid"`), fixtureSettings(server), false)
	if err == nil || fake.uploads != 0 {
		t.Fatal("invalid serialization uploaded")
	}
}
