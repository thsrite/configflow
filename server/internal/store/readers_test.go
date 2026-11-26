package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// idleReaders returns the connections a store currently keeps.
func idleReaders(s *Store) []*Conn {
	s.readers.mu.Lock()
	defer s.readers.mu.Unlock()
	return append([]*Conn(nil), s.readers.idle...)
}

// head reads the head through Store.Reading.
func head(t testing.TB, s *Store) Head {
	t.Helper()
	var result Head
	err := s.Reading(func(c *Conn) (err error) {
		result, err = c.Head()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestReadsReuseOneConnection(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	must(target.Load())(t)
	first := idleReaders(target.Store)
	if len(first) != 1 {
		t.Fatalf("idle after one read: %d", len(first))
	}
	for range 5 {
		head(t, target.Store)
		must(target.Meta("root_order"))(t)
		must(target.Fingerprint())(t)
	}
	if again := idleReaders(target.Store); len(again) != 1 || again[0] != first[0] {
		t.Fatalf("reads did not reuse the connection: %v vs %v", again, first)
	}
}

// A commit by another connection that leaves the file's size and mtime as
// they were must still be seen: visibility comes from SQLite's change
// counter at BEGIN, the stat check only guards against file replacement.
func TestReusedReaderSeesCommitWithUnchangedStat(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	if got := head(t, target.Store).Generation; got != 1 {
		t.Fatal(got)
	}
	pooled := idleReaders(target.Store)[0]
	before := must(os.Stat(target.Path()))(t)
	execSQL(t, target.Path(), `UPDATE meta SET value = '7' WHERE key = 'generation'`)
	if err := os.Chtimes(target.Path(), time.Time{}, before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := must(os.Stat(target.Path()))(t)
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("precondition: stat changed %v/%v -> %v/%v", before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
	if got := head(t, target.Store).Generation; got != 7 {
		t.Fatalf("reused reader saw generation %d", got)
	}
	if idle := idleReaders(target.Store); len(idle) != 1 || idle[0] != pooled {
		t.Fatal("the matching connection was not reused")
	}
}

func TestReplacedFileIsReadThroughANewConnection(t *testing.T) {
	target := newStore(t)
	created := must(target.Create(sampleDocument(t), nil))(t)
	if got := head(t, target.Store); got != created.Head {
		t.Fatal(got)
	}
	pooled := idleReaders(target.Store)[0]
	// Create builds a new file and renames it over the path, as restores do.
	replaced := must(target.Create(sampleDocument(t), nil))(t)
	if replaced.Head.StoreID == created.Head.StoreID {
		t.Fatal("precondition: same store id")
	}
	if got := head(t, target.Store); got != replaced.Head {
		t.Fatalf("read the replaced file: %v, want %v", got, replaced.Head)
	}
	if idle := idleReaders(target.Store); len(idle) != 1 || idle[0] == pooled {
		t.Fatal("the connection to the replaced file was reused")
	}
}

func TestFailedReadIsNotKeptAndCloseStopsPooling(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	failure := errors.New("fn failed")
	if err := target.Reading(func(*Conn) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if idle := idleReaders(target.Store); len(idle) != 0 {
		t.Fatalf("kept %d connections after a failed read", len(idle))
	}
	head(t, target.Store)
	target.Close()
	if idle := idleReaders(target.Store); len(idle) != 0 {
		t.Fatal("Close kept idle connections")
	}
	if got := head(t, target.Store).Generation; got != 1 {
		t.Fatal(got)
	}
	if idle := idleReaders(target.Store); len(idle) != 0 {
		t.Fatal("pooled after Close")
	}
}

func TestBuildTemporaryKeepsNoReader(t *testing.T) {
	target := newStore(t)
	temporary, _, err := target.Store.BuildTemporary(must(legacyEncode(sampleDocument(t), nil))(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer DiscardTemporary(temporary)
	if idle := idleReaders(target.Store); len(idle) != 0 {
		t.Fatal(len(idle))
	}
}

// Concurrent readers and a writer: every read sees a committed head, and
// each reader's generations never go backwards.
//
// The lock wait is long: eight readers in a tight loop wait for every commit's
// EXTRA fsyncs, which on a loaded test server (load 30+, other fsyncs) took
// longer than newStore's 2.5 s busy_timeout and failed reads with "database is
// locked" equally with pooling disabled (HR O18 T1a). Lock timeouts are not
// what this test checks.
func TestPooledReadsRaceWithCommits(t *testing.T) {
	target := legacyNew(t, filepath.Join(t.TempDir(), Filename), seconds(60))
	entry := must(target.Create(sampleDocument(t), nil))(t)
	const commits = 20
	done := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			last := int64(0)
			for {
				var h Head
				err := target.Reading(func(c *Conn) (err error) { h, err = c.Head(); return err })
				if err != nil {
					errs <- err
					return
				}
				if h.Generation < last {
					errs <- errors.New("generation went backwards")
					return
				}
				last = h.Generation
				select {
				case <-done:
					return
				default:
				}
			}
		}()
	}
	for i := range commits {
		document := sampleDocument(t)
		document.GetObject("profiles").GetObject("default").Set("description", formatInt64(int64(i)))
		entry = mustCommit(t, target, entry, document, nil)
	}
	close(done)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := head(t, target.Store).Generation; got != commits+1 {
		t.Fatal(got)
	}
	if idle := idleReaders(target.Store); len(idle) > maxIdleReaders {
		t.Fatal(len(idle))
	}
}
