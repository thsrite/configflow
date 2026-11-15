// Package store is the SQLite storage engine for the configuration
// document, a port of backend/common/config_store.py.
//
// The repository keeps one JSON-shaped document in memory. Here it is
// stored in configflow.sqlite3 as one row per (scope, owner, field): root
// keys besides the sections, "system" and "shared" fields, and each
// profile's fields, with the positions that make the document's key order
// reproducible. Databases written by this package and by the Python module
// are interchangeable: same schema, pragmas, row contents and revisions.
//
// A Store opens a new connection for every operation and holds no state
// besides its path, so it may be used from several goroutines; writers are
// serialised by the repository's lock and by SQLite itself. The package
// never interprets the document beyond splitting it into rows.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/jsonx"

	"modernc.org/sqlite"
)

const (
	// Filename is the name of the database file inside the data directory.
	Filename = "configflow.sqlite3"
	// StorageVersion is the newest storage version this code reads.
	StorageVersion = 1
	// ApplicationID is the SQLite application_id of a store ("CFLW").
	ApplicationID = 0x43464C57
)

var requiredMeta = []string{"storage_version", "store_id", "generation", "root_order"}

var schema = []string{
	"CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID",
	`CREATE TABLE sections (
        scope TEXT NOT NULL CHECK (scope IN ('root', 'system', 'shared', 'profile')),
        owner TEXT NOT NULL,
        position INTEGER NOT NULL,
        PRIMARY KEY (scope, owner),
        CHECK ((scope = 'profile') = (owner <> ''))
    ) WITHOUT ROWID`,
	`CREATE TABLE fields (
        scope TEXT NOT NULL,
        owner TEXT NOT NULL,
        field TEXT NOT NULL,
        position INTEGER NOT NULL,
        value TEXT NOT NULL,
        revision INTEGER NOT NULL CHECK (revision >= 1),
        PRIMARY KEY (scope, owner, field),
        FOREIGN KEY (scope, owner) REFERENCES sections (scope, owner) ON DELETE CASCADE
    ) WITHOUT ROWID`,
}

// Head identifies one committed state of a database. Heads are comparable
// with ==. CommitID is random per committed generation, so a restored older
// database that reaches the same generation number again never equals a
// cached head; it is "" for a database that has none (Python: None).
type Head struct {
	StoreID    string
	Generation int64
	CommitID   string
}

// Entry is a document as stored at Head. Document must be treated as
// immutable: entries returned by Commit share unchanged values with the
// entry they were based on. Size is the total length of the stored field
// values in characters.
type Entry struct {
	Head     Head
	Document *jsonx.Object
	Size     int64
}

// Fingerprint identifies the exact contents of a database: Digest is a
// SHA-256 over every row of every table. Comparable with ==; tests use it
// to assert "nothing was written". It is identical to the Python
// ConfigStore.fingerprint() of the same file.
type Fingerprint struct {
	StoreID    string
	Generation int64
	Digest     string
}

// Store is one SQLite file.
type Store struct {
	path    string
	timeout func() time.Duration

	// BeforeCommit, when set, runs inside Commit after every statement of
	// the transaction has been executed and before COMMIT. An error rolls
	// the transaction back and is returned by Commit unchanged. It is the
	// seam for failure injection and for observing the open write
	// transaction (ConfigStore._apply and ._commit in the Python tests);
	// production code leaves it nil.
	BeforeCommit func() error
}

// New returns the store for the database file at path. timeout is asked
// before every connection for how long to wait on a locked database.
func New(path string, timeout func() time.Duration) *Store {
	return &Store{path: path, timeout: timeout}
}

// Path returns the database file.
func (s *Store) Path() string { return s.path }

func (s *Store) connect(readonly bool) (*Conn, error) {
	return connect(s.path, readonly, s.timeout())
}

