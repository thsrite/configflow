package store

// Port of backend/test_config_store.py (SQLite storage engine: row layout,
// pragmas, diff commits and conflicts), plus tests of the behaviour the
// Python suite only reaches through ProfileRepository.

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

const sampleJSON = `{
  "schema_version": 5,
  "system": {"system_config": {"server_domain": "https://例子.test", "z": 1, "a": 2},
             "backup": {}, "agents": [], "_revision": 3},
  "shared": {"subscriptions": [], "nodes": [{"id": "n", "name": "节点 🚀", "port": 1.5}],
             "subscription_aggregations": [], "rule_library": [], "_revision": 7},
  "profiles": {
    "zeta": {"id": "zeta", "name": "Zeta", "_revision": 2, "description": "最后创建",
             "created_at": "2026-01-02T00:00:00Z", "updated_at": "2026-01-03T00:00:00Z",
             "extra": {"b": [1, {"c": null}], "a": true}},
    "default": {"name": "Default", "id": "default", "_revision": 0, "description": "",
                "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"}
  },
  "future_root_key": {"kept": true}
}`

func sampleDocument(t testing.TB) *jsonx.Object {
	t.Helper()
	value, err := jsonx.ParseString(sampleJSON)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func seconds(value float64) func() time.Duration {
	return func() time.Duration { return time.Duration(value * float64(time.Second)) }
}

func newStore(t testing.TB) *legacyStore {
	t.Helper()
	return legacyNew(filepath.Join(t.TempDir(), Filename), seconds(2.5))
}

func compact(t testing.TB, value any) string {
	t.Helper()
	text, err := jsonx.Compact(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func must[T any](value T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}

func mustCommit(t testing.TB, target *legacyStore, base legacyEntry, document, meta *jsonx.Object) legacyEntry {
	t.Helper()
	entry, committed, err := target.Commit(base, document, meta)
	if err != nil || !committed {
		t.Fatalf("Commit: committed=%v err=%v", committed, err)
	}
	return entry
}

type storedField struct {
	value    string
	revision int64
}

// fieldRows reads every field row with a plain connection.
func fieldRows(t testing.TB, path string) map[fieldKey]storedField {
	t.Helper()
	connection := must(connect(path, false, time.Second))(t)
	defer connection.close()
	result := map[fieldKey]storedField{}
	err := connection.query("SELECT scope, owner, field, value, revision FROM fields", nil, func(row *sql.Rows) error {
		var key fieldKey
		var field storedField
		if err := row.Scan(&key.scope, &key.owner, &key.field, &field.value, &field.revision); err != nil {
			return err
		}
		result[key] = field
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// execSQL runs statements on a plain connection, outside the store.
func execSQL(t testing.TB, path string, statements ...string) {
	t.Helper()
	connection := must(connect(path, false, time.Second))(t)
	defer connection.close()
	for _, statement := range statements {
		if err := connection.exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func pragma(t testing.TB, connection *Conn, name string) string {
	t.Helper()
	return strings.Join(must(connection.strings1("PRAGMA "+name))(t), ",")
}

func TestRowsRoundTripPreservesOrderUnicodeAndExtraKeys(t *testing.T) {
	document := sampleDocument(t)

	target := newStore(t)
	must(target.Create(document, nil))(t)
	loaded := must(legacyNew(target.Path(), seconds(1)).Load())(t).Document
	if compact(t, loaded) != compact(t, document) {
		t.Fatal(compact(t, loaded))
	}
	if !reflect.DeepEqual(loaded.GetObject("profiles").Keys(), []string{"zeta", "default"}) {
		t.Fatal(loaded.GetObject("profiles").Keys())
	}
	if !reflect.DeepEqual(jsonx.Path(loaded, "profiles", "default").(*jsonx.Object).Keys(),
		jsonx.Path(document, "profiles", "default").(*jsonx.Object).Keys()) {
		t.Fatal("field order of a profile changed")
	}
}

// Replaces test_repository_export_is_byte_identical_after_reopening, which
// goes through ProfileRepository: the same property at the store level.
func TestDocumentIsByteIdenticalAfterReopening(t *testing.T) {
	target := newStore(t)
	entry := must(target.Create(sampleDocument(t), nil))(t)
	document := entry.Document.DeepCopy()
	profiles := document.GetObject("profiles")
	profiles.Set("second", jsonx.Obj("id", "second", "name", "第二个", "description", "desc", "_revision", 0))
	profiles.Set("alpha", jsonx.Obj("id", "alpha", "name", "Alpha", "_revision", 0))
	entry = mustCommit(t, target, entry, document, nil)
	document = entry.Document.DeepCopy()
	second := document.GetObject("profiles").GetObject("second")
	second.Set("name", "改名")
	second.Set("_revision", 1)
	entry = mustCommit(t, target, entry, document, nil)

	exported := must(jsonx.Indent2(entry.Document))(t)
	reopened := must(legacyNew(target.Path(), seconds(1)).Load())(t)
	if again := must(jsonx.Indent2(reopened.Document))(t); string(again) != string(exported) || string(again) != string(must(jsonx.Indent2(document))(t)) {
		t.Fatalf("%s", again)
	}
	if !reflect.DeepEqual(reopened.Document.GetObject("profiles").Keys(), []string{"zeta", "default", "second", "alpha"}) {
		t.Fatal(reopened.Document.GetObject("profiles").Keys())
	}
	if reopened.Head != entry.Head || reopened.Size != entry.Size || entry.Head.Generation != 3 {
		t.Fatal(reopened.Head, entry.Head, reopened.Size, entry.Size)
	}
}

func TestPragmasAndFileMode(t *testing.T) {
	target := legacyNew(filepath.Join(t.TempDir(), Filename), seconds(7))
	must(target.Create(sampleDocument(t), nil))(t)
	connection := must(target.connect(false))(t)
	defer connection.close()
	for name, want := range map[string]string{
		"foreign_keys": "1",
		"synchronous":  "3", // EXTRA: the directory is synced after the journal is unlinked
		"journal_mode": "delete",
		"busy_timeout": "7000",
		// 0x43464C57
		"application_id": "1128680535",
	} {
		if got := pragma(t, connection, name); got != want {
			t.Errorf("PRAGMA %s = %s, want %s", name, got, want)
		}
	}
	info := must(os.Stat(target.Path()))(t)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if ApplicationID != 1128680535 {
		t.Fatal(ApplicationID)
	}
}

// test_wal_mode_is_reset_to_delete_at_startup: startup calls Check.
func TestWalModeIsResetToDeleteByCheck(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	connection := must(connect(target.Path(), false, time.Second))(t)
	if got := pragma(t, connection, "journal_mode=WAL"); got != "wal" {
		t.Fatal(got)
	}
	connection.close()
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	connection = must(target.connect(false))(t)
	defer connection.close()
	if got := pragma(t, connection, "journal_mode"); got != "delete" {
		t.Fatal(got)
	}
}

func TestCommitWritesOnlyChangedRows(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	if base.Head.Generation != 1 {
		t.Fatal(base.Head)
	}
	document := sampleDocument(t)
	document.GetObject("profiles").GetObject("zeta").Set("name", "Renamed")
	entry := mustCommit(t, target, base, document, nil)
	if entry.Head.StoreID != base.Head.StoreID || entry.Head.Generation != 2 {
		t.Fatal(entry.Head)
	}
	if entry.Head.CommitID == "" || entry.Head.CommitID == base.Head.CommitID {
		t.Fatal(entry.Head)
	}
	stored := fieldRows(t, target.Path())
	renamed := fieldKey{"profile", "zeta", "name"}
	if stored[renamed] != (storedField{`"Renamed"`, 2}) {
		t.Fatal(stored[renamed])
	}
	for key, field := range stored {
		if (field.revision == 2) != (key == renamed) {
			t.Errorf("%v has revision %d", key, field.revision)
		}
	}
	if !jsonx.Equal(entry.Document, document) || compact(t, entry.Document) != compact(t, document) {
		t.Fatal(compact(t, entry.Document))
	}

}

func TestDeletingAProfileCascadesItsFieldsAndReorders(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	document := sampleDocument(t)
	document.GetObject("profiles").Delete("zeta")
	entry := mustCommit(t, target, base, document, nil)
	for key := range fieldRows(t, target.Path()) {
		if key.owner == "zeta" {
			t.Errorf("%v survived", key)
		}
	}
	if loaded := must(target.Load())(t); compact(t, loaded.Document) != compact(t, document) || loaded.Head != entry.Head {
		t.Fatal(compact(t, loaded.Document))
	}
	if entry.Head.Generation != 2 {
		t.Fatal(entry.Head)
	}
}

func TestEmptyDiffAndMetaOnlyCommitDoNotBumpGeneration(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	before := must(target.Fingerprint())(t)
	if _, committed, err := target.Commit(base, sampleDocument(t), nil); committed || err != nil {
		t.Fatal(committed, err)
	}
	if _, committed, err := target.Commit(base, sampleDocument(t), jsonx.NewObject()); committed || err != nil {
		t.Fatal(committed, err)
	}
	if must(target.Fingerprint())(t) != before {
		t.Fatal("an empty commit wrote something")
	}
	entry := mustCommit(t, target, base, sampleDocument(t), jsonx.Obj("note", jsonx.Obj("kept", 1)))
	if entry.Head != base.Head {
		t.Fatal(entry.Head, base.Head)
	}
	if note := must(target.Meta("note"))(t); compact(t, note) != `{"kept":1}` {
		t.Fatal(compact(t, note))
	}
	if missing := must(target.Meta("no_such_key"))(t); missing != nil {
		t.Fatal(missing)
	}
	after := must(target.Fingerprint())(t)
	if after.StoreID != before.StoreID || after.Generation != before.Generation || after == before {
		t.Fatal(after, before)
	}
}

func TestReorderingProfilesIsAChange(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	document := sampleDocument(t)
	profiles := document.GetObject("profiles")
	document.Set("profiles", jsonx.Obj("default", profiles.Value("default"), "zeta", profiles.Value("zeta")))
	mustCommit(t, target, base, document, nil)
	if keys := must(target.Load())(t).Document.GetObject("profiles").Keys(); !reflect.DeepEqual(keys, []string{"default", "zeta"}) {
		t.Fatal(keys)
	}
}

func TestHeadMovedOutsideTheLockRaisesConflict(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	execSQL(t, target.Path(), "UPDATE meta SET value = '5' WHERE key = 'generation'")
	before := must(target.Fingerprint())(t)
	document := sampleDocument(t)
	document.GetObject("shared").Set("_revision", 8)
	_, committed, err := target.Commit(base, document, nil)
	if committed || !errors.Is(err, ErrConflict) || !errors.Is(err, ErrStore) || errors.Is(err, ErrStorage) || err.Error() != "配置数据库在事务外被修改" {
		t.Fatal(committed, err)
	}
	if must(target.Fingerprint())(t) != before {
		t.Fatal("a conflicting commit wrote something")
	}
	// Same generation, other commit id: also a conflict.
	stale := must(target.Load())(t)
	execSQL(t, target.Path(), `UPDATE meta SET value = '"other"' WHERE key = 'commit_id'`)
	if _, _, err := target.Commit(stale, document, nil); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestDocumentShapeErrors(t *testing.T) {
	cases := map[string]string{
		`{"system":[]}`:                   "配置节 system/ 必须是对象",
		`{"shared":null}`:                 "配置节 shared/ 必须是对象",
		`{"profiles":[]}`:                 "profiles 必须是对象",
		`{"profiles":{"":{}}}`:            "配置空间 ID 必须是非空字符串",
		`{"profiles":{"p":"not a dict"}}`: "配置节 profile/p 必须是对象",
	}
	target := newStore(t)
	for text, want := range cases {
		document := must(jsonx.ParseString(text))(t).(*jsonx.Object)
		if _, err := target.Create(document, nil); !errors.Is(err, ErrDocument) || err.Error() != want {
			t.Errorf("%s: %v", text, err)
		}
	}
	if _, err := target.Create(nil, nil); !errors.Is(err, ErrDocument) || err.Error() != "配置文档必须是对象" {
		t.Fatal(err)
	}
	if entries := must(os.ReadDir(filepath.Dir(target.Path())))(t); len(entries) != 0 {
		t.Fatal("a rejected document left files behind")
	}
}

// test_fingerprint_is_stable_across_reads_and_changes_on_write at the store level.
func TestFingerprintIsStableAcrossReadsAndChangesOnWrite(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	first := must(target.Fingerprint())(t)
	must(target.Load())(t)
	must(target.Meta("root_order"))(t)
	must(legacyNew(target.Path(), seconds(1)).Load())(t)
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	if must(target.Fingerprint())(t) != first || first.StoreID != base.Head.StoreID || first.Generation != 1 || len(first.Digest) != 64 {
		t.Fatal(first)
	}
	document := sampleDocument(t)
	document.GetObject("profiles").GetObject("default").Set("description", "changed")
	mustCommit(t, target, base, document, nil)
	if must(target.Fingerprint())(t) == first {
		t.Fatal("the fingerprint did not change")
	}
}

func TestNewDatabaseMeta(t *testing.T) {
	target := newStore(t)
	entry := must(target.Create(sampleDocument(t), jsonx.Obj("migration", jsonx.Obj("source", "defaults"), "generation", 1)))(t)
	hex32 := regexp.MustCompile(`^[0-9a-f]{12}4[0-9a-f]{3}[89ab][0-9a-f]{15}$`)
	if !hex32.MatchString(entry.Head.StoreID) || !hex32.MatchString(entry.Head.CommitID) || entry.Head.StoreID == entry.Head.CommitID {
		t.Fatal(entry.Head)
	}
	meta := target.SalvageMeta()
	if !reflect.DeepEqual(meta.Keys(), []string{"commit_id", "created_at", "generation", "migration", "root_order", "storage_version", "store_id"}) {
		t.Fatal(meta.Keys())
	}
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d{6})?Z$`).MatchString(meta.GetString("created_at")) {
		t.Fatal(meta.GetString("created_at"))
	}
	if meta.GetInt("storage_version") != 1 || compact(t, meta.Value("migration")) != `{"source":"defaults"}` {
		t.Fatal(compact(t, meta))
	}
	wantSize := int64(0)
	for _, field := range fieldRows(t, target.Path()) {
		wantSize += int64(len([]rune(field.value)))
	}
	if entry.Size != wantSize || entry.Size == 0 {
		t.Fatal(entry.Size, wantSize)
	}
	if formatTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))) != "2026-01-02T02:04:05Z" ||
		formatTime(time.Date(2026, 1, 2, 3, 4, 5, 120_999, time.UTC)) != "2026-01-02T03:04:05.000120Z" {
		t.Fatal("formatTime")
	}
}

func TestMissingDatabaseIsNotCreated(t *testing.T) {
	target := newStore(t)
	if _, err := target.Load(); !errors.Is(err, ErrStorage) || err.Error() != "配置数据库访问失败：unable to open database file" {
		t.Fatal(err)
	}
	if err := target.Check(); !errors.Is(err, ErrSQLite) || errors.Is(err, ErrStore) || err.Error() != "unable to open database file" {
		t.Fatal(err)
	}
	if target.SalvageMeta() != nil {
		t.Fatal("SalvageMeta of a missing file")
	}
	if _, err := os.Stat(target.Path()); !os.IsNotExist(err) {
		t.Fatal("opening created the file")
	}
}

func TestCheckRejectsDamagedAndNewerFiles(t *testing.T) {
	target := newStore(t)
	if err := os.WriteFile(target.Path(), []byte(strings.Repeat("this is not a database at all\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := target.Check(); !errors.Is(err, ErrCorrupt) || !errors.Is(err, ErrStore) || err.Error() != "file is not a database" {
		t.Fatal(err)
	}
	if _, err := target.Load(); !errors.Is(err, ErrStorage) || err.Error() != "配置数据库已损坏：file is not a database" {
		t.Fatal(err)
	}

	must(target.Create(sampleDocument(t), nil))(t) // replaces the damaged file
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	execSQL(t, target.Path(), "UPDATE meta SET value = '2' WHERE key = 'storage_version'")
	if err := target.Check(); !errors.Is(err, ErrUnsupported) || !errors.Is(err, ErrStore) || errors.Is(err, ErrCorrupt) ||
		err.Error() != "数据库存储版本 2 高于当前支持的 1" {
		t.Fatal(err)
	}
	execSQL(t, target.Path(), `UPDATE meta SET value = '"1"' WHERE key = 'storage_version'`)
	if err := target.Check(); !errors.Is(err, ErrCorrupt) || err.Error() != "数据库存储版本无效：\"1\"" {
		t.Fatal(err)
	}
	execSQL(t, target.Path(), "DELETE FROM meta WHERE key IN ('storage_version', 'root_order')")
	if err := target.Check(); !errors.Is(err, ErrCorrupt) || err.Error() != "缺少数据库元信息：storage_version、root_order" {
		t.Fatal(err)
	}
	execSQL(t, target.Path(), "DROP TABLE fields", "DROP TABLE meta")
	if err := target.Check(); !errors.Is(err, ErrCorrupt) || err.Error() != "缺少数据表：fields、meta" {
		t.Fatal(err)
	}
}

// test_locked_database_fails_without_renaming, at the store level: a locked
// database is a failure to report, not a damaged file.
func TestLockedDatabaseIsNotCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), Filename)
	base := must(legacyNew(path, seconds(1)).Create(sampleDocument(t), nil))(t)
	target := legacyNew(path, seconds(0.2))
	holder := must(connect(path, false, 0))(t)
	if err := holder.begin("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := target.Check(); !errors.Is(err, ErrSQLite) || errors.Is(err, ErrStore) || err.Error() != "database is locked" {
		t.Fatal(err)
	}
	if waited := time.Since(started); waited < 150*time.Millisecond || waited > 5*time.Second {
		t.Fatalf("waited %v for a 0.2 s timeout", waited)
	}
	if _, err := target.Load(); !errors.Is(err, ErrStorage) || err.Error() != "配置数据库访问失败：database is locked" {
		t.Fatal(err)
	}
	document := sampleDocument(t)
	document.Set("schema_version", 6)
	if _, _, err := target.Commit(base, document, nil); !errors.Is(err, ErrStorage) || err.Error() != "配置数据库访问失败：database is locked" {
		t.Fatal(err)
	}
	holder.close()
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	mustCommit(t, target, base, document, nil)
}

// The seam used by the Python tests that patch ConfigStore._apply and
// ._commit: a failure before COMMIT leaves the database untouched, and a
// reader is not blocked by a writer that holds its transaction open
// (test_reader_is_not_blocked_by_a_writer_holding_its_transaction).
func TestBeforeCommitHook(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	before := must(target.Fingerprint())(t)
	document := sampleDocument(t)
	document.GetObject("profiles").Delete("zeta")
	document.Set("added", "x")

	injected := errors.New("injected write failure")
	target.BeforeCommit = func() error { return injected }
	if _, committed, err := target.Commit(base, document, jsonx.Obj("note", 1)); err != injected || committed {
		t.Fatal(committed, err)
	}
	if must(target.Fingerprint())(t) != before {
		t.Fatal("a failed commit wrote something")
	}

	calls := 0
	target.BeforeCommit = func() error {
		calls++
		started := time.Now()
		seen := must(legacyNew(target.Path(), seconds(2)).Load())(t)
		if seen.Head != base.Head || time.Since(started) > time.Second {
			t.Errorf("reader saw %v after %v", seen.Head, time.Since(started))
		}
		return nil
	}
	if _, committed, err := target.Commit(base, sampleDocument(t), nil); committed || err != nil || calls != 0 {
		t.Fatal("the hook ran for an empty commit", committed, err, calls)
	}
	entry := mustCommit(t, target, base, document, nil)
	if calls != 1 || must(target.Load())(t).Head != entry.Head {
		t.Fatal(calls)
	}
}

func TestBackupTo(t *testing.T) {
	target := newStore(t)
	base := must(target.Create(sampleDocument(t), nil))(t)
	backup := filepath.Join(filepath.Dir(target.Path()), "backup.sqlite3")
	if err := target.BackupTo(backup); err != nil {
		t.Fatal(err)
	}
	if info := must(os.Stat(backup))(t); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if err := target.BackupTo(backup); !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	document := sampleDocument(t)
	document.Set("schema_version", 6)
	mustCommit(t, target, base, document, nil)

	copied := legacyNew(backup, seconds(1))
	if err := copied.Check(); err != nil {
		t.Fatal(err)
	}
	entry := must(copied.Load())(t)
	if entry.Head != base.Head || compact(t, entry.Document) != compact(t, base.Document) {
		t.Fatal(entry.Head)
	}
	connection := must(copied.connect(false))(t)
	defer connection.close()
	if pragma(t, connection, "application_id") != "1128680535" || pragma(t, connection, "journal_mode") != "delete" {
		t.Fatal("the backup lost the database header")
	}
	missing := legacyNew(filepath.Join(t.TempDir(), Filename), seconds(1))
	if err := missing.BackupTo(filepath.Join(t.TempDir(), "x.sqlite3")); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
}

func TestPreserveCorruptThenInstall(t *testing.T) {
	target := newStore(t)
	damaged := []byte(strings.Repeat("damaged", 300))
	if err := os.WriteFile(target.Path(), damaged, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.Path()+"-journal", []byte("hot journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	temporary, entry, err := target.BuildTemporary(sampleDocument(t), jsonx.Obj("recovered_from", jsonx.Obj("source", "config.json")))
	if err != nil {
		t.Fatal(err)
	}
	if info := must(os.Stat(temporary))(t); info.Mode().Perm() != 0o600 || filepath.Dir(temporary) != filepath.Dir(target.Path()) ||
		!regexp.MustCompile(`^\.configflow\.sqlite3\..+\.tmp$`).MatchString(filepath.Base(temporary)) {
		t.Fatal(temporary, info.Mode())
	}
	name := target.CorruptTarget()
	if !regexp.MustCompile(`configflow\.sqlite3\.corrupt-\d{19}$`).MatchString(name) {
		t.Fatal(name)
	}
	kept := must(target.PreserveCorrupt(name))(t)
	if kept != name || string(must(os.ReadFile(kept))(t)) != string(damaged) || string(must(os.ReadFile(target.Path()))(t)) != string(damaged) {
		t.Fatal("the damaged file must stay in place and be kept under the new name")
	}
	if string(must(os.ReadFile(kept+"-journal"))(t)) != "hot journal" {
		t.Fatal("journal not moved")
	}
	if _, err := os.Stat(target.Path() + "-journal"); !os.IsNotExist(err) {
		t.Fatal("the journal would be applied to the new file")
	}
	if err := target.Install(temporary); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatal("temporary still there")
	}
	if loaded := must(target.Load())(t); loaded.Head != entry.Head || compact(t, must(target.Meta("recovered_from"))(t)) != `{"source":"config.json"}` {
		t.Fatal(loaded.Head)
	}
	if automatic := must(target.PreserveCorrupt(""))(t); automatic == name || !strings.Contains(automatic, ".corrupt-") {
		t.Fatal(automatic)
	}
}

func TestDiscardStaleTemporaries(t *testing.T) {
	target := newStore(t)
	directory := filepath.Dir(target.Path())
	must(target.Create(sampleDocument(t), nil))(t)
	temporary, _, err := target.BuildTemporary(sampleDocument(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	keep := []string{Filename, Filename + "-journal", ".configflow.sqlite3.tmp", "configflow.sqlite3.abc.tmp", ".other.sqlite3.abc.tmp", "dir.tmp"}
	remove := []string{filepath.Base(temporary), filepath.Base(temporary) + "-journal", ".configflow.sqlite3.x.tmp", ".configflow.sqlite3..tmp-journal"}
	for _, name := range append(append([]string{}, keep[1:5]...), remove[1:]...) {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, ".configflow.sqlite3.dir.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target.Path(), filepath.Join(directory, ".configflow.sqlite3.link.tmp")); err != nil {
		t.Fatal(err)
	}
	target.DiscardStaleTemporaries()
	var left []string
	for _, item := range must(os.ReadDir(directory))(t) {
		left = append(left, item.Name())
	}
	want := []string{".configflow.sqlite3.dir.tmp", ".configflow.sqlite3.link.tmp", ".configflow.sqlite3.tmp", ".other.sqlite3.abc.tmp",
		Filename, Filename + "-journal", "configflow.sqlite3.abc.tmp"}
	if !reflect.DeepEqual(left, want) {
		t.Fatalf("left %q\nwant %q", left, want)
	}
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), Filename)
	must(legacyNew(path, seconds(1)).Create(sampleDocument(t), nil))(t)
	connection := must(connect(path, false, time.Second))(t)
	defer connection.close()
	for statement, want := range map[string]string{
		"SELECT * FROM nowhere": "no such table: nowhere",
		"INSERT INTO meta (key, value) VALUES ('generation', '1')":                  "UNIQUE constraint failed: meta.key",
		"INSERT INTO sections (scope, owner, position) VALUES ('bogus', '', 0)":     "CHECK constraint failed: scope IN ('root', 'system', 'shared', 'profile')",
		"INSERT INTO fields VALUES ('profile', 'nobody', 'f', 0, 'null', 1)":        "FOREIGN KEY constraint failed",
		"INSERT INTO fields VALUES ('root', '', 'schema_version: x', 0, 'null', 0)": "CHECK constraint failed: revision >= 1",
	} {
		err := connection.exec(statement)
		if got := sqliteMessage(err); err == nil || got != want {
			t.Errorf("%s:\n got %q (%v)\nwant %q", statement, got, err, want)
		}
		if translated := translate(err); !errors.Is(translated, ErrStorage) || translated.Error() != "配置数据库访问失败："+want {
			t.Errorf("translate: %v", translated)
		}
	}
	plain := errors.New("not sqlite")
	if translate(plain) != plain || untranslated(plain) != plain || translate(nil) != nil {
		t.Fatal("other errors must pass through")
	}
}
