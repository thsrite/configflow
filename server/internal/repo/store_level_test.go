package repo

// The repository-level cases of backend/test_config_store.py.

import (
	"bytes"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestRepositoryExportIsByteIdenticalAfterReopening(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "second", Name: "第二个", Description: "desc"}, ""))(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "alpha", Name: "Alpha"}, ""))(t)
	renamed := "改名"
	must(r.UpdateProfileMetadataRecord(bg, "second", ProfileMetadataUpdate{Name: &renamed}))(t)
	exported := must(r.ExportAllJSON(bg, false))(t)
	reopened := openRepo(t, r.DataDir())
	if got := must(reopened.ExportAllJSON(bg, false))(t); !bytes.Equal(got, exported) {
		t.Fatal("export differs after reopening")
	}
	var ids []string
	for _, item := range must(reopened.ListProfileRecords(bg))(t) {
		ids = append(ids, item.ID)
	}
	if !reflect.DeepEqual(ids, []string{"default", "second", "alpha"}) {
		t.Fatalf("ids %v", ids)
	}
	profile := must(reopened.Profile(bg, "second"))(t)
	if profile.Name != "改名" || profile.Description != "desc" || profile.Revision != 1 {
		t.Fatalf("profile %+v", profile)
	}
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
	must(r.System(bg))(t)
	must(r.ExportAllJSON(bg, false))(t)
	must(openRepo(t, r.DataDir()).ListProfileRecords(bg))(t)
	if got := must(r.Store().Fingerprint())(t); got != first {
		t.Fatal("fingerprint changed by reads")
	}
	desc := "changed"
	must(r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Description: &desc}))(t)
	if got := must(r.Store().Fingerprint())(t); got == first {
		t.Fatal("fingerprint unchanged by a write")
	}
}
