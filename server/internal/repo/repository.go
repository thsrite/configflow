// Package repo is the repository layer: the single owner of
// DATA_DIR/configflow.sqlite3 and of the files next to it. It is a port of
// backend/common/config_repository.py (ProfileRepository) and of the
// module-level access in backend/common/config.py.
//
// # Transactions and contexts
//
// Python keeps "this thread is inside a transaction" in a thread-local. Here
// the context carries it: every method takes a context.Context, and a write
// transaction hands its updater a derived context. Repository reads made
// with that context see the transaction's working copy; a write made with it
// fails with ErrInUse ("不能在配置事务中再次开启写事务"), exactly like a
// nested write on one Python thread. Always pass the context you were given.
//
// The same context chain records the lock files held (Python: _HELD_LOCKS)
// and, when installed with WithScope, the request-scoped caches of
// common/config.py (see scope.go).
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/store"
)

var logger = logx.Get("backend.common.config_repository")

// File names inside the data directory.
const (
	DatabaseFilename = store.Filename
	LegacyFilename   = "config.json"
	// DocumentCacheMaxBytes is DOCUMENT_CACHE_MAX_BYTES: larger documents
	// are not kept in memory between reads.
	DocumentCacheMaxBytes = 8 * 1024 * 1024
)

// DerivedDirs is ProfileRepository.DERIVED_DIRS.
var DerivedDirs = []string{"providers", "rules", "generated"}

// Options are the constructor arguments of ProfileRepository.
type Options struct {
	// DefaultSettings supplies caller-owned typed defaults for new profiles and imports.
	DefaultSettings func() *model.Config
	// InitialSettings supplies the first-start/reset configuration.
	InitialSettings func() *model.Config
}

type storedEntry struct {
	store.Entry
	Model  *model.Document
	Issues []model.FieldIssue
}

// Repository is ProfileRepository: one locked atomic document; only derived
// artifacts are profile-local. It is safe for concurrent use.
type Repository struct {
	dataDir       string
	profilesDir   string
	dbPath        string
	migrationsDir string
	lockFile      string // .config.lock
	options       Options
	store         *store.Store

	// One validated generation only, keyed by the database head; public
	// readers return independent values.
	cacheMu           sync.Mutex
	documentCache     *storedEntry
	modelIssueHead    store.Head
	modelIssueHeadSet bool
	tokensCache       atomic.Pointer[sanitizationTokens]
	// commitCount is the number of write transactions this process
	// committed; request-scoped profile validation is invalidated by it.
	commitCount atomic.Int64

	// Test seams: the number of full loads and of validations of loaded
	// documents, a callback after a full load (inside the read
	// transaction), and the cache size limit.
	loads, validations, metadataReads atomic.Int64
	afterLoad                         func()
	cacheMaxBytes                     int64
	// viewGuard, when set, is called if a View callback modified the
	// shared snapshot (tests set it to fail).
	viewGuard func()

	// The registry row has its own one-generation immutable cache. It is
	// shared by hot Agent readers even when unrelated profiles exceed the
	// full-document cache budget. Reader callbacks never run under these locks.
	agentDefaultMu     sync.Mutex
	agentDefault       *agentDefaultSnapshot
	agentsCacheMu      sync.Mutex
	agentsLoadMu       sync.Mutex
	agentsCache        *agentSnapshot
	agentsIssueHead    store.Head
	agentsIssueHeadSet bool
	agentLoads         atomic.Int64
}

