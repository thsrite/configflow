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
	Hosts   *HostsData   `json:"hosts,omitzero"`
	Dialers *DialersData `json:"dialers,omitzero"`
	// DialersStored: the stored metadata has a dialers member (even null or
	// damaged): the source's dialer decision is stored and the download is
	// not consulted again (SourceDialers).
	DialersStored bool `json:"-"`
	// hostsDamaged: the stored hosts record has no entries object, so there
	// is no host index (a missing map is not an empty valid one).
	hostsDamaged bool
}

type sourceMetadataWire SourceMetadata

// UnmarshalJSON reads stored cache metadata leniently.
func (m *SourceMetadata) UnmarshalJSON(b []byte) error {
	if err := model.DecodeLenient(b, (*sourceMetadataWire)(m)); err != nil {
		return err
	}
	var members struct {
		Hosts   json.RawMessage `json:"hosts"`
		Dialers json.RawMessage `json:"dialers"`
	}
	if jsondecode.Unmarshal(b, &members) != nil {
		return nil
	}
	m.DialersStored = members.Dialers != nil
	if m.Hosts == nil {
		return nil
	}
	var hosts struct {
		Entries json.RawMessage `json:"entries"`
	}
	if jsondecode.Unmarshal(members.Hosts, &hosts) != nil || len(hosts.Entries) == 0 || hosts.Entries[0] != '{' {
		m.hostsDamaged = true
	}
	return nil
}

// SubscriptionCache is a single already-loaded subscription cache, never a full config.
type SubscriptionCache struct {
	Nodes    []model.Node    `json:"nodes"`
	Metadata *SourceMetadata `json:"metadata"`
}

type sourceCachePayload struct {
	Nodes    json.RawMessage `json:"nodes"`
	Metadata *SourceMetadata `json:"metadata"`
}

// UnmarshalJSON consumes valid node records individually, so a corrupt sibling
// does not discard usable cached nodes. It never writes or repairs the source.
func (c *SubscriptionCache) UnmarshalJSON(b []byte) error {
	var payload sourceCachePayload
	if err := model.DecodeLenient(b, &payload); err != nil {
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
	// A missing or malformed entries map is not an empty valid cache index.
	if cache.Metadata.hostsDamaged {
		return nil, nil
	}
	data, err := model.Marshal(record.Entries)
	if err != nil {
		return nil, err
	}
	entries, err := yamlx.FromJSON(data)
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
