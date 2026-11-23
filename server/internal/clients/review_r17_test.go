package clients

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/oplock"
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/repo"
	"configflow/internal/routers"
)

// TestReviewR17RepositoryHookSharesJobLock is the O3-6b integration guard: the
// repository commit guard's JobLock (registered by routers.Wire) and Apply's L1
// must be one table. If a second router-job table appears after batch-6 lands,
// Apply would no longer see the hook's holder and this test fails.
func TestReviewR17RepositoryHookSharesJobLock(t *testing.T) {
	s, f, intent := fixedService(t)
	routers.Wire()
	preview, err := s.Preview(t.Context(), "default", intent)
	if err != nil {
		t.Fatal(err)
	}
	hooks := repo.SetHooks(repo.Hooks{})
	repo.SetHooks(hooks)
	if hooks.JobLockRecord == nil {
		t.Fatal("routers.Wire registered no repository JobLockRecord")
	}
	profile, err := s.Repo.Profile(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	router := profile.Routers.Items[0]
	release, ok := hooks.JobLockRecord(t.Context(), "default", "router-one", router)
	if !ok {
		t.Fatal("repository hook could not take an idle router job lock")
	}
	calls := len(f.Calls())
	err = s.Apply(t.Context(), "default", preview.Token)
	var busy *device.RouterError
	if !errors.As(err, &busy) || busy.Kind != device.KindConflict || busy.Message != messageFixedBusy {
		release()
		t.Fatalf("Apply ignored the repository hook's job lock: %v", err)
	}
	if len(f.Calls()) != calls {
		release()
		t.Fatal("busy Apply contacted the router")
	}
	release()
	if err = s.Apply(t.Context(), "default", preview.Token); err != nil {
		t.Fatalf("Apply after release: %v", err)
	}
}

// TestReviewR17ShutdownWhileWaitingForDeviceLock holds the R06 L2 of the
// router, starts Apply under a real Runtime and shuts down while Apply waits.
// The wait must end with cancellation and no device or metadata write.
func TestReviewR17ShutdownWhileWaitingForDeviceLock(t *testing.T) {
	s, f, intent := fixedService(t)
	preview, err := s.Preview(t.Context(), "default", intent)
	if err != nil {
		t.Fatal(err)
	}
	config, err := fixedConfig(t.Context(), s.Repo, "default")
	if err != nil {
		t.Fatal(err)
	}
	r, err := fixedRouter(config, "router-one")
	if err != nil {
		t.Fatal(err)
	}
	_, unlock, err := oplock.RouterOperationLock(t.Context(), *r, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	rt := testRuntime(t)
	before, _ := s.Repo.ConfigRevision(t.Context(), "default")
	calls := len(f.Calls())
	result := make(chan error, 1)
	if err = rt.Spawn(rt.BindRequest(t.Context()), "fixed-IP-review", func(ctx context.Context) { result <- s.Apply(ctx, "default", preview.Token) }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case err = <-result:
		t.Fatalf("Apply did not wait for the held device lock: %v", err)
	default:
	}
	shutdown, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	rt.Shutdown(shutdown)
	select {
	case err = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("Apply kept waiting for L2 after Runtime shutdown")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown wait returned %v", err)
	}
	after, _ := s.Repo.ConfigRevision(t.Context(), "default")
	if after != before || len(f.Calls()) != calls {
		t.Fatalf("shutdown wrote: revision %d/%d calls %d/%d", before, after, calls, len(f.Calls()))
	}
}
