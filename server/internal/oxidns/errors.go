package oxidns

import (
	"errors"

	"configflow/internal/convert"
)

// Python exception classes of the OxiDNS cluster. All of them are ValueError
// subclasses in Python (OxidnsSettingsError derives from ValueError, the
// others from OxidnsSettingsError).
const (
	ClassSettings   = "OxidnsSettingsError"
	ClassReference  = "OxidnsReferenceError"
	ClassValidation = "OxidnsValidationError"
)

// Error is OxidnsSettingsError and its subclasses. Error() is str(exc).
//
// Issues is exc.issues when Python sets it (native_document's structure
// errors, OxidnsValidationError); HasIssues tells whether the attribute exists
// at all (getattr(exc, 'issues', default) callers). References is
// OxidnsReferenceError.references.
type Error struct {
	Class      string
	Message    string
	Issues     []any
	HasIssues  bool
	References []any
}

func (e *Error) Error() string { return e.Message }

// PythonClass is the Python exception class name.
func (e *Error) PythonClass() string { return e.Class }

// settingsError is OxidnsSettingsError(message).
func settingsError(message string) *Error { return &Error{Class: ClassSettings, Message: message} }

// IsSettingsError reports whether err is an OxidnsSettingsError (or a
// subclass): what `except OxidnsSettingsError` catches.
func IsSettingsError(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

// AsError returns err as *Error when it is an OxidnsSettingsError.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// IsValueError reports whether `except ValueError` catches err: the OxiDNS
// errors and Python's own ValueErrors (*convert.PyError of class ValueError).
func IsValueError(err error) bool {
	if IsSettingsError(err) {
		return true
	}
	return convert.PyErrorClass(err) == "ValueError"
}

// PendingError is returned where the Python code calls a function of a later
// module of the OxiDNS cluster (C23–C27) that is not ported yet. It never
// stands for a Python result; callers must treat it as "cannot answer".
type PendingError struct {
	Module   string
	Function string
}

func (e *PendingError) Error() string {
	return "not ported yet: " + e.Function + " (" + e.Module + ")"
}

// IsPending reports whether err is a PendingError.
func IsPending(err error) bool {
	var p *PendingError
	return errors.As(err, &p)
}

func pending(module, function string) {
	raise(&PendingError{Module: module, Function: function})
}
