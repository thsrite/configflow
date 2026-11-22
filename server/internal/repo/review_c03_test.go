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
	if err == nil || err.Error() != "Symlink loop from '"+link+"'" {
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

// CPython 3.11 posixpath.realpath: a loop stops at the first link seen
// again, with the rest of the path appended unresolved.
func TestRealpathSymlinkLoopStopsAtFirstRepeatedLink(t *testing.T) {
	directory := resolvePath(t.TempDir())
	noError(t, os.Symlink("b", filepath.Join(directory, "a")))
	noError(t, os.Symlink("a", filepath.Join(directory, "b")))
	got, ok := realpath(filepath.Join(directory, "a", "x"))
	if ok || got != filepath.Join(directory, "a", "x") {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, err := resolve(filepath.Join(directory, "a")); err == nil ||
		!strings.HasPrefix(err.Error(), "Symlink loop from '") {
		t.Fatalf("resolve: %v", err)
	}
	noError(t, os.Symlink("../"+filepath.Base(directory)+"/c", filepath.Join(directory, "l")))
	if got := resolvePath(filepath.Join(directory, "l", "..", "d")); got != filepath.Join(directory, "d") {
		t.Fatalf("dotdot after link: %q", got)
	}
}

// Path(data_dir).expanduser(): "~" is $HOME, "~name" the user's home, an
// unknown user is RuntimeError("Could not determine home directory.").
func TestDataDirExpandUserLikePython(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home+"/")
	for input, want := range map[string]string{"~": home, "~/a//b": home + "/a/b", "x~": "x~", "~root/d": "/root/d"} {
		if got := must(expandUser(input))(t); got != want {
			t.Errorf("%q: %q, want %q", input, got, want)
		}
	}
	if _, err := expandUser("~no-such-user-c03/x"); err == nil || err.Error() != "Could not determine home directory." {
		t.Fatalf("unknown user: %v", err)
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

// test_config_storage_migration.py::test_write_from_another_process_is_seen
// (the other process is the Python repository).
func TestWriteFromAnotherProcessIsSeen(t *testing.T) {
	r := newRepo(t)
	if must(r.Profile(bg, "default"))(t).Name == "From another process" {
		t.Fatal("precondition")
	}
	script := filepath.Join(t.TempDir(), "ops.json")
	noError(t, os.WriteFile(script, []byte(`[{"op":"update_profile","id":"default","updates":{"name":"From another process"}}]`), 0o600))
	xcPython(t, "run", r.DataDir(), "plain", script)
	if got := must(r.Profile(bg, "default"))(t).Name; got != "From another process" {
		t.Fatalf("got %q", got)
	}
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