// Reading runs fn inside one deferred read transaction: everything fn reads
// through the connection is consistent. SQLite failures are returned as
// ErrStorage; errors returned by fn pass through.
func (s *Store) Reading(fn func(*Conn) error) error {
	connection, err := s.connect(false)
	if err != nil {
		return translate(err)
	}
	defer connection.close()
	if err := connection.begin("BEGIN"); err != nil {
		return translate(err)
	}
	if err := fn(connection); err != nil {
		return translate(err)
	}
	return translate(connection.commit())
}

// Meta returns the parsed value of one meta key, or nil when it is missing.
func (s *Store) Meta(key string) (value any, err error) {
	err = s.Reading(func(connection *Conn) error {
		value, err = connection.Meta(key)
		return err
	})
	return value, err
}

// Load is a fresh read of the whole document, never cached.
func (s *Store) Load() (entry Entry, err error) {
	err = s.Reading(func(connection *Conn) error {
		entry, err = connection.Load()
		return err
	})
	return entry, err
}

// Fingerprint returns the store id, the generation and a digest of every
// row.
func (s *Store) Fingerprint() (Fingerprint, error) {
	digest := sha256.New()
	var head Head
	row := func(values ...any) error {
		// json.dumps(row, ensure_ascii=False) of the row tuple.
		text, err := jsonx.Encode(values, jsonx.Options{})
		digest.Write(text)
		return err
	}
	err := s.Reading(func(connection *Conn) (err error) {
		if head, err = connection.Head(); err != nil {
			return err
		}
		digest.Write([]byte("meta"))
		err = connection.query("SELECT * FROM meta ORDER BY key", nil, func(rows *sql.Rows) error {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				return err
			}
			if err := checkText(metaColumns, key, value); err != nil {
				return err
			}
			return row(key, value)
		})
		if err != nil {
			return err
		}
		digest.Write([]byte("sections"))
		err = connection.query("SELECT * FROM sections ORDER BY scope, owner", nil, func(rows *sql.Rows) error {
			var scope, owner string
			var position int64
			if err := rows.Scan(&scope, &owner, &position); err != nil {
				return err
			}
			if err := checkText(sectionColumns, scope, owner); err != nil {
				return err
			}
			return row(scope, owner, position)
		})
		if err != nil {
			return err
		}
		digest.Write([]byte("fields"))
		return connection.query("SELECT * FROM fields ORDER BY scope, owner, field", nil, func(rows *sql.Rows) error {
			var scope, owner, field, value string
			var position, revision int64
			if err := rows.Scan(&scope, &owner, &field, &position, &value, &revision); err != nil {
				return err
			}
			if err := checkText(fieldColumns, scope, owner, field, value); err != nil {
				return err
			}
			return row(scope, owner, field, position, value, revision)
		})
	})
	if err != nil {
		return Fingerprint{}, err
	}
	return Fingerprint{StoreID: head.StoreID, Generation: head.Generation, Digest: hex.EncodeToString(digest.Sum(nil))}, nil
}

