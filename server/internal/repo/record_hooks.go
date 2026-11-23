package repo

import (
	"context"
	"encoding/json"

	"configflow/internal/model"
)

// DeletionNode is a selected node's public identity.
type DeletionNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DeletionUsage identifies a resource affected by node deletion.
type DeletionUsage struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DeletionGroup identifies an affected group and its profile.
type DeletionGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
}

// NodeDeletionPreview is the reviewed deletion plan, including its confirmation digest.
type NodeDeletionPreview struct {
	Nodes        []DeletionNode  `json:"nodes"`
	Usages       []DeletionUsage `json:"usages"`
	Groups       []DeletionGroup `json:"groups"`
	Blockers     []string        `json:"blockers"`
	EmptySources []string        `json:"empty_sources"`
	Confirmation string          `json:"confirmation,omitempty"`
}

// RecordHooks supplies typed collaborators for repository operations. Register
// these through Hooks.RecordHooks; a typed collaborator always takes precedence.
type RecordHooks struct {
	// JobLockRecord attempts the same nonblocking router publication lease.
	JobLockRecord func(context.Context, string, string, model.Router) (func(), bool)
	// ConfigurationCommittedRecords observes immutable native generations after commit.
	ConfigurationCommittedRecords func(*Repository, *model.Document, *model.Document)

	// PlanNodeDeletionRecords returns an owned preview and candidate without I/O.
	PlanNodeDeletionRecords func(*model.Document, []string) (*NodeDeletionPreview, *model.Document, error)
	// SanitizeExternalDocument receives a redacted candidate and original settings.
	SanitizeExternalDocument func(context.Context, *model.Document, model.SystemConfig) (*model.Document, error)
	// RedactOxidnsJSON redacts schema-free OxiDNS settings.
	RedactOxidnsJSON func(json.RawMessage) (json.RawMessage, error)
	// RedactRoutersJSON removes secrets from the routers JSON value.
	RedactRoutersJSON func(json.RawMessage) (json.RawMessage, error)
	// RedactEasytierJSON is deprecated and ignored; repo calls
	// convert.RedactEasyTierJSON directly. Retained temporarily so
	// internal/routers tests remain untouched until W9.
	RedactEasytierJSON func(json.RawMessage) (json.RawMessage, error)
}
