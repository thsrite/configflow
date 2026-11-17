package oxidns

import (
	"context"
	"errors"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func settingsRepository(t *testing.T) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return repo.WithScope(repo.WithRepository(context.Background(), r)), r
}
func TestC29SaveSettingsDetachesBeforeRepositoryLock(t *testing.T) {
	ctx, r := settingsRepository(t)
	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		_, _, err := r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			close(locked)
			<-release
			p.GetObject("mosdns").Set("custom_hosts", "192.0.2.7 preserved.example")
			return p, nil
		})
		holder <- err
	}()
	<-locked
	request, cancel := context.WithCancel(repo.WithScope(repo.WithRepository(context.Background(), r)))
	cancel()
	result := make(chan error, 1)
	go func() { _, err := SaveSettings(request, "default", M("worker_threads", I(6)), ""); result <- err }()
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("cancelled request aborted owned save: %v", err)
	}
	profile, err := r.GetProfile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if profile.GetObject("oxidns").GetInt("worker_threads") != 6 || profile.GetObject("mosdns").GetString("custom_hosts") != "192.0.2.7 preserved.example" {
		t.Fatal("fresh transaction snapshot lost concurrent fields", profile)
	}
}
func TestC29IfMatchSerializesWholeDraftSaves(t *testing.T) {
	ctx, r := settingsRepository(t)
	before, err := ReadSettings(ctx, "default")
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
			_, err := SaveSettings(repo.WithScope(repo.WithRepository(context.Background(), r)), "default", M("worker_threads", I(threads)), before.ETag)
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
	current, err := ReadSettings(repo.WithScope(ctx), "default")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := SaveSettings(ctx, "default", M("worker_threads", I(3)), " W/"+current.ETag+" ")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Guard.ConfigRevision != after+1 || saved.Guard.ProfileID != "default" || saved.Guard.ServiceType != "oxidns" {
		t.Fatalf("publication guard: %#v", saved.Guard)
	}
}
