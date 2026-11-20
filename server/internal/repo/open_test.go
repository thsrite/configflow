package repo

// Startup, corruption, concurrency, tombstone and import cases of
// backend/test_config_storage_migration.py that concern behaviour this port
// still has, plus the Go-only rule: legacy files without a database are
// refused. The cases about migrating or re-importing config.json and the
// split layout, and about rebuilding a corrupt database from config.json,
// are not ported (not implemented by decision); nor is
// backend/test_split_layout_migration.py, whose import-path behaviour
// (_convert_import, _scope_legacy_dialers) is compared with Python in
// TestCrossCheckOperations.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/store"
)

func opGlob(t *testing.T, pattern string) []string {
	t.Helper()
	return must(filepath.Glob(pattern))(t)
}

func opSnapshots(t *testing.T, r *Repository, reason string) []string {
	return opGlob(t, filepath.Join(r.MigrationsDir(), "*", reason))
}

func TestFreshDirectoryCreatesDatabaseWithoutConfigJson(t *testing.T) {
	r := newRepo(t)
	if info, err := os.Stat(r.DatabasePath()); err != nil || !info.Mode().IsRegular() {
		t.Fatal("no database")
	}
	if lexists(filepath.Join(r.DataDir(), "config.json")) {
		t.Fatal("config.json created")
	}
	wantJSONEqual(t, parseJSONObj(t, must(r.Store().Meta("migration"))(t)).Value("source"), "defaults")
	record := parseJSONObj(t, must(r.Store().Meta("legacy_config"))(t))
	if record.Value("sha256") != nil || record.Value("size") != nil {
		t.Fatal("legacy record")
	}
	if left := opGlob(t, filepath.Join(r.DataDir(), "."+DatabaseFilename+".*")); len(left) > 0 {
		t.Fatalf("temporaries %v", left)
	}
	if len(opSnapshots(t, r, "*")) > 0 {
		t.Fatal("snapshot taken")
	}
	if got := must(r.ListProfileRecords(bg))(t)[0].ID; got != "default" {
		t.Fatalf("first profile %q", got)
	}
}

func TestStaleTemporaryBuildIsDiscarded(t *testing.T) {
	directory := t.TempDir()
	noError(t, os.WriteFile(filepath.Join(directory, "."+DatabaseFilename+".abc.tmp"), []byte("partial build"), 0o600))
	noError(t, os.WriteFile(filepath.Join(directory, "."+DatabaseFilename+".abc.tmp-journal"), []byte("partial journal"), 0o600))
	openRepo(t, directory)
	if left := opGlob(t, filepath.Join(directory, "."+DatabaseFilename+".*")); len(left) > 0 {
		t.Fatalf("temporaries %v", left)
	}
}

func TestCorruptDatabaseWithoutAnySourceFailsLoudly(t *testing.T) {
	for name, content := range map[string][]byte{"garbage": bytes.Repeat([]byte("garbage"), 100), "empty": {}} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			noError(t, os.WriteFile(r.DatabasePath(), content, 0o600))
			// A config.json next to it is never a rebuild source in this port.
			noError(t, os.WriteFile(filepath.Join(r.DataDir(), "config.json"), []byte("{}"), 0o600))
			_, err := Open(r.DataDir(), Options{})
			wantError(t, err, ErrRepository, "请先停止服务并保留原文件")
			wantError(t, err, nil, "原数据库未改动")
			if !bytes.Equal(must(os.ReadFile(r.DatabasePath()))(t), content) {
				t.Fatal("damaged file changed")
			}
			if left := opGlob(t, r.DatabasePath()+".corrupt-*"); len(left) > 0 {
				t.Fatalf("corrupt copies %v", left)
			}
		})
	}
}

func TestMissingDatabaseAfterAnEarlierOneIsReported(t *testing.T) {
	r := newRepo(t)
	noError(t, os.Rename(r.DatabasePath(), r.DatabasePath()+".corrupt-1"))
	if earlier := r.earlierDatabases(); len(earlier) != 1 || !strings.Contains(earlier[0], ".corrupt-") {
		t.Fatalf("earlier %v", earlier)
	}
	// Python re-migrates from the legacy files; without any, as here, both
	// start from the defaults (and log the earlier copies as an error).
	reopened := openRepo(t, r.DataDir())
	if got := must(reopened.ListProfileRecords(bg))(t)[0].ID; got != "default" {
		t.Fatalf("first profile %q", got)
	}
}

