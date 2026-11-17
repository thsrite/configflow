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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
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
	wantEqual(t, must(jsonx.Parse(must(r.Store().Meta("migration"))(t)))(t).(*jsonx.Object).Value("source"), "defaults")
	record := must(jsonx.Parse(must(r.Store().Meta("legacy_config"))(t)))(t).(*jsonx.Object)
	if record.Value("sha256") != nil || record.Value("size") != nil {
		t.Fatal("legacy record")
	}
	if left := opGlob(t, filepath.Join(r.DataDir(), "."+DatabaseFilename+".*")); len(left) > 0 {
		t.Fatalf("temporaries %v", left)
	}
	if len(opSnapshots(t, r, "*")) > 0 {
		t.Fatal("snapshot taken")
	}
	wantEqual(t, must(r.ListProfiles(bg))(t)[0].(*jsonx.Object).Value("id"), "default")
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
	wantEqual(t, must(reopened.ListProfiles(bg))(t)[0].(*jsonx.Object).Value("id"), "default")
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

func TestInvalidRowsFailValidationWithoutRebuilding(t *testing.T) {
	r := newRepo(t)
	rcSQL(t, r.DatabasePath(), `UPDATE fields SET value = '[{"id": "a", "profile_id": "missing"}]' WHERE scope = 'system' AND field = 'agents'`)
	fingerprint := must(r.Store().Fingerprint())(t)
	_, err := Open(r.DataDir(), Options{})
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
	token := must(r.GetSystemSettings(bg))(t).GetString("rule_proxy_token")
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
	settings := must(openRepo(t, r.DataDir()).GetSystemSettings(bg))(t)
	if len(settings.GetString("rule_proxy_token")) != 43 || settings.GetString("rule_proxy_token") == token {
		t.Fatal("token not ensured at startup")
	}
	wantEqual(t, settings.Value("retired_rule_proxy_tokens"), []any{})
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
	wantEqual(t, must(jsonx.Parse(must(r.Store().Meta("migration"))(t)))(t).(*jsonx.Object).Value("source"), "defaults")
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
	wantEqual(t, must(reopened.GetShared(bg))(t).Value("subscriptions"), []any{})
}

func TestReaderIsNotBlockedByAWriterHoldingItsTransaction(t *testing.T) {
	writer := newRepo(t)
	reader := openRepo(t, writer.DataDir())
	originalName := must(reader.GetProfile(bg, "default"))(t).Value("name")
	inside, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	writer.Store().BeforeCommit = func() error {
		close(inside)
		<-release
		return nil
	}
	go func() {
		_, err := writer.UpdateProfile(bg, "default", jsonx.Obj("name", "Committed"))
		done <- err
	}()
	<-inside
	reader.dropCache()
	started := time.Now()
	wantEqual(t, must(reader.GetProfile(bg, "default"))(t).Value("name"), originalName)
	if time.Since(started) > 2*time.Second {
		t.Fatal("reader was blocked")
	}
	close(release)
	noError(t, <-done)
	wantEqual(t, must(reader.GetProfile(bg, "default"))(t).Value("name"), "Committed")
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
	wantEqual(t, must(reader.ListProfiles(bg))(t)[0].(*jsonx.Object).Value("id"), "default")
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
		must(r.UpdateSystemTransaction(bg, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
			system.GetObject("system_config").Set(key, value)
			return nil, nil
		}))(t)
	}
	marker := func(repository *Repository) any {
		t.Helper()
		return must(repository.GetSystem(bg))(t).GetObject("system_config").Value("marker")
	}
	mark("marker", "snapshot")
	backup := filepath.Join(t.TempDir(), "backup.sqlite3")
	noError(t, r.Store().BackupTo(backup))
	for _, value := range []string{"live0", "live1", "live2"} {
		mark("marker", value)
	}
	wantEqual(t, marker(other), "live2")
	// An operator restores the snapshot while the service runs; later
	// writes bring the generation back to the number cached before.
	noError(t, os.WriteFile(r.DatabasePath(), must(os.ReadFile(backup))(t), 0o600))
	for _, value := range []string{"post0", "post1", "post2"} {
		mark("marker", value)
	}
	wantEqual(t, marker(r), "post2")
	wantEqual(t, marker(other), "post2")
	mark("other", 1)
	settings := must(openRepo(t, r.DataDir()).GetSystem(bg))(t).GetObject("system_config")
	wantEqual(t, []any{settings.Value("marker"), settings.Value("other")}, []any{"post2", 1})
}

