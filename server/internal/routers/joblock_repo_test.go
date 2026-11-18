package routers

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

func jobRepository(t *testing.T) (*repo.Repository, context.Context, *jsonx.Object) {
	t.Helper()
	previous := repo.SetHooks(repo.Hooks{})
	t.Cleanup(func() { repo.SetHooks(previous) })
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), r)
	if _, err := r.CreateProfile(ctx, jsonx.Obj("id", "live", "name", "Synthetic"), ""); err != nil {
		t.Fatal(err)
	}
	connection := jsonx.Obj("id", "router", "name", "Synthetic", "type", "routeros", "endpoint", "http://192.0.2.40/",
		"username", "synthetic-user", "password", "synthetic-password", "verify_tls", true)
	if _, _, err := r.UpdateProfileFields(ctx, "live", jsonx.Obj("routers", jsonx.Obj("items", []any{connection})), nil); err != nil {
		t.Fatal(err)
	}
	return r, ctx, connection
}

func jobUpdate(r *repo.Repository, ctx context.Context, change string, connection *jsonx.Object) error {
	items := []any{}
	if change != "delete" {
		updated := connection.DeepCopy()
		if change == "manifest" {
			updated.Set("ros_policy_state", jsonx.Obj("owner", "configflow:synthetic", "resources", []any{}))
		} else {
			updated.Set("password", "synthetic-replacement")
		}
		items = append(items, updated)
	}
	_, _, err := r.UpdateProfileFields(ctx, "live", jsonx.Obj("routers", jsonx.Obj("items", items)), nil)
	return err
}

func TestO36bWireUnlocksRepository(t *testing.T) {
	r, ctx, connection := jobRepository(t)
	if err := jobUpdate(r, ctx, "password", connection); !errors.Is(err, repo.ErrInUse) {
		t.Fatal("unwired hook must stay fail-closed", err)
	}
	Wire()
	if err := jobUpdate(r, ctx, "password", connection); err != nil {
		t.Fatal("wired idle device was rejected", err)
	}
	current, err := r.GetProfile(ctx, "live")
	if err != nil || current.GetObject("routers").GetList("items")[0].(*jsonx.Object).GetString("password") != "synthetic-replacement" {
		t.Fatal("idle connection repair was not persisted", err)
	}
}

func TestO36bRepositorySharesPublicationKey(t *testing.T) {
	for _, change := range []string{"password", "manifest", "delete"} {
		t.Run(change, func(t *testing.T) {
			r, ctx, connection := jobRepository(t)
			Wire()
			violations := lockx.Violations()
			_, release, err := RouterJobLock(ctx, "other-profile", "alias", jobConnection("http://192.0.2.40"))
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			before, err := r.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if err := jobUpdate(r, ctx, change, connection); !errors.Is(err, repo.ErrInUse) {
				t.Fatal("repository bypassed publisher", err)
			}
			after, err := r.Store().Fingerprint()
			if err != nil || before != after {
				t.Fatal("busy rejection changed DB/revisions", err)
			}
			release()
			if err := jobUpdate(r, ctx, change, connection); err != nil {
				t.Fatal("idle writer stayed blocked", err)
			}
			if lockx.Violations() != violations {
				t.Fatal("repo's L3 to L1 try violated order")
			}
		})
	}
}

func TestO36bCallerHeldSettingsAndPublication(t *testing.T) {
	r, ctx, connection := jobRepository(t)
	Wire()
	violations := lockx.Violations()
	device := jobConnection("http://192.0.2.40")
	held, release, err := RouterJobLock(ctx, "live", "router", device)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	held, unlock, err := RouterOperationLock(held, *device, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	updated := connection.DeepCopy()
	updated.Set("password", "synthetic-updated")
	if err := r.SaveRouterSettings(held, "live", connection, updated); err != nil {
		t.Fatal("settings tried to reacquire its caller's L1", err)
	}
	manifest := jsonx.Obj("owner", "configflow:synthetic", "resources", []any{})
	if err := r.SaveRouterPublication(held, "live", updated, repo.RouterPublication{Manifest: repo.Some(manifest)}); err != nil {
		t.Fatal("publication tried to reacquire its caller's L1", err)
	}
	if err := r.SaveRouterPublication(held, "live", updated, repo.RouterPublication{Manifest: repo.Some(nil)}); err != nil {
		t.Fatal("withdrawal tried to reacquire its caller's L1", err)
	}
	if err := r.SaveRouterSettings(held, "live", updated, nil); err != nil {
		t.Fatal("delete tried to reacquire its caller's L1", err)
	}
	if lockx.Violations() != violations {
		t.Fatal("L1 to L2 to L3 ordering violated")
	}
}
