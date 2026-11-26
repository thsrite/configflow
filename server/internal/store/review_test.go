package store

// Regression tests added by the C01 adversarial review. Expected values are
// CPython 3.11 results of backend/common/config_store.py (generated on the
// test server; the same scenarios run live in TestCrossCheckPythonErrors).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Stored invalid UTF-8 is corrupt data, reported without its contents.
func TestInvalidUTF8TextIsCorrupt(t *testing.T) {
	tests := []struct {
		name, statement, key  string
		loadFails, checkFails bool
	}{
		{"value", "UPDATE fields SET value = CAST(X'5B22FF2041E4B8225D' AS TEXT) WHERE scope = 'system' AND field = 'agents'", "system//agents", true, false},
		{"meta", "UPDATE meta SET value = CAST(X'22F09F9822' AS TEXT) WHERE key = 'migration'", "migration", false, true},
		{"meta-head", "UPDATE meta SET value = CAST(X'22F09F9822' AS TEXT) WHERE key = 'store_id'", "store_id", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := newStore(t)
			must(target.Create(sampleDocument(t), testObj("migration", "seed")))(t)
			execSQL(t, target.Path(), tt.statement)
			check := func(err error, want bool) {
				t.Helper()
				if !want {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), tt.key) || !strings.Contains(err.Error(), msgInvalidUTF8) {
					t.Fatalf("unsafe or incomplete diagnostic: %v", err)
				}
			}
			_, err := target.Load()
			check(err, tt.loadFails)
			_, err = target.Fingerprint()
			check(err, true)
			check(target.Check(), tt.checkFails)
			if tt.checkFails && target.SalvageMeta() != nil {
				t.Fatal("salvage accepted corrupt metadata")
			}
		})
	}
}

func assertKind(t *testing.T, what string, err, kind error, message string) {
	t.Helper()
	if message == "" {
		if err != nil {
			t.Errorf("%s: %v, Python succeeds", what, err)
		}
		return
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != kind || err.Error() != message {
		t.Errorf("%s:\n got %v (%v)\nwant %s (%v)", what, err, kindOf(err), message, kind)
	}
}

// TestTranslateKeepsClassifiedErrors: Python's `except sqlite3.Error` in
// _translate never sees a StoreError, even one caused by a SQLite failure,
// so an *Error passes through translate and untranslated unchanged.
func TestTranslateKeepsClassifiedErrors(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	connection := must(connect(target.Path(), false, 0))(t)
	defer connection.close()
	cause := connection.exec("SELECT * FROM no_such_table")
	if _, ok := sqliteError(cause); !ok {
		t.Fatalf("want a SQLite error, got %v", cause)
	}
	classified := &Error{Kind: ErrCorrupt, Message: "已分类", Cause: cause}
	if got := translate(classified); got != classified {
		t.Errorf("translate: %v (%v), want the same *Error", got, kindOf(got))
	}
	if got := untranslated(classified); got != classified {
		t.Errorf("untranslated: %v (%v), want the same *Error", got, kindOf(got))
	}
	if got := translate(cause); kindOf(got) != ErrStorage || got.Error() != "配置数据库访问失败：no such table: no_such_table" {
		t.Errorf("translate(raw): %v (%v)", got, kindOf(got))
	}
}

// TestCopyFileKeepsTimes: the no-hard-link fallback of preserve_corrupt is
// shutil.copy2, which copies st_atime and st_mtime (nanoseconds) and the
// permission bits.
func TestCopyFileKeepsTimes(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source")
	if err := os.WriteFile(source, []byte("damaged"), 0o640); err != nil {
		t.Fatal(err)
	}
	atime := time.Unix(1700000000, 123456789)
	mtime := time.Unix(1600000000, 987654321)
	if err := os.Chtimes(source, atime, mtime); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target")
	if err := copyFile(source, target); err != nil {
		t.Fatal(err)
	}
	// Reading the source may itself update its atime (relatime): copy2
	// copies the atime the source has after the copy.
	atime = accessTime(must(os.Stat(source))(t))
	info := must(os.Stat(target))(t)
	if got := accessTime(info); !got.Equal(atime) {
		t.Errorf("atime %v, want %v", got, atime)
	}
	if !info.ModTime().Equal(mtime) || info.Mode().Perm() != 0o640 {
		t.Errorf("mtime %v mode %v, want %v 0640", info.ModTime(), info.Mode().Perm(), mtime)
	}
}

// TestConcurrentCommitsFromOneBase: of many writers committing from the same
// base at once (no repository lock), exactly one wins; every other one
// waits for the write lock and then fails with the conflict.
func TestConcurrentCommitsFromOneBase(t *testing.T) {
	target := newStore(t)
	target.timeout = seconds(10)
	must(target.Create(sampleDocument(t), nil))(t)
	base := must(target.Load())(t)
	const writers = 8
	results := make(chan error, writers)
	for i := range writers {
		go func() {
			document := base.Document.Clone()
			document.Set("writer", i)
			_, committed, err := target.Commit(base, document, nil)
			if err == nil && !committed {
				err = errors.New("not committed")
			}
			results <- err
		}()
	}
	wins := 0
	for range writers {
		err := <-results
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrConflict) || err.Error() != "配置数据库在事务外被修改":
			t.Errorf("writer: %v (%v)", err, kindOf(err))
		}
	}
	final := must(target.Load())(t)
	if wins != 1 || final.Head.Generation != 2 || !final.Document.Has("writer") {
		t.Errorf("wins=%d generation=%d", wins, final.Head.Generation)
	}
}