func TestOrphanJournalWithoutDatabaseIsSetAside(t *testing.T) {
	directory := t.TempDir()
	noError(t, os.WriteFile(filepath.Join(directory, DatabaseFilename+"-journal"), []byte("orphan"), 0o600))
	openRepo(t, directory)
	orphans := opGlob(t, filepath.Join(directory, DatabaseFilename+"-journal.orphan-*"))
	if len(orphans) != 1 || must(readFileString(orphans[0]))(t) != "orphan" {
		t.Fatalf("orphans %v", orphans)
	}
}

func TestLockedDatabaseFailsWithoutRenaming(t *testing.T) {
	r := newRepo(t)
	t.Setenv(LockTimeoutEnv, "0.2")
	db := must(sql.Open("sqlite", "file:"+r.DatabasePath()))(t)
	defer db.Close()
	holder := must(db.Conn(bg))(t)
	must(holder.ExecContext(bg, "BEGIN EXCLUSIVE"))(t)
	_, err := Open(r.DataDir(), Options{})
	wantError(t, err, store.ErrStorage, "未改动")
	must(holder.ExecContext(bg, "ROLLBACK"))(t)
	holder.Close()
	if left := opGlob(t, r.DatabasePath()+".corrupt-*"); len(left) > 0 {
		t.Fatalf("corrupt copies %v", left)
	}
	openRepo(t, r.DataDir())
}

func TestNewerStorageVersionFailsWithoutRenaming(t *testing.T) {
	r := newRepo(t)
	rcSQL(t, r.DatabasePath(), "UPDATE meta SET value = '2' WHERE key = 'storage_version'")
	_, err := Open(r.DataDir(), Options{})
	wantError(t, err, ErrRepository, "更新版本")
	if left := opGlob(t, r.DatabasePath()+".corrupt-*"); len(left) > 0 {
		t.Fatalf("corrupt copies %v", left)
	}
}

// Business rules are write validation (N8-5): a stored document that breaks
// one still opens, unchanged, and writes are refused until it is repaired.
// Structural damage (here: no default profile) still refuses loading.
func TestInvalidRowsLoadButRefuseWritesWithoutRebuilding(t *testing.T) {
	r := newRepo(t)
	rcSQL(t, r.DatabasePath(), `UPDATE fields SET value = '[{"id": "a", "profile_id": "missing"}]' WHERE scope = 'system' AND field = 'agents'`)
	fingerprint := must(r.Store().Fingerprint())(t)
	opened := must(Open(r.DataDir(), Options{}))(t)
	if must(r.Store().Fingerprint())(t) != fingerprint {
		t.Fatal("database changed")
	}
	err := opened.UpdateShared(bg, func(context.Context, *model.Shared) error { return nil })
	wantError(t, err, ErrValidation, "绑定的配置不存在")
	if must(r.Store().Fingerprint())(t) != fingerprint {
		t.Fatal("database changed")
	}
	rcSQL(t, r.DatabasePath(), `UPDATE fields SET value = '"other"' WHERE scope = 'profile' AND owner = 'default' AND field = 'id'`)
	fingerprint = must(r.Store().Fingerprint())(t)
	_, err = Open(r.DataDir(), Options{})
	wantError(t, err, ErrRepository, "无法加载配置数据库")
	if must(r.Store().Fingerprint())(t) != fingerprint {
		t.Fatal("database changed")
	}
	if left := opGlob(t, r.DatabasePath()+".corrupt-*"); len(left) > 0 {
		t.Fatalf("corrupt copies %v", left)
	}
}

func TestSymlinkedDatabaseIsRejected(t *testing.T) {
	base := t.TempDir()
	real := openRepo(t, filepath.Join(base, "real"))
	root := filepath.Join(base, "data")
	noError(t, os.Mkdir(root, 0o755))
	noError(t, os.Symlink(real.DatabasePath(), filepath.Join(root, DatabaseFilename)))
	_, err := Open(root, Options{})
	wantError(t, err, ErrRepository, "必须是普通文件")
}

