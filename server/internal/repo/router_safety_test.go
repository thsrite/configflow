package repo

// Port of the repository cases of backend/test_router_repository_safety.py:
// all configuration writers preserve live RouterOS ownership and publication
// locks. routers.rule_sync_store.job_lock is replaced by an in-process fake
// registered through Hooks.JobLock (same key: device type + endpoint).
//
// Not ported here (they test other packages): the rule_sync _save_manifest
// case (routers rule sync) and the four HTTP cases (routes/routers).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

const (
	rsConnection = `{"id": "ros", "name": "ROS", "type": "routeros", "endpoint": "http://192.168.0.1",
		"username": "admin", "password": "secret", "verify_tls": true}`
	rsManifest = `{"owner": "configflow:test", "resources": [
		{"path": "/ip/firewall/mangle", "id": "*1", "fields": {"comment": "configflow:test"}}]}`
)

type rsJobLocks struct {
	mu   sync.Mutex
	held map[string]bool
}

func (l *rsJobLocks) acquire(_ context.Context, _, _ string, router *jsonx.Object) (func(), bool) {
	endpoint := ""
	if value := router.Value("endpoint"); jsonx.Truthy(value) {
		endpoint = jsonx.Str(value)
	}
	key := jsonx.Repr(router.Value("type")) + "|" + strings.TrimRight(endpoint, "/")
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		return nil, false
	}
	l.held[key] = true
	return func() {
		l.mu.Lock()
		delete(l.held, key)
		l.mu.Unlock()
	}, true
}

// hold is `with job_lock(...) as acquired: assert acquired`.
func (l *rsJobLocks) hold(t *testing.T, router *jsonx.Object) func() {
	t.Helper()
	release, acquired := l.acquire(bg, "", "", router)
	if !acquired {
		t.Fatal("job lock is held")
	}
	return release
}

func rsRepository(t *testing.T) (*Repository, *rsJobLocks) {
	t.Helper()
	locks := &rsJobLocks{held: map[string]bool{}}
	previous := SetHooks(Hooks{JobLock: locks.acquire})
	t.Cleanup(func() { SetHooks(previous) })
	r := newRepo(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "live", "name", "Live"), ""))(t)
	_, _, err := r.SaveProfile(bg, "live", jsonx.Obj("routers", jsonx.Obj("items", []any{parse(t, rsConnection)})), nil)
	noError(t, err)
	return r, locks
}

func rsRouter(t *testing.T, r *Repository) *jsonx.Object {
	t.Helper()
	return must(r.GetProfile(bg, "live"))(t).GetObject("routers").GetList("items")[0].(*jsonx.Object)
}

func rsPersist(t *testing.T, r *Repository, locks *rsJobLocks) *jsonx.Object {
	t.Helper()
	router := rsRouter(t, r)
	release := locks.hold(t, router)
	noError(t, r.SaveRouterPublication(bg, "live", router, RouterPublication{Manifest: Some(parse(t, rsManifest))}))
	release()
	return rsRouter(t, r)
}

var rsWriters = []string{"profile-import", "whole-import", "fields", "save", "transaction"}

func rsReplaceRouters(t *testing.T, r *Repository, writer string, routers *jsonx.Object) error {
	t.Helper()
	switch writer {
	case "profile-import":
		_, err := r.ImportProfile(bg, "live", jsonx.Obj("routers", routers))
		return err
	case "whole-import":
		document := must(r.ExportAll(bg, false))(t)
		document.GetObject("profiles").GetObject("live").Set("routers", routers)
		_, err := r.ImportAll(bg, document)
		return err
	case "fields":
		_, _, err := r.UpdateProfileFields(bg, "live", jsonx.Obj("routers", routers), nil)
		return err
	case "save":
		_, _, err := r.SaveProfile(bg, "live", jsonx.Obj("routers", routers), nil)
		return err
	}
	_, _, err := r.UpdateProfileTransaction(bg, "live", func(_ context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		config.Set("routers", routers)
		return nil, nil
	})
	return err
}

