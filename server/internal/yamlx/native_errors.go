package yamlx

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrSyntax identifies YAML syntax errors reported by the parser.
	ErrSyntax = errors.New("yamlx: invalid YAML")
	// ErrAliasRecursion identifies a recursive YAML alias graph.
	ErrAliasRecursion = errors.New("yamlx: recursive alias")
	// ErrAliasBudget identifies alias expansion beyond the shared budget.
	ErrAliasBudget = errors.New("yamlx: alias budget exceeded")
	// ErrPolicy identifies input refused by the facade's validation policy.
	ErrPolicy = errors.New("yamlx: policy violation")
	// ErrDuplicateKey identifies a duplicate mapping key; it also matches ErrPolicy.
	ErrDuplicateKey = errors.New("yamlx: duplicate mapping key")
	// ErrType identifies YAML values incompatible with a typed destination.
	ErrType = errors.New("yamlx: incompatible YAML type")
	// ErrMultipleDocuments identifies input containing more than one document.
	ErrMultipleDocuments = errors.New("yamlx: more than one document")
	// ErrUnsafeEdit identifies an edit whose output cannot be verified.
	ErrUnsafeEdit = errors.New("yamlx: edit cannot keep the rest of the text")
)

// multipleDocumentsError is the Chinese diagnostic for ErrMultipleDocuments.
type multipleDocumentsError struct{ line int }

func (e *multipleDocumentsError) Error() string { return fmt.Sprintf(msgMultipleDocuments, e.line) }

func (e *multipleDocumentsError) Unwrap() error { return errors.Join(ErrMultipleDocuments, ErrPolicy) }

type decodeTypeError struct{ cause error }

func (e *decodeTypeError) Error() string { return fmt.Sprintf(msgTypeMismatch, e.cause) }

func (e *decodeTypeError) Unwrap() []error { return []error{ErrType, e.cause} }

// SyntaxError describes invalid YAML. Positions are one-based when available;
// yaml.v3 sometimes supplies a line only, or no position at all (zero).
type SyntaxError struct {
	Line, Column int
	Problem      string
	cause        error
}

// Error returns the diagnostic supplied by yaml.v3 or the facade's policy check.
func (e *SyntaxError) Error() string {
	if e.cause == nil {
		if e.Line > 0 {
			return fmt.Sprintf(msgSyntaxAtLine, e.Line, e.Problem)
		}
		return fmt.Sprintf(msgSyntaxDetail, e.Problem)
	}
	return e.Problem
}

// Unwrap identifies syntax, policy or alias failures without conflating them.
func (e *SyntaxError) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	return ErrSyntax
}

// DuplicateKeyError identifies the duplicate key and its one-based source line.
// It is carried by SyntaxError and can be inspected with errors.As.
type DuplicateKeyError struct {
	Key  string
	Line int
}

// Error reports the key and line using the facade diagnostic.
func (e *DuplicateKeyError) Error() string { return fmt.Sprintf(msgDuplicateKey, e.Key, e.Line) }

// Unwrap identifies a duplicate-key policy failure.
func (e *DuplicateKeyError) Unwrap() error { return errors.Join(ErrDuplicateKey, ErrPolicy) }

func duplicateKeyError(n *Node, key string) error {
	detail := &DuplicateKeyError{Key: key, Line: n.Line}
	return &SyntaxError{Line: n.Line, Column: n.Column, Problem: detail.Error(), cause: detail}
}

var nativePosition = regexp.MustCompile(`line ([0-9]+)(?:, column ([0-9]+))?`)

func syntaxError(err error) error {
	if err == nil {
		return nil
	}
	var native *SyntaxError
	if errors.As(err, &native) {
		return err
	}
	e := &SyntaxError{Problem: err.Error()}
	if depth, ok := strings.CutPrefix(e.Problem, "yaml: exceeded max depth of "); ok {
		e.Problem = fmt.Sprintf(msgDepthLimit, depth)
	}
	if m := nativePosition.FindStringSubmatch(e.Problem); m != nil {
		e.Line, _ = strconv.Atoi(m[1])
		if m[2] != "" {
			e.Column, _ = strconv.Atoi(m[2])
		}
	}
	return e
}

func nodeError(n *Node, problem string) error {
	e := &SyntaxError{Problem: problem, cause: ErrPolicy}
	if n != nil {
		e.Line, e.Column = n.Line, n.Column
	}
	return e
}

func invalidNode(n *Node, problem string) error {
	return nodeError(n, fmt.Sprintf("yamlx: %s", problem))
}

func aliasNodeError(n *Node, problem string, cause error) error {
	e := &SyntaxError{Problem: problem, cause: cause}
	if n != nil {
		e.Line, e.Column = n.Line, n.Column
	}
	return e
}