// Commit writes the rows in which document differs from the database, and
// the given meta keys (nil for none), in one transaction.
//
// base is the entry the caller derived document from. If the database head
// is no longer base.Head the commit fails with ErrConflict and nothing is
// written. When rows differ, the generation is incremented, a new commit id
// is stored and the changed fields get the new generation as revision. A
// commit that only sets meta keys keeps the head. When neither the document
// nor meta changes anything, nothing is written and committed is false.
//
// A document that cannot be stored fails with ErrDocument before anything
// is written; SQLite failures are ErrStorage.
func (s *Store) Commit(base Entry, document, meta *jsonx.Object) (entry Entry, committed bool, err error) {
	// Serialize first: a non-JSON value must fail before anything is written.
	target, err := documentRows(document)
	if err != nil {
		return Entry{}, false, err
	}
	meta = meta.Clone()
	for _, value := range meta.Values() {
		if _, err := dumps(value); err != nil {
			return Entry{}, false, err
		}
	}
	connection, err := s.connect(false)
	if err != nil {
		return Entry{}, false, translate(err)
	}
	defer connection.close()

	var head Head
	var storedFields *ordered[fieldKey, fieldRow]
	err = func() error {
		if err := connection.begin("BEGIN IMMEDIATE"); err != nil {
			return err
		}
		if head, err = connection.Head(); err != nil {
			return err
		}
		if head != base.Head {
			return newError(ErrConflict, "配置数据库在事务外被修改")
		}
		storedRootOrder, err := connection.Meta("root_order")
		if err != nil {
			return err
		}
		storedSections, fields, err := connection.storedRows()
		if err != nil {
			return err
		}
		storedFields = fields
		pending := diff(storedRootOrder, storedSections, storedFields, target)
		changed := pending.any()
		if !changed && meta.Len() == 0 {
			connection.rollback()
			return nil
		}
		committed = true
		if changed {
			head.Generation++
			head.CommitID = newUUIDHex()
			meta.Set("commit_id", head.CommitID)
		}
		if err := connection.apply(pending, target, head.Generation, meta); err != nil {
			return err
		}
		if s.BeforeCommit != nil {
			if err := s.BeforeCommit(); err != nil {
				return err
			}
		}
		return connection.commit()
	}()
	if err != nil {
		return Entry{}, false, translate(err)
	}
	if !committed {
		return Entry{}, false, nil
	}
	// The new entry: unchanged rows reuse the previous (immutable) values,
	// changed rows are parsed from the text already written.
	parsed := newOrdered[fieldKey, parsedField]()
	size := int64(0)
	for _, key := range target.fields.keys {
		field := target.fields.values[key]
		size += textLength(field.value)
		var value any
		reused := false
		if stored, ok := storedFields.get(key); ok && stored.value == field.value {
			value, reused = lookup(base.Document, key)
		}
		if !reused {
			if value, err = jsonx.ParseString(field.value); err != nil {
				return Entry{}, false, err
			}
		}
		parsed.set(key, parsedField{field.position, value})
	}
	built, err := buildDocument(target.rootOrder, target.sections, parsed)
	if err != nil {
		return Entry{}, false, err
	}
	return Entry{Head: head, Document: built, Size: size}, true, nil
}

// apply executes every statement of one commit.
func (c *Conn) apply(pending *changes, target *rows, generation int64, meta *jsonx.Object) error {
	for _, key := range pending.deleteSections {
		if err := c.exec("DELETE FROM sections WHERE scope = ? AND owner = ?", key.scope, key.owner); err != nil {
			return err
		}
	}
	for _, key := range pending.upsertSections {
		err := c.exec("INSERT INTO sections (scope, owner, position) VALUES (?, ?, ?) "+
			"ON CONFLICT (scope, owner) DO UPDATE SET position = excluded.position",
			key.scope, key.owner, target.sections.values[key])
		if err != nil {
			return err
		}
	}
	for _, key := range pending.deleteFields {
		err := c.exec("DELETE FROM fields WHERE scope = ? AND owner = ? AND field = ?", key.scope, key.owner, key.field)
		if err != nil {
			return err
		}
	}
	for _, key := range pending.upsertFields {
		field := target.fields.values[key]
		err := c.exec("INSERT INTO fields (scope, owner, field, position, value, revision) VALUES (?, ?, ?, ?, ?, ?) "+
			"ON CONFLICT (scope, owner, field) DO UPDATE SET position = excluded.position, "+
			"value = excluded.value, revision = excluded.revision",
			key.scope, key.owner, key.field, field.position, field.value, generation)
		if err != nil {
			return err
		}
	}
	updates := meta.Clone()
	if pending.rootOrder {
		updates.Set("root_order", jsonx.ListOf(target.rootOrder))
	}
	updates.Set("generation", generation)
	for key, value := range updates.All() {
		text, err := dumps(value)
		if err != nil {
			return err
		}
		if err := c.exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, text); err != nil {
			return err
		}
	}
	return nil
}