func rsChanged(field string) any {
	if field == "verify_tls" {
		return false
	}
	return "changed"
}

func TestEveryWriterRejectsLiveOwnershipLoss(t *testing.T) {
	for _, writer := range rsWriters {
		for _, change := range []string{"remove-router", "remove-manifest", "replace-manifest", "id", "type", "endpoint"} {
			t.Run(writer+"/"+change, func(t *testing.T) {
				r, locks := rsRepository(t)
				rsPersist(t, r, locks)
				before := must(r.ExportAll(bg, false))(t)
				raw := must(r.Store().Fingerprint())(t)
				routers := before.GetObject("profiles").GetObject("live").GetObject("routers").DeepCopy()
				router := routers.GetList("items")[0].(*jsonx.Object)
				switch change {
				case "remove-router":
					routers.Set("items", []any{})
				case "remove-manifest":
					router.Delete("ros_policy_state")
				case "replace-manifest":
					router.GetObject("ros_policy_state").Set("resources", []any{})
				default:
					router.Set(change, "changed")
				}
				wantError(t, rsReplaceRouters(t, r, writer, routers), ErrInUse, "")
				if got := must(r.Store().Fingerprint())(t); got != raw {
					t.Fatal("database changed")
				}
				wantEqual(t, must(r.ExportAll(bg, false))(t), before)
			})
		}
	}
}

func TestCredentialRepairPreservesOwnershipInEveryWriter(t *testing.T) {
	for _, writer := range rsWriters {
		for _, field := range []string{"username", "password", "verify_tls"} {
			t.Run(writer+"/"+field, func(t *testing.T) {
				r, locks := rsRepository(t)
				rsPersist(t, r, locks)
				routers := must(r.ExportAll(bg, false))(t).GetObject("profiles").GetObject("live").GetObject("routers").DeepCopy()
				var value any = "replacement"
				if field == "verify_tls" {
					value = false
				}
				routers.GetList("items")[0].(*jsonx.Object).Set(field, value)
				noError(t, rsReplaceRouters(t, r, writer, routers))
				saved := rsRouter(t, r)
				wantEqual(t, saved, routers.GetList("items")[0])
				wantEqual(t, saved.Value("ros_policy_state"), parse(t, rsManifest))
			})
		}
	}
}

func TestLivePublicationStillLocksCredentialRepairs(t *testing.T) {
	for _, field := range []string{"username", "password", "verify_tls"} {
		t.Run(field, func(t *testing.T) {
			r, locks := rsRepository(t)
			router := rsPersist(t, r, locks)
			before := must(r.ExportAll(bg, false))(t)
			replacement := router.DeepCopy()
			replacement.Set(field, rsChanged(field))
			release := locks.hold(t, router)
			wantError(t, rsReplaceRouters(t, r, "profile-import", jsonx.Obj("items", []any{replacement})), ErrInUse, "")
			release()
			wantEqual(t, must(r.ExportAll(bg, false))(t), before)
		})
	}
}

func rsArtifact(t *testing.T, r *Repository, name, content string) string {
	t.Helper()
	artifact := must(r.ProfilePath("live", "generated/"+name))(t)
	noError(t, os.MkdirAll(filepath.Dir(artifact), 0o755))
	noError(t, os.WriteFile(artifact, []byte(content), 0o644))
	return artifact
}

func TestDeleteAndResetKeepLiveManifestAndProfileArtifacts(t *testing.T) {
	for _, operation := range []string{"delete-profile", "reset"} {
		t.Run(operation, func(t *testing.T) {
			r, locks := rsRepository(t)
			rsPersist(t, r, locks)
			artifact := rsArtifact(t, r, "owned.txt", "retain me")
			before := must(r.ExportAll(bg, false))(t)
			var err error
			if operation == "delete-profile" {
				err = r.DeleteProfile(bg, "live", "")
			} else {
				_, err = r.ResetAll(bg)
			}
			wantError(t, err, ErrInUse, "")
			wantEqual(t, must(r.ExportAll(bg, false))(t), before)
			if text := must(readFileString(artifact))(t); text != "retain me" {
				t.Fatalf("artifact %q", text)
			}
		})
	}
}

