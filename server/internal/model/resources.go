package model

import (
	"encoding/json"
	"encoding/json/jsontext"
)

// ResourceRef identifies a resource by its stable kind and ID.
type ResourceRef struct {
	Type string `json:"type,omitzero"`
	ID   string `json:"id,omitzero"`
}

// failClosed: a stored reference of another JSON kind (a node's dialer_ref
// stored as text) reads as an empty reference, which consumers refuse, not as
// no reference: a node meant to dial through a relay must not dial directly.
func (*ResourceRef) failClosed() {}

// ProviderHealthCheck holds explicit provider probe settings; nil members retain engine defaults.
type ProviderHealthCheck struct {
	Enable         *bool   `json:"enable,omitzero" input:"strict"`
	URL            *string `json:"url,omitzero" input:"strict"`
	Interval       *Int    `json:"interval,omitzero" input:"strict"`
	Timeout        *Int    `json:"timeout,omitzero" input:"strict"`
	Lazy           *bool   `json:"lazy,omitzero" input:"strict"`
	ExpectedStatus *string `json:"expected-status,omitzero" input:"strict"`
}

// Node is a shared node. Scalar attributes use the shared model codecs.
// Params is an open Mihomo engine mapping; protocol extensions retain their
// values and YAML tags.
type Node struct {
	ID               string       `json:"id,omitzero"`
	Name             string       `json:"name"`
	Type             string       `json:"type,omitzero"`
	Protocol         string       `json:"protocol,omitzero"`
	Server           string       `json:"server,omitzero"`
	Port             *Int         `json:"port,omitzero"`
	ProxyString      *string      `json:"proxy_string,omitzero"`
	Params           Dynamic      `json:"params,omitzero"`
	Enabled          *bool        `json:"enabled,omitzero"`
	Remark           *string      `json:"remark,omitzero"`
	SubscriptionID   string       `json:"subscription_id,omitzero"`
	SubscriptionName string       `json:"subscription_name,omitzero"`
	DialerRef        *ResourceRef `json:"dialer_ref,omitzero"`
	// ProxyChainID marks a node cloned by proxy-chain lowering on a resolved
	// configuration (generator runtime only; editors never write it).
	ProxyChainID string `json:"_proxy_chain_id,omitzero"`
	// ProtocolParams are the proxy protocol settings of a flat node (one
	// without proxy_string or params): its top-level members other than the
	// declared ones, an open set kept as stored and in order. Treat the value
	// as immutable; replace it to change it.
	ProtocolParams jsontext.Value `json:",embed"`
}

// Subscription is a stored source, excluding health/cache statistics added only to HTTP responses.
type Subscription struct {
	ID             string               `json:"id,omitzero"`
	Name           string               `json:"name,omitzero"`
	URL            string               `json:"url,omitzero"`
	Type           string               `json:"type,omitzero"`
	Enabled        *bool                `json:"enabled,omitzero"`
	Interval       *Int                 `json:"interval,omitzero" input:"strict"`
	RegexFilter    *string              `json:"regex_filter,omitzero"`
	Description    *string              `json:"description,omitzero"`
	HealthCheck    *ProviderHealthCheck `json:"health_check,omitzero"`
	HealthCheckURL *string              `json:"health_check_url,omitzero"`
}

// Aggregation selects shared subscriptions and manual nodes for a provider.
type Aggregation struct {
	ID             string               `json:"id,omitzero"`
	Name           string               `json:"name,omitzero"`
	Description    *string              `json:"description,omitzero"`
	Enabled        *bool                `json:"enabled,omitzero"`
	Subscriptions  []string             `json:"subscriptions,omitzero" input:"strict"`
	Nodes          []string             `json:"nodes,omitzero" input:"strict"`
	RegexFilter    *string              `json:"regex_filter,omitzero"`
	HealthCheck    *ProviderHealthCheck `json:"health_check,omitzero"`
	HealthCheckURL *string              `json:"health_check_url,omitzero"`
	CreatedAt      string               `json:"created_at,omitzero"`
	UpdatedAt      string               `json:"updated_at,omitzero"`
}

// RuleLibraryItem is a shared rule source; source, behavior and download defaults are resolved by consumers.
type RuleLibraryItem struct {
	ID         string  `json:"id,omitzero"`
	Name       string  `json:"name,omitzero"`
	Enabled    *bool   `json:"enabled,omitzero"`
	SourceType *string `json:"source_type,omitzero"`
	// Behavior stored as null is the empty behavior, not the default
	// "domain": Python's get('behavior', 'domain') returned None for it.
	Behavior       *string `json:"behavior,omitzero" null:"empty"`
	Format         *string `json:"format,omitzero"`
	URL            *string `json:"url,omitzero"`
	Content        *string `json:"content,omitzero"`
	Remark         *string `json:"remark,omitzero"`
	UpdateInterval *Int    `json:"update_interval,omitzero"`
	// Policy and Target are never valid on a library item (the target policy
	// belongs to the rule configuration); they are declared so validation can
	// refuse them with Python's message instead of dropping them.
	Policy json.RawMessage `json:"policy,omitzero"`
	Target json.RawMessage `json:"target,omitzero"`
}
