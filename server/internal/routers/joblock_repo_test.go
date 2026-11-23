package routers

import (
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/routerfake"
)

func rawRouterJSON(t *testing.T, router model.Router) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(router)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func jobRepository(t *testing.T) (*repo.Repository, context.Context, model.Router) {
	t.Helper()
	previous := repo.SetHooks(repo.Hooks{})
	t.Cleanup(func() { repo.SetHooks(previous) })
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), r)
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "live", Name: "Synthetic"}, ""); err != nil {
		t.Fatal(err)
	}
	connection := model.Router{
		ID:        "router",
		Name:      "Synthetic",
		Type:      "routeros",
		Endpoint:  "http://192.0.2.40/",
		Username:  "synthetic-user",
		Password:  "synthetic-password",
		VerifyTLS: new(true),
	}
	if _, err := r.UpdateProfile(ctx, "live", func(_ context.Context, p *model.Profile) error {
		p.Routers.Items = []model.Router{connection}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return r, ctx, connection
}

func jobUpdate(r *repo.Repository, ctx context.Context, change string, connection model.Router) error {
	items := []model.Router{}
	if change != "delete" {
		updated := connection
		if change == "manifest" {
			// The synthetic owner marker is not a declared manifest member; as a
			// stored router member the manifest is kept whole (O18 T1c), the
			// same as through SaveRouterPublicationJSON.
			var holder model.Router
			if err := json.Unmarshal([]byte(`{"ros_policy_state":{"owner":"configflow:synthetic","resources":[]}}`), &holder); err != nil {
				return err
			}
			updated.ROSPolicyState = holder.ROSPolicyState
		} else {
			updated.Password = "synthetic-replacement"
		}
		items = append(items, updated)
	}
	_, err := r.UpdateProfile(ctx, "live", func(_ context.Context, p *model.Profile) error {
		p.Routers.Items = items
		return nil
	})
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
	current, err := r.Profile(ctx, "live")
	if err != nil || len(current.Routers.Items) != 1 || current.Routers.Items[0].Password != "synthetic-replacement" {
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
			if lockx.Violations() != violations || violations != 0 {
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
	updated := connection
	updated.Password = "synthetic-updated"
	if err := r.SaveRouterSettingsJSON(held, "live", rawRouterJSON(t, connection), rawRouterJSON(t, updated)); err != nil {
		t.Fatal("settings tried to reacquire its caller's L1", err)
	}
	manifest := json.RawMessage(`{"owner":"configflow:synthetic","resources":[]}`)
	if err := r.SaveRouterPublicationJSON(held, "live", rawRouterJSON(t, updated), repo.RouterPublicationJSON{Manifest: repo.JSONUpdate{Set: true, Value: manifest}}); err != nil {
		t.Fatal("publication tried to reacquire its caller's L1", err)
	}
	if err := r.SaveRouterPublicationJSON(held, "live", rawRouterJSON(t, updated), repo.RouterPublicationJSON{Manifest: repo.JSONUpdate{Set: true, Value: json.RawMessage("null")}}); err != nil {
		t.Fatal("withdrawal tried to reacquire its caller's L1", err)
	}
	if err := r.SaveRouterSettingsJSON(held, "live", rawRouterJSON(t, updated), nil); err != nil {
		t.Fatal("delete tried to reacquire its caller's L1", err)
	}
	if lockx.Violations() != violations || violations != 0 {
		t.Fatal("L1 to L2 to L3 ordering violated")
	}
}

func TestO36bConcurrentPublicationsShareJobLock(t *testing.T) {
	r, base, connection := jobRepository(t)
	Wire()
	_, request, _ := o36bRuntime(t)
	base = repo.WithRepository(request, r)
	fake := routerfake.New(nil)
	server := httptest.NewServer(fake)
	defer server.Close()
	driver := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	defer driver.Close()
	connection.Endpoint = server.URL
	if _, err := r.UpdateProfile(base, "live", func(_ context.Context, p *model.Profile) error {
		p.Routers.Items = []model.Router{connection}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	connRaw := rawRouterJSON(t, connection)
	violations := lockx.Violations()
	entered, releaseCommit := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(releaseCommit) }) }
	defer unblock()
	r.Store().BeforeCommit = func() error { close(entered); <-releaseCommit; return nil }
	defer func() { r.Store().BeforeCommit = nil }()
	type result struct {
		undo json.RawMessage
		err  error
	}
	done := make(chan result, 1)
	go func() {
		done <- func() result {
			held, release, err := RouterJobLock(base, "live", "router", jobConnection(server.URL))
			if err != nil {
				return result{err: err}
			}
			defer release()
			held, unlock, err := RouterOperationLock(held, *jobConnection(server.URL), true)
			if err != nil {
				return result{err: err}
			}
			defer unlock()
			undo, err := driver.Apply(held, json.RawMessage(`{"kind":"route_add","cidr":"198.51.100.0/24","gateway":"192.0.2.1","comment":"cf-o36b"}`))
			if err == nil {
				err = r.SaveRouterPublicationJSON(held, "live", connRaw, repo.RouterPublicationJSON{Manifest: repo.JSONUpdate{Set: true, Value: json.RawMessage(`{"owner":"configflow:synthetic","resources":[]}`)}})
			}
			return result{undo, err}
		}()
	}()
	select {
	case <-entered:
	case early := <-done:
		t.Fatal("publisher failed before DB commit", early.err)
	case <-time.After(10 * time.Second):
		t.Fatal("publisher did not reach DB commit")
	}
	calls := len(fake.Calls())
	const rivals = 24
	start := make(chan struct{})
	errorsOut := make(chan error, rivals)
	for range rivals {
		go func() {
			<-start
			_, release, err := RouterJobLock(base, "alias-profile", "alias-router", jobConnection(server.URL+"/"))
			if release != nil {
				release()
			}
			errorsOut <- err
		}()
	}
	close(start)
	for range rivals {
		select {
		case err := <-errorsOut:
			failure, ok := errors.AsType[*RouterError](err)
			if !ok || failure.Kind != KindConflict {
				t.Fatal("concurrent publisher acquired a live device", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("L1 rival blocked")
		}
	}
	if len(fake.Calls()) != calls || len(fake.Snapshot().Tables[ros.PathRoutes]) != 1 {
		t.Fatal("another publisher wrote the device")
	}
	unblock()
	var published result
	select {
	case published = <-done:
		if published.err != nil {
			t.Fatal(published.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("publisher did not finish")
	}
	r.Store().BeforeCommit = nil
	held, release, err := RouterJobLock(base, "live", "router", jobConnection(server.URL))
	if err != nil {
		t.Fatal("finished publisher retained its job lock", err)
	}
	defer release()
	held, unlock, err := RouterOperationLock(held, *jobConnection(server.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := driver.Apply(held, published.undo); err != nil {
		t.Fatal("next publisher could not restore", err)
	}
	if err := r.SaveRouterPublicationJSON(held, "live", connRaw, repo.RouterPublicationJSON{Manifest: repo.JSONUpdate{Set: true, Value: json.RawMessage("null")}}); err != nil {
		t.Fatal("next publisher could not save withdrawal", err)
	}
	if len(fake.Snapshot().Tables[ros.PathRoutes]) != 0 || lockx.Violations() != violations || violations != 0 {
		t.Fatal("withdrawal failed or L1/L2/L3 order violated")
	}
}
