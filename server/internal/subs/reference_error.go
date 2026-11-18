package subs

import (
	"configflow/internal/rules"
	"errors"
)

// DialerReferenceError classifies a dependency or emitted-name conflict while
// retaining both the strategy category and any native validation cause.
type DialerReferenceError struct {
	Message string
	cause   error
}

func (e *DialerReferenceError) Error() string { return e.Message }

// Unwrap preserves the shared strategy category and native error chain.
func (e *DialerReferenceError) Unwrap() error {
	base := &rules.StrategyReferenceError{Message: e.Message}
	if e.cause != nil {
		return errors.Join(base, e.cause)
	}
	return base
}
func dre(message string) error { return &DialerReferenceError{Message: message} }
func referenceFailure(err error) error {
	if err == nil {
		return nil
	}
	var reference *DialerReferenceError
	if errors.As(err, &reference) {
		return err
	}
	return &DialerReferenceError{Message: err.Error(), cause: err}
}
