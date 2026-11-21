package store

import (
	"os"
	"strings"
	"sync"
)

// maxIdleReaders is how many read connections a Store keeps open between
// reads. Reads are short; two cover a heartbeat overlapping a page load.
const maxIdleReaders = 2

// readers keeps idle connections for Store.Reading, so that a read does not
// open the file, parse the schema and set the connection pragmas every time
// (O10-R10). Reusing a connection does not change what a read sees: every
// Reading starts a new deferred transaction, and SQLite checks the file
// change counter (and for a hot journal) whenever a transaction takes its
// shared lock, so commits by other connections and processes are visible
// exactly as on a fresh connection. A connection holds no lock between
// transactions in rollback-journal mode, so idle readers never block writers;
// connections that found the database in WAL mode are never kept (see
// rollbackJournal).
//
// What SQLite cannot see is the file being replaced (Install renames a new
// database over the path) or changed outside SQLite. A connection is
// therefore reused only while os.Stat of the path still reports the file it
// opened, with the same size and modification time; any difference, or a
// failed read, closes it and the next read opens a fresh connection.
type readers struct {
	mu     sync.Mutex
	idle   []*Conn
	closed bool
}

// reader returns an idle connection that still matches the database file,
// or a new one.
func (s *Store) reader() (*Conn, error) {
	timeout := s.timeout()
	file, statErr := os.Stat(s.path)
	for {
		s.readers.mu.Lock()
		var c *Conn
		if n := len(s.readers.idle); n > 0 {
			c = s.readers.idle[n-1]
			s.readers.idle[n-1] = nil
			s.readers.idle = s.readers.idle[:n-1]
		}
		s.readers.mu.Unlock()
		if c == nil {
			return connect(s.path, false, timeout)
		}
		if statErr == nil && c.timeout == timeout && sameFile(c.file, file) {
			return c, nil
		}
		c.close()
	}
}

// sameFile reports whether opened (stat'ed before the connection was
// opened) is still the file at the path, unmodified outside SQLite's view.
func sameFile(opened, current os.FileInfo) bool {
	return opened != nil && os.SameFile(opened, current) &&
		opened.Size() == current.Size() && opened.ModTime().Equal(current.ModTime())
}

// release returns a connection after a read. Only a read that completed
// (its transaction committed) on a database in DELETE journal mode is kept;
// anything else is closed.
func (s *Store) release(c *Conn, ok bool) {
	if !ok || c.inTransaction || !c.rollbackJournal() {
		c.close()
		return
	}
	s.readers.mu.Lock()
	if s.readers.closed || len(s.readers.idle) >= maxIdleReaders {
		s.readers.mu.Unlock()
		c.close()
		return
	}
	s.readers.idle = append(s.readers.idle, c)
	s.readers.mu.Unlock()
}

// rollbackJournal reports whether the connection found the database in
// DELETE journal mode, the only mode the store writes. A connection that
// found WAL (switched by another process) is not kept idle: as long as it is
// open, "<path>-wal" outlives every other connection, and when a new file is
// renamed over the path, the next connection to open it applies the old
// file's WAL frames to it and reads the old data. Closing it right away makes
// the last connection checkpoint and remove the WAL, as before pooling.
func (c *Conn) rollbackJournal() bool {
	mode, err := c.strings1("PRAGMA journal_mode")
	return err == nil && len(mode) == 1 && strings.EqualFold(mode[0], "delete")
}

// Close closes the idle read connections and stops keeping new ones. The
// Store stays usable: later reads open a connection each and close it.
func (s *Store) Close() {
	s.closeIdle(true)
}

// closeIdle closes the idle read connections; stop also ends pooling.
// Check calls it first: changing the journal mode of a WAL database needs
// the only open connection, which an idle reader would otherwise hold.
func (s *Store) closeIdle(stop bool) {
	s.readers.mu.Lock()
	idle := s.readers.idle
	s.readers.idle = nil
	s.readers.closed = s.readers.closed || stop
	s.readers.mu.Unlock()
	for _, c := range idle {
		c.close()
	}
}