func TestStartupEnsuresTheRuleProxyToken(t *testing.T) {
	r := newRepo(t)
	token := must(r.System(bg))(t).SystemConfig.RuleProxyToken
	if len(token) != 43 {
		t.Fatalf("token %q", token)
	}
	fingerprint := must(r.Store().Fingerprint())(t)
	openRepo(t, r.DataDir())
	if must(r.Store().Fingerprint())(t) != fingerprint {
		t.Fatal("a restart with a token wrote to the database")
	}
	// A database whose token was removed gets one at the next start.
	rcSQL(t, r.DatabasePath(), `UPDATE fields SET value = '{"server_domain": ""}' WHERE scope = 'system' AND field = 'system_config'`,
		"UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'generation'")
	settings := must(openRepo(t, r.DataDir()).System(bg))(t).SystemConfig
	if len(settings.RuleProxyToken) != 43 || settings.RuleProxyToken == token {
		t.Fatal("token not ensured at startup")
	}
	if len(settings.RetiredRuleProxyTokens) != 0 {
		t.Fatalf("retired tokens %v", settings.RetiredRuleProxyTokens)
	}
}

func TestLegacyFilesWithoutDatabaseAreRefused(t *testing.T) {
	for _, relative := range []string{"config.json", "config.json.bak", "system.json", "system.json.bak",
		"profiles/x/config.json", "profiles/x/config.json.bak"} {
		t.Run(relative, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, relative)
			noError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			noError(t, os.WriteFile(path, []byte(`{"subscriptions": []}`), 0o600))
			_, err := Open(directory, Options{})
			wantError(t, err, ErrRepository, "请先升级到最后一个 Python 版本")
			wantError(t, err, nil, relative)
			if lexists(filepath.Join(directory, DatabaseFilename)) || lexists(filepath.Join(directory, "shared")) ||
				lexists(filepath.Join(directory, "migrations")) {
				t.Fatal("something was created")
			}
			if must(readFileString(path))(t) != `{"subscriptions": []}` {
				t.Fatal("legacy file changed")
			}
		})
	}
}

func TestRootCacheDirectoriesAloneAreAFreshInstall(t *testing.T) {
	directory := t.TempDir()
	for _, relative := range []string{"subscribes/a.json", "rules/b.list"} {
		noError(t, os.MkdirAll(filepath.Dir(filepath.Join(directory, relative)), 0o755))
		noError(t, os.WriteFile(filepath.Join(directory, relative), []byte("x"), 0o600))
	}
	r := openRepo(t, directory)
	wantJSONEqual(t, parseJSONObj(t, must(r.Store().Meta("migration"))(t)).Value("source"), "defaults")
	// Unlike Python's migration, the old caches are not copied into shared/.
	if lexists(filepath.Join(directory, "shared", "subscribes", "a.json")) {
		t.Fatal("legacy cache copied")
	}
}

func TestLegacyConfigNextToADatabaseIsIgnored(t *testing.T) {
	r := newRepo(t)
	fingerprint := must(r.Store().Fingerprint())(t)
	noError(t, os.WriteFile(filepath.Join(r.DataDir(), "config.json"), []byte(`{"subscriptions": [{"id": "s", "name": "S", "url": "u"}]}`), 0o600))
	reopened := openRepo(t, r.DataDir())
	if must(r.Store().Fingerprint())(t) != fingerprint {
		t.Fatal("config.json was imported")
	}
	if subs := must(reopened.Shared(bg))(t).Subscriptions; len(subs) != 0 {
		t.Fatalf("subscriptions %+v", subs)
	}
}

func TestReaderIsNotBlockedByAWriterHoldingItsTransaction(t *testing.T) {
	writer := newRepo(t)
	reader := openRepo(t, writer.DataDir())
	originalName := must(reader.Profile(bg, "default"))(t).Name
	inside, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	writer.Store().BeforeCommit = func() error {
		close(inside)
		<-release
		return nil
	}
	go func() {
		committed := "Committed"
		_, err := writer.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &committed})
		done <- err
	}()
	<-inside
	reader.dropCache()
	started := time.Now()
	if got := must(reader.Profile(bg, "default"))(t).Name; got != originalName {
		t.Fatalf("name %q, want %q", got, originalName)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("reader was blocked")
	}
	close(release)
	noError(t, <-done)
	if got := must(reader.Profile(bg, "default"))(t).Name; got != "Committed" {
		t.Fatalf("name %q", got)
	}
}

