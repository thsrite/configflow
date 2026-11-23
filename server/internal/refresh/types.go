package refresh

import (
	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/substore"
	"context"
	"encoding/json"
)

// Traffic holds provider quota numbers without rounding arbitrary-size
// tokens; only the numbers the provider reported are present.
type Traffic struct {
	Upload   *json.Number `json:"upload,omitzero"`
	Download *json.Number `json:"download,omitzero"`
	Total    *json.Number `json:"total,omitzero"`
	Expire   *json.Number `json:"expire,omitzero"`
}

// empty reports traffic without any number (Python: `if traffic:`).
func (r *Traffic) empty() bool { return r == nil || *r == (Traffic{}) }

// SourceResult is the text and provenance of one subscription fetch.
type SourceResult struct {
	Text, Source, FetchedAt string
	Traffic                 *Traffic
}

// SourceMetadata belongs to the subscription cache. Hosts/Dialers are opaque
// owner payloads; refresh neither interprets nor changes their private schema.
// subscription_name, url and fetched_at are always written (url and
// fetched_at may be null).
type SourceMetadata struct {
	SubscriptionName string          `json:"subscription_name"`
	URL              *string         `json:"url"`
	FetchedAt        *string         `json:"fetched_at"`
	Hosts            json.RawMessage `json:"hosts,omitzero"`
	Dialers          json.RawMessage `json:"dialers,omitzero"`
}

// NodeCache is the consumed view of the subscription owner's stored receipt.
// MetadataDamaged: the stored metadata is not an object or its url is not
// text (the cache must not be claimed for any URL); UpdatedAtDamaged: the
// stored updated_at is not text.
type NodeCache struct {
	SubscriptionID   string          `json:"subscription_id"`
	Nodes            []model.Node    `json:"nodes"`
	Count            model.Int       `json:"count"`
	UpdatedAt        *string         `json:"updated_at"`
	Metadata         *SourceMetadata `json:"metadata,omitzero"`
	MetadataDamaged  bool            `json:"-"`
	UpdatedAtDamaged bool            `json:"-"`
}

// SourceOwners are the existing subscription/parser/cache collaborators.
// Every I/O callback receives the admitted owner's Runtime generation.
type SourceOwners struct {
	Fetch      func(context.Context, string, string, substore.FetchOptions) (*SourceResult, error)
	ParseNodes func(context.Context, string) ([]model.Node, error)
	Metadata   func(context.Context, string) (json.RawMessage, json.RawMessage, error)
	SaveNodes  func(context.Context, string, []model.Node, *SourceMetadata) (*NodeCache, error)
	// LoadNodes may return a record shared with other readers: read only.
	LoadNodes        func(context.Context, string) (*NodeCache, error)
	LoadCacheSummary func(context.Context, string) (*substore.CacheSummary, error)
	FilterNodes      func(model.Subscription, []model.Node) []model.Node
	Converter        convert.NodeConverter
}

// RefreshEnv supplies native collaborators; no configuration/DTO is untyped.
type RefreshEnv struct {
	Clock      substore.Clock
	Sources    SourceOwners
	RuleClient *rulefetch.Client
	HTTP       substore.HTTPClient
	RandomHex  func() string
}
