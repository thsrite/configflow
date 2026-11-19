package provider

import (
	"errors"
	"strconv"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// FieldStep identifies a mapping key or a sequence index in provider input.
type FieldStep struct {
	Key     string
	Index   int
	Indexed bool
}

// FieldPath locates a rejected value for source diagnostics.
type FieldPath []FieldStep

// Error attaches provider ownership and a source path to a typed reference
// failure. Unwrap retains both its domain classification and original cause.
type Error struct {
	Message   string
	contexts  []contextKey
	field     FieldPath
	reference error
	cause     error
}

// Error returns the user-facing diagnostic.
func (e *Error) Error() string { return e.Message }

// Unwrap exposes the typed domain failure and its original native cause.
func (e *Error) Unwrap() error { return errors.Join(e.reference, e.cause) }

// Field returns an independent path to the rejected provider value.
func (e *Error) Field() FieldPath { return append(FieldPath(nil), e.field...) }

type contextKey struct{ kind, name, path string }

func dialerErr(message string) *Error {
	return &Error{Message: message, reference: &convert.DialerReferenceError{Message: message}}
}
func referenceError(err error, message string) error {
	var dialer *convert.DialerReferenceError
	if errors.As(err, &dialer) {
		return &convert.DialerReferenceError{Message: message}
	}
	var listener *subs.ListenerReferenceError
	if errors.As(err, &listener) {
		return &subs.ListenerReferenceError{Message: message}
	}
	return &rules.StrategyReferenceError{Message: message}
}

func isStrategyReference(err error) bool {
	var strategy *rules.StrategyReferenceError
	return errors.As(err, &strategy)
}

func isDialerReference(err error) bool {
	var dialer *convert.DialerReferenceError
	return errors.As(err, &dialer)
}

func contextsOf(err error) []contextKey {
	var own *Error
	if errors.As(err, &own) {
		return own.contexts
	}
	return nil
}

// providerContext attaches an owning provider once, preserving its typed cause.
func providerContext(err error, name, path, kind string) error {
	contexts := contextsOf(err)
	key := contextKey{kind, name, path}
	for _, context := range contexts {
		if context == key {
			return err
		}
	}
	if name == "" && path == "" {
		return &Error{reference: referenceError(err, err.Error()), Message: err.Error(), contexts: contexts, cause: err}
	}
	message := currentLocations().PreparationContext(err.Error(), name, path, kind)
	return &Error{reference: referenceError(err, message), Message: message, contexts: append(append([]contextKey{}, contexts...), key), cause: err}
}

func proxyField(index int, member string) FieldPath {
	path := FieldPath{{Key: "proxies"}, {Index: index, Indexed: true}}
	if member != "" {
		path = append(path, FieldStep{Key: member})
	}
	return path
}
func shapeError(message string, field FieldPath) *Error {
	var label strings.Builder
	for index, step := range field {
		if step.Indexed {
			label.WriteString("[" + strconv.Itoa(step.Index) + "]")
		} else {
			if index > 0 {
				label.WriteByte('.')
			}
			label.WriteString(step.Key)
		}
	}
	result := dialerErr(message + msgField + label.String())
	result.field = field
	return result
}

// yamlCause selects facade failures for the native A11 diagnostic hook.
func yamlCause(err error) error {
	if !errors.Is(err, yamlx.ErrType) && !errors.Is(err, yamlx.ErrSyntax) && !errors.Is(err, yamlx.ErrPolicy) && !errors.Is(err, yamlx.ErrAliasRecursion) && !errors.Is(err, yamlx.ErrAliasBudget) {
		return nil
	}
	return err
}

// nodeAt is the field lookup of _provider_source_error: the start mark of
// the node a provider_field points at, following shared merge/alias policies.
func nodeAtPath(text string, field FieldPath) (*yamlx.Node, bool) {
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, false
	}
	for _, item := range field {
		node = yamlx.Resolve(node)
		if node == nil {
			break
		}
		if item.Indexed {
			if node.Kind != yaml.SequenceNode || item.Index < 0 || item.Index >= len(node.Content) {
				node = nil
			} else {
				node = node.Content[item.Index]
			}
		} else {
			node = yamlx.Get(node, item.Key)
		}
	}
	node = yamlx.Resolve(node)
	if node == nil {
		return nil, true
	}
	return node, true
}

// providerSourceError is _provider_source_error(exc, text, source_label):
// resolve a known field only on failure; never guess a node's line number.
// yamlErr is the yaml.YAMLError of the load (nil for reference errors).
func providerSourceError(err, yamlErr error, text, sourceLabel string) error {
	var line, column *int
	var syntax *yamlx.SyntaxError
	if errors.As(yamlErr, &syntax) && syntax.Line > 0 {
		l, c := syntax.Line, max(1, syntax.Column)
		line, column = &l, &c
	}
	var field FieldPath
	var own *Error
	if errors.As(err, &own) {
		field = own.field
	}
	if len(field) > 0 {
		if found, ok := nodeAtPath(text, field); ok {
			line, column = nil, nil
			if found != nil {
				l, c := found.Line, found.Column
				line, column = &l, &c
			}
		}
	}
	message := err.Error()
	var cause error
	if yamlErr != nil {
		message, cause = msgProviderYAMLInvalid, yamlErr
	}
	detail := currentLocations().SourceError(message, text, line, column, cause, sourceLabel)
	result := dialerErr(diag.FailureText(detail, nil, 0))
	result.cause = err
	return result
}

// InputError identifies malformed internal input without emulating language exceptions.
// It deliberately remains outside reference errors and their transport fallback policy.
type InputError struct {
	Field string
	Cause error
}

// Error describes the invalid field without revealing its stored value.
func (e *InputError) Error() string { return "provider: invalid " + e.Field }

// Unwrap retains the native decode or validation failure.
func (e *InputError) Unwrap() error { return e.Cause }
