package store

import (
	"os"
	"testing"
	"time"
)

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

func TestAutocommitHeadTracksJournalAndReplacement(t *testing.T) {
	target := newStore(t)
	created := must(target.Create(sampleDocument(t), nil))(t)
	identity := func() Head {
		t.Helper()
		got := must(target.Store.Head())(t)
		if want := head(t, target.Store); got != want {
			t.Fatalf("single statement %v, read transaction %v", got, want)
		}
		return got
	}
	if identity() != created.Head {
		t.Fatal("initial head")
	}
	execSQL(t, target.Path(), "PRAGMA journal_mode=WAL", `UPDATE meta SET value = '9' WHERE key = 'generation'`)
	if got := must(target.Store.Head())(t); got.Generation != 9 || len(idleReaders(target.Store)) != 0 {
		t.Fatal("foreign WAL generation or connection lifetime")
	}
	temp, replacement, err := target.Store.BuildTemporary(must(legacyEncode(sampleDocument(t), nil))(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Store.Install(temp); err != nil {
		t.Fatal(err)
	}
	if identity() != replacement.Head {
		t.Fatal("replacement head")
	}
}

func BenchmarkHeadValidation(b *testing.B) {
	target := newStore(b)
	must(target.Create(sampleDocument(b), nil))(b)
	for _, single := range []bool{false, true} {
		b.Run(map[bool]string{false: "transaction", true: "statement"}[single], func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if single {
					_, err = target.Store.Head()
				} else {
					err = target.Reading(func(c *Conn) error { _, err := c.Head(); return err })
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