func TestReaderDoesNotNeedTheDocumentLock(t *testing.T) {
	r := newRepo(t)
	reader := openRepo(t, r.DataDir())
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- withLock(bg, r.lockFile, func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	reader.dropCache()
	started := time.Now()
	if got := must(reader.ListProfileRecords(bg))(t)[0].ID; got != "default" {
		t.Fatalf("first profile %q", got)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("reader waited for the document lock")
	}
	close(release)
	noError(t, <-done)
}

func TestRestoredOlderDatabaseIsNeverServedFromCache(t *testing.T) {
	r := newRepo(t)
	other := openRepo(t, r.DataDir())
	mark := func(key string, value any) {
		t.Helper()
		raw := mustMarshalJSON(t, value)
		noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
			return system.SystemConfig.Fields.Set(key, raw)
		}))
	}
	marker := func(repository *Repository) any {
		t.Helper()
		raw, ok := must(repository.System(bg))(t).SystemConfig.Fields.Get("marker")
		if !ok {
			return nil
		}
		return parseJSONVal(t, raw)
	}
	mark("marker", "snapshot")
	backup := filepath.Join(t.TempDir(), "backup.sqlite3")
	noError(t, r.Store().BackupTo(backup))
	for _, value := range []string{"live0", "live1", "live2"} {
		mark("marker", value)
	}
	wantJSONEqual(t, marker(other), "live2")
	// An operator restores the snapshot while the service runs; later
	// writes bring the generation back to the number cached before.
	noError(t, os.WriteFile(r.DatabasePath(), must(os.ReadFile(backup))(t), 0o600))
	for _, value := range []string{"post0", "post1", "post2"} {
		mark("marker", value)
	}
	wantJSONEqual(t, marker(r), "post2")
	wantJSONEqual(t, marker(other), "post2")
	mark("other", 1)
	settings := must(openRepo(t, r.DataDir()).System(bg))(t).SystemConfig
	rawMarker, _ := settings.Fields.Get("marker")
	rawOther, _ := settings.Fields.Get("other")
	wantJSONEqual(t, rawMarker, "post2")
	wantJSONEqual(t, rawOther, 1)
}

func TestDeletingAProfileWithoutDerivedFilesKeepsItsJson(t *testing.T) {
	// This storage fixture has no publisher; explicitly provide its cleanup gate.
	previous := SetHooks(Hooks{RecordHooks: RecordHooks{JobLockRecord: func(context.Context, string, string, model.Router) (func(), bool) { return func() {}, true }}})
	defer SetHooks(previous)
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "gone", Name: "Gone", Description: "含凭据"}, ""))(t)
	_, _, err := r.SaveProfileFieldsJSON(bg, "gone", json.RawMessage(`{"routers": {"items": [
		{"id": "r", "name": "Router", "type": "routeros", "endpoint": "https://r.example",
		 "username": "admin", "password": "secret"}]}}`), nil)
	noError(t, err)
	exported := must(r.ExportProfileJSON(bg, "gone"))(t)
	if lexists(must(r.ProfileDir("gone"))(t)) {
		t.Fatal("profile directory exists")
	}
	noError(t, r.DeleteProfile(bg, "gone", ""))
	tombstones := opGlob(t, filepath.Join(r.MigrationsDir(), "deleted-gone-*"))
	if len(tombstones) != 1 {
		t.Fatalf("tombstones %v", tombstones)
	}
	saved := filepath.Join(tombstones[0], "deleted-profile.json")
	text := must(os.ReadFile(saved))(t)
	wantJSONEqual(t, text, exported)
	if !bytes.Equal(text, must(fileJSON(json.RawMessage(exported)))(t)) {
		t.Fatal("not json.dumps(profile, ensure_ascii=False, indent=2) + newline")
	}
	if mode := must(os.Stat(saved))(t).Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode %o", mode)
	}
	if left := opGlob(t, filepath.Join(tombstones[0], "*.tmp")); len(left) > 0 {
		t.Fatalf("temporaries %v", left)
	}
}

