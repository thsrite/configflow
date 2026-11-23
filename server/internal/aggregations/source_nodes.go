package aggregations

import (
	"container/list"
	"sync"

	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// sourceNodes is substore.ProxyNodes of a source's parsed proxies, remembered
// per subscription (one generation per sourceID, O10 R9): the proxies are a
// pure function of the text (provider.ParseNodes), and every render of an
// aggregation converts the same few hundred cached proxies again. When a
// subscription's text changes, its previous generation is replaced immediately.
// The caller gets its own copy. The table is bounded by an estimate of the
// heap it keeps alive: converted nodes keep about 16 bytes per byte of text
// (measured on a 0.5 MB, 2000-proxy text).
func sourceNodes(sourceID, text string, proxies []*yamlx.Node) ([]model.Node, error) {
	key := sourceID
	if key == "" {
		key = text
	}
	sourceNodeMemo.mu.Lock()
	if elem, ok := sourceNodeMemo.entries[key]; ok {
		entry := elem.Value.(*sourceNodeEntry)
		if entry.text == text {
			sourceNodeMemo.order.MoveToBack(elem)
			kept := entry.nodes
			sourceNodeMemo.mu.Unlock()
			return subs.CloneNodes(kept), nil
		}
	}
	sourceNodeMemo.mu.Unlock()
	nodes, err := substore.ProxyNodes(proxies)
	cost := len(text) * sourceNodeRetainedPerByte
	if err != nil || cost > sourceNodeRetainedLimit/2 {
		return nodes, err
	}
	kept := subs.CloneNodes(nodes)
	sourceNodeMemo.mu.Lock()
	defer sourceNodeMemo.mu.Unlock()
	if sourceNodeMemo.entries == nil {
		sourceNodeMemo.entries = map[string]*list.Element{}
		sourceNodeMemo.order = list.New()
	}
	if prev, ok := sourceNodeMemo.entries[key]; ok {
		sourceNodeMemo.bytes -= prev.Value.(*sourceNodeEntry).cost
		sourceNodeMemo.order.Remove(prev)
		delete(sourceNodeMemo.entries, key)
	}
	for sourceNodeMemo.order.Len() > 0 && (sourceNodeMemo.order.Len() >= sourceNodeMaxEntries || sourceNodeMemo.bytes+cost > sourceNodeRetainedLimit) {
		front := sourceNodeMemo.order.Front()
		evicted := front.Value.(*sourceNodeEntry)
		sourceNodeMemo.order.Remove(front)
		delete(sourceNodeMemo.entries, evicted.key)
		sourceNodeMemo.bytes -= evicted.cost
	}
	sourceNodeMemo.entries[key] = sourceNodeMemo.order.PushBack(&sourceNodeEntry{key: key, text: text, nodes: kept, cost: cost})
	sourceNodeMemo.bytes += cost
	return nodes, nil
}

const (
	sourceNodeRetainedPerByte = 16
	sourceNodeRetainedLimit   = 16 << 20
	sourceNodeMaxEntries      = 32
)

type sourceNodeEntry struct {
	key   string
	text  string
	nodes []model.Node
	cost  int
}

var sourceNodeMemo struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	bytes   int // estimated retained heap
}
