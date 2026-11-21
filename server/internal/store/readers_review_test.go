package store

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// pythonSQLite runs a short script with the cross-check interpreter (another
// process, so its locks are real POSIX locks against ours); argv[1] is path.
func pythonSQLite(t *testing.T, path, script string) (string, error) {
	t.Helper()
	interpreter := os.Getenv("CONFIGFLOW_PY")
	if interpreter == "" {
		t.Skip("CONFIGFLOW_PY is not set")
	}
	output, err := exec.Command(interpreter, "-I", "-c", script, path).CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

const exclusiveScript = `
import sqlite3, sys
c = sqlite3.connect(sys.argv[1], timeout=0, isolation_level=None)
try:
    c.execute("BEGIN EXCLUSIVE")
    c.execute("ROLLBACK")
    print("ok")
except sqlite3.OperationalError as e:
    print(e)
`

// An idle reader holds no lock between reads (rollback journal): another
// process takes an EXCLUSIVE lock without waiting. Control: inside a read
// transaction the same lock is refused, so the probe does detect a lock.
func TestIdleReaderHoldsNoLockAcrossProcesses(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	head(t, target.Store)
	if len(idleReaders(target.Store)) != 1 {
		t.Fatal("precondition: no idle reader")
	}
	if out, err := pythonSQLite(t, target.Path(), exclusiveScript); err != nil || out != "ok" {
		t.Fatalf("exclusive lock with an idle reader: %q %v", out, err)
	}
	err := target.Reading(func(c *Conn) error {
		if _, err := c.Head(); err != nil {
			return err
		}
		out, err := pythonSQLite(t, target.Path(), exclusiveScript)
		if err != nil || out != "database is locked" {
			t.Errorf("control: exclusive lock during a read: %q %v", out, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A file renamed over the path with the same size and modification time
// (two restores within one mtime tick) is still noticed through its inode.
// Control: the pooled connection itself still reads the old, unlinked file,
// so it is the stat check, not SQLite, that keeps reads current.
func TestReplacementWithSameSizeAndMtime(t *testing.T) {
	target := newStore(t)
	created := must(target.Create(sampleDocument(t), nil))(t)
	head(t, target.Store)
	pooled := idleReaders(target.Store)[0]
	before := must(os.Stat(target.Path()))(t)

	temporary, replaced, err := target.Store.BuildTemporary(must(legacyEncode(sampleDocument(t), nil))(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Store.Install(temporary); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target.Path(), time.Time{}, before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := must(os.Stat(target.Path()))(t)
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || os.SameFile(before, after) {
		t.Fatalf("precondition: want same size and mtime, other inode (%d/%v -> %d/%v)", before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}

	// Control: read through the stale connection directly.
	if err := pooled.begin("BEGIN"); err != nil {
		t.Fatal(err)
	}
	stale, err := pooled.Head()
	pooled.rollback()
	if err != nil || stale != created.Head {
		t.Fatalf("control: pooled connection read %v (%v), want the old head %v", stale, err, created.Head)
	}

	if got := head(t, target.Store); got != replaced.Head {
		t.Fatalf("read %v after the replacement, want %v", got, replaced.Head)
	}
	if idle := idleReaders(target.Store); len(idle) != 1 || idle[0] == pooled {
		t.Fatal("the connection to the replaced file was reused")
	}
}

// A schema change by another connection (here one that leaves size and
// mtime as they were) is seen by the reused connection: SQLite checks the
// schema cookie at every transaction.
func TestReusedReaderSeesSchemaChange(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	head(t, target.Store)
	pooled := idleReaders(target.Store)[0]
	before := must(os.Stat(target.Path()))(t)
	execSQL(t, target.Path(), "CREATE VIEW review_view AS SELECT key FROM meta")
	if err := os.Chtimes(target.Path(), time.Time{}, before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if after := must(os.Stat(target.Path()))(t); after.Size() != before.Size() {
		t.Fatalf("precondition: size changed %d -> %d", before.Size(), after.Size())
	}
	var views []string
	err := target.Reading(func(c *Conn) (err error) {
		views, err = c.strings1("SELECT name FROM sqlite_master WHERE type = 'view'")
		return err
	})
	if err != nil || len(views) != 1 || views[0] != "review_view" {
		t.Fatalf("views %v, %v", views, err)
	}
	if idle := idleReaders(target.Store); len(idle) != 1 || idle[0] != pooled {
		t.Fatal("precondition: the connection was not reused")
	}
}

// A connection that found the database in WAL mode (switched by another
// process; the store itself only uses DELETE) is not kept idle: an idle WAL
// connection keeps "<path>-wal" alive, which a file later renamed over the
// path would be opened with, and it blocks switching back to DELETE.
func TestWALConnectionIsNotKept(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	head(t, target.Store)
	execSQL(t, target.Path(), "PRAGMA journal_mode=WAL")
	if got := head(t, target.Store).Generation; got != 1 {
		t.Fatal(got)
	}
	if idle := idleReaders(target.Store); len(idle) != 0 {
		t.Fatalf("kept %d WAL connections", len(idle))
	}
	if _, err := os.Stat(target.Path() + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("-wal left behind after the read: %v", err)
	}
	// Back in DELETE mode, reads are pooled again.
	execSQL(t, target.Path(), "PRAGMA journal_mode=DELETE")
	head(t, target.Store)
	if idle := idleReaders(target.Store); len(idle) != 1 {
		t.Fatalf("idle after DELETE: %d", len(idle))
	}
}

// The hazard behind TestWALConnectionIsNotKept, end to end: another process
// put the database in WAL mode and committed; then a restore renamed a new
// file over the path. Whoever opens the new file next must read it, not the
// old file's WAL frames.
func TestRenameAfterForeignWALCommitReadsNewFile(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	head(t, target.Store)
	execSQL(t, target.Path(), "PRAGMA journal_mode=WAL")
	head(t, target.Store)
	execSQL(t, target.Path(), `UPDATE meta SET value = '9' WHERE key = 'generation'`)

	temporary, replaced, err := target.Store.BuildTemporary(must(legacyEncode(sampleDocument(t), nil))(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Store.Install(temporary); err != nil {
		t.Fatal(err)
	}
	// A second opener (a writer, Check, another process) before the next read.
	other := New(target.Path(), seconds(1))
	defer other.Close()
	var got Head
	err = other.Reading(func(c *Conn) (err error) { got, err = c.Head(); return err })
	if err != nil || got != replaced.Head {
		t.Fatalf("new file read as %v (%v), want %v", got, err, replaced.Head)
	}
	if got := head(t, target.Store); got != replaced.Head {
		t.Fatalf("store read %v, want %v", got, replaced.Head)
	}
}
