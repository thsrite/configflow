package subs

import (
	"slices"

	"configflow/internal/model"
)

// CloneNodes copies nodes so that the copy can be changed freely: pointer
// members point to new values; copy-on-write Fields and Dynamic are shared.
func CloneNodes(nodes []model.Node) []model.Node {
	if nodes == nil {
		return nil
	}
	out := make([]model.Node, len(nodes))
	for i, node := range nodes {
		node.ProxyString = clonePointer(node.ProxyString)
		node.Enabled = clonePointer(node.Enabled)
		node.DialerRef = clonePointer(node.DialerRef)
		out[i] = node
	}
	return out
}

// Clone copies the cache so that the copy can be changed freely (see
// CloneNodes); copy-on-write Map and Fields values are shared.
func (c *SubscriptionCache) Clone() *SubscriptionCache {
	if c == nil {
		return nil
	}
	out := &SubscriptionCache{Nodes: CloneNodes(c.Nodes)}
	if c.Metadata != nil {
		metadata := *c.Metadata
		if metadata.Hosts != nil {
			hosts := *metadata.Hosts
			hosts.Skipped = slices.Clone(hosts.Skipped)
			metadata.Hosts = &hosts
		}
		if metadata.Dialers != nil {
			dialers := *metadata.Dialers
			dialers.Unresolved = slices.Clone(dialers.Unresolved)
			metadata.Dialers = &dialers
		}
		out.Metadata = &metadata
	}
	return out
}

// clonePointer copies the value p points to. (The constraint is spelled out:
// the boundary test keeps the identifier `any` out of the native owners.)
func clonePointer[T interface{}](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
