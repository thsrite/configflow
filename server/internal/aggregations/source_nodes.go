package aggregations

import (
	"sync"

	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// sourceNodes is substore.ProxyNodes of a source's parsed proxies, remembered
// per source text (O10 R9): the proxies are a pure function of the text
// (provider.ParseNodes), and every render of an aggregation converts the same
// few hundred cached proxies again. The caller gets its own copy. The table
// is bounded by an estimate of the heap it keeps alive: converted nodes keep
// about 16 bytes per byte of text (measured on a 0.5 MB, 2000-proxy text).
func sourceNodes(text string, proxies []*yamlx.Node) ([]model.Node, error) {
	sourceNodeMemo.mu.Lock()
	kept, ok := sourceNodeMemo.entries[text]
	sourceNodeMemo.mu.Unlock()
	if ok {
		return subs.CloneNodes(kept), nil
	}
	nodes, err := substore.ProxyNodes(proxies)
	cost := len(text) * sourceNodeRetainedPerByte
	if err != nil || cost > sourceNodeRetainedLimit/2 {
		return nodes, err
	}
	kept = subs.CloneNodes(nodes)
	sourceNodeMemo.mu.Lock()
	defer sourceNodeMemo.mu.Unlock()
	if sourceNodeMemo.entries == nil || len(sourceNodeMemo.entries) >= 32 || sourceNodeMemo.bytes+cost > sourceNodeRetainedLimit {
		sourceNodeMemo.entries, sourceNodeMemo.bytes = map[string][]model.Node{}, 0
	}
	if _, ok := sourceNodeMemo.entries[text]; !ok {
		sourceNodeMemo.entries[text] = kept
		sourceNodeMemo.bytes += cost
	}
	return nodes, nil
}

const (
	sourceNodeRetainedPerByte = 16
	sourceNodeRetainedLimit   = 32 << 20
)

var sourceNodeMemo struct {
	mu      sync.Mutex
	entries map[string][]model.Node
	bytes   int // estimated retained heap
}
