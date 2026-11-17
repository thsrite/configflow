package model

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