// -- building and checking the file ------------------------------------------

func fsyncPath(path string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func (s *Store) fsyncDir() error {
	return fsyncPath(filepath.Dir(s.path))
}

// BuildTemporary writes a complete, checked database holding document and
// the given meta keys (nil for none) into a new file next to the real one
// (mode 0600) and returns its path and the entry read back from it. The
// caller moves it into place with Install or removes it with
// DiscardTemporary; on error nothing is left behind.
func (s *Store) BuildTemporary(document, meta *jsonx.Object) (temporary string, entry Entry, err error) {
	target, err := documentRows(document)
	if err != nil {
		return "", Entry{}, err
	}
	handle, err := os.CreateTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return "", Entry{}, err
	}
	temporary = handle.Name()
	handle.Close()
	defer func() {
		if err != nil {
			DiscardTemporary(temporary)
			temporary = ""
		}
	}()
	if err = s.writeTemporary(temporary, target, meta); err != nil {
		return "", Entry{}, untranslated(err)
	}
	if entry, err = New(temporary, s.timeout).Load(); err != nil {
		return "", Entry{}, err
	}
	written, err := dumps(entry.Document)
	if err != nil {
		return "", Entry{}, err
	}
	if wanted, _ := dumps(document); written != wanted {
		return "", Entry{}, newError(ErrStore, "新建数据库的内容与迁移文档不一致")
	}
	if err = fsyncPath(temporary); err != nil {
		return "", Entry{}, err
	}
	return temporary, entry, nil
}

func (s *Store) writeTemporary(temporary string, target *rows, meta *jsonx.Object) error {
	connection, err := connect(temporary, false, s.timeout())
	if err != nil {
		return err
	}
	defer connection.close()
	mode, err := connection.strings1("PRAGMA journal_mode=DELETE")
	if err != nil {
		return err
	}
	if len(mode) != 1 || strings.ToLower(mode[0]) != "delete" {
		return newError(ErrStore, "无法设置数据库日志模式："+strings.Join(mode, ""))
	}
	if err := connection.exec("PRAGMA application_id=" + strconv.Itoa(ApplicationID)); err != nil {
		return err
	}
	if err := connection.begin("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	for _, statement := range schema {
		if err := connection.exec(statement); err != nil {
			return err
		}
	}
	values := jsonx.Obj("storage_version", StorageVersion, "store_id", newUUIDHex(), "generation", 1,
		"commit_id", newUUIDHex(), "root_order", jsonx.ListOf(target.rootOrder), "created_at", now())
	values.Update(meta)
	for key, value := range values.All() {
		text, err := dumps(value)
		if err != nil {
			return err
		}
		if err := connection.exec("INSERT INTO meta (key, value) VALUES (?, ?)", key, text); err != nil {
			return err
		}
	}
	for _, key := range target.sections.keys {
		err := connection.exec("INSERT INTO sections (scope, owner, position) VALUES (?, ?, ?)",
			key.scope, key.owner, target.sections.values[key])
		if err != nil {
			return err
		}
	}
	for _, key := range target.fields.keys {
		field := target.fields.values[key]
		err := connection.exec("INSERT INTO fields (scope, owner, field, position, value, revision) VALUES (?, ?, ?, ?, ?, 1)",
			key.scope, key.owner, key.field, field.position, field.value)
		if err != nil {
			return err
		}
	}
	if err := connection.commit(); err != nil {
		return err
	}
	result, err := connection.strings1("PRAGMA quick_check")
	if err != nil {
		return err
	}
	if len(result) != 1 || result[0] != "ok" {
		return newError(ErrStore, "新建数据库未通过完整性检查："+rowsRepr(result))
	}
	return nil
}

// rowsRepr is repr(cursor.fetchall()) for one-column text rows:
// [('a',), ('b',)].
func rowsRepr(values []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(" + jsonx.Repr(value) + ",)")
	}
	b.WriteByte(']')
	return b.String()
}

