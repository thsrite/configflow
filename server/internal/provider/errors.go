package provider

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/pyyaml"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// Python classes of the reference errors this package raises or re-wraps.
const (
	classStrategy = "StrategyReferenceError"
	classDialer   = "DialerReferenceError"
	classListener = "ListenerReferenceError"
)

// Error is a StrategyReferenceError (or its subclass DialerReferenceError /
// ListenerReferenceError, see Class) raised by this package: a re-wrapped
// exception carrying the provider contexts already added (Python's
// _provider_contexts) and, for shape errors, the offending field
// (Python's provider_field). errors.As finds the matching
// *convert.DialerReferenceError, *subs.ListenerReferenceError and
// *rules.StrategyReferenceError.
type Error struct {
	Class   string
	Message string

	contexts []contextKey
	field    []any
}

func (e *Error) Error() string { return e.Message }

// PythonClass is the Python exception class name.
func (e *Error) PythonClass() string { return e.Class }

// Unwrap exposes the Python base classes.
func (e *Error) Unwrap() error {
	switch e.Class {
	case classDialer:
		return &convert.DialerReferenceError{Message: e.Message}
	case classListener:
		return &subs.ListenerReferenceError{Message: e.Message}
	}
	return &rules.StrategyReferenceError{Message: e.Message}
}

// Field is the provider_field of a shape error (nil otherwise): strings
// for keys, ints for list indices.
func (e *Error) Field() []any { return e.field }

type contextKey struct{ kind, name, path string }

func dialerErr(message string) *Error { return &Error{Class: classDialer, Message: message} }

// referenceClass is type(exc).__name__ for a StrategyReferenceError.
func referenceClass(err error) string {
	var own *Error
	if errors.As(err, &own) {
		return own.Class
	}
	var dialer *convert.DialerReferenceError
	if errors.As(err, &dialer) {
		return classDialer
	}
	var listener *subs.ListenerReferenceError
	if errors.As(err, &listener) {
		return classListener
	}
	return classStrategy
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

// pyStrIfTruthy is str(value) for a truthy value, else the empty string.
func pyStrIfTruthy(value any) string {
	if !jsonx.Truthy(value) {
		return ""
	}
	return jsonx.Str(value)
}

// providerContext is _provider_context(exc, name=, path=, kind=): keep
// validation exceptions outside the transport-fallback boundary, adding the
// owning provider once.
func providerContext(err error, name, path any, kind string) error {
	class := referenceClass(err)
	contexts := contextsOf(err)
	key := contextKey{kind, jsonx.Repr(name), jsonx.Repr(path)}
	seen := false
	for _, c := range contexts {
		if c == key {
			seen = true
			break
		}
	}
	if !(jsonx.Truthy(name) || jsonx.Truthy(path)) || seen {
		return &Error{Class: class, Message: err.Error(), contexts: contexts}
	}
	message := currentLocations().PreparationContext(err.Error(), pyStrIfTruthy(name), pyStrIfTruthy(path), kind)
	next := append(append([]contextKey{}, contexts...), key)
	return &Error{Class: class, Message: message, contexts: next}
}

// shapeError is _shape_error(message, *field).
func shapeError(message string, field ...any) *Error {
	var label strings.Builder
	for index, item := range field {
		switch v := item.(type) {
		case int:
			label.WriteString("[" + strconv.Itoa(v) + "]")
		case string:
			if index > 0 {
				label.WriteString(".")
			}
			label.WriteString(v)
		}
	}
	return &Error{Class: classDialer, Message: message + "\n字段：" + label.String(), field: field}
}

// yamlCause returns the yaml.YAMLError of a failed yaml.safe_load(text) as
// PyYAML's exception object (class, context, problem, marks), or nil when
// err is not a yaml.YAMLError.
func yamlCause(err error, text string) *pyyaml.Error {
	var ye *yamlx.Error
	if !errors.As(err, &ye) || !ye.IsYAMLError() {
		return nil
	}
	if ye.Class == "ReaderError" {
		return &pyyaml.Error{Class: "ReaderError"}
	}
	// Reader, scanner, parser and composer errors come from composing; the
	// same text composes to the same error with its marks.
	if _, composeErr := pyyaml.Compose(text); composeErr != nil {
		var pe *pyyaml.Error
		if errors.As(composeErr, &pe) {
			return pe
		}
	}
	// A ConstructorError: rebuild context/problem/marks from str(exc).
	return parseMarkedError(ye.Class, ye.Msg)
}

// parseMarkedError reads str(MarkedYAMLError) back: lines are context,
// context mark, problem, problem mark, where a mark is `  in "<name>", line
// L, column C` optionally followed by `:` and two snippet lines.
func parseMarkedError(class, msg string) *pyyaml.Error {
	e := &pyyaml.Error{Class: class}
	lines := strings.Split(msg, "\n")
	var texts []string
	var marks []*pyyaml.Mark
	var order []byte // 't' text, 'm' mark
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if mark, snippet, ok := parseMarkLine(line); ok {
			marks = append(marks, mark)
			order = append(order, 'm')
			if snippet {
				i += 2
			}
			continue
		}
		texts = append(texts, line)
		order = append(order, 't')
	}
	// Assign in the order str() writes them.
	ti, mi := 0, 0
	stage := 0 // 0 context, 1 context mark, 2 problem, 3 problem mark
	for _, kind := range order {
		switch kind {
		case 't':
			if stage == 0 {
				e.Context = texts[ti]
				stage = 1
			} else {
				e.Problem = texts[ti]
				stage = 3
			}
			ti++
		case 'm':
			if stage <= 1 {
				e.ContextMark = marks[mi]
				stage = 2
			} else {
				e.ProblemMark = marks[mi]
				stage = 4
			}
			mi++
		}
	}
	// A single text with only a problem mark is the problem (context None).
	if e.Problem == "" && e.Context != "" && e.ContextMark != nil && e.ProblemMark == nil {
		e.Problem, e.Context = e.Context, ""
		e.ProblemMark, e.ContextMark = e.ContextMark, nil
	} else if e.Problem == "" && e.Context != "" && e.ContextMark == nil {
		e.Problem, e.Context = e.Context, ""
	}
	return e
}

