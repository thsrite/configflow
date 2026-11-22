package provider

import (
	"sync"

	"configflow/internal/yamlx"
)

// Parsed provider texts are remembered (O10 R9). One generated configuration
// or provider response reads the same subscription and provider texts several
// times (rendering, bundle preparation, validation), and parsing a few hundred
// proxies allocates tens of MB. A result depends only on the text; errors are
// not remembered. Callers get their own copies unless they only read.

// parsedProviders keeps readProviderNodes' validated proxy lists.
var parsedProviders = parsedMemo{limitBytes: 8 << 20, limitEntries: 32}

// parsedSubscriptions keeps ParseNodes' normalized proxies, each its own graph.
var parsedSubscriptions = parsedMemo{limitBytes: 8 << 20, limitEntries: 32}

type parsedMemo struct {
	mu                       sync.Mutex
	entries                  map[string][]*yamlx.Node
	bytes                    int
	limitBytes, limitEntries int
}

func (m *parsedMemo) get(text string) ([]*yamlx.Node, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nodes, ok := m.entries[text]
	return nodes, ok
}

// lookup returns the list (shared when own is false, else copied as one
// graph so an alias keeps pointing at its anchor).
func (m *parsedMemo) lookup(text string, own bool) ([]*yamlx.Node, bool) {
	nodes, ok := m.get(text)
	if !ok || !own {
		return nodes, ok
	}
	return copyNodes(nodes), true
}

// lookupEach returns a list whose nodes are copied one by one.
func (m *parsedMemo) lookupEach(text string) ([]*yamlx.Node, bool) {
	nodes, ok := m.get(text)
	if !ok {
		return nil, false
	}
	out := make([]*yamlx.Node, len(nodes))
	for i, node := range nodes {
		out[i] = yamlx.CopyGraph(node)
	}
	return out, true
}

func (m *parsedMemo) store(text string, nodes []*yamlx.Node) { m.keep(text, copyNodes(nodes)) }

func (m *parsedMemo) storeEach(text string, nodes []*yamlx.Node) {
	kept := make([]*yamlx.Node, len(nodes))
	for i, node := range nodes {
		kept[i] = yamlx.CopyGraph(node)
	}
	m.keep(text, kept)
}

func (m *parsedMemo) keep(text string, nodes []*yamlx.Node) {
	if len(text) > m.limitBytes/4 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[text]; ok {
		return
	}
	if m.entries == nil || len(m.entries) >= m.limitEntries || m.bytes+len(text) > m.limitBytes {
		m.entries, m.bytes = map[string][]*yamlx.Node{}, 0
	}
	m.entries[text] = nodes
	m.bytes += len(text)
}

// copyNodes copies the list as one graph (nil and empty lists stay as they are).
func copyNodes(nodes []*yamlx.Node) []*yamlx.Node {
	if len(nodes) == 0 {
		return nodes[:len(nodes):len(nodes)]
	}
	return yamlx.CopyGraph(yamlx.Seq(nodes...)).Content
}