// DiscardTemporary removes a file made by BuildTemporary and its journal.
func DiscardTemporary(temporary string) {
	os.Remove(temporary)
	os.Remove(temporary + "-journal")
}

// Install atomically moves a file made by BuildTemporary into place and
// syncs the directory.
func (s *Store) Install(temporary string) error {
	if err := os.Rename(temporary, s.path); err != nil {
		return err
	}
	return s.fsyncDir()
}

// Create builds the database for document (with the given meta keys, nil
// for none) in a temporary file and atomically moves it into place,
// replacing any existing file.
func (s *Store) Create(document, meta *jsonx.Object) (Entry, error) {
	temporary, entry, err := s.BuildTemporary(document, meta)
	if err != nil {
		return Entry{}, err
	}
	if err := s.Install(temporary); err != nil {
		DiscardTemporary(temporary)
		return Entry{}, err
	}
	return entry, nil
}

// Check verifies an existing file: quick_check, tables, required meta keys
// and the storage version; a journal mode other than DELETE is reset.
//
// It returns ErrCorrupt for a damaged file or one that is not a store,
// ErrUnsupported for a newer storage version, ErrStore when the journal
// mode cannot be reset, and ErrSQLite for any other SQLite failure (for
// example a locked database), which says nothing about the file's state.
func (s *Store) Check() error {
	err := s.check()
	if isCorrupt(err) {
		return &Error{Kind: ErrCorrupt, Message: sqliteMessage(err), Cause: err}
	}
	return untranslated(err)
}

func (s *Store) check() error {
	connection, err := s.connect(false)
	if err != nil {
		return err
	}
	defer connection.close()
	result, err := connection.strings1("PRAGMA quick_check")
	if err != nil {
		return err
	}
	if len(result) != 1 || result[0] != "ok" {
		return newError(ErrCorrupt, "完整性检查失败："+strings.Join(result[:min(len(result), 5)], "; "))
	}
	tables, err := connection.strings1("SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return err
	}
	var missing []string
	for _, name := range []string{"fields", "meta", "sections"} { // sorted
		found := false
		for _, table := range tables {
			found = found || table == name
		}
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return newError(ErrCorrupt, "缺少数据表："+strings.Join(missing, "、"))
	}
	meta, err := connection.metaRows()
	if err != nil {
		return err
	}
	missing = nil
	for _, key := range requiredMeta {
		if !meta.Has(key) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return newError(ErrCorrupt, "缺少数据库元信息："+strings.Join(missing, "、"))
	}
	version := meta.Value("storage_version")
	number, _ := jsonx.AsNumber(version)
	value, isInt := number.BigInt()
	if !isInt || value.Sign() < 1 {
		return newError(ErrCorrupt, "数据库存储版本无效："+jsonx.Repr(version))
	}
	if !value.IsInt64() || value.Int64() > StorageVersion {
		return newError(ErrUnsupported, "数据库存储版本 "+number.String()+" 高于当前支持的 "+strconv.Itoa(StorageVersion))
	}
	mode, err := connection.strings1("PRAGMA journal_mode")
	if err != nil {
		return err
	}
	if strings.ToLower(strings.Join(mode, "")) != "delete" {
		if mode, err = connection.strings1("PRAGMA journal_mode=DELETE"); err != nil {
			return err
		}
		if current := strings.ToLower(strings.Join(mode, "")); current != "delete" {
			return newError(ErrStore, "无法把数据库日志模式恢复为 DELETE："+current)
		}
	}
	return nil
}

// SalvageMeta is a best-effort read-only read of the meta table of a
// damaged file; it returns nil on any error.
func (s *Store) SalvageMeta() *jsonx.Object {
	connection, err := s.connect(true)
	if err != nil {
		return nil
	}
	defer connection.close()
	meta, err := connection.metaRows()
	if err != nil {
		return nil
	}
	return meta
}