func TestDuplicateIdsCannotShadowTheActiveManifest(t *testing.T) {
	r, locks := rsRepository(t)
	rsPersist(t, r, locks)
	before := must(r.ExportAll(bg, false))(t)
	routers := before.GetObject("profiles").GetObject("live").GetObject("routers").DeepCopy()
	routers.Set("items", append([]any{parse(t, rsConnection)}, routers.GetList("items")...))
	_, err := r.ImportProfile(bg, "live", jsonx.Obj("routers", routers))
	wantError(t, err, ErrValidation, "")
	wantEqual(t, must(r.ExportAll(bg, false))(t), before)
}

func TestVerifiedPublicationAndWithdrawalPreserveConcurrentMetadata(t *testing.T) {
	r, locks := rsRepository(t)
	original := rsRouter(t, r)
	release := locks.hold(t, original)
	_, _, err := r.UpdateProfileTransaction(bg, "live", func(_ context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		config.GetObject("routers").GetList("items")[0].(*jsonx.Object).Set("name", "Concurrent edit")
		return nil, nil
	})
	noError(t, err)
	noError(t, r.SaveRouterPublication(bg, "live", original, RouterPublication{
		Manifest: Some(parse(t, rsManifest)), LastRelease: Some(jsonx.Obj("ok", true))}))
	// Restored RouterOS rows may receive new IDs; the rollback readback is
	// authoritative even when the caller still has the original snapshot.
	restored := parse(t, rsManifest)
	restored.GetList("resources")[0].(*jsonx.Object).Set("id", "*2")
	noError(t, r.SaveRouterPublication(bg, "live", original, RouterPublication{Manifest: Some(restored)}))
	saved := rsRouter(t, r)
	wantEqual(t, saved.Value("ros_policy_state"), restored)
	wantEqual(t, saved.Value("name"), "Concurrent edit")
	wantEqual(t, saved.Value("last_release"), jsonx.Obj("ok", true))
	noError(t, r.SaveRouterPublication(bg, "live", original, RouterPublication{Manifest: Some(nil)}))
	release()
	must(r.ImportProfile(bg, "live", jsonx.Obj("routers", jsonx.Obj("items", []any{}))))(t)
	wantEqual(t, must(r.GetProfile(bg, "live"))(t).GetObject("routers").Value("items"), []any{})
}

func TestPublicationCannotPersistAgainstADifferentConnection(t *testing.T) {
	for _, change := range []string{"endpoint", "username", "password", "verify_tls"} {
		t.Run(change, func(t *testing.T) {
			r, locks := rsRepository(t)
			stale := parse(t, rsConnection)
			if change == "verify_tls" {
				stale.Set(change, false)
			} else {
				stale.Set(change, "other")
			}
			before := must(r.ExportAll(bg, false))(t)
			release := locks.hold(t, parse(t, rsConnection))
			err := r.SaveRouterPublication(bg, "live", stale, RouterPublication{Manifest: Some(parse(t, rsManifest))})
			wantError(t, err, ErrInUse, "")
			release()
			wantEqual(t, must(r.ExportAll(bg, false))(t), before)
		})
	}
}

func TestSettingsCommitDoesNotReacquireHeldJobLock(t *testing.T) {
	r, locks := rsRepository(t)
	connection := parse(t, rsConnection)
	release := locks.hold(t, connection)
	changed := connection.DeepCopy()
	changed.Set("password", "new password")
	noError(t, r.SaveRouterSettings(bg, "live", connection, changed))
	noError(t, r.SaveRouterSettings(bg, "live", changed, nil))
	release()
	wantEqual(t, must(r.GetProfile(bg, "live"))(t).GetObject("routers").Value("items"), []any{})
}

