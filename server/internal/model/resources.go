package model

// ResourceRef identifies a resource by its stable kind and ID.
type ResourceRef struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes ResourceRef with stored-member preservation and R5 fallback.
func (r *ResourceRef) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when ResourceRef is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *ResourceRef) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged ResourceRef members.
func (r ResourceRef) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// ProviderHealthCheck holds explicit provider probe settings; nil members retain engine defaults.
type ProviderHealthCheck struct {
	Enable         *bool   `json:"enable"`
	URL            *string `json:"url"`
	Interval       *Int    `json:"interval"`
	Timeout        *Int    `json:"timeout"`
	Lazy           *bool   `json:"lazy"`
	ExpectedStatus *string `json:"expected-status"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes ProviderHealthCheck with stored-member preservation and R5 fallback.
func (r *ProviderHealthCheck) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when ProviderHealthCheck is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *ProviderHealthCheck) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged ProviderHealthCheck members.
func (r ProviderHealthCheck) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// Node is a shared node. Params is an open Mihomo engine mapping, expanded wholesale by Python; protocol extensions remain dynamic.
type Node struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Type             string       `json:"type"`
	Protocol         string       `json:"protocol"`
	Server           string       `json:"server"`
	Port             Int          `json:"port"`
	ProxyString      *string      `json:"proxy_string"`
	Params           Dynamic      `json:"params"`
	Enabled          *bool        `json:"enabled"`
	Remark           string       `json:"remark"`
	SubscriptionID   string       `json:"subscription_id"`
	SubscriptionName string       `json:"subscription_name"`
	DialerRef        *ResourceRef `json:"dialer_ref"`
	Fields           Fields
	raw              Raw
}

// UnmarshalJSON decodes Node with stored-member preservation and R5 fallback.
func (r *Node) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Node is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Node) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Node members.
func (r Node) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// Subscription is a stored source, excluding health/cache statistics added only to HTTP responses.
type Subscription struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	URL            string               `json:"url"`
	Type           string               `json:"type"`
	Enabled        *bool                `json:"enabled"`
	Interval       *Int                 `json:"interval"`
	RegexFilter    *string              `json:"regex_filter"`
	Description    string               `json:"description"`
	HealthCheck    *ProviderHealthCheck `json:"health_check"`
	HealthCheckURL *string              `json:"health_check_url"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes Subscription with stored-member preservation and R5 fallback.
func (r *Subscription) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Subscription is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Subscription) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Subscription members.
func (r Subscription) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// Aggregation selects shared subscriptions and manual nodes for a provider.
type Aggregation struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Description    string               `json:"description"`
	Enabled        *bool                `json:"enabled"`
	Subscriptions  []string             `json:"subscriptions"`
	Nodes          []string             `json:"nodes"`
	RegexFilter    *string              `json:"regex_filter"`
	HealthCheck    *ProviderHealthCheck `json:"health_check"`
	HealthCheckURL *string              `json:"health_check_url"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes Aggregation with stored-member preservation and R5 fallback.
func (r *Aggregation) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Aggregation is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Aggregation) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Aggregation members.
func (r Aggregation) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RuleLibraryItem is a shared rule source; source, behavior and download defaults are resolved by consumers.
type RuleLibraryItem struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Enabled        *bool   `json:"enabled"`
	SourceType     *string `json:"source_type"`
	Behavior       *string `json:"behavior"`
	Format         *string `json:"format"`
	URL            *string `json:"url"`
	Content        *string `json:"content"`
	UpdateInterval *Int    `json:"update_interval"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes RuleLibraryItem with stored-member preservation and R5 fallback.
func (r *RuleLibraryItem) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when RuleLibraryItem is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *RuleLibraryItem) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged RuleLibraryItem members.
func (r RuleLibraryItem) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