func parseMarkLine(line string) (*pyyaml.Mark, bool, bool) {
	if !strings.HasPrefix(line, "  in \"") {
		return nil, false, false
	}
	rest := line[len("  in \""):]
	end := strings.LastIndex(rest, "\", line ")
	if end < 0 {
		return nil, false, false
	}
	name := rest[:end]
	tail := rest[end+len("\", line "):]
	snippet := strings.HasSuffix(tail, ":")
	tail = strings.TrimSuffix(tail, ":")
	var l, c int
	if _, err := fmt.Sscanf(tail, "%d, column %d", &l, &c); err != nil {
		return nil, false, false
	}
	return &pyyaml.Mark{Name: name, Line: l - 1, Column: c - 1}, snippet, true
}

// nodeAt is the field lookup of _provider_source_error: the start mark of
// the node a provider_field points at (SafeLoader composition, merge keys
// flattened, last duplicate key wins), or nil.
func nodeAt(text string, field []any) (*pyyaml.Mark, bool) {
	node, err := pyyaml.Compose(text)
	if err != nil {
		return nil, false
	}
	for _, item := range field {
		var next *pyyaml.Node
		switch {
		case node != nil && node.Kind == pyyaml.MappingNode && isString(item):
			if err := yamlx.FlattenMapping(node); err != nil {
				return nil, false
			}
			key := item.(string)
			for i := len(node.Pairs) - 1; i >= 0; i-- {
				k := node.Pairs[i][0]
				if k.Kind == pyyaml.ScalarNode && k.Value == key {
					next = node.Pairs[i][1]
					break
				}
			}
		case node != nil && node.Kind == pyyaml.SequenceNode && isInt(item) && item.(int) < len(node.Items):
			next = node.Items[item.(int)]
		}
		node = next
		if node == nil {
			break
		}
	}
	if node == nil {
		return nil, true
	}
	start := node.Start
	return &start, true
}

func isString(v any) bool { _, ok := v.(string); return ok }
func isInt(v any) bool    { _, ok := v.(int); return ok }

// providerSourceError is _provider_source_error(exc, text, source_label):
// resolve a known field only on failure; never guess a node's line number.
// yamlErr is the yaml.YAMLError of the load (nil for reference errors).
func providerSourceError(err error, yamlErr *pyyaml.Error, text, sourceLabel string) error {
	var mark *pyyaml.Mark
	if yamlErr != nil {
		mark = yamlErr.ProblemMark
	}
	var field []any
	var own *Error
	if errors.As(err, &own) {
		field = own.field
	}
	if len(field) > 0 {
		if found, ok := nodeAt(text, field); ok {
			mark = found
		}
	}
	message := err.Error()
	var cause *pyyaml.Error
	if yamlErr != nil {
		message, cause = "Provider YAML 内容无效", yamlErr
	}
	var line, column *int
	if mark != nil {
		l, c := mark.Line+1, mark.Column+1
		line, column = &l, &c
	}
	detail := currentLocations().SourceError(message, text, line, column, cause, sourceLabel)
	return dialerErr(diag.FailureText(detail, nil, 0))
}

// pyError is an exception Python raises on a value the code does not check.
type pyError struct {
	class   string
	message string
}

func (e *pyError) Error() string       { return e.message }
func (e *pyError) PythonClass() string { return e.class }

func keyError(key string) error { return &pyError{"KeyError", jsonx.Repr(key)} }

func attributeError(v any, attribute string) error {
	return &pyError{"AttributeError", "'" + httpx.PyTypeName(v) + "' object has no attribute '" + attribute + "'"}
}

func typeError(message string) error { return &pyError{"TypeError", message} }

// subscriptError is CPython 3.11's TypeError for v['key'] on a non-dict.
func subscriptError(v any) error {
	switch v.(type) {
	case string:
		return typeError("string indices must be integers, not 'str'")
	case []any:
		return typeError("list indices must be integers or slices, not str")
	}
	return typeError("'" + pyTypeName(v) + "' object is not subscriptable")
}
