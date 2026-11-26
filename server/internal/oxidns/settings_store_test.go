package oxidns

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func settingsRepository(t *testing.T) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return repo.WithScope(context.Background()), r
}
func TestC29SaveSettingsDetachesBeforeRepositoryLock(t *testing.T) {
	ctx, r := settingsRepository(t)
	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		_, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
			close(locked)
			<-release
			return p.SetJSON("mosdns", json.RawMessage(`{"custom_hosts":"192.0.2.7 preserved.example"}`))
		})
		holder <- err
	}()
	<-locked
	runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
	runtime.Start(ctx)
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, cancel := context.WithCancel(runtime.BindRequest(repo.WithScope(context.Background())))
	cancel()
	result := make(chan error, 1)
	go func() { _, err := SaveSettings(request, r, "default", M("worker_threads", I(6)), ""); result <- err }()
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("cancelled request aborted owned save: %v", err)
	}
	profile, err := r.Profile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := testjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	workers, _ := testjson.Path(parsed, "oxidns", "worker_threads")
	hosts, _ := testjson.Path(parsed, "mosdns", "custom_hosts")
	if !testjson.Equal(workers, 6) || hosts != "192.0.2.7 preserved.example" {
		t.Fatal("fresh transaction snapshot lost concurrent fields", string(raw))
	}
}
func TestC29IfMatchSerializesWholeDraftSaves(t *testing.T) {
	ctx, r := settingsRepository(t)
	before, err := ReadSettings(ctx, r, "default")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := r.ConfigRevision(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, threads := range []int64{4, 8} {
		wait.Add(1)
		go func(threads int64) {
			defer wait.Done()
			<-start
			_, err := SaveSettings(repo.WithScope(context.Background()), r, "default", M("worker_threads", I(threads)), before.ETag)
			results <- err
		}(threads)
	}
	close(start)
	wait.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrSettingsChanged):
			stale++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("success=%d stale=%d", success, stale)
	}
	after, err := r.ConfigRevision(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if after != revision+1 {
		t.Fatalf("stale save committed: before=%d after=%d", revision, after)
	}
	current, err := ReadSettings(repo.WithScope(ctx), r, "default")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := SaveSettings(ctx, r, "default", M("worker_threads", I(3)), " W/"+current.ETag+" ")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Guard.ConfigRevision != after+1 || saved.Guard.ProfileID != "default" || saved.Guard.ServiceType != "oxidns" {
		t.Fatalf("publication guard: %#v", saved.Guard)
	}
}

func TestStoredSettingsProjectionKeepsStoredValidator(t *testing.T) {
	ctx, r := settingsRepository(t)
	value := M("custom_matches", L(M("enabled", I(1), "matches", L("qname example.com"), "exec", "accept")))
	_, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.OxiDNS.SetNode(value)
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadSettings(repo.WithScope(ctx), r, "default")
	if err != nil {
		t.Fatal(err)
	}
	if read.ETag != settingsETag(value) {
		t.Fatal("reader used projected settings as stored validator")
	}
	projected, err := read.Settings.Node()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SaveSettings(repo.WithScope(ctx), r, "default", projected, read.ETag); err != nil {
		t.Fatalf("editor cannot repair compatible stored settings: %v", err)
	}
	if _, err = SaveSettings(repo.WithScope(ctx), r, "default", projected, read.ETag); !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("old stored validator remained valid after repair: %v", err)
	}
}
