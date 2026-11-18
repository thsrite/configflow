package substore

import (
	"configflow/internal/model"
	"configflow/internal/subs"
	"encoding/json"
)

// Traffic is the arbitrary-precision usage reported by the subscription source.
type Traffic struct {
	Upload   *json.Number `json:"upload"`
	Download *json.Number `json:"download"`
	Total    *json.Number `json:"total"`
	Expire   *json.Number `json:"expire"`
	Fields   model.Fields
	Raw      model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *Traffic) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r Traffic) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// CacheMetadata records only data learned from the same subscription response.
type CacheMetadata struct {
	SubscriptionName string            `json:"subscription_name"`
	URL              string            `json:"url"`
	Source           string            `json:"source"`
	FetchedAt        string            `json:"fetched_at"`
	Traffic          *Traffic          `json:"traffic"`
	Hosts            *subs.HostsData   `json:"hosts"`
	Dialers          *subs.DialersData `json:"dialers"`
	Fields           model.Fields
	Raw              model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *CacheMetadata) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r CacheMetadata) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }

// Cache is one subscription's nodes and source metadata, retaining unknown members.
type Cache struct {
	SubscriptionID string         `json:"subscription_id"`
	UpdatedAt      string         `json:"updated_at"`
	Count          *model.Int     `json:"count"`
	Nodes          []model.Node   `json:"nodes"`
	Metadata       *CacheMetadata `json:"metadata"`
	Fields         model.Fields
	Raw            model.Raw
}

// UnmarshalJSON retains stored members with the shared model codec.
func (r *Cache) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }

// MarshalJSON preserves unknown and unchanged members.
func (r Cache) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.Raw) }
