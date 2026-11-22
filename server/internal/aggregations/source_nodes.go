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
// few hundred cached proxies again. The caller gets its own copy.
func sourceNodes(text string, proxies []*yamlx.Node) ([]model.Node, error) {
	sourceNodeMemo.mu.Lock()
	kept, ok := sourceNodeMemo.entries[text]
	sourceNodeMemo.mu.Unlock()
	if ok {
		return subs.CloneNodes(kept), nil
	}
	nodes, err := substore.ProxyNodes(proxies)
	if err != nil || len(text) > sourceNodeTextLimit/4 {
		return nodes, err
	}
	kept = subs.CloneNodes(nodes)
	sourceNodeMemo.mu.Lock()
	defer sourceNodeMemo.mu.Unlock()
	if sourceNodeMemo.entries == nil || len(sourceNodeMemo.entries) >= 32 || sourceNodeMemo.bytes+len(text) > sourceNodeTextLimit {
		sourceNodeMemo.entries, sourceNodeMemo.bytes = map[string][]model.Node{}, 0
	}
	if _, ok := sourceNodeMemo.entries[text]; !ok {
		sourceNodeMemo.entries[text] = kept
		sourceNodeMemo.bytes += len(text)
	}
	return nodes, nil
}

const sourceNodeTextLimit = 8 << 20

var sourceNodeMemo struct {
	mu      sync.Mutex
	entries map[string][]model.Node
	bytes   int
}
