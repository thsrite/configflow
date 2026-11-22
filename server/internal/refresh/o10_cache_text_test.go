package refresh

import (
	"context"
	"testing"

	"configflow/internal/model"
)

// O10 R9: provider texts are only remembered for records of the shared
// default loader and without proxy_string nodes.
func TestO10CacheTextOnlyForSharedStructuredRecords(t *testing.T) {
	text := "https://feed.example.com/sub"
	shared := &RefreshEnv{Sources: SourceOwners{LoadNodes: loadNodeCache}}
	fake := &RefreshEnv{Sources: SourceOwners{LoadNodes: func(context.Context, string) (*NodeCache, error) { return nil, nil }}}
	structured := &NodeCache{Nodes: []model.Node{{Name: "A", Type: "ss"}}}
	withString := &NodeCache{Nodes: []model.Node{{Name: "B", ProxyString: &text}}}
	memo := cacheTextMemo{limit: 2}
	tests := []struct {
		name   string
		env    *RefreshEnv
		cache  *NodeCache
		usable bool
	}{
		{"shared structured", shared, structured, true},
		{"test loader", fake, structured, false},
		{"proxy_string", shared, withString, false},
		{"no record", shared, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memo.store(tt.env, tt.cache, "proxies: []\n")
			got, ok := memo.lookup(tt.env, tt.cache)
			if ok != tt.usable || (ok && got != "proxies: []\n") {
				t.Fatalf("lookup = %q, %v; want usable %v", got, ok, tt.usable)
			}
		})
	}
	// The table is bounded.
	for range 3 {
		memo.store(shared, &NodeCache{}, "x")
	}
	if len(memo.texts) > 2 {
		t.Fatalf("%d texts kept", len(memo.texts))
	}
}
