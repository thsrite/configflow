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
			memo.store(tt.env, "sub", tt.cache, "proxies: []\n")
			got, ok := memo.lookup(tt.env, "sub", tt.cache)
			if ok != tt.usable || (ok && got != "proxies: []\n") {
				t.Fatalf("lookup = %q, %v; want usable %v", got, ok, tt.usable)
			}
		})
	}
	// A changed file (a new shared record) replaces the subscription's entry
	// instead of keeping the superseded record alive, and its text is not
	// served for the new record.
	replaced := &cacheTextMemo{limit: 2}
	first, second := &NodeCache{}, &NodeCache{}
	replaced.store(shared, "sub", first, "old")
	if _, ok := replaced.lookup(shared, "sub", second); ok {
		t.Fatal("text of a superseded record served for the new one")
	}
	replaced.store(shared, "sub", second, "new")
	if got, ok := replaced.lookup(shared, "sub", second); !ok || got != "new" || len(replaced.texts) != 1 {
		t.Fatalf("lookup = %q, %v with %d entries", got, ok, len(replaced.texts))
	}
	// The table is bounded by entries and by retained text bytes.
	for _, id := range []string{"a", "b", "c"} {
		replaced.store(shared, id, &NodeCache{}, "x")
	}
	if len(replaced.texts) > 2 {
		t.Fatalf("%d texts kept", len(replaced.texts))
	}
	bounded := &cacheTextMemo{limit: 16, limitBytes: 10}
	bounded.store(shared, "big", &NodeCache{}, "123456") // > limitBytes/2
	if len(bounded.texts) != 0 {
		t.Fatalf("oversized text kept: %d", len(bounded.texts))
	}
	bounded.store(shared, "k1", &NodeCache{}, "12345")
	bounded.store(shared, "k2", &NodeCache{}, "67890")
	bounded.store(shared, "k3", &NodeCache{}, "abcde")
	if bounded.bytes > bounded.limitBytes || len(bounded.texts) != 2 {
		t.Fatalf("byte budget exceeded: bytes=%d entries=%d", bounded.bytes, len(bounded.texts))
	}
	// A changed record whose text is too large to keep still drops the
	// superseded generation (batch-47 review).
	bounded.store(shared, "k3", &NodeCache{}, "1234567")
	if _, ok := bounded.texts["k3"]; ok || bounded.bytes != 5 || len(bounded.texts) != 1 {
		t.Fatalf("superseded text kept: bytes=%d entries=%d", bounded.bytes, len(bounded.texts))
	}
}
