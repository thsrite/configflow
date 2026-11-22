package repo

// Port of the repository cases of backend/test_router_repository_safety.py:
// all configuration writers preserve live RouterOS ownership and publication
// locks. routers.rule_sync_store.job_lock is replaced by an in-process fake
// registered through RecordHooks.JobLockRecord (same key: device type + endpoint).
//
// Not ported here (they test other packages): the rule_sync _save_manifest
// case (routers rule sync) and the four HTTP cases (routes/routers).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
)

const (
	rsConnection = `{"id": "ros", "name": "ROS", "type": "routeros", "endpoint": "http://192.168.0.1",
		"username": "admin", "password": "secret", "verify_tls": true}`
	rsManifest = `{"spec": {"gateway": "192.0.2.1", "policy_version": 1}, "resources": [
		{"path": "/ip/firewall/mangle", "id": "*1", "config": {"comment": "configflow:test"}}]}`
)

type rsJobLocks struct {
	mu   sync.Mutex
	held map[string]bool
}

func (l *rsJobLocks) acquire(_ context.Context, _, _ string, router model.Router) (func(), bool) {
	key := router.Type + "|" + strings.TrimRight(router.Endpoint, "/")
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
func (l *rsJobLocks) hold(t *testing.T, router model.Router) func() {
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
	previous := SetHooks(Hooks{RecordHooks: RecordHooks{JobLockRecord: locks.acquire}})
	t.Cleanup(func() { SetHooks(previous) })
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "live", Name: "Live"}, ""))(t)
	_, _, err := r.SaveProfileFieldsJSON(bg, "live", json.RawMessage(`{"routers":{"items":[`+rsConnection+`]}}`), nil)
	noError(t, err)
	return r, locks
}

func rsRouter(t *testing.T, r *Repository) model.Router {
	t.Helper()
	return must(r.Profile(bg, "live"))(t).Routers.Items[0]
}

func rsRouterRaw(t *testing.T, r *Repository) json.RawMessage {
	t.Helper()
	return must(model.Marshal(rsRouter(t, r)))(t)
}

func rsRouterObj(t *testing.T, r *Repository) *testObj {
	t.Helper()
	return parseJSONObj(t, rsRouterRaw(t, r))
}

func rsPersist(t *testing.T, r *Repository, locks *rsJobLocks) model.Router {
	t.Helper()
	router := rsRouter(t, r)
	release := locks.hold(t, router)
	noError(t, r.SaveRouterPublicationJSON(bg, "live", must(model.Marshal(router))(t), RouterPublicationJSON{
		Manifest: JSONUpdate{Set: true, Value: json.RawMessage(rsManifest)},
	}))
	release()
	return rsRouter(t, r)
}

var rsWriters = []string{"profile-import", "whole-import", "fields", "update", "transaction"}

func rsReplaceRouters(t *testing.T, r *Repository, writer string, routers json.RawMessage) error {
	t.Helper()
	switch writer {
	case "profile-import":
		_, err := r.ImportProfileJSON(bg, "live", json.RawMessage(`{"routers":`+string(routers)+`}`))
		return err
	case "whole-import":
		document := parseJSONObj(t, must(r.ExportAllJSON(bg, false))(t))
		document.GetObject("profiles").GetObject("live").Set("routers", parseJSONVal(t, routers))
		_, err := r.ImportAllJSON(bg, mustMarshalJSON(t, document))
		return err
	case "fields":
		_, _, err := r.SaveProfileFieldsJSON(bg, "live", json.RawMessage(`{"routers":`+string(routers)+`}`), nil)
		return err
	case "update":
		return r.Update(bg, func(_ context.Context, document *model.Document) error {
			return profileByKey(document, "live").SetJSON("routers", routers)
		})
	}
	_, err := r.UpdateProfile(bg, "live", func(_ context.Context, profile *model.Profile) error {
		return profile.SetJSON("routers", routers)
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
				before := must(r.ExportAllJSON(bg, false))(t)
				raw := must(r.Store().Fingerprint())(t)
				routers := parseJSONObj(t, before).GetObject("profiles").GetObject("live").GetObject("routers")
				router := routers.GetList("items")[0].(*testObj)
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
				wantError(t, rsReplaceRouters(t, r, writer, mustMarshalJSON(t, routers)), ErrInUse, "")
				if got := must(r.Store().Fingerprint())(t); got != raw {
					t.Fatal("database changed")
				}
				wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
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
				routers := parseJSONObj(t, must(r.ExportAllJSON(bg, false))(t)).GetObject("profiles").GetObject("live").GetObject("routers")
				var value any = "replacement"
				if field == "verify_tls" {
					value = false
				}
				routers.GetList("items")[0].(*testObj).Set(field, value)
				noError(t, rsReplaceRouters(t, r, writer, mustMarshalJSON(t, routers)))
				saved := rsRouterObj(t, r)
				wantJSONEqual(t, saved, routers.GetList("items")[0])
				wantJSONEqual(t, saved.Value("ros_policy_state"), json.RawMessage(rsManifest))
			})
		}
	}
}

