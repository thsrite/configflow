package store

import "encoding/json"

// Field is one stored field. Value is opaque JSON, including its original bytes.
type Field struct {
	Name  string
	Value json.RawMessage
}

// Section contains fields in their stored order.
type Section struct{ Fields []Field }

// ProfileRows contains the fields of one profile, identified by ID.
type ProfileRows struct {
	ID     string
	Fields []Field
}

// Document is the row-level representation of a configuration. Order records
// the root member order; a nil section means that the section is absent.
// Documents returned by the store are immutable, including their byte slices.
//
// State holds the agent_state rows (storage version 2) in (owner, member)
// order as read; they are not part of the configuration rows.
type Document struct {
	Order          []string
	Root           []Field
	System, Shared *Section
	Profiles       []ProfileRows
	State          []StateRow
}

// Meta holds the store's metadata and opaque additional metadata members.
// Field values in Document are never decoded into this type.
type Meta struct {
	StorageVersion int
	StoreID        string
	Generation     int64
	CommitID       string
	RootOrder      []string
	CreatedAt      string
	Extra          map[string]json.RawMessage
}