// TestOpenAndPermissionFailures: messages and kinds Python gives for an
// unreadable file, a directory and a read-only file (CPython 3.11 results).
func TestOpenAndPermissionFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, Filename)
	must(legacyNew(t, path, seconds(1)).Create(testObj("a", 1), nil))(t)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	unreadable := legacyNew(t, path, seconds(1))
	_, err := unreadable.Load()
	assertKind(t, "unreadable load", err, ErrStorage, "配置数据库访问失败：unable to open database file")
	assertKind(t, "unreadable check", unreadable.Check(), ErrSQLite, "unable to open database file")
	if unreadable.SalvageMeta() != nil {
		t.Error("unreadable salvage: want nil")
	}
	folder := legacyNew(t, directory, seconds(1))
	_, err = folder.Load()
	assertKind(t, "directory load", err, ErrStorage, "配置数据库访问失败：unable to open database file")
	assertKind(t, "directory check", folder.Check(), ErrSQLite, "unable to open database file")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	readonly := legacyNew(t, path, seconds(1))
	entry := must(readonly.Load())(t)
	_, _, err = readonly.Commit(entry, testObj("a", 2), nil)
	assertKind(t, "read-only commit", err, ErrStorage, "配置数据库访问失败：attempt to write a readonly database")
	assertKind(t, "read-only check", readonly.Check(), ErrSQLite, "")
}

// TestPathsWithURISpecialCharacters: the database is opened through a
// file: URI (as Path.as_uri() does in Python); characters with a meaning in
// URIs must reach SQLite as the literal path.
func TestPathsWithURISpecialCharacters(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "数据 dir #1 ?x=%41&mode=ro;'")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, Filename)
	target := legacyNew(t, path, seconds(1))
	must(target.Create(sampleDocument(t), nil))(t)
	entry := must(target.Load())(t)
	document := entry.Document.Clone()
	document.Set("written", true)
	mustCommit(t, target, entry, document, nil)
	if err := target.Check(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(directory, "backup #2.sqlite3")
	if err := target.BackupTo(backup); err != nil {
		t.Fatal(err)
	}
	if got := must(legacyNew(t, backup, seconds(1)).Load())(t); !got.Document.Has("written") {
		t.Error("backup lacks the commit")
	}
	entries := must(os.ReadDir(directory))(t)
	if len(entries) != 2 {
		t.Errorf("files in the directory: %v", entries)
	}
}
