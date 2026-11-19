package subs

import (
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"encoding/json"
)

// SourceMetadata is the cache metadata consumed by subscription collaborators.
// It is separate from substore's owner record to keep the dependency acyclic.
type SourceMetadata struct {
	URL     string       `json:"url"`
	Hosts   *HostsData   `json:"hosts"`
	Dialers *DialersData `json:"dialers"`
	Fields  model.Fields
	Raw     model.Raw
}

// UnmarshalJSON reads the cache record with shared R5 and retains null presence.
func (m *SourceMetadata) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, m, &m.Raw) }

// MarshalJSON retains original cache metadata for boundary transcoding.
func (m SourceMetadata) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&m, &m.Raw) }

// SubscriptionCache is a single already-loaded subscription cache, never a full config.
type SubscriptionCache struct {
	Nodes    []model.Node    `json:"nodes"`
	Metadata *SourceMetadata `json:"metadata"`
}

type sourceCachePayload struct {
	Nodes    json.RawMessage `json:"nodes"`
	Metadata *SourceMetadata `json:"metadata"`
	Fields   model.Fields
	Raw      model.Raw
}

func (p *sourceCachePayload) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, p, &p.Raw) }
func (p sourceCachePayload) MarshalJSON() ([]byte, error)  { return model.EncodeRecord(&p, &p.Raw) }

// UnmarshalJSON consumes valid node records individually, so a corrupt sibling
// does not discard usable cached nodes. It never writes or repairs the source.
func (c *SubscriptionCache) UnmarshalJSON(b []byte) error {
	var payload sourceCachePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return err
	}
	var records []json.RawMessage
	if len(payload.Nodes) > 0 {
		_ = json.Unmarshal(payload.Nodes, &records)
	}
	nodes := make([]model.Node, 0, len(records))
	for _, record := range records {
		var node model.Node
		if err := json.Unmarshal(record, &node); err == nil {
			nodes = append(nodes, node)
		}
	}
	*c = SubscriptionCache{Nodes: nodes, Metadata: payload.Metadata}
	return nil
}

// HostIndex holds one subscription's immutable entry graph and server/name index.
type HostIndex struct {
	URL          string
	Entries      *yamlx.Node
	Nodes        map[string][]string
	Total        int
	SkippedCount int
	Skipped      []HostSkip
}

// NewHostIndex indexes a loaded typed cache once. The cache is never modified.
func NewHostIndex(cache *SubscriptionCache) (*HostIndex, error) {
	if cache == nil || cache.Metadata == nil || cache.Metadata.Hosts == nil {
		return nil, nil
	}
	record := cache.Metadata.Hosts
	// Record provenance retains absent/null/wrong entries. Inspect that owned
	// subrecord once; do not turn a missing map into an empty valid cache index.
	data, err := model.Marshal(record)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Entries json.RawMessage `json:"entries"`
	}
	if err := jsondecode.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	if len(wire.Entries) == 0 || wire.Entries[0] != '{' {
		return nil, nil
	}
	entries, err := yamlx.FromJSON(wire.Entries)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, record.Entries.Len())
	for key := range record.Entries.All() {
		keys[key] = true
	}
	nodes := map[string][]string{}
	for _, node := range cache.Nodes {
		server := serverDomain(node.Server)
		if server != "" && keys[server] {
			nodes[server] = append(nodes[server], node.Name)
		}
	}
	return &HostIndex{URL: cache.Metadata.URL, Entries: entries, Nodes: nodes, Total: int(record.Total), SkippedCount: int(record.SkippedCount), Skipped: append([]HostSkip(nil), record.Skipped...)}, nil
}

// ForSubscription rejects cache metadata learned from a different source URL.
func (h *HostIndex) ForSubscription(sub model.Subscription) *HostIndex {
	if h == nil || h.URL == sub.URL {
		return h
	}
	return nil
}
func serverDomain(text string) string {
	if usableAddress(text) != "" {
		return ""
	}
	return hostDomain(text)
}

// HostIndexLoader supplies a source-owned, URL-checked index at most once per subscription.
type HostIndexLoader func(sub model.Subscription) (*HostIndex, error)