func TestFailedDeleteRemovesTheTombstone(t *testing.T) {
	for _, withDirectory := range []bool{true, false} {
		t.Run(map[bool]string{true: "directory", false: "no directory"}[withDirectory], func(t *testing.T) {
			r := newRepo(t)
			must(r.CreateProfileRecord(bg, ProfileCreation{ID: "kept", Name: "Kept"}, ""))(t)
			directory := must(r.ProfileDir("kept"))(t)
			if withDirectory {
				noError(t, os.Mkdir(directory, 0o755))
				noError(t, os.WriteFile(filepath.Join(directory, "important.txt"), []byte("keep"), 0o600))
			}
			failure := errors.New("injected commit failure")
			r.Store().BeforeCommit = func() error { return failure }
			err := r.DeleteProfile(bg, "kept", "")
			r.Store().BeforeCommit = nil
			if !errors.Is(err, failure) {
				t.Fatalf("got %v", err)
			}
			if _, err := r.ProfileMetadataRecord(bg, "kept"); err != nil {
				t.Fatal("profile deleted")
			}
			if left := opGlob(t, filepath.Join(r.MigrationsDir(), "deleted-kept-*")); len(left) > 0 {
				t.Fatalf("tombstones %v", left)
			}
			if withDirectory {
				entries := must(os.ReadDir(directory))(t)
				if len(entries) != 1 || entries[0].Name() != "important.txt" {
					t.Fatal("profile directory not restored")
				}
			} else if lexists(directory) {
				t.Fatal("profile directory created")
			}
		})
	}
}

func TestExportImportRoundTripAndImportSnapshot(t *testing.T) {
	r := newRepo(t)
	must(r.CreateProfileRecord(bg, ProfileCreation{ID: "second", Name: "Second"}, ""))(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		shared.Nodes = append(shared.Nodes, decodeJSON[model.Node](t, `{"id": "n", "name": "Node", "type": "ss", "server": "s.example", "port": 1}`))
		return nil
	}))
	exported := must(r.ExportAllJSON(bg, false))(t)
	imported := must(r.ImportAllJSON(bg, exported))(t)
	withoutRevisions := func(raw json.RawMessage) *testObj {
		document := parseJSONObj(t, raw)
		document.GetObject("system").Delete("_revision")
		document.GetObject("shared").Delete("_revision")
		for _, profile := range document.GetObject("profiles").All() {
			profile.(*testObj).Delete("_revision")
		}
		return document
	}
	wantJSONEqual(t, withoutRevisions(imported), withoutRevisions(exported))
	if !reflect.DeepEqual(parseJSONObj(t, imported).GetObject("profiles").Keys(), parseJSONObj(t, exported).GetObject("profiles").Keys()) {
		t.Fatal("profile key order changed")
	}
	snapshots := opSnapshots(t, r, "import")
	if len(snapshots) != 1 {
		t.Fatalf("snapshots %v", snapshots)
	}
	saved := must(store.New(filepath.Join(snapshots[0], DatabaseFilename), LockTimeout).Load())(t)
	savedDoc, _, err := decodeModelRows(saved.Document)
	noError(t, err)
	wantJSONEqual(t, must(model.Marshal(savedDoc))(t), exported)
	if lexists(filepath.Join(snapshots[0], "config.json")) {
		t.Fatal("legacy file in an import snapshot")
	}
}

