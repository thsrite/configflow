package model

import "strings"

// IssueKind identifies an invalid stored field without including its value.
type IssueKind string

const (
	// IssueType means a stored JSON value cannot decode into its declared Go type.
	IssueType IssueKind = "type"
)

// joinPath appends a member name to an issue path.
func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// FieldIssue describes a tolerated invalid field. Paths use schema names and
// numeric indices, never user-supplied map keys, identifiers or stored values.
type FieldIssue struct {
	Path string
	Kind IssueKind
}

// IssueUnder reports whether an issue is at path or nested below it. Whole path
// segments are compared, so "ipv6_pd_guard" does not match "ipv6_pd_guard_state".
func IssueUnder(issues []FieldIssue, path string) bool {
	for _, issue := range issues {
		rest, found := strings.CutPrefix(issue.Path, path)
		if found && (rest == "" || rest[0] == '.' || rest[0] == '[' || rest[0] == '{') {
			return true
		}
	}
	return false
}