// CorruptTarget returns a new name for setting the damaged database aside:
// "<path>.corrupt-<unix nanoseconds>".
func (s *Store) CorruptTarget() string {
	return s.path + ".corrupt-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

// PreserveCorrupt keeps the damaged file (and its hot journal) under the
// name target, which is CorruptTarget() when empty, and returns that name.
//
// The file itself stays in place until Install atomically replaces it: a
// crash in between leaves the damaged database, never a missing one. The
// journal is moved away, because it would be applied to the new file.
func (s *Store) PreserveCorrupt(target string) (string, error) {
	if target == "" {
		target = s.CorruptTarget()
	}
	if err := os.Link(s.path, target); err != nil { // filesystems without hard links (some NAS)
		if err := copyFile(s.path, target); err != nil {
			return "", err
		}
		if err := fsyncPath(target); err != nil {
			return "", err
		}
	}
	journal := s.path + "-journal"
	if _, err := os.Stat(journal); err == nil {
		if err := os.Rename(journal, target+"-journal"); err != nil {
			return "", err
		}
	}
	if err := s.fsyncDir(); err != nil {
		return "", err
	}
	return target, nil
}

// copyFile is shutil.copy2: copyfile (contents; a new file gets 0666 minus
// umask) followed by copystat, which stats the source after the copy and
// sets access and modification time, then the permission bits.
func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := os.Chtimes(target, accessTime(info), info.ModTime()); err != nil {
		return err
	}
	return os.Chmod(target, info.Mode().Perm())
}

// BackupTo writes a consistent copy of the live database to destination
// through the SQLite backup API. destination must not exist; it is created
// with mode 0600. SQLite failures are ErrStorage.
func (s *Store) BackupTo(destination string) error {
	handle, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	handle.Close()
	uri, err := fileURI(destination, false)
	if err != nil {
		return err
	}
	source, err := s.connect(false)
	if err != nil {
		return translate(err)
	}
	defer source.close()
	err = source.conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("store: the SQLite driver has no backup API")
		}
		backup, err := backuper.NewBackup(uri)
		if err != nil {
			return err
		}
		for {
			more, err := backup.Step(-1)
			if code := sqliteCode(err); code == sqliteBusy || code == sqliteLocked {
				// As sqlite3.Connection.backup does: wait and try again.
				time.Sleep(250 * time.Millisecond)
				continue
			}
			if err != nil {
				backup.Finish()
				return asBackupError(err)
			}
			if !more {
				return asBackupError(backup.Finish())
			}
		}
	})
	if err != nil {
		return translate(err)
	}
	return fsyncPath(destination)
}

// DiscardStaleTemporaries removes builds interrupted by a crash
// (".<name>.*.tmp" and their journals); callers hold the repository lock.
func (s *Store) DiscardStaleTemporaries() {
	directory := filepath.Dir(s.path)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	prefix := "." + filepath.Base(s.path) + "."
	for _, item := range entries {
		name := item.Name()
		if !strings.HasPrefix(name, prefix) || !item.Type().IsRegular() {
			continue
		}
		rest := name[len(prefix):] // "<random>.tmp"
		if strings.HasSuffix(rest, ".tmp") || strings.HasSuffix(rest, ".tmp-journal") {
			os.Remove(filepath.Join(directory, name))
		}
	}
}

// -- small helpers ---------------------------------------------------------

// textLength is Python's len(str): the number of code points.
func textLength(text string) int64 {
	return int64(utf8.RuneCountInString(text))
}

// newUUIDHex is uuid.uuid4().hex.
func newUUIDHex() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return hex.EncodeToString(raw[:])
}

// now is datetime.now(timezone.utc).isoformat() with "Z" for the offset:
// microseconds, left out entirely when they are zero.
func now() string {
	return formatTime(time.Now())
}

func formatTime(t time.Time) string {
	t = t.UTC().Truncate(time.Microsecond)
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	return t.Format("2006-01-02T15:04:05.000000Z")
}
