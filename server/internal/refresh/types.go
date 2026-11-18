package refresh

import (
	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/substore"
	"context"
	"encoding/json"
)

// Traffic holds provider quota numbers without rounding arbitrary-size tokens.
type Traffic struct {
	Upload   *json.Number `json:"upload"`
	Download *json.Number `json:"download"`
	Total    *json.Number `json:"total"`
	Expire   *json.Number `json:"expire"`
	Fields   model.Fields
	Raw      model.Raw
}

func (r *Traffic) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }
func (r Traffic) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&r, &r.Raw) }

// SourceResult is the text and provenance of one subscription fetch.
type SourceResult struct {
	Text, Source, FetchedAt string
	Traffic                 *Traffic
}

// SourceMetadata belongs to the subscription cache. Hosts/Dialers are opaque
// owner payloads; refresh neither interprets nor changes their private schema.
type SourceMetadata struct {
	SubscriptionName string          `json:"subscription_name"`
	URL              *string         `json:"url"`
	FetchedAt        *string         `json:"fetched_at"`
	Hosts            json.RawMessage `json:"hosts,omitempty"`
	Dialers          json.RawMessage `json:"dialers,omitempty"`
	Fields           model.Fields
	Raw              model.Raw
}

func (r *SourceMetadata) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }
func (r SourceMetadata) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&r, &r.Raw) }

// NodeCache is the consumed view of the subscription owner's stored receipt.
type NodeCache struct {
	Nodes     []model.Node    `json:"nodes"`
	Count     model.Int       `json:"count"`
	UpdatedAt *string         `json:"updated_at"`
	Metadata  *SourceMetadata `json:"metadata"`
	Fields    model.Fields
	Raw       model.Raw
}

func (r *NodeCache) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.Raw) }
func (r NodeCache) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&r, &r.Raw) }

// SourceOwners are the existing subscription/parser/cache collaborators.
// Every I/O callback receives the admitted owner's Runtime generation.
type SourceOwners struct {
	Fetch       func(context.Context, string, string, substore.FetchOptions) (*SourceResult, error)
	ParseNodes  func(context.Context, string) ([]model.Node, error)
	Metadata    func(context.Context, string) (json.RawMessage, json.RawMessage, error)
	SaveNodes   func(context.Context, string, []model.Node, *SourceMetadata) (*NodeCache, error)
	LoadNodes   func(context.Context, string) (*NodeCache, error)
	FilterNodes func(model.Subscription, []model.Node) []model.Node
	Converter   convert.NodeConverter
}

// RefreshEnv supplies native collaborators; no configuration/DTO is untyped.
type RefreshEnv struct {
	Clock      substore.Clock
	Sources    SourceOwners
	RuleClient *rulefetch.Client
	HTTP       substore.HTTPClient
	RandomHex  func() string
}
