package repo

// The repository-level cases of backend/test_config_store.py.

import (
	"os"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func TestRepositoryExportIsByteIdenticalAfterReopening(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "second", "name", "第二个", "description", "desc"), ""))(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "alpha", "name", "Alpha"), ""))(t)
	must(r.UpdateProfileMetadata(bg, "second", jsonx.Obj("name", "改名")))(t)
	exported := string(must(jsonx.Encode(must(r.ExportAll(bg, false))(t), jsonx.Options{}))(t))
	reopened := openRepo(t, r.DataDir())
	if got := string(must(jsonx.Encode(must(reopened.ExportAll(bg, false))(t), jsonx.Options{}))(t)); got != exported {
		t.Fatal("export differs after reopening")
	}
	var ids []any
	for _, item := range must(reopened.ListProfiles(bg))(t) {
		ids = append(ids, item.(*jsonx.Object).Value("id"))
	}
	wantEqual(t, ids, []any{"default", "second", "alpha"})
	profile := must(reopened.GetProfile(bg, "second"))(t)
	wantEqual(t, []any{profile.Value("name"), profile.Value("description"), profile.Value("_revision")}, []any{"改名", "desc", 1})
}

// The per-connection pragmas (foreign_keys, synchronous, busy_timeout) are
// asserted in internal/store's TestPragmasAndFileMode; here: the repository
// passes its lock timeout to the store and creates the file as Python does.
func TestPragmasAndFileMode(t *testing.T) {
	t.Setenv(LockTimeoutEnv, "7")
	r := newRepo(t)
	if LockTimeout() != 7*time.Second {
		t.Fatalf("timeout %v", LockTimeout())
	}
	if got := rcPragma(t, r.DatabasePath(), "PRAGMA journal_mode"); got != "delete" {
		t.Fatalf("journal_mode %q", got)
	}
	if got := rcPragma(t, r.DatabasePath(), "PRAGMA application_id"); got != "1128680535" {
		t.Fatalf("application_id %q", got)
	}
	if mode := must(os.Stat(r.DatabasePath()))(t).Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode %o", mode)
	}
}

func TestWalModeIsResetToDeleteAtStartup(t *testing.T) {
	r := newRepo(t)
	if got := rcPragma(t, r.DatabasePath(), "PRAGMA journal_mode=WAL"); got != "wal" {
		t.Fatalf("journal_mode %q", got)
	}
	reopened := openRepo(t, r.DataDir())
	if got := rcPragma(t, reopened.DatabasePath(), "PRAGMA journal_mode"); got != "delete" {
		t.Fatalf("journal_mode %q", got)
	}
}

func TestFingerprintIsStableAcrossReadsAndChangesOnWrite(t *testing.T) {
	r := newRepo(t)
	first := must(r.Store().Fingerprint())(t)
	must(r.GetSystem(bg))(t)
	must(r.ExportAll(bg, false))(t)
	must(openRepo(t, r.DataDir()).ListProfiles(bg))(t)
	if got := must(r.Store().Fingerprint())(t); got != first {
		t.Fatal("fingerprint changed by reads")
	}
	must(r.UpdateProfileMetadata(bg, "default", jsonx.Obj("description", "changed")))(t)
	if got := must(r.Store().Fingerprint())(t); got == first {
		t.Fatal("fingerprint unchanged by a write")
	}
}