func TestBackupsOfEverySupportedSchemaImport(t *testing.T) {
	single := `{"subscriptions": [{"id": "sub-1", "name": "Sub", "url": "https://example.test/sub"}],
		"nodes": [{"id": "node-1", "name": "Node", "type": "ss", "server": "s.example", "port": 1}],
		"proxy_groups": [{"id": "g", "name": "Group", "type": "select", "manual_nodes": ["node-1"]}],
		"rule_configs": [{"id": "r", "itemType": "rule", "rule_type": "DOMAIN", "value": "a.test", "policy": "Group"}],
		"system_config": {"server_domain": "https://cf.example"}}`
	schema2 := `{"schema_version": 2, "system": {"schema_version": 2, "profiles": [{"id": "default", "name": "默认"}, {"id": "p2", "name": "P2"}]},
		"profiles": {"default": ` + single + `, "p2": {"nodes": [{"id": "node-1", "name": "Other", "type": "ss", "server": "o.example", "port": 2}]}}}`
	current := string(must(newRepo(t).ExportAllJSON(bg, false))(t))
	for name, backup := range map[string]string{"single": single, "schema2": schema2, "schema5": current} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			result := must(r.ImportAllJSON(bg, json.RawMessage(backup)))(t)
			wantJSONEqual(t, parseJSONObj(t, result).Value("schema_version"), 5)
			wantJSONEqual(t, must(openRepo(t, r.DataDir()).ExportAllJSON(bg, false))(t), result)
			if lexists(filepath.Join(r.DataDir(), "config.json")) {
				t.Fatal("config.json written")
			}
		})
	}
}

// test_profile_id_cannot_escape_profiles_directory (test_multi_profile_repository.py).
func TestProfileIdCannotEscapeProfilesDirectory(t *testing.T) {
	r := newRepo(t)
	for _, profileID := range []string{"../outside", "..", "a/b", `a\b`, "C:", "", "x\n"} {
		_, err := r.ProfileDir(profileID)
		wantError(t, err, ErrValidation, "")
		_, err = r.Profile(bg, profileID)
		wantError(t, err, ErrValidation, "")
	}
	for _, relative := range []string{"../outside", "", "/etc/passwd", "a/../../x", "a\x00b"} {
		_, err := r.ProfilePath("default", relative)
		wantError(t, err, ErrValidation, "")
		_, err = r.SharedPath(relative)
		wantError(t, err, ErrValidation, "")
	}
	// A symbolic link below the profile directory cannot lead outside either.
	directory := must(r.ProfileDir("default"))(t)
	noError(t, os.MkdirAll(directory, 0o755))
	noError(t, os.Symlink(t.TempDir(), filepath.Join(directory, "link")))
	_, err := r.ProfilePath("default", "link/file")
	wantError(t, err, ErrValidation, "Profile path escapes profile directory")
	if got, want := must(r.ProfilePath("default", "generated/config.yaml"))(t), filepath.Join(directory, "generated", "config.yaml"); got != want {
		t.Fatalf("path %q, want %q", got, want)
	}
}

// The repository half of backend/test_profile_clone_router_isolation.py: a
// clone keeps the configuration but none of the source's router access, and
// can be deleted while the source still has published ownership. (The cases
// themselves go through the HTTP and MCP entry points.)
func TestProfileCloneDoesNotCopyRouterAccess(t *testing.T) {
	r, locks := rsRepository(t)
	rsPersist(t, r, locks)
	_, _, err := r.SaveProfileFieldsJSON(bg, "live", json.RawMessage(`{"mihomo": {"custom_config": "mixed-port: 7890"},
		"client_fixed_ips": {}, "device_notes": {"192.168.0.9": "TV"}}`), nil)
	noError(t, err)
	must(r.CloneProfileRecord(bg, "live", ProfileCreation{ID: "copy", Name: "Copy"}))(t)
	clone := parseJSONObj(t, must(r.ExportProfileJSON(bg, "copy"))(t))
	live := parseJSONObj(t, must(r.ExportProfileJSON(bg, "live"))(t))
	wantJSONEqual(t, clone.Value("routers"), json.RawMessage(`{"items": []}`))
	wantJSONEqual(t, clone.Value("client_fixed_ips"), json.RawMessage(`{}`))
	wantJSONEqual(t, clone.Value("mihomo"), live.Value("mihomo"))
	wantJSONEqual(t, clone.Value("device_notes"), json.RawMessage(`{"192.168.0.9": "TV"}`))
	wantJSONEqual(t, clone.Value("_revision"), 0)
	noError(t, r.DeleteProfile(bg, "copy", ""))
	wantJSONEqual(t, rsRouterObj(t, r).Value("ros_policy_state"), json.RawMessage(rsManifest))
}
