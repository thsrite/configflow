package store

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"configflow/internal/jsonx"

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

// metaRows returns the meta table (or the given keys of it) parsed, in key
// order.
func (c *Conn) metaRows(keys ...string) (*jsonx.Object, error) {
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
	meta := jsonx.NewObject()
	err := c.query(statement, args, func(row *sql.Rows) error {
		var key, text string
		if err := row.Scan(&key, &text); err != nil {
			return err
		}
		if err := checkText(metaColumns, key, text); err != nil {
			return err
		}
		value, err := jsonx.ParseString(text)
		if err != nil {
			return &Error{Kind: ErrCorrupt, Message: "数据库元信息无法解析：" + err.Error(), Cause: err}
		}
		meta.Set(key, value)
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
	storeID, isString := jsonx.AsString(meta.Value("store_id"))
	generation, isInt := jsonx.AsInt(meta.Value("generation"))
	if !isString || !isInt {
		return Head{}, newError(ErrCorrupt, "数据库元信息缺少 store_id/generation")
	}
	commitID := ""
	if value := meta.Value("commit_id"); value != nil {
		if commitID, isString = jsonx.AsString(value); !isString {
			return Head{}, newError(ErrCorrupt, "数据库元信息 commit_id 无效")
		}
	}
	return Head{StoreID: storeID, Generation: generation, CommitID: commitID}, nil
}

// Meta returns the parsed value of one meta key, or nil when it is missing.
func (c *Conn) Meta(key string) (any, error) {
	meta, err := c.metaRows(key)
	if err != nil {
		return nil, err
	}
	return meta.Value(key), nil
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
	stored, err := c.Meta("root_order")
	if err != nil {
		return Entry{}, err
	}
	rootOrder, ok := stringList(stored)
	if !ok {
		return Entry{}, newError(ErrCorrupt, "数据库元信息 root_order 无效")
	}
	sections, fields, err := c.storedRows()
	if err != nil {
		return Entry{}, err
	}
	size := int64(0)
	parsed := newOrdered[fieldKey, parsedField]()
	for _, key := range fields.keys {
		field := fields.values[key]
		size += textLength(field.value)
		value, err := jsonx.ParseString(field.value)
		if err != nil {
			return Entry{}, &Error{Kind: ErrCorrupt, Cause: err,
				Message: "字段 " + key.scope + "/" + key.owner + "/" + key.field + " 不是有效 JSON：" + err.Error()}
		}
		parsed.set(key, parsedField{field.position, value})
	}
	document, err := buildDocument(rootOrder, sections, parsed)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Head: head, Document: document, Size: size}, nil
}

// stringList is `isinstance(v, list) and all(isinstance(x, str) for x in v)`.
func stringList(v any) ([]string, bool) {
	list, ok := jsonx.AsList(v)
	if !ok {
		return nil, false
	}
	result := make([]string, len(list))
	for i, item := range list {
		if result[i], ok = jsonx.AsString(item); !ok {
			return nil, false
		}
	}
	return result, true
}
