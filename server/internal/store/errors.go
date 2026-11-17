package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// Error kinds. Every error this package creates is an *Error whose Kind is
// one of these; test with errors.Is. The Python exception each one stands
// for is given in parentheses.
var (
	// ErrStore (StoreError) also matches ErrCorrupt, ErrUnsupported and
	// ErrConflict, which are its subclasses in Python.
	ErrStore = errors.New("store error")
	// ErrCorrupt (StoreCorrupt): the file is physically damaged, is not a
	// configuration store, or its rows do not form a document.
	ErrCorrupt = errors.New("store corrupt")
	// ErrUnsupported (StoreUnsupported): written by a newer storage version.
	ErrUnsupported = errors.New("store unsupported")
	// ErrConflict (StoreConflict): the database changed under a writer that
	// holds the repository lock.
	ErrConflict = errors.New("store conflict")
	// ErrStorage (ConfigStorageError, an OSError): a runtime SQLite failure
	// while reading or committing; keeps the 500 semantics of disk errors.
	// It is not an ErrStore.
	ErrStorage = errors.New("config storage error")
	// ErrSQLite (sqlite3.Error): a SQLite failure that Python lets through
	// untranslated, from Check and BuildTemporary. It is not an ErrStore.
	ErrSQLite = errors.New("sqlite error")
	// ErrDocument (TypeError): the document cannot be stored, either
	// because of its shape or because it holds a value that is not JSON.
	ErrDocument = errors.New("document type error")
)

// Error is an error of the storage engine. Error() returns Message alone,
// which is str(exception) in Python, so messages built from it match.
type Error struct {
	Kind    error  // one of the Err* values
	Message string // the Python exception message
	Cause   error  // the underlying error, if any
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

// Is makes errors.Is(err, ErrX) work, including ErrStore for its subclasses.
func (e *Error) Is(target error) bool {
	if target == e.Kind {
		return true
	}
	return target == ErrStore && (e.Kind == ErrCorrupt || e.Kind == ErrUnsupported || e.Kind == ErrConflict)
}

func newError(kind error, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

// SQLITE_CORRUPT and SQLITE_NOTADB (primary result codes).
const (
	sqliteBusy    = 5
	sqliteLocked  = 6
	sqliteCorrupt = 11
	sqliteNotADB  = 26
)

func sqliteError(err error) (*sqlite.Error, bool) {
	var target *sqlite.Error
	ok := errors.As(err, &target)
	return target, ok
}

func sqliteCode(err error) int {
	if target, ok := sqliteError(err); ok {
		return target.Code() & 0xff
	}
	return 0
}

func isCorrupt(err error) bool {
	code := sqliteCode(err)
	return code == sqliteCorrupt || code == sqliteNotADB
}

var resultCodeSuffix = regexp.MustCompile(` \(\d+\)$`)

// sqliteMessage is str(sqlite3.Error): the text of sqlite3_errmsg. The
// driver formats its errors as "errstr (code)" or "errstr: errmsg (code)",
// with " (SQLITE_BUSY)" appended for that code; errstr never contains ": ".
func sqliteMessage(err error) string {
	var backup *backupError
	if errors.As(err, &backup) {
		return backup.Error()
	}
	target, ok := sqliteError(err)
	if !ok {
		return err.Error()
	}
	message := strings.TrimSuffix(target.Error(), " (SQLITE_BUSY)")
	message = resultCodeSuffix.ReplaceAllString(message, "")
	if _, detail, found := strings.Cut(message, ": "); found {
		return detail
	}
	return message
}

// decodeError is the sqlite3.OperationalError Python's sqlite3 module raises
// when a TEXT value read from the database is not valid UTF-8.
type decodeError struct {
	column string
	text   string // the raw value
}

func (e *decodeError) Error() string {
	// PyErr_Format writes the raw bytes with one U+FFFD per invalid byte.
	var b strings.Builder
	for _, r := range e.text {
		b.WriteRune(r)
	}
	return "Could not decode to UTF-8 column '" + e.column + "' with text '" + b.String() + "'"
}

// backupError is a failure of the SQLite backup API. Python reports it
// from the destination connection, where sqlite3_backup_finish stores the
// plain sqlite3_errstr text of the code; the driver appends the source
// connection's sqlite3_errmsg instead, which says nothing about it (often
// "not an error").
type backupError struct {
	cause *sqlite.Error
}

func (e *backupError) Error() string {
	message := strings.TrimSuffix(e.cause.Error(), " (SQLITE_BUSY)")
	message = resultCodeSuffix.ReplaceAllString(message, "")
	errstr, _, _ := strings.Cut(message, ": ")
	return errstr
}

func (e *backupError) Unwrap() error { return e.cause }

// asBackupError wraps a SQLite error from a backup step or finish.
func asBackupError(err error) error {
	if target, ok := err.(*sqlite.Error); ok {
		return &backupError{cause: target}
	}
	return err
}

// checkText fails like Python's row fetch when a TEXT column holds bytes
// that are not UTF-8. columns and values are given in SELECT order; the
// first invalid one is reported.
func checkText(columns []string, values ...string) error {
	for i, value := range values {
		if !utf8.ValidString(value) {
			return &decodeError{column: columns[i], text: value}
		}
	}
	return nil
}

// isSQLiteFailure reports whether err is a failure Python raises as
// sqlite3.Error: a SQLite result code or a text decoding failure. Errors
// this package already classified (*Error) are not, even when their cause
// is one: Python's except sqlite3.Error does not see a StoreError.
func isSQLiteFailure(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*Error); ok {
		return false
	}
	var decode *decodeError
	if errors.As(err, &decode) {
		return true
	}
	_, ok := sqliteError(err)
	return ok
}

// translate is ConfigStore._translate: SQLite failures become ErrStorage,
// everything else passes through unchanged.
func translate(err error) error {
	if !isSQLiteFailure(err) {
		return err
	}
	if isCorrupt(err) {
		return &Error{Kind: ErrStorage, Message: fmt.Sprintf(msgCorruptAccess, sqliteMessage(err)), Cause: err}
	}
	return &Error{Kind: ErrStorage, Message: fmt.Sprintf(msgAccess, sqliteMessage(err)), Cause: err}
}

// untranslated wraps a SQLite failure that Python raises as it is.
func untranslated(err error) error {
	if !isSQLiteFailure(err) {
		return err
	}
	return &Error{Kind: ErrSQLite, Message: sqliteMessage(err), Cause: err}
}