func TestLivePublicationStillLocksCredentialRepairs(t *testing.T) {
	for _, field := range []string{"username", "password", "verify_tls"} {
		t.Run(field, func(t *testing.T) {
			r, locks := rsRepository(t)
			router := rsPersist(t, r, locks)
			before := must(r.ExportAllJSON(bg, false))(t)
			replacement := parseJSONObj(t, must(model.Marshal(router))(t))
			replacement.Set(field, rsChanged(field))
			release := locks.hold(t, router)
			wantError(t, rsReplaceRouters(t, r, "profile-import", mustMarshalJSON(t, testObjKV("items", []any{replacement}))), ErrInUse, "")
			release()
			wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
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
			before := must(r.ExportAllJSON(bg, false))(t)
			var err error
			if operation == "delete-profile" {
				err = r.DeleteProfile(bg, "live", "")
			} else {
				_, err = r.ResetAllJSON(bg)
			}
			wantError(t, err, ErrInUse, "")
			wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
			if text := must(readFileString(artifact))(t); text != "retain me" {
				t.Fatalf("artifact %q", text)
			}
		})
	}
}

func TestDuplicateIdsCannotShadowTheActiveManifest(t *testing.T) {
	r, locks := rsRepository(t)
	rsPersist(t, r, locks)
	before := must(r.ExportAllJSON(bg, false))(t)
	routers := parseJSONObj(t, before).GetObject("profiles").GetObject("live").GetObject("routers")
	routers.Set("items", append([]any{parseJSONObj(t, []byte(rsConnection))}, routers.GetList("items")...))
	_, err := r.ImportProfileJSON(bg, "live", mustMarshalJSON(t, testObjKV("routers", routers)))
	wantError(t, err, ErrValidation, "")
	wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
}

func TestVerifiedPublicationAndWithdrawalPreserveConcurrentMetadata(t *testing.T) {
	r, locks := rsRepository(t)
	original := rsRouter(t, r)
	originalRaw := must(model.Marshal(original))(t)
	release := locks.hold(t, original)
	_, err := r.UpdateProfile(bg, "live", func(_ context.Context, profile *model.Profile) error {
		profile.Routers.Items[0].Name = "Concurrent edit"
		return nil
	})
	noError(t, err)
	noError(t, r.SaveRouterPublicationJSON(bg, "live", originalRaw, RouterPublicationJSON{
		Manifest:    JSONUpdate{Set: true, Value: json.RawMessage(rsManifest)},
		LastRelease: JSONUpdate{Set: true, Value: json.RawMessage(`{"at":"2026-01-01T00:00:00","ok":true,"fingerprint":"f","message":""}`)},
	}))
	// Restored RouterOS rows may receive new IDs; the rollback readback is
	// authoritative even when the caller still has the original snapshot.
	restored := parseJSONObj(t, []byte(rsManifest))
	restored.GetList("resources")[0].(*testObj).Set("id", "*2")
	noError(t, r.SaveRouterPublicationJSON(bg, "live", originalRaw, RouterPublicationJSON{
		Manifest: JSONUpdate{Set: true, Value: mustMarshalJSON(t, restored)},
	}))
	saved := rsRouterObj(t, r)
	wantJSONEqual(t, saved.Value("ros_policy_state"), restored)
	wantJSONEqual(t, saved.Value("name"), "Concurrent edit")
	wantJSONEqual(t, saved.Value("last_release"), json.RawMessage(`{"at":"2026-01-01T00:00:00","ok":true,"fingerprint":"f","message":""}`))
	noError(t, r.SaveRouterPublicationJSON(bg, "live", originalRaw, RouterPublicationJSON{
		Manifest: JSONUpdate{Set: true, Value: json.RawMessage("null")},
	}))
	release()
	must(r.ImportProfileJSON(bg, "live", json.RawMessage(`{"routers":{"items":[]}}`)))(t)
	if items := must(r.Profile(bg, "live"))(t).Routers.Items; len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
}

func TestPublicationCannotPersistAgainstADifferentConnection(t *testing.T) {
	for _, change := range []string{"endpoint", "username", "password", "verify_tls"} {
		t.Run(change, func(t *testing.T) {
			r, locks := rsRepository(t)
			stale := parseJSONObj(t, []byte(rsConnection))
			if change == "verify_tls" {
				stale.Set(change, false)
			} else {
				stale.Set(change, "other")
			}
			before := must(r.ExportAllJSON(bg, false))(t)
			conn := decodeJSON[model.Router](t, rsConnection)
			release := locks.hold(t, conn)
			err := r.SaveRouterPublicationJSON(bg, "live", mustMarshalJSON(t, stale), RouterPublicationJSON{
				Manifest: JSONUpdate{Set: true, Value: json.RawMessage(rsManifest)},
			})
			wantError(t, err, ErrInUse, "")
			release()
			wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
		})
	}
}

