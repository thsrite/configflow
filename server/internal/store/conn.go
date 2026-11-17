package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// Conn is one SQLite connection inside Store.Reading: everything read
// through it comes from the same snapshot.
type Conn struct {
	db            *sql.DB
	conn          *sql.Conn
	inTransaction bool
}

var background = context.Background()

func fileURI(path string, readonly bool) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// mode=rw never creates a missing database as an empty file.
	mode := "?mode=rw"
	if readonly {
		mode = "?mode=ro"
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String() + mode, nil
}

// connect opens a new connection, as Python does for every operation; a
// connection is never shared.
func connect(path string, readonly bool, timeout time.Duration) (*Conn, error) {
	uri, err := fileURI(path, readonly)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(background)
	if err != nil {
		db.Close()
		return nil, err
	}
	c := &Conn{db: db, conn: conn}
	for _, statement := range []string{
		"PRAGMA busy_timeout=" + strconv.FormatInt(timeout.Milliseconds(), 10),
		"PRAGMA foreign_keys=ON",
		// EXTRA = FULL plus a directory fsync after the DELETE-mode journal
		// is unlinked, which is the commit point.
		"PRAGMA synchronous=EXTRA",
	} {
		if err := c.exec(statement); err != nil {
			c.close()
			return nil, err
		}
	}
	return c, nil
}

func (c *Conn) exec(statement string, args ...any) error {
	_, err := c.conn.ExecContext(background, statement, args...)
	return err
}

func (c *Conn) begin(statement string) error {
	if err := c.exec(statement); err != nil {
		return err
	}
	c.inTransaction = true
	return nil
}

func (c *Conn) commit() error {
	if err := c.exec("COMMIT"); err != nil {
		return err
	}
	c.inTransaction = false
	return nil
}

func (c *Conn) rollback() {
	if c.inTransaction {
		c.exec("ROLLBACK")
		c.inTransaction = false
	}
}

// close rolls back an open transaction and closes the connection.
func (c *Conn) close() {
	c.rollback()
	c.conn.Close()
	c.db.Close()
}

// query runs a statement and calls scan for every row.
func (c *Conn) query(statement string, args []any, scan func(*sql.Rows) error) error {
	result, err := c.conn.QueryContext(background, statement, args...)
	if err != nil {
		return err
	}
	defer result.Close()
	for result.Next() {
		if err := scan(result); err != nil {
			return err
		}
	}
	return result.Err()
}

// strings1 returns the first column of every row as text.
func (c *Conn) strings1(statement string) ([]string, error) {
	var values []string
	err := c.query(statement, nil, func(row *sql.Rows) error {
		var value string
		if err := row.Scan(&value); err != nil {
			return err
		}
		values = append(values, value)
		return nil
	})
	return values, err
}

// Text columns in SELECT order, for checkText (position is not text).
var (
	metaColumns    = []string{"key", "value"}
	sectionColumns = []string{"scope", "owner"}
	fieldColumns   = []string{"scope", "owner", "field", "value"}
)

