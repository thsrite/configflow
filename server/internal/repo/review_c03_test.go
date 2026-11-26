package repo

// Regression tests of the C03 review. Expected values come from runs of the
// Python repository on the test server (CPython 3.11), noted per test.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/model"
)

func c03ProfileWithLink(t *testing.T, target string) (*Repository, string) {
	t.Helper()
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "p2", Name: "P2"}, ""))(t)
	link := filepath.Join(r.ProfilesDir(), "p2")
	noError(t, os.RemoveAll(link))
	noError(t, os.Symlink(target, link))
	return r, link
}

// Python: delete_profile → profile_dir → Path.resolve() raises
// RuntimeError("Symlink loop from '<data>/profiles/p2'"); nothing changes.
func TestDeleteProfileThroughSymlinkLoopFailsLikePython(t *testing.T) {
	r, link := c03ProfileWithLink(t, "p2")
	err := r.DeleteProfile(bg, "p2", "")
	// O18 C10: filepath.EvalSymlinks reports the loop (Python's text was
	// "Symlink loop from ..."); it is still an internal error.
	if err == nil || !strings.Contains(err.Error(), "resolve ") || !strings.Contains(err.Error(), filepath.Base(link)) {
		t.Fatalf("got %v", err)
	}
	if _, isRepo := err.(*Error); isRepo {
		t.Fatal("a symlink loop is not a repository error (Python RuntimeError, HTTP 500)")
	}
	if len(must(r.ListProfileRecords(bg))(t)) != 2 {
		t.Fatal("profile deleted")
	}
	if _, err := r.ProfilePath("p2", "generated/config.yaml"); err == nil {
		t.Fatal("profile path through a loop")
	}
}

// Python: a dangling profile directory link resolves to its (missing)
// target: a fresh tombstone directory receives deleted-profile.json and the
// link stays where it is.
func TestDeleteProfileWithDanglingDirectoryLink(t *testing.T) {
	r, link := c03ProfileWithLink(t, "gone")
	noError(t, r.DeleteProfile(bg, "p2", ""))
	if !lexists(link) || lexists(filepath.Join(r.ProfilesDir(), "gone")) {
		t.Fatal("link moved or target created")
	}
	exported := opGlob(t, filepath.Join(r.MigrationsDir(), "deleted-p2-*", "deleted-profile.json"))
	if len(exported) != 1 {
		t.Fatalf("tombstone %v", exported)
	}
	if info := must(os.Lstat(filepath.Dir(exported[0])))(t); !info.IsDir() {
		t.Fatal("tombstone is not a directory")
	}
}

// Python: Path.exists() is false for a dangling journal link, so it is not
// set aside; the database is created next to it.
func TestDanglingJournalLinkIsNotSetAside(t *testing.T) {
	directory := t.TempDir()
	journal := filepath.Join(directory, DatabaseFilename+"-journal")
	noError(t, os.Symlink("nowhere", journal))
	openRepo(t, directory)
	if !lexists(journal) || len(opGlob(t, journal+".orphan-*")) > 0 {
		t.Fatal("dangling journal link was renamed")
	}
}

// Python: '、'.join over a non-string Agent name raises TypeError (HTTP 500)
// and the profile stays. Go reports the binding as ProfileInUse with the
// name's str() instead (accepted difference: handled error for a crash).
func TestDeleteProfileBoundToAgentWithNonStringName(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "p2", Name: "P2"}, ""))(t)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", json.RawMessage(`[{"id":"a1","name":5,"profile_id":"p2"}]`))
	}))
	err := r.DeleteProfile(bg, "p2", "")
	wantError(t, err, ErrInUse, "配置「P2」仍被以下 Agent 使用：5")
	if len(must(r.ListProfileRecords(bg))(t)) != 2 {
		t.Fatal("profile deleted")
	}
}