func TestDeletingAProfileWithoutDerivedFilesKeepsItsJson(t *testing.T) {
	// This storage fixture has no publisher; explicitly provide its cleanup gate.
	previous := SetHooks(Hooks{JobLock: func(context.Context, string, string, *jsonx.Object) (func(), bool) { return func() {}, true }})
	defer SetHooks(previous)
	r := newRepo(t)
	must(r.CreateProfile(bg, jsonx.Obj("id", "gone", "name", "Gone", "description", "含凭据"), ""))(t)
	_, _, err := r.UpdateProfileFields(bg, "gone", parse(t, `{"routers": {"items": [
		{"id": "r", "name": "Router", "type": "routeros", "endpoint": "https://r.example",
		 "username": "admin", "password": "secret"}]}}`), nil)
	noError(t, err)
	exported := must(r.ExportProfile(bg, "gone"))(t)
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
	wantEqual(t, must(jsonx.Parse(text))(t), exported)
	if !bytes.Equal(text, must(fileJSON(exported))(t)) {
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
			must(r.CreateProfile(bg, jsonx.Obj("id", "kept", "name", "Kept"), ""))(t)
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
			if _, err := r.ProfileMetadata(bg, "kept"); err != nil {
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
	must(r.CreateProfile(bg, jsonx.Obj("id", "second", "name", "Second"), ""))(t)
	must(r.UpdateSharedTransaction(bg, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("nodes", append(shared.GetList("nodes"), parse(t, `{"id": "n", "name": "Node", "type": "ss", "server": "s.example", "port": 1}`)))
		return nil, nil
	}))(t)
	exported := must(r.ExportAll(bg, false))(t)
	imported := must(r.ImportAll(bg, exported.DeepCopy()))(t)
	withoutRevisions := func(document *jsonx.Object) *jsonx.Object {
		document = document.DeepCopy()
		document.GetObject("system").Delete("_revision")
		document.GetObject("shared").Delete("_revision")
		for _, profile := range document.GetObject("profiles").All() {
			profile.(*jsonx.Object).Delete("_revision")
		}
		return document
	}
	wantEqual(t, withoutRevisions(imported), withoutRevisions(exported))
	wantEqual(t, jsonx.ListOf(imported.GetObject("profiles").Keys()), jsonx.ListOf(exported.GetObject("profiles").Keys()))
	snapshots := opSnapshots(t, r, "import")
	if len(snapshots) != 1 {
		t.Fatalf("snapshots %v", snapshots)
	}
	saved := must(store.New(filepath.Join(snapshots[0], DatabaseFilename), LockTimeout).Load())(t)
	wantEqual(t, must(decodeRows(saved.Document))(t), exported)
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
	current := string(must(jsonx.Compact(must(newRepo(t).ExportAll(bg, false))(t)))(t))
	for name, backup := range map[string]string{"single": single, "schema2": schema2, "schema5": current} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			result := must(r.ImportAll(bg, parse(t, backup)))(t)
			wantEqual(t, result.Value("schema_version"), 5)
			wantEqual(t, must(openRepo(t, r.DataDir()).ExportAll(bg, false))(t), result)
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
		_, err = r.GetProfile(bg, profileID)
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
	wantEqual(t, must(r.ProfilePath("default", "generated/config.yaml"))(t), filepath.Join(directory, "generated", "config.yaml"))
}

// The repository half of backend/test_profile_clone_router_isolation.py: a
// clone keeps the configuration but none of the source's router access, and
// can be deleted while the source still has published ownership. (The cases
// themselves go through the HTTP and MCP entry points.)
func TestProfileCloneDoesNotCopyRouterAccess(t *testing.T) {
	r, locks := rsRepository(t)
	rsPersist(t, r, locks)
	_, _, err := r.UpdateProfileFields(bg, "live", parse(t, `{"mihomo": {"custom_config": "mixed-port: 7890"},
		"client_fixed_ips": {}, "device_notes": {"192.168.0.9": "TV"}}`), nil)
	noError(t, err)
	must(r.CloneProfile(bg, "live", jsonx.Obj("id", "copy", "name", "Copy")))(t)
	clone := must(r.GetProfile(bg, "copy"))(t)
	wantEqual(t, clone.Value("routers"), parse(t, `{"items": []}`))
	wantEqual(t, clone.Value("client_fixed_ips"), jsonx.NewObject())
	wantEqual(t, clone.Value("mihomo"), must(r.GetProfile(bg, "live"))(t).Value("mihomo"))
	wantEqual(t, clone.Value("device_notes"), parse(t, `{"192.168.0.9": "TV"}`))
	wantEqual(t, clone.Value("_revision"), 0)
	noError(t, r.DeleteProfile(bg, "copy", ""))
	wantEqual(t, rsRouter(t, r).Value("ros_policy_state"), parse(t, rsManifest))
}
