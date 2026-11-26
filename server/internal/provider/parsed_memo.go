package provider

import (
	"container/list"
	"sync"

	"configflow/internal/yamlx"
)

// Parsed provider texts are remembered (O10 R9). One generated configuration
// or provider response reads the same subscription and provider texts several
// times (rendering, bundle preparation, validation), and parsing a few hundred
// proxies allocates tens of MB. A result depends only on the text; errors are
// not remembered. Callers get their own copies unless they only read.
//
// A parsed graph keeps about 20 bytes of heap per byte of text (measured on
// a 0.5 MB, 2000-proxy text), so the tables are bounded by that estimate of
// what they keep alive, not by the length of their keys, and evict
// least-recently used entries first so superseded texts do not linger.

// parsedRetainedPerByte estimates the heap a parsed proxy list keeps per byte
// of its text (text key included).
const parsedRetainedPerByte = 20

// parsedProviders keeps readProviderNodes' validated proxy lists.
var parsedProviders = parsedMemo{limitBytes: 16 << 20, limitEntries: 32}

// parsedSubscriptions keeps ParseNodes' normalized proxies, each its own graph.
var parsedSubscriptions = parsedMemo{limitBytes: 16 << 20, limitEntries: 32}

type parsedMemoEntry struct {
	text  string
	nodes []*yamlx.Node
	cost  int
}

type parsedMemo struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	// bytes and limitBytes are estimated retained heap, not text length.
	bytes                    int
	limitBytes, limitEntries int
}

func (m *parsedMemo) get(text string) ([]*yamlx.Node, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	elem, ok := m.entries[text]
	if !ok {
		return nil, false
	}
	m.order.MoveToBack(elem)
	return elem.Value.(*parsedMemoEntry).nodes, true
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
	cost := len(text) * parsedRetainedPerByte
	if cost > m.limitBytes/2 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = map[string]*list.Element{}
		m.order = list.New()
	}
	if elem, ok := m.entries[text]; ok {
		m.order.MoveToBack(elem)
		return
	}
	for m.order.Len() > 0 && (m.order.Len() >= m.limitEntries || m.bytes+cost > m.limitBytes) {
		front := m.order.Front()
		evicted := front.Value.(*parsedMemoEntry)
		m.order.Remove(front)
		delete(m.entries, evicted.text)
		m.bytes -= evicted.cost
	}
	m.entries[text] = m.order.PushBack(&parsedMemoEntry{text: text, nodes: nodes, cost: cost})
	m.bytes += cost
}

// copyNodes copies the list as one graph (nil and empty lists stay as they are).
func copyNodes(nodes []*yamlx.Node) []*yamlx.Node {
	if len(nodes) == 0 {
		return nodes[:len(nodes):len(nodes)]
	}
	return yamlx.CopyGraph(yamlx.Seq(nodes...)).Content
}
