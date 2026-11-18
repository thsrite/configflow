package subs

import (
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"sync"
)

// DependencyRef identifies a shared dialer graph vertex without a package cycle.
type DependencyRef struct{ Kind, ID string }

// DependencyGraph carries the shared validator's deterministic graph projection.
type DependencyGraph struct {
	Order []DependencyRef
	Edges map[DependencyRef][]DependencyRef
}

// FilterCandidate keeps a name/type filter's opaque caller key intact.
type FilterCandidate struct{ Name, Type, Key string }

// SharedDialers connects the sole convert-owned implementations. These pure
// collaborators consume existing typed records; they never re-decode a config.
type SharedDialers struct {
	Validate       func(*model.Config) error
	ValidateChains func(*model.Config) error
	Shapes         func(*model.Config, bool) error
	Members        func(model.ProxyGroup) []DependencyRef
	Sources        func(*model.Config, model.ProxyGroup) ([]DependencyRef, error)
	NodeTarget     func(*model.Config, model.Node) (*DependencyRef, error)
	NodeDialer     func(model.Node) (*yamlx.Node, error)
	Resolve        func(*model.Config, model.Node) (*string, error)
	Graph          func(*model.Config) (*DependencyGraph, error)
	Filter         func([]FilterCandidate, string, string, string) ([]FilterCandidate, error)
	Structured     func(string) (*yamlx.Node, error)
}

var dialerCollaborators struct {
	sync.RWMutex
	value SharedDialers
}

// SetSharedDialers installs one coherent collaborator set and returns restoration.
// Production installs it once from convert; tests may scope replacements.
func SetSharedDialers(value SharedDialers) func() {
	dialerCollaborators.Lock()
	old := dialerCollaborators.value
	dialerCollaborators.value = value
	dialerCollaborators.Unlock()
	return func() { dialerCollaborators.Lock(); dialerCollaborators.value = old; dialerCollaborators.Unlock() }
}
func sharedDialers() SharedDialers {
	dialerCollaborators.RLock()
	defer dialerCollaborators.RUnlock()
	return dialerCollaborators.value
}
