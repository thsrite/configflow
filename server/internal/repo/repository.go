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

	"configflow/internal/jsonx"
	"configflow/internal/logx"
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
	// DefaultConfig is default_config_factory: defaults merged over the
	// built-in profile defaults for new profiles and old-format imports.
	DefaultConfig func() *jsonx.Object
	// InitialConfig is initial_config_factory: the single-profile document
	// a fresh data directory and ResetAll start from. Nil: the defaults.
	InitialConfig func() *jsonx.Object
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
	cacheMu       sync.Mutex
	documentCache *storedEntry
	tokensCache   atomic.Pointer[sanitizationTokens]
	// commitCount is the number of write transactions this process
	// committed; request-scoped profile validation is invalidated by it.
	commitCount atomic.Int64

	// Test seams: the number of full loads and of validations of loaded
	// documents, a callback after a full load (inside the read
	// transaction), and the cache size limit.
	loads, validations, metadataReads atomic.Int64
	afterLoad                         func()
	cacheMaxBytes                     int64
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
	expanded, err := expandUser(dataDir)
	if err != nil {
		return nil, err
	}
	root, err := resolve(expanded)
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
	return Open(dataDir, Options{DefaultConfig: GetDefaultConfig, InitialConfig: GetInitialConfig})
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

// legacyDefaults is _legacy_defaults().
func (r *Repository) legacyDefaults() *jsonx.Object {
	override := jsonx.NewObject()
	if r.options.DefaultConfig != nil {
		override = r.options.DefaultConfig()
	}
	return DeepMerge(defaultProfileConfig(), override)
}

func (r *Repository) initialConfig() *jsonx.Object {
	if r.options.InitialConfig != nil {
		return r.options.InitialConfig()
	}
	return r.legacyDefaults()
}

// emptyProfile is _empty_profile.
func (r *Repository) emptyProfile(profileID string, name any, description any) *jsonx.Object {
	defaults := r.legacyDefaults()
	result := jsonx.NewObject()
	for _, key := range ProfileFields {
		if value, ok := defaults.Get(key); ok {
			result.Set(key, jsonx.DeepCopy(value))
		} else if profileObjectFields[key] {
			result.Set(key, jsonx.NewObject())
		} else {
			result.Set(key, []any{})
		}
	}
	result.Set("id", profileID)
	result.Set("name", name)
	result.Set("description", description)
	result.Set("_revision", jsonx.Int(0))
	result.Set("created_at", nowISO())
	result.Set("updated_at", nowISO())
	return result
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
	system, _ := jsonx.AsObject(jsonx.DeepCopy(entry.Document.Value("system")))
	if changed, err := ensureRuleProxyToken(system); err != nil {
		return err
	} else if changed {
		return r.writeLocked(ctx, writeOptions{}, func(context.Context, *jsonx.Object, *jsonx.Object) error { return nil })
	}
	return nil
}

func revisionsOf(document *jsonx.Object) *jsonx.Object {
	profiles := jsonx.NewObject()
	for key, value := range document.GetObject("profiles").All() {
		profile, _ := jsonx.AsObject(value)
		profiles.Set(key, profile.GetOr("_revision", jsonx.Int(0)))
	}
	return jsonx.Obj(
		"system", document.GetObject("system").GetOr("_revision", jsonx.Int(0)),
		"shared", document.GetObject("shared").GetOr("_revision", jsonx.Int(0)),
		"profiles", profiles)
}

