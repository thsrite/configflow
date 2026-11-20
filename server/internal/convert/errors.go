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

// DialerFailure classifies a native validation failure for public provider/API
// boundaries while retaining the underlying error chain and visible diagnostic.
func DialerFailure(err error) error {
	if err == nil {
		return nil
	}
	var reference *DialerReferenceError
	var yamlError *ProxyYAMLError
	if errors.As(err, &reference) || errors.As(err, &yamlError) {
		return err
	}
	return &DialerReferenceError{Message: err.Error(), cause: err}
}

// EasyTierSettingsError is mihomo_easytier.EasyTierSettingsError (a
// ValueError).
type EasyTierSettingsError struct{ Message string }

func (e *EasyTierSettingsError) Error() string { return e.Message }

func easytierError(message string) error { return &EasyTierSettingsError{Message: message} }

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