func TestSettingsCommitDoesNotReacquireHeldJobLock(t *testing.T) {
	r, locks := rsRepository(t)
	connection := decodeJSON[model.Router](t, rsConnection)
	release := locks.hold(t, connection)
	changed := parseJSONObj(t, []byte(rsConnection))
	changed.Set("password", "new password")
	changedRaw := mustMarshalJSON(t, changed)
	noError(t, r.SaveRouterSettingsJSON(bg, "live", json.RawMessage(rsConnection), changedRaw))
	noError(t, r.SaveRouterSettingsJSON(bg, "live", changedRaw, nil))
	release()
	if items := must(r.Profile(bg, "live"))(t).Routers.Items; len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
}

func TestOrdinaryWriterCannotChangeConnectionDuringFirstPublication(t *testing.T) {
	for _, field := range []string{"type", "endpoint", "username", "password", "verify_tls"} {
		t.Run(field, func(t *testing.T) {
			r, locks := rsRepository(t)
			before := must(r.ExportAllJSON(bg, false))(t)
			routers := parseJSONObj(t, before).GetObject("profiles").GetObject("live").GetObject("routers")
			routers.GetList("items")[0].(*testObj).Set(field, rsChanged(field))
			connection := decodeJSON[model.Router](t, rsConnection)
			release := locks.hold(t, connection)
			_, err := r.ImportProfileJSON(bg, "live", mustMarshalJSON(t, testObjKV("routers", routers)))
			wantError(t, err, ErrInUse, "")
			wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
			noError(t, r.SaveRouterPublicationJSON(bg, "live", json.RawMessage(rsConnection), RouterPublicationJSON{
				Manifest: JSONUpdate{Set: true, Value: json.RawMessage(rsManifest)},
			}))
			release()
			wantJSONEqual(t, rsRouterObj(t, r).Value("ros_policy_state"), json.RawMessage(rsManifest))
		})
	}
}

func TestBatchRemovalDeduplicatesDeviceConnectionLocks(t *testing.T) {
	r, _ := rsRepository(t)
	second := parseJSONObj(t, []byte(rsConnection))
	second.Set("id", "same-device")
	raw := mustMarshalJSON(t, testObjKV("routers", testObjKV("items", []any{parseJSONObj(t, []byte(rsConnection)), second})))
	_, _, err := r.SaveProfileFieldsJSON(bg, "live", raw, nil)
	noError(t, err)
	noError(t, r.DeleteProfile(bg, "live", ""))
	profiles := must(r.ListProfileRecords(bg))(t)
	if len(profiles) != 1 || profiles[0].ID != "default" {
		t.Fatalf("profiles %+v", profiles)
	}
}

// The Python case holds the publication lock in another process; the lock
// itself belongs to the routers package, so here another holder of the fake
// lock stands in for it.
func TestFirstPublicationBlocksBatchReplacement(t *testing.T) {
	for _, writer := range []string{"profile-import", "whole-import", "fields", "transaction", "reset", "delete-profile"} {
		t.Run(writer, func(t *testing.T) {
			r, locks := rsRepository(t)
			before := must(r.ExportAllJSON(bg, false))(t)
			artifact := rsArtifact(t, r, "first.txt", "still publishing")
			release := locks.hold(t, decodeJSON[model.Router](t, rsConnection))
			var err error
			switch writer {
			case "reset":
				_, err = r.ResetAllJSON(bg)
			case "delete-profile":
				err = r.DeleteProfile(bg, "live", "")
			default:
				err = rsReplaceRouters(t, r, writer, json.RawMessage(`{"items":[]}`))
			}
			wantError(t, err, ErrInUse, "")
			wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
			if text := must(readFileString(artifact))(t); text != "still publishing" {
				t.Fatalf("artifact %q", text)
			}
			release()
			// Rejection must release the document lock so the publisher can
			// save its first manifest, and must not leak the job lock.
			rsPersist(t, r, locks)
			wantJSONEqual(t, rsRouterObj(t, r).Value("ros_policy_state"), json.RawMessage(rsManifest))
		})
	}
}

func TestReplacementHoldsPublicationLockThroughDatabaseCommit(t *testing.T) {
	r, locks := rsRepository(t)
	connection := decodeJSON[model.Router](t, rsConnection)
	var observations []bool
	r.Store().BeforeCommit = func() error {
		release, acquired := locks.acquire(bg, "other", "other-router", connection)
		observations = append(observations, acquired)
		if acquired {
			release()
		}
		return nil
	}
	must(r.ImportProfileJSON(bg, "live", json.RawMessage(`{"routers":{"items":[]}}`)))(t)
	r.Store().BeforeCommit = nil
	if len(observations) != 1 || observations[0] {
		t.Fatalf("observations %v", observations)
	}
	locks.hold(t, connection)()
}