// Open is ProfileRepository(data_dir, default_config_factory,
// initial_config_factory): it creates the directory layout and, under
// .config.lock, checks and loads the database, or creates it for an empty
// data directory.
//
// Unlike Python it never migrates pre-SQLite data: a data directory with
// legacy configuration files and no configflow.sqlite3 is refused, and a
// corrupt database is never rebuilt from config.json.
func Open(dataDir string, options Options) (*Repository, error) {
	root, err := ResolveDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	r := &Repository{
		dataDir:       root,
		profilesDir:   filepath.Join(root, "profiles"),
		dbPath:        filepath.Join(root, DatabaseFilename),
		migrationsDir: filepath.Join(root, "migrations"),
		lockFile:      filepath.Join(root, ".config.lock"),
		options:       options,
		cacheMaxBytes: DocumentCacheMaxBytes,
	}
	r.store = store.New(r.dbPath, LockTimeout)
	if err := os.MkdirAll(r.dataDir, 0o777); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(r.profilesDir, 0o777); err != nil {
		return nil, err
	}
	err = withLock(context.Background(), r.lockFile, r.openLocked)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ResolveDataDir expands a home-directory alias and resolves the same absolute
// path Open uses, without opening or creating a repository. The server resolves
// its directory before taking instance locks so every alias shares ownership.
func ResolveDataDir(path string) (string, error) {
	expanded, err := expandUser(path)
	if err != nil {
		return "", err
	}
	return resolve(expanded)
}

// expandUser is Path.expanduser() of CPython 3.11: "~" is $HOME (or the
// password database entry of the current user), "~name" that user's home
// directory; when it cannot be determined Python raises RuntimeError.
func expandUser(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	name, rest, _ := strings.Cut(path[1:], "/")
	home, found := "", false
	if name == "" {
		if value, ok := os.LookupEnv("HOME"); ok {
			home, found = value, true
		} else if current, err := user.Current(); err == nil {
			home, found = current.HomeDir, true
		}
	} else if account, err := user.Lookup(name); err == nil {
		home, found = account.HomeDir, true
	}
	if home = strings.TrimRight(home, "/"); home == "" {
		home = "/"
	}
	if !found || strings.HasPrefix(home, "~") {
		return "", errors.New("Could not determine home directory.")
	}
	return filepath.Join(home, rest), nil
}

// OpenDefault opens the application's repository, as common/config.py's
// get_repository() does: GetDefaultConfig and GetInitialConfig as factories.
func OpenDefault(dataDir string) (*Repository, error) {
	return Open(dataDir, Options{DefaultSettings: DefaultConfig, InitialSettings: InitialConfig})
}

// DataDir returns the resolved data directory.
func (r *Repository) DataDir() string { return r.dataDir }

// ProfilesDir returns DATA_DIR/profiles.
func (r *Repository) ProfilesDir() string { return r.profilesDir }

// DatabasePath returns DATA_DIR/configflow.sqlite3.
func (r *Repository) DatabasePath() string { return r.dbPath }

// MigrationsDir returns DATA_DIR/migrations (snapshots and deleted profiles).
func (r *Repository) MigrationsDir() string { return r.migrationsDir }

// Store returns the storage engine, for fingerprints and meta reads. Writes
// must go through the repository.
func (r *Repository) Store() *store.Store { return r.store }

// CommitCount is the number of write transactions this process committed.
func (r *Repository) CommitCount() int64 { return r.commitCount.Load() }

// nowISO is _now(): UTC, ISO 8601 with microseconds (omitted when zero), "Z".
// It is a variable so tests can fix the time (Python: a patched _now).
var nowISO = func() string {
	now := time.Now().UTC()
	text := now.Format("2006-01-02T15:04:05")
	if micro := now.Nanosecond() / 1000; micro != 0 {
		text += "." + leftPad(strconv.Itoa(micro), 6)
	}
	return text + "Z"
}

func leftPad(text string, width int) string {
	for len(text) < width {
		text = "0" + text
	}
	return text
}

func lexists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// pathExists is Path.exists(): symbolic links are followed, so a dangling
// or looping link does not exist.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// legacyData names the first pre-SQLite configuration file found in the data
// directory, or "".
func (r *Repository) legacyData() string {
	for _, name := range []string{"config.json", "config.json.bak", "system.json", "system.json.bak"} {
		if lexists(filepath.Join(r.dataDir, name)) {
			return name
		}
	}
	for _, pattern := range []string{"*/config.json", "*/config.json.bak"} {
		if matches, _ := filepath.Glob(filepath.Join(r.profilesDir, pattern)); len(matches) > 0 {
			sort.Strings(matches)
			relative, _ := filepath.Rel(r.dataDir, matches[0])
			return relative
		}
	}
	return ""
}

// earlierDatabases is _earlier_databases: damaged copies and snapshots that
// show a database existed here before.
func (r *Repository) earlierDatabases() []string {
	paths, _ := filepath.Glob(r.dbPath + ".corrupt-*")
	sort.Strings(paths)
	if info, err := os.Stat(r.migrationsDir); err == nil && info.IsDir() {
		snapshots, _ := filepath.Glob(filepath.Join(r.migrationsDir, "*", "*", DatabaseFilename))
		sort.Strings(snapshots)
		if len(snapshots) > 3 {
			snapshots = snapshots[len(snapshots)-3:]
		}
		paths = append(paths, snapshots...)
	}
	var result []string
	for _, path := range paths {
		if strings.HasSuffix(filepath.Base(path), "-journal") {
			continue
		}
		relative, _ := filepath.Rel(r.dataDir, path)
		result = append(result, relative)
	}
	return result
}

// openLocked is _open_locked: startup under .config.lock.
func (r *Repository) openLocked(ctx context.Context) error {
	r.store.DiscardStaleTemporaries()
	info, err := os.Lstat(r.dbPath)
	if err == nil && !info.Mode().IsRegular() {
		return validationError(DatabaseFilename + " 必须是普通文件")
	}
	if err != nil {
		if legacy := r.legacyData(); legacy != "" {
			return repositoryError("数据目录中有旧版配置文件（" + legacy + "），但没有 " + DatabaseFilename +
				"。此版本不再迁移 SQLite 之前的旧格式：请先升级到最后一个 Python 版本的 ConfigFlow 并启动一次完成迁移，" +
				"再升级到此版本。数据目录未改动。")
		}
		journal := r.dbPath + "-journal"
		if pathExists(journal) {
			// Never apply a journal left by another file to a new database.
			if err := os.Rename(journal, journal+".orphan-"+strconv.FormatInt(time.Now().UnixNano(), 10)); err != nil {
				return err
			}
		}
		if earlier := r.earlierDatabases(); len(earlier) > 0 {
			logger.Error(DatabaseFilename + " 不存在，但数据目录里有它以前的副本（" + strings.Join(earlier, "、") +
				"）；将从旧配置文件重新迁移，之后的修改不在迁移结果中，请核对并按需从这些副本恢复")
		}
		if err := r.createFreshLocked(); err != nil {
			return err
		}
	} else if err := r.store.Check(); err != nil {
		switch {
		case errors.Is(err, store.ErrUnsupported):
			return &Error{Kind: ErrRepository, Cause: err, Message: "配置数据库由更新版本的 ConfigFlow 创建（" + err.Error() +
				"），当前版本无法读取；请升级到对应版本，或用“导出全部数据”的备份恢复。数据库未改动。"}
		case errors.Is(err, store.ErrCorrupt):
			return &Error{Kind: ErrRepository, Cause: err, Message: fmt.Sprintf(msgStoreRecovery, r.dbPath, err.Error())}
		default:
			return &store.Error{Kind: store.ErrStorage, Cause: err,
				Message: "无法打开配置数据库 " + DatabaseFilename + "：" + err.Error() + "；数据库未改动"}
		}
	}
	entry, err := r.readEntry(ctx, true)
	if err != nil {
		if errors.Is(err, store.ErrStorage) {
			return err
		}
		return &Error{Kind: ErrRepository, Cause: err, Message: fmt.Sprintf(msgStoreRecovery, r.dbPath, err.Error())}
	}
	// Token initialization is an optional write, not a load prerequisite.
	// R5 permits stored business-invalid values to be read and repaired; do
	// not let startup maintenance turn them into a failed Open (or rewrite
	// malformed settings). The eventual repairing transaction ensures tokens.
	if err := validateDocument(entry.Model); err != nil {
		return nil
	}
	system, err := copyRecord(&entry.Model.System)
	if err != nil {
		return err
	}
	if changed, err := ensureSystemTokens(system); err != nil {
		return err
	} else if changed {
		return r.transactLocked(ctx, writeOptions{}, func(context.Context, *transaction) error { return nil })
	}
	return nil
}

// createFreshLocked is _migrate_to_store_locked for a data directory without
// any earlier configuration: the database is built from the initial config.
func (r *Repository) createFreshLocked() error {
	raw, err := r.initialSettingsJSON()
	if err != nil {
		return err
	}
	typed, err := r.convertImportJSON(raw, nil)
	if err != nil {
		return err
	}
	if _, err := ensureSystemTokens(&typed.System); err != nil {
		return err
	}
	if err := validateDocument(typed); err != nil {
		return err
	}
	// _migrate_raw_cache: the shared cache directories exist from the start.
	for _, kind := range []string{"subscribes", "rules"} {
		target, err := r.SharedPath(kind)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(target, 0o777); err != nil {
			return err
		}
	}
	now := nowISO()
	revisions := map[string]any{"system": typed.System.Revision, "shared": typed.Shared.Revision, "profiles": map[string]model.Int{}}
	for _, profile := range typed.Profiles {
		revisions["profiles"].(map[string]model.Int)[profile.Key()] = profile.Revision
	}
	migration, _ := json.Marshal(map[string]any{"source": "defaults", "at": now, "snapshot": nil})
	legacy, _ := json.Marshal(map[string]any{"sha256": nil, "size": nil, "revisions": revisions, "content": nil, "status": "migrated", "at": now, "data_source": false})
	meta := map[string]json.RawMessage{"migration": migration, "legacy_config": legacy}
	entry, err := r.createStore(typed, meta)
	if err != nil {
		return err
	}
	r.setCache(entry)
	return nil
}

func (r *Repository) cached() *storedEntry {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	return r.documentCache
}

// setCache keeps the latest loaded or committed entry: every read compares
// its head with the database, so an older one only costs a reload.
func (r *Repository) setCache(entry storedEntry) {
	r.reportModelIssues(entry)
	if entry.Size > r.cacheMaxBytes {
		return
	}
	r.cacheMu.Lock()
	r.documentCache = &entry
	r.cacheMu.Unlock()
}

func (r *Repository) dropCache() {
	r.cacheMu.Lock()
	r.documentCache = nil
	r.cacheMu.Unlock()
}

// loadEntry is _load_entry: head and rows from one snapshot; the cached
// generation is returned when the database head still equals it.
func (r *Repository) loadEntry() (storedEntry, error) {
	var entry storedEntry
	hit := false
	err := r.store.Reading(func(connection *store.Conn) error {
		head, err := connection.Head()
		if err != nil {
			return err
		}
		if cached := r.cached(); cached != nil && cached.Head == head {
			entry, hit = *cached, true
			return nil
		}
		r.loads.Add(1)
		raw, failure := connection.Load()
		err = failure
		if err == nil {
			entry, err = decodeEntry(raw)
		}
		if err == nil && r.afterLoad != nil {
			r.afterLoad()
		}
		return err
	})
	if err != nil || hit {
		return entry, err
	}
	r.reportModelIssues(entry)
	r.validations.Add(1)
	if err := validateStored(entry.Model); err != nil {
		return storedEntry{}, err
	}
	r.setCache(entry)
	return entry, nil
}

const conflictMessage = "配置数据库在事务外被修改，已放弃本次写入，请重试"

// readEntry is _read_entry: the current validated document. Reads never
// need .config.lock; a failed read is retried once serialized with writers
// (network filesystems may not honour SQLite's own locks).
func (r *Repository) readEntry(ctx context.Context, locked bool) (storedEntry, error) {
	if snapshot := r.lockedSnapshot(ctx); snapshot != nil {
		return snapshot.entry, nil
	}
	entry, err := r.loadEntry()
	if err != nil && !locked && !holdsLock(ctx, r.lockFile) {
		err = withLock(ctx, r.lockFile, func(context.Context) error {
			var failure error
			entry, failure = r.loadEntry()
			return failure
		})
	}
	switch {
	case err == nil:
		return entry, nil
	case errors.Is(err, store.ErrConflict):
		return storedEntry{}, &Error{Kind: ErrRepository, Message: conflictMessage, Cause: err}
	case errors.Is(err, store.ErrStore):
		return storedEntry{}, &Error{Kind: ErrRepository, Message: "配置数据库内容无效：" + err.Error(), Cause: err}
	}
	return storedEntry{}, err
}

// Head is head(): the identity of the committed generation; every commit,
// by any process, changes it.
func (r *Repository) Head(ctx context.Context) (store.Head, error) {
	entry, err := r.readEntry(ctx, false)
	return entry.Head, err
}