// createFreshLocked is _migrate_to_store_locked for a data directory without
// any earlier configuration: the database is built from the initial config.
func (r *Repository) createFreshLocked() error {
	document, err := r.convertImport(r.initialConfig(), nil)
	if err != nil {
		return err
	}
	if _, err := ensureRuleProxyToken(document.GetObject("system")); err != nil {
		return err
	}
	if err := ValidateDocument(document); err != nil {
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
	meta := jsonx.Obj(
		"migration", jsonx.Obj("source", "defaults", "at", now, "snapshot", nil),
		"legacy_config", jsonx.Obj("sha256", nil, "size", nil, "revisions", revisionsOf(document),
			"content", nil, "status", "migrated", "at", now, "data_source", false))
	entry, err := r.createStore(document, meta)
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
	r.validations.Add(1)
	if err := ValidateDocument(entry.Document); err != nil {
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

type txKey struct{ repository *Repository }

// working returns the working copy of the write transaction ctx belongs to.
func (r *Repository) working(ctx context.Context) *jsonx.Object {
	document, _ := ctx.Value(txKey{r}).(*jsonx.Object)
	return document
}

// InTransaction reports whether ctx belongs to an open write transaction of
// this repository (Python: repository._local.document is not None).
func (r *Repository) InTransaction(ctx context.Context) bool { return r.working(ctx) != nil }

const nestedWriteMessage = "不能在配置事务中再次开启写事务"

// view is `with self._document() as document`: fn reads the transaction's
// working copy when ctx is inside one, the committed generation otherwise.
// fn must not modify the document or keep references to it.
func (r *Repository) view(ctx context.Context, fn func(document *jsonx.Object) error) error {
	if document := r.working(ctx); document != nil {
		return fn(document)
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return err
	}
	return fn(entry.Document)
}

// committedSource is _committed_source: the document and whether it is a
// committed generation (which is never mutated and may be kept as the source
// of a three-way merge baseline) rather than a transaction's working copy.
func (r *Repository) committedSource(ctx context.Context) (document *jsonx.Object, committed bool, err error) {
	if document := r.working(ctx); document != nil {
		return document, false, nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, false, err
	}
	return entry.Document, true, nil
}

type writeOptions struct {
	lockedRouters map[routerKey]bool
	policyRouter  *routerKey
	meta          *jsonx.Object
}

// update is `with self._document(write=True, ...) as document`: one write
// transaction under .config.lock. fn mutates document; a nil return commits,
// an error discards everything. After the commit and the lock release the
// ConfigurationCommitted hook runs when auto backup is on.
func (r *Repository) update(ctx context.Context, options writeOptions, fn func(ctx context.Context, document *jsonx.Object) error) error {
	if r.working(ctx) != nil {
		return inUse(nestedWriteMessage)
	}
	var previous, current *jsonx.Object
	err := withLock(ctx, r.lockFile, func(ctx context.Context) error {
		return r.writeLocked(ctx, options, func(ctx context.Context, before, document *jsonx.Object) error {
			previous, current = before, document
			return fn(ctx, document)
		})
	})
	if err != nil {
		return err
	}
	if jsonx.Truthy(current.GetObject("system").GetObject("backup").Value("auto_backup")) {
		if committed := currentHooks().ConfigurationCommitted; committed != nil {
			committed(r, previous, current)
		}
	}
	return nil
}

// writeLocked is _write_locked: one write transaction; the caller holds
// .config.lock. fn receives the committed generation and the copy to
// mutate. Only rows that changed are written, in one SQLite transaction.
func (r *Repository) writeLocked(ctx context.Context, options writeOptions,
	fn func(ctx context.Context, previous, document *jsonx.Object) error) error {
	if r.working(ctx) != nil {
		return inUse(nestedWriteMessage)
	}
	base, err := r.readEntry(ctx, true)
	if err != nil {
		return err
	}
	previous := base.Document
	document := previous.DeepCopy()
	if err := fn(context.WithValue(ctx, txKey{r}, document), previous, document); err != nil {
		return err
	}
	system, err := compatObject(document.Value("system"))
	if err != nil {
		return err
	}
	if _, err := ensureRuleProxyToken(system); err != nil {
		return err
	}
	if err := ValidateDocument(document); err != nil {
		return err
	}
	release, err := r.routerCommitGuard(previous, document, options.lockedRouters, options.policyRouter)
	if err != nil {
		return err
	}
	defer release()
	committed, changed, err := r.commitStore(base, document, options.meta)
	if err != nil {
		r.dropCache()
		if errors.Is(err, store.ErrConflict) {
			return &Error{Kind: ErrRepository, Message: conflictMessage, Cause: err}
		}
		return err
	}
	if changed {
		r.setCache(committed)
		r.commitCount.Add(1)
	}
	return nil
}

var manifestLabels = [][2]string{{"ros_policy_state", "策略路由"}, {"dns_guard_state", "强制 DNS 规则"},
	{"ipv6_fakeip_state", "IPv6 Fake-IP 路由"}, {"ipv6_routes_state", "IPv6 规则集路由"},
	{"ipv6_pd_guard_state", "IPv6 前缀变化保护"}}

func fieldsDiffer(old, updated *jsonx.Object, fields []string) bool {
	if updated == nil {
		return true
	}
	for _, field := range fields {
		if !jsonx.Equal(old.Value(field), updated.Value(field)) {
			return true
		}
	}
	return false
}

// routerCommitGuard is _router_commit_guard: it protects device ownership
// under the document lock, through COMMIT. It refuses changes that would
// orphan a published ownership record and takes the JobLock of every router
// whose connection or manifest changes; release frees those locks.
func (r *Repository) routerCommitGuard(previous, proposed *jsonx.Object, lockedRouters map[routerKey]bool,
	policyRouter *routerKey) (release func(), err error) {
	oldRouters, err := routerRecordsOf(previous)
	if err != nil {
		return nil, err
	}
	newRouters, err := routerRecordsOf(proposed)
	if err != nil {
		return nil, err
	}
	var changed []routerKey
	for _, key := range oldRouters.keys {
		old := oldRouters.values[key]
		updated := newRouters.values[key]
		connectionChanged := fieldsDiffer(old, updated, RouterConnectionFields)
		identityChanged := fieldsDiffer(old, updated, RouterIdentityFields)
		manifestChanged := fieldsDiffer(old, updated, RouterManifestFields)
		// Credentials may have expired on the same device. Keep its exact
		// ownership receipt while allowing access to be repaired; every
		// connection change still takes the publication lock below.
		for _, manifest := range manifestLabels {
			fieldChanged := updated == nil || !jsonx.Equal(old.Value(manifest[0]), updated.Value(manifest[0]))
			isPolicyRouter := policyRouter != nil && *policyRouter == key
			if jsonx.Truthy(old.Value(manifest[0])) && (identityChanged || (fieldChanged && !isPolicyRouter)) {
				profile := previous.GetObject("profiles").GetObject(key.ProfileID)
				return nil, inUse("路由器仍有已发布的"+manifest[1]+"；请先发布并核对撤回，再修改类型、地址或删除发布记录",
					jsonx.Obj("kind", "router", "id", key.RouterID, "name", old.GetOr("name", key.RouterID),
						"profile_id", key.ProfileID, "profile_name", profile.Value("name")))
			}
		}
		if connectionChanged || manifestChanged {
			changed = append(changed, key)
		}
	}
	// Never wait for a job while holding the document lock: a publisher may
	// itself be waiting to persist its readback. A failed acquisition rejects
	// the whole transaction, including first publication without a manifest.
	var releases []func()
	release = func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}
	jobLock := currentHooks().JobLock
	connections := map[[2]string]bool{}
	for _, key := range changed {
		if lockedRouters[key] {
			continue
		}
		router := oldRouters.values[key]
		endpoint := ""
		if value := router.Value("endpoint"); jsonx.Truthy(value) {
			endpoint = jsonx.Str(value)
		}
		connection := [2]string{jsonx.Repr(router.Value("type")), strings.TrimRight(endpoint, "/")}
		if connections[connection] {
			continue
		}
		if jobLock != nil {
			unlock, acquired := jobLock(key.ProfileID, key.RouterID, router)
			if !acquired {
				release()
				return nil, inUse("路由器正在发布或同步，不能替换连接或删除记录，请稍后重试")
			}
			if unlock != nil {
				releases = append(releases, unlock)
			}
		}
		connections[connection] = true
	}
	return release, nil
}

// Head is head(): the identity of the committed generation; every commit,
// by any process, changes it.
func (r *Repository) Head(ctx context.Context) (store.Head, error) {
	entry, err := r.readEntry(ctx, false)
	return entry.Head, err
}
