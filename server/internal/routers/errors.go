package routers

import (
	"errors"
)

// Python exception classes of backend/routers/base.py. Later modules add
// their own subclasses as further Class values (e.g. UncertainWrite).
const (
	ClassRouterError         = "RouterError"
	ClassRouterConflictError = "RouterConflictError"
	ClassRouterRollbackError = "RouterRollbackError"
)

// RouterError is base.RouterError and its subclasses: a router request
// failed; the message is safe to show to users. errors.As(err,
// **RouterError) is Python's `except RouterError` (it catches every
// subclass); Class names the Python class.
type RouterError struct {
	Class   string
	Message string
}

func (e *RouterError) Error() string { return e.Message }

// PythonClass is the Python exception class name.
func (e *RouterError) PythonClass() string {
	if e.Class == "" {
		return ClassRouterError
	}
	return e.Class
}

// NewRouterError is RouterError(message).
func NewRouterError(message string) *RouterError {
	return &RouterError{Class: ClassRouterError, Message: message}
}

// NewConflictError is RouterConflictError(message): the route changed or
// cannot be edited; a fresh preview is required.
func NewConflictError(message string) *RouterError {
	return &RouterError{Class: ClassRouterConflictError, Message: message}
}

// NewRollbackError is RouterRollbackError(message): a driver could not fully
// compensate its partially applied operation.
func NewRollbackError(message string) *RouterError {
	return &RouterError{Class: ClassRouterRollbackError, Message: message}
}

// AsRouterError is `except RouterError as exc`.
func AsRouterError(err error) (*RouterError, bool) {
	var r *RouterError
	ok := errors.As(err, &r)
	return r, ok
}

// IsClass reports whether err is a RouterError of exactly that class.
func IsClass(err error, class string) bool {
	r, ok := AsRouterError(err)
	return ok && r.PythonClass() == class
}

// RouterValidationError is store.RouterValidationError (a ValueError).
type RouterValidationError struct{ Message string }

func (e *RouterValidationError) Error() string { return e.Message }

// PythonClass is "RouterValidationError".
func (e *RouterValidationError) PythonClass() string { return "RouterValidationError" }

// IsValueError is true: the class derives from ValueError.
func (e *RouterValidationError) IsValueError() bool { return true }

func validation(message string) error { return &RouterValidationError{Message: message} }

// PyError is a built-in Python exception (ValueError, TypeError, KeyError,
// AttributeError, IndexError, …) raised by the ported code where Python
// raises it; handlers treat it like the Python app treats the exception
// (mostly: 500).
type PyError struct {
	Class   string
	Message string
}

func (e *PyError) Error() string { return e.Message }

// PythonClass is the Python exception class name.
func (e *PyError) PythonClass() string { return e.Class }

// IsValueError reports ValueError and its subclasses used here.
func (e *PyError) IsValueError() bool {
	switch e.Class {
	case "ValueError", "AddressValueError", "NetmaskValueError":
		return true
	}
	return false
}

func pyErr(class, message string) error { return &PyError{Class: class, Message: message} }

// PendingError is returned where Python calls a function of a later routers
// module (or another domain) that is not ported yet. It never stands for a
// Python result.
type PendingError struct {
	Module   string
	Function string
}

func (e *PendingError) Error() string {
	return "not ported yet: " + e.Function + " (" + e.Module + ")"
}

// IsPending reports a PendingError.
func IsPending(err error) bool {
	var p *PendingError
	return errors.As(err, &p)
}

func pending(module, function string) error { return &PendingError{Module: module, Function: function} }