// Python: a profile whose mosdns is not an object is refused by the earlier
// "mosdns 参数必须是对象" check, before the MosDNS rule references are read
// (see also the "mosdns list with bad dns" reference case).
func TestNonObjectMosdnsIsRefusedBeforeRuleReferences(t *testing.T) {
	r := newRepo(t)
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{"mosdns":["direct_rules"]}`), nil)
	wantError(t, err, ErrValidation, "mosdns 参数必须是对象")
}

// resolve follows links (also before ".."), keeps a missing tail and
// reports a symlink loop (O18 C10).
func TestResolveFollowsLinksAndKeepsMissingTail(t *testing.T) {
	directory := resolvePath(t.TempDir())
	noError(t, os.Symlink("b", filepath.Join(directory, "a")))
	noError(t, os.Symlink("a", filepath.Join(directory, "b")))
	if _, err := resolve(filepath.Join(directory, "a")); err == nil {
		t.Fatal("symlink loop resolved")
	}
	noError(t, os.Mkdir(filepath.Join(directory, "real"), 0o700))
	noError(t, os.Symlink("real", filepath.Join(directory, "l")))
	if got := must(resolve(filepath.Join(directory, "l", "new", "file")))(t); got != filepath.Join(directory, "real", "new", "file") {
		t.Fatalf("missing tail: %q", got)
	}
	noError(t, os.Symlink("../"+filepath.Base(directory)+"/real", filepath.Join(directory, "up")))
	if got := resolvePath(filepath.Join(directory, "up", "..", "d")); got != filepath.Join(directory, "d") {
		t.Fatalf("dotdot after link: %q", got)
	}
}

// expandUser: "~" is the home directory, "~name" that user's, an unknown
// user is an error (O18 C10: os.UserHomeDir, user.Lookup).
func TestDataDirExpandUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for input, want := range map[string]string{"~": home, "~/a//b": home + "/a/b", "x~": "x~", "~root/d": "/root/d"} {
		if got := must(expandUser(input))(t); got != want {
			t.Errorf("%q: %q, want %q", input, got, want)
		}
	}
	if _, err := expandUser("~no-such-user-c03/x"); err == nil {
		t.Fatal("unknown user expanded")
	}
	r := must(Open("~/data", Options{}))(t)
	if r.DataDir() != filepath.Join(resolvePath(home), "data") {
		t.Fatalf("data dir %q", r.DataDir())
	}
}

func c03Increment(t *testing.T, r *Repository, count int) {
	for range count {
		err := r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
			var n int64
			if raw, ok := markerGet(json.RawMessage(system.SystemConfig.GitHubProxyDomain)); ok {
				_ = json.Unmarshal(raw, &n)
			}
			raw, err := json.Marshal(n + 1)
			if err != nil {
				return err
			}
			return markerSet(&system.SystemConfig.GitHubProxyDomain, raw)
		})
		if err != nil {
			t.Error(err)
		}
	}
}

// test_config_storage_migration.py::test_two_threads_on_one_instance_lose_no_update
func TestTwoThreadsOnOneInstanceLoseNoUpdate(t *testing.T) {
	r := newRepo(t)
	done := make(chan struct{})
	for range 2 {
		go func() { c03Increment(t, r, 10); done <- struct{}{} }()
	}
	<-done
	<-done
	raw, _ := markerGet(json.RawMessage(must(r.System(bg))(t).SystemConfig.GitHubProxyDomain))
	wantJSONEqual(t, raw, 20)
	reopenedRaw, _ := markerGet(json.RawMessage(must(openRepo(t, r.DataDir()).System(bg))(t).SystemConfig.GitHubProxyDomain))
	wantJSONEqual(t, reopenedRaw, 20)
}

// test_config_storage_migration.py::test_cache_follows_the_generation and
// the reopening half of test_restart_is_idempotent (without legacy files).
func TestCacheFollowsTheGeneration(t *testing.T) {
	r := newRepo(t)
	cached := "Cached"
	must(r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &cached}))(t)
	loads, validations := r.loads.Load(), r.validations.Load()
	for range 3 {
		must(r.Profile(bg, "default"))(t)
		must(r.ExportAllJSON(bg, false))(t)
	}
	if r.loads.Load() != loads || r.validations.Load() != validations {
		t.Fatal("cached generation reloaded")
	}
	fingerprint := must(r.Store().Fingerprint())(t)
	for range 3 {
		reopened := openRepo(t, r.DataDir())
		if must(reopened.Store().Fingerprint())(t) != fingerprint {
			t.Fatal("reopening wrote")
		}
	}
	if len(opSnapshots(t, r, "*")) > 0 {
		t.Fatal("snapshot taken")
	}
	elsewhere := "Elsewhere"
	must(openRepo(t, r.DataDir()).UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &elsewhere}))(t)
	for range 3 {
		if got := must(r.Profile(bg, "default"))(t).Name; got != "Elsewhere" {
			t.Fatalf("got %q", got)
		}
	}
	if r.loads.Load() != loads+1 || r.validations.Load() != validations+1 {
		t.Fatalf("loads %d validations %d", r.loads.Load()-loads, r.validations.Load()-validations)
	}
}
