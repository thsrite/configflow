package repo

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error kinds, the exception classes of backend/common/config_repository.py.
// Test with errors.Is; every repository error also matches ErrRepository.
var (
	// ErrRepository is ProfileRepositoryError, the base class.
	ErrRepository = errors.New("profile repository error")
	// ErrValidation is ProfileValidationError (also a ValueError).
	ErrValidation = errors.New("profile validation error")
	// ErrNotFound is ProfileNotFound (also a KeyError).
	ErrNotFound = errors.New("profile not found")
	// ErrExists is ProfileExists (also a ValueError).
	ErrExists = errors.New("profile exists")
	// ErrInUse is ProfileInUse (also a ValueError); the error carries Usages.
	ErrInUse = errors.New("profile in use")
	// ErrNodeDeletionChanged is NodeDeletionChanged, a ProfileInUse.
	ErrNodeDeletionChanged = errors.New("node deletion changed")
	// ErrRuleLibraryCreationUncertain is RuleLibraryCreationUncertain.
	ErrRuleLibraryCreationUncertain = errors.New("rule library creation uncertain")
)

// Error is a repository exception. Error() is Python's str(exc): the message,
// except for ErrNotFound, where it is the repr of the profile id (KeyError).
type Error struct {
	Kind    error
	Message string
	// Usages is ProfileInUse.usages: a list of usage objects, never nil for
	// ErrInUse errors.
	Usages []any
	Cause  error
}

func (e *Error) Error() string {
	if e.Kind == ErrNotFound {
		return pyQuote(e.Message)
	}
	return e.Message
}

func pyQuote(text string) string {
	var b strings.Builder
	quote := byte('\'')
	if strings.Contains(text, "'") && !strings.Contains(text, `"`) {
		quote = '"'
	}
	b.WriteByte(quote)
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < ' ' || r == 0x7f:
			writePyHex(&b, 'x', r, 2)
		case r < 0x7f || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xff:
			writePyHex(&b, 'x', r, 2)
		case r <= 0xffff:
			writePyHex(&b, 'u', r, 4)
		default:
			writePyHex(&b, 'U', r, 8)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

func writePyHex(b *strings.Builder, prefix byte, r rune, width int) {
	const hex = "0123456789abcdef"
	b.WriteByte('\\')
	b.WriteByte(prefix)
	for shift := (width - 1) * 4; shift >= 0; shift -= 4 {
		b.WriteByte(hex[(r>>shift)&0xf])
	}
}

func (e *Error) Unwrap() error { return e.Cause }

// Is reports class membership like isinstance: every Error is an
// ErrRepository, a NodeDeletionChanged is also an ErrInUse.
func (e *Error) Is(target error) bool {
	if target == e.Kind || target == ErrRepository {
		return true
	}
	return target == ErrInUse && e.Kind == ErrNodeDeletionChanged
}

// IsValueError reports whether Python code catching ValueError would catch
// err: validation, exists and in-use errors, and helper ValueErrors.
func IsValueError(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind == ErrValidation || e.Kind == ErrExists || e.Kind == ErrInUse || e.Kind == ErrNodeDeletionChanged
	}
	var h *HelperError
	if errors.As(err, &h) {
		return h.Class == ClassValueError || h.Class == ClassDialerReference || h.Class == ClassOxidnsSettings
	}
	return false
}

// UsagesOf returns the usages of a ProfileInUse error, or nil.
func UsagesOf(err error) []any {
	var e *Error
	if errors.As(err, &e) {
		return e.Usages
	}
	return nil
}

func newError(kind error, message string) *Error {
	e := &Error{Kind: kind, Message: message}
	if kind == ErrInUse || kind == ErrNodeDeletionChanged {
		e.Usages = []any{}
	}
	return e
}

func validationError(message string) *Error { return newError(ErrValidation, message) }
func repositoryError(message string) *Error { return newError(ErrRepository, message) }
func notFound(profileID string) *Error      { return newError(ErrNotFound, profileID) }
func exists(message string) *Error          { return newError(ErrExists, message) }

func inUse(message string, usages ...any) *Error {
	e := newError(ErrInUse, message)
	if len(usages) > 0 {
		e.Usages = usages
	}
	return e
}

// NewInUse builds a ProfileInUse for packages whose updaters reject a
// change the way the repository does (Python: raise ProfileInUse(msg, usages)).
func NewInUse(message string, usages []any) *Error {
	return inUse(message, usages...)
}

// NewValidationError builds a ProfileValidationError.
func NewValidationError(message string) *Error { return validationError(message) }

// Classes of HelperError.
const (
	ClassValueError      = "ValueError"
	ClassDialerReference = "DialerReferenceError" // a StrategyReferenceError, a ValueError
	ClassOxidnsSettings  = "OxidnsSettingsError"  // a ValueError
	ClassTypeError       = "TypeError"
	ClassKeyError        = "KeyError"
	ClassAttributeError  = "AttributeError"
)

// HelperError is an exception raised by one of the validation helpers the
// repository calls (dialer references, health checks, OxiDNS settings ...).
// Class is the Python exception class; the repository converts the classes
// its Python counterpart catches and passes the others through unchanged.
//
// ClassTypeError, ClassKeyError and ClassAttributeError stand for the
// unintended exceptions Python raises on malformed values the helper does
// not check itself; their messages are not Python's.
type HelperError struct {
	Class   string
	Message string
}

func (e *HelperError) Error() string { return e.Message }

func helperError(class, message string) *HelperError {
	return &HelperError{Class: class, Message: message}
}

func isHelperClass(err error, classes ...string) bool {
	var h *HelperError
	if !errors.As(err, &h) {
		return false
	}
	for _, class := range classes {
		if h.Class == class {
			return true
		}
	}
	return false
}
