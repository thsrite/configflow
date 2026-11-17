package repo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func TestViewSharesOneSnapshotPerGeneration(t *testing.T) {
	r := newRepo(t)
	var first, second *model.Document
	noError(t, r.View(bg, func(d *model.Document) error { first = d; return nil }))
	noError(t, r.View(bg, func(d *model.Document) error { second = d; return nil }))
	if first != second {
		t.Fatal("one generation decoded twice")
	}
	noError(t, r.Update(bg, func(_ context.Context, d *model.Document) error {
		profileByKey(d, DefaultProfileID).Name = "next"
		return nil
	}))
	noError(t, r.View(bg, func(d *model.Document) error {
		if d == first || profileByKey(d, DefaultProfileID).Name != "next" {
			t.Fatal("new generation not visible")
		}
		return nil
	}))
	if profileByKey(first, DefaultProfileID).Name == "next" {
		t.Fatal("old snapshot modified by the commit")
	}
}

func TestViewGuardDetectsModification(t *testing.T) {
	r := newRepo(t)
	modified := false
	r.viewGuard = func() { modified = true }
	noError(t, r.View(bg, func(d *model.Document) error {
		profileByKey(d, DefaultProfileID).Name = "changed in place"
		return nil
	}))
	if !modified {
		t.Fatal("guard missed a modification")
	}
}

func TestTypedReadsAreCallerOwned(t *testing.T) {
	r := newRepo(t)
	profile := must(r.Profile(bg, DefaultProfileID))(t)
	profile.Name = "local"
	shared := must(r.Shared(bg))(t)
	shared.Nodes = append(shared.Nodes, model.Node{ID: "local"})
	system := must(r.System(bg))(t)
	system.SystemConfig.ServerDomain = "local.example.com"
	noError(t, r.View(bg, func(d *model.Document) error {
		if profileByKey(d, DefaultProfileID).Name == "local" || len(d.Shared.Nodes) != 0 || d.System.SystemConfig.ServerDomain != "" {
			t.Fatal("caller copy shares state with the snapshot")
		}
		return nil
	}))
	_, err := r.Profile(bg, "missing")
	wantError(t, err, ErrNotFound, "missing")
	_, err = r.Profile(bg, "../x")
	wantError(t, err, ErrValidation, "Invalid profile id")
}

func TestReadsInsideTransactionsSeeTheWorkingCopy(t *testing.T) {
	r := newRepo(t)
	noError(t, r.Update(bg, func(ctx context.Context, d *model.Document) error {
		profileByKey(d, DefaultProfileID).Name = "typed working"
		if got := must(r.Profile(ctx, DefaultProfileID))(t).Name; got != "typed working" {
			t.Fatalf("typed read in typed tx: %q", got)
		}
		if got := must(r.GetProfile(ctx, DefaultProfileID))(t).Value("name"); got != "typed working" {
			t.Fatalf("legacy read in typed tx: %v", got)
		}
		if err := r.Update(ctx, func(context.Context, *model.Document) error { return nil }); err == nil ||
			!errors.Is(err, ErrInUse) {
			t.Fatalf("nested write: %v", err)
		}
		return nil
	}))
	noError(t, r.update(bg, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		document.GetObject("profiles").GetObject(DefaultProfileID).Set("name", "legacy working")
		if got := must(r.Profile(ctx, DefaultProfileID))(t).Name; got != "legacy working" {
			t.Fatalf("typed read in legacy tx: %q", got)
		}
		return nil
	}))
	if got := must(r.Profile(bg, DefaultProfileID))(t).Name; got != "legacy working" {
		t.Fatalf("committed %q", got)
	}
	failed := errors.New("discard")
	if err := r.Update(bg, func(_ context.Context, d *model.Document) error {
		profileByKey(d, DefaultProfileID).Name = "discarded"
		return failed
	}); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	if got := must(r.Profile(bg, DefaultProfileID))(t).Name; got != "legacy working" {
		t.Fatalf("failed update committed %q", got)
	}
	noError(t, r.WithReadSnapshot(bg, func(held context.Context) error {
		if err := r.Update(held, func(context.Context, *model.Document) error { return nil }); !errors.Is(err, ErrReadOnlySnapshot) {
			t.Fatalf("write inside a read snapshot: %v", err)
		}
		return r.View(held, func(d *model.Document) error { return nil })
	}))
}

func TestCancellationReadsStopWritesDetach(t *testing.T) {
	r := newRepo(t)
	cancelled, cancel := context.WithCancel(bg)
	cancel()
	if err := r.View(cancelled, func(*model.Document) error { t.Fatal("ran"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	if _, err := r.Profile(cancelled, DefaultProfileID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled typed read: %v", err)
	}
	// A client that went away does not abort a write (PORTING 4b).
	noError(t, r.Update(cancelled, func(ctx context.Context, d *model.Document) error {
		if ctx.Err() != nil {
			t.Fatal("transaction context still cancelled")
		}
		profileByKey(d, DefaultProfileID).Name = "after disconnect"
		return nil
	}))
	if got := must(r.Profile(bg, DefaultProfileID))(t).Name; got != "after disconnect" {
		t.Fatalf("committed %q", got)
	}
}

// Readers share snapshots while writers commit; run with -race.
func TestConcurrentViewsAndUpdates(t *testing.T) {
	r := newRepo(t)
	var wg sync.WaitGroup
	for reader := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				err := r.View(bg, func(d *model.Document) error {
					for _, profile := range d.Profiles {
						_ = profile.Name + profile.Description
					}
					_, err := model.Marshal(d)
					return err
				})
				if err != nil {
					t.Errorf("reader %d: %v", reader, err)
					return
				}
			}
		}()
	}
	for index := range 20 {
		noError(t, r.Update(bg, func(_ context.Context, d *model.Document) error {
			profileByKey(d, DefaultProfileID).Name = fmt.Sprintf("writer %d", index)
			return nil
		}))
	}
	wg.Wait()
	if got := must(r.Profile(bg, DefaultProfileID))(t).Name; got != "writer 19" {
		t.Fatalf("last write %q", got)
	}
}
