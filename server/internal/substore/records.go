package substore

import (
	"configflow/internal/model"
	"configflow/internal/subs"
	"encoding/json"
	"strings"
)

// Traffic is the arbitrary-precision usage reported by the subscription
// source; only the numbers it reported are present (parse_userinfo).
type Traffic struct {
	Upload   *json.Number `json:"upload,omitzero"`
	Download *json.Number `json:"download,omitzero"`
	Total    *json.Number `json:"total,omitzero"`
	Expire   *json.Number `json:"expire,omitzero"`
}

// CacheMetadata records only data learned from the same subscription response
// (subscription_refresh.source_cache_metadata): subscription_name, url and
// fetched_at are always written (url and fetched_at may be null), the others
// only when known.
type CacheMetadata struct {
	SubscriptionName string            `json:"subscription_name"`
	URL              *string           `json:"url" text:"strict"`
	Source           string            `json:"source,omitzero"`
	FetchedAt        *string           `json:"fetched_at"`
	Traffic          *Traffic          `json:"traffic,omitzero"`
	Hosts            *subs.HostsData   `json:"hosts,omitzero"`
	Dialers          *subs.DialersData `json:"dialers,omitzero"`
}

// Cache is one subscription's nodes and source metadata
// (subscription_cache.save_subscription_nodes). Issues are the value-free
// diagnostics of the stored file: a member of the wrong type reads as its
// zero value, and readers that must not trust such a cache (the refresh
// owner's URL check) look at them.
type Cache struct {
	SubscriptionID string             `json:"subscription_id"`
	UpdatedAt      string             `json:"updated_at" text:"strict"`
	Count          *model.Int         `json:"count"`
	Nodes          []model.Node       `json:"nodes"`
	Metadata       *CacheMetadata     `json:"metadata,omitzero"`
	Issues         []model.FieldIssue `json:"-"`
}

// NodesDamaged reports a stored node list that is not a list or holds an
// element that is not a node object (such elements are left out of Nodes).
// A node member of the wrong type does not count: the node reads with that
// member absent, as for any stored node.
func (c *Cache) NodesDamaged() bool {
	for _, issue := range c.Issues {
		if issue.Kind != model.IssueType {
			continue
		}
		if issue.Path == "nodes" {
			return true
		}
		if rest, ok := strings.CutPrefix(issue.Path, "nodes["); ok && strings.HasSuffix(rest, "]") && !strings.ContainsAny(rest, ".[") {
			return true
		}
	}
	return false
}
