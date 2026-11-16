// Package importer converts Mihomo, Surge, Loon and Shadowrocket files into
// shared-resource additions and profile fields. It performs no storage or I/O.
package importer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Proxy is a decoded Mihomo node. Protocol-specific options stay as lossless JSON.
type Proxy struct {
	Name    string
	Type    string
	Server  string
	Port    json.RawMessage
	Options map[string]json.RawMessage
}

// Provider identifies a subscription in the imported file.
type Provider struct {
	Name string
	URL  string
}

// Group is a client-independent policy selection before resource IDs are assigned.
type Group struct {
	Name, Type                             string
	Members, Providers                     []string
	Filter                                 string
	IncludeAllProxies, IncludeAllProviders bool
	URL                                    string
	Interval                               *int
	Extra                                  map[string]json.RawMessage
}

// Rule is a parsed ordered rule; RuleSet rules refer to provider names.
type Rule struct {
	Type, Value, Policy string
	NoResolve           bool
}

// RuleProvider describes a remote or inline ruleset.
type RuleProvider struct {
	Name, URL, Behavior string
	Content             *string
}

// CustomConfig preserves client settings outside the generated sections.
type CustomConfig struct{ Engine, Text string }

// Plan holds a parsed file; Build assigns IDs and resolves its references.
type Plan struct {
	Proxies       []Proxy
	Aliases       map[string]string
	Providers     []Provider
	Groups        []Group
	RuleProviders []RuleProvider
	Rules         []Rule
	Custom        *CustomConfig
	warnings      warnings
}

// Node is a shared node definition. Params preserve arbitrary protocol options.
type Node struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Type     string                     `json:"type"`
	Server   string                     `json:"server"`
	Port     json.RawMessage            `json:"port"`
	Params   map[string]json.RawMessage `json:"params"`
	Enabled  bool                       `json:"enabled"`
	original map[string]json.RawMessage
}

// UnmarshalJSON keeps the original definition for exact shared-node reuse.
func (n *Node) UnmarshalJSON(data []byte) error {
	type value Node
	var out value
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*n = Node(out)
	return json.Unmarshal(data, &n.original)
}

// Subscription is a shared source. Build reuses an existing source by URL.
type Subscription struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	Interval int    `json:"interval"`
	Enabled  bool   `json:"enabled"`
}

// LibraryItem is a ruleset added to the shared library.
type LibraryItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Behavior   string  `json:"behavior"`
	Enabled    bool    `json:"enabled"`
	SourceType string  `json:"source_type,omitempty"`
	URL        string  `json:"url,omitempty"`
	Content    *string `json:"content,omitempty"`
}

// Shared is the existing shared catalog or the additions returned by Build.
type Shared struct {
	Subscriptions []Subscription `json:"subscriptions"`
	Nodes         []Node         `json:"nodes"`
	RuleLibrary   []LibraryItem  `json:"rule_library"`
}

// OrderItem preserves interleaved manual-node and group member order.
type OrderItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ProxyGroup is the profile-local policy configuration.
type ProxyGroup struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	Type          string                     `json:"type"`
	Enabled       bool                       `json:"enabled"`
	Subscriptions []string                   `json:"subscriptions"`
	Regex         string                     `json:"regex"`
	ManualNodes   []string                   `json:"manual_nodes"`
	Aggregations  []string                   `json:"aggregations"`
	IncludeGroups []string                   `json:"include_groups"`
	Order         []OrderItem                `json:"proxies_order"`
	URL           string                     `json:"url,omitempty"`
	Interval      *int                       `json:"interval,omitempty"`
	Extra         map[string]json.RawMessage `json:"-"`
}

// MarshalJSON merges supported client-specific group options with typed fields.
func (g ProxyGroup) MarshalJSON() ([]byte, error) {
	type base ProxyGroup
	data, err := json.Marshal(base(g))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for k, v := range g.Extra {
		fields[k] = v
	}
	return json.Marshal(fields)
}

// RuleConfig is a profile rule or a reference to a shared ruleset.
type RuleConfig struct {
	ID        string  `json:"id"`
	ItemType  string  `json:"itemType"`
	RuleType  string  `json:"rule_type,omitempty"`
	Value     *string `json:"value,omitempty"`
	LibraryID string  `json:"library_rule_id,omitempty"`
	Policy    string  `json:"policy"`
	Enabled   bool    `json:"enabled"`
	NoResolve bool    `json:"no_resolve,omitempty"`
}

// EngineSettings contains the preserved custom configuration for one client.
type EngineSettings struct {
	CustomConfig string `json:"custom_config"`
}

// Profile contains only the fields contributed by an import.
type Profile struct {
	Groups []ProxyGroup    `json:"proxy_groups"`
	Rules  []RuleConfig    `json:"rule_configs"`
	Mihomo *EngineSettings `json:"mihomo,omitempty"`
	Surge  *EngineSettings `json:"surge,omitempty"`
	Loon   *EngineSettings `json:"loon,omitempty"`
}

// Summary reports added/reused resources and usable profile configuration.
type Summary struct {
	Nodes               int     `json:"nodes"`
	NodesReused         int     `json:"nodes_reused"`
	Subscriptions       int     `json:"subscriptions"`
	SubscriptionsReused int     `json:"subscriptions_reused"`
	RuleLibrary         int     `json:"rule_library"`
	RuleLibraryReused   int     `json:"rule_library_reused"`
	Groups              int     `json:"proxy_groups"`
	Rules               int     `json:"rules"`
	Rulesets            int     `json:"rulesets"`
	CustomConfig        *string `json:"custom_config"`
}

// Result can be marshalled directly into the repository's import-client callback.
type Result struct {
	Shared   Shared   `json:"shared"`
	Profile  Profile  `json:"profile"`
	Summary  Summary  `json:"summary"`
	Warnings []string `json:"warnings"`
}

// BuildOptions supplies default subscription selection and optional ID allocation.
type BuildOptions struct {
	DefaultSubscriptionIDs []string
	NewID                  func(prefix string) (string, error)
}

func randomID(prefix string) (string, error) {
	var data [6]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("create import resource ID: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(data[:]), nil
}