func TestOrdinaryWriterCannotChangeConnectionDuringFirstPublication(t *testing.T) {
	for _, field := range []string{"type", "endpoint", "username", "password", "verify_tls"} {
		t.Run(field, func(t *testing.T) {
			r, locks := rsRepository(t)
			before := must(r.ExportAll(bg, false))(t)
			routers := before.GetObject("profiles").GetObject("live").GetObject("routers").DeepCopy()
			routers.GetList("items")[0].(*jsonx.Object).Set(field, rsChanged(field))
			connection := parse(t, rsConnection)
			release := locks.hold(t, connection)
			_, err := r.ImportProfile(bg, "live", jsonx.Obj("routers", routers))
			wantError(t, err, ErrInUse, "")
			wantEqual(t, must(r.ExportAll(bg, false))(t), before)
			noError(t, r.SaveRouterPublication(bg, "live", connection, RouterPublication{Manifest: Some(parse(t, rsManifest))}))
			release()
			wantEqual(t, rsRouter(t, r).Value("ros_policy_state"), parse(t, rsManifest))
		})
	}
}

func TestBatchRemovalDeduplicatesDeviceConnectionLocks(t *testing.T) {
	r, _ := rsRepository(t)
	second := parse(t, rsConnection)
	second.Set("id", "same-device")
	_, _, err := r.SaveProfile(bg, "live", jsonx.Obj("routers", jsonx.Obj("items", []any{parse(t, rsConnection), second})), nil)
	noError(t, err)
	noError(t, r.DeleteProfile(bg, "live", ""))
	profiles := must(r.ListProfiles(bg))(t)
	if len(profiles) != 1 || profiles[0].(*jsonx.Object).Value("id") != "default" {
		t.Fatalf("profiles %s", jsonx.Repr(profiles))
	}
}

// The Python case holds the publication lock in another process; the lock
// itself belongs to the routers package, so here another holder of the fake
// lock stands in for it.
func TestFirstPublicationBlocksBatchReplacement(t *testing.T) {
	for _, writer := range []string{"profile-import", "whole-import", "fields", "transaction", "reset", "delete-profile"} {
		t.Run(writer, func(t *testing.T) {
			r, locks := rsRepository(t)
			before := must(r.ExportAll(bg, false))(t)
			artifact := rsArtifact(t, r, "first.txt", "still publishing")
			release := locks.hold(t, parse(t, rsConnection))
			var err error
			switch writer {
			case "reset":
				_, err = r.ResetAll(bg)
			case "delete-profile":
				err = r.DeleteProfile(bg, "live", "")
			default:
				err = rsReplaceRouters(t, r, writer, jsonx.Obj("items", []any{}))
			}
			wantError(t, err, ErrInUse, "")
			wantEqual(t, must(r.ExportAll(bg, false))(t), before)
			if text := must(readFileString(artifact))(t); text != "still publishing" {
				t.Fatalf("artifact %q", text)
			}
			release()
			// Rejection must release the document lock so the publisher can
			// save its first manifest, and must not leak the job lock.
			wantEqual(t, rsPersist(t, r, locks).Value("ros_policy_state"), parse(t, rsManifest))
		})
	}
}

func TestReplacementHoldsPublicationLockThroughDatabaseCommit(t *testing.T) {
	r, locks := rsRepository(t)
	connection := parse(t, rsConnection)
	var observations []bool
	r.Store().BeforeCommit = func() error {
		release, acquired := locks.acquire(bg, "other", "other-router", connection)
		observations = append(observations, acquired)
		if acquired {
			release()
		}
		return nil
	}
	must(r.ImportProfile(bg, "live", jsonx.Obj("routers", jsonx.Obj("items", []any{}))))(t)
	r.Store().BeforeCommit = nil
	if len(observations) != 1 || observations[0] {
		t.Fatalf("observations %v", observations)
	}
	locks.hold(t, connection)()
}
