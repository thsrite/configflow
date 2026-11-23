package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/oplock"
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

// Exercise the actual R12 tracked owner, not a stand-in publisher. The same L1
// must exclude aliases and repo edits while R12 holds L2, then release before
// StartPublish delivers its result.
func TestO36bR12TrackedPublicationUsesSharedJobLock(t *testing.T) {
	_, db, req, before := r12ReviewSetup(t, nil)
	previous := repo.SetHooks(repo.Hooks{})
	defer repo.SetHooks(previous)
	Wire()
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	entered, resume := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	factory := req.NewDriver
	req.NewDriver = func(r *Router) Driver { close(entered); <-resume; return factory(r) }
	held, unlock, err := oplock.RouterJobLock(repo.WithRepository(runtime.BindRequest(t.Context()), db), req.ProfileID, req.RouterID, req.Router)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	result, err := StartPublish(held, req, unlock)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not reach L2")
	}
	for i := 0; i < 24; i++ {
		_, free, err := oplock.RouterJobLock(t.Context(), "alias-profile", "alias-router", req.Router)
		var conflict *RouterError
		if !errors.As(err, &conflict) || conflict.Kind != KindConflict || free != nil {
			t.Fatalf("competing publisher admitted: %v", err)
		}
	}
	edited := *req.Router
	edited.Password = "synthetic-replacement"
	_, _, err = db.SaveProfileFieldsJSON(t.Context(), req.ProfileID, device.JSON(map[string]any{"routers": map[string]any{"items": []*Router{&edited}}}), nil)
	if !errors.Is(err, repo.ErrInUse) {
		t.Fatalf("repository bypassed active publication: %v", err)
	}
	close(resume)
	select {
	case outcome := <-result:
		if outcome.Err != nil || outcome.Result == nil || !outcome.Result.OK {
			t.Fatalf("publication failed: %+v", outcome)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("publisher did not finish")
	}
	_, stored, after := r12Config(t, db)
	if after.Revision != before.Revision+1 || stored.Password != req.Router.Password {
		t.Fatalf("publication/competing edit revision %d -> %d", before.Revision, after.Revision)
	}
	_, free, err := oplock.RouterJobLock(t.Context(), "alias-profile", "alias-router", req.Router)
	if err != nil {
		t.Fatal("result delivered before L1 release", err)
	}
	free()
	_, free, err = oplock.RouterOperationLock(context.Background(), *req.Router, false)
	if err != nil {
		t.Fatal("L2 leaked", err)
	}
	free()
	if lockx.Violations() != 0 {
		t.Fatalf("lock order violations: %d", lockx.Violations())
	}
}