// metaRows reads opaque metadata JSON, without decoding unknown values.
func (c *Conn) metaRows(keys ...string) (map[string]json.RawMessage, error) {
	statement := "SELECT key, value FROM meta"
	var args []any
	if len(keys) > 0 {
		statement += " WHERE key IN ("
		for i, key := range keys {
			if i > 0 {
				statement += ","
			}
			statement += "?"
			args = append(args, key)
		}
		statement += ")"
	}
	meta := make(map[string]json.RawMessage)
	err := c.query(statement, args, func(row *sql.Rows) error {
		var key, text string
		if err := row.Scan(&key, &text); err != nil {
			return err
		}
		if err := checkText(metaColumns, key, text); err != nil {
			return err
		}
		if !json.Valid([]byte(text)) {
			return newError(ErrCorrupt, fmt.Sprintf(msgMetaInvalid, "invalid JSON"))
		}
		meta[key] = json.RawMessage(text)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return meta, nil
}

// Head returns the identity of the committed state this connection sees.
func (c *Conn) Head() (Head, error) {
	meta, err := c.metaRows("store_id", "generation", "commit_id")
	if err != nil {
		return Head{}, err
	}
	var storeID, commitID string
	var generation int64
	if !decodeString(meta["store_id"], &storeID) || json.Unmarshal(meta["generation"], &generation) != nil || bytes.Equal(bytes.TrimSpace(meta["generation"]), []byte("null")) {
		return Head{}, newError(ErrCorrupt, msgHeadInvalid)
	}
	if value := meta["commit_id"]; len(value) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		if !decodeString(value, &commitID) {
			return Head{}, newError(ErrCorrupt, msgCommitIDInvalid)
		}
	}
	return Head{StoreID: storeID, Generation: generation, CommitID: commitID}, nil
}

func decodeString(value json.RawMessage, target *string) bool {
	return len(bytes.TrimSpace(value)) > 0 && bytes.TrimSpace(value)[0] == '"' && json.Unmarshal(value, target) == nil
}

// Meta returns the raw JSON of a metadata key, or nil when absent.
func (c *Conn) Meta(key string) (json.RawMessage, error) {
	meta, err := c.metaRows(key)
	return meta[key], err
}

func (c *Conn) rootOrder() ([]string, error) {
	raw, err := c.Meta("root_order")
	if err != nil {
		return nil, err
	}
	var order []string
	// Decoding each item separately rejects null entries, which Unmarshal
	// into []string would otherwise silently turn into empty strings.
	var items []json.RawMessage
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' || json.Unmarshal(raw, &items) != nil {
		return nil, newError(ErrCorrupt, msgRootOrderInvalid)
	}
	for _, item := range items {
		var key string
		if !decodeString(item, &key) {
			return nil, newError(ErrCorrupt, msgRootOrderInvalid)
		}
		order = append(order, key)
	}
	return order, nil
}

// storedRows reads the sections and fields tables in primary key order.
func (c *Conn) storedRows() (*ordered[sectionKey, int64], *ordered[fieldKey, fieldRow], error) {
	sections := newOrdered[sectionKey, int64]()
	err := c.query("SELECT scope, owner, position FROM sections", nil, func(row *sql.Rows) error {
		var key sectionKey
		var position int64
		if err := row.Scan(&key.scope, &key.owner, &position); err != nil {
			return err
		}
		if err := checkText(sectionColumns, key.scope, key.owner); err != nil {
			return err
		}
		sections.set(key, position)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	fields := newOrdered[fieldKey, fieldRow]()
	err = c.query("SELECT scope, owner, field, position, value FROM fields", nil, func(row *sql.Rows) error {
		var key fieldKey
		var field fieldRow
		if err := row.Scan(&key.scope, &key.owner, &key.field, &field.position, &field.value); err != nil {
			return err
		}
		if err := checkText(fieldColumns, key.scope, key.owner, key.field, field.value); err != nil {
			return err
		}
		fields.set(key, field)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return sections, fields, nil
}

// Load reads the whole document as this connection sees it, never cached.
func (c *Conn) Load() (Entry, error) {
	head, err := c.Head()
	if err != nil {
		return Entry{}, err
	}
	rootOrder, err := c.rootOrder()
	if err != nil {
		return Entry{}, err
	}
	sections, fields, err := c.storedRows()
	if err != nil {
		return Entry{}, err
	}
	size := int64(0)
	for _, key := range fields.keys {
		field := fields.values[key]
		size += textLength(field.value)
		if !json.Valid([]byte(field.value)) {
			return Entry{}, newError(ErrCorrupt, fmt.Sprintf(msgInvalidField, key.scope, key.owner, key.field))
		}
	}
	doc, err := buildDocument(rootOrder, sections, fields)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Head: head, Document: doc, Size: size}, nil
}
