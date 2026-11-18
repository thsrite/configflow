package convert

import (
	"errors"
	"fmt"

	"configflow/internal/rules"
)

// DialerReferenceError is dialer_references.DialerReferenceError, a
// StrategyReferenceError (and so a ValueError).
type DialerReferenceError struct {
	Message string
	cause   error
}

func (e *DialerReferenceError) Error() string { return e.Message }

// Unwrap lets errors.As find the StrategyReferenceError base class.
func (e *DialerReferenceError) Unwrap() error {
	return errors.Join(e.cause, &rules.StrategyReferenceError{Message: e.Message})
}

func dialerError(message string) error { return &DialerReferenceError{Message: message} }

// EasyTierSettingsError is mihomo_easytier.EasyTierSettingsError (a
// ValueError).
type EasyTierSettingsError struct{ Message string }

func (e *EasyTierSettingsError) Error() string { return e.Message }

func easytierError(message string) error { return &EasyTierSettingsError{Message: message} }

// PyError stands for an exception Python raises unintentionally on a value
// the code does not check (TypeError, KeyError, AttributeError, ValueError,
// IndexError ...). Class is the Python class name. Messages follow Python's
// where the tests or users can see them; callers treat these like Python
// treats an uncaught exception (usually a 500).
type PyError struct {
	Class   string
	Message string
}

func (e *PyError) Error() string { return e.Message }

func pyError(class, message string) error { return &PyError{Class: class, Message: message} }

// PyErrorClass returns the Python class of a PyError, or "".
func PyErrorClass(err error) string {
	var p *PyError
	if errors.As(err, &p) {
		return p.Class
	}
	return ""
}

func attributeError(v any, attribute string) error {
	return pyError("AttributeError", "'"+pyTypeName(v)+"' object has no attribute '"+attribute+"'")
}

// ProxyYAMLError is a structured proxy validation failure. The underlying
// yamlx identity remains available, and API callers can use the existing
// strategy-validation channel instead of treating malformed YAML as a 500.
type ProxyYAMLError struct {
	// Cause is the native reader or proxy-shape validation error.
	Cause error
}

func (e *ProxyYAMLError) Error() string { return fmt.Sprintf("%s: %v", msgProxyYAMLInvalid, e.Cause) }

// Unwrap retains both the reader cause and the public validation category.
func (e *ProxyYAMLError) Unwrap() []error {
	return []error{e.Cause, &rules.StrategyReferenceError{Message: e.Error()}}
}
