package model

import (
	"reflect"
	"strings"
)

// IssueKind identifies an invalid stored field without including its value.
type IssueKind string

const (
	// IssueType means a stored JSON value cannot decode into its declared Go type.
	IssueType IssueKind = "type"
)

// FieldIssue describes a tolerated invalid field. Paths use schema names and
// numeric indices, never user-supplied map keys, identifiers or stored values.
type FieldIssue struct {
	Path string
	Kind IssueKind
}

// Issues returns the value-free diagnostics recorded when value and everything
// nested in it were decoded, with paths relative to value. It describes the
// stored bytes, not later assignments; a record built in Go has none. Code that
// acts on stored evidence (router ownership manifests, deployment receipts) uses
// it to tell an R5 fallback zero value from a genuinely empty one.
func Issues(value any) []FieldIssue { return valueIssues(reflect.ValueOf(value), "") }

// IssueUnder reports whether an issue is at path or nested below it. Whole path
// segments are compared, so "ipv6_pd_guard" does not match "ipv6_pd_guard_state".
func IssueUnder(issues []FieldIssue, path string) bool {
	for _, issue := range issues {
		rest, found := strings.CutPrefix(issue.Path, path)
		if found && (rest == "" || rest[0] == '.' || rest[0] == '[') {
			return true
		}
	}
	return false
}
