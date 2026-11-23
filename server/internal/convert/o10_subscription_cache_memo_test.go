package convert

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/subs"
)

// O10 R9: a remembered subscription cache decode is handed out as a copy.
func TestO10DecodedSubscriptionCacheCopies(t *testing.T) {
	data := []byte(`{"nodes":[{"id":"n1","name":"A","type":"ss","server":"a.example.com","port":443,"enabled":true}],"metadata":{"url":"https://feed.example.com/a","hosts":{"entries":{"a.example.com":"192.0.2.1"},"total":1,"skipped_count":0,"skipped":[]}}}`)
	first, err := decodedSubscriptionCache(data)
	if err != nil {
		t.Fatal(err)
	}
	first.Nodes[0].Name = "changed"
	*first.Nodes[0].Enabled = false
	first.Metadata.URL = "https://changed.example.com"
	first.Nodes = append(first.Nodes, first.Nodes[0])
	second, err := decodedSubscriptionCache(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Nodes) != 1 || second.Nodes[0].Name != "A" || !*second.Nodes[0].Enabled || second.Metadata.URL != "https://feed.example.com/a" || second.Metadata.Hosts.Entries.Len() != 1 {
		t.Fatalf("remembered decode changed through a caller: %+v", second)
	}
	if _, err := decodedSubscriptionCache([]byte(`{"nodes":`)); err == nil {
		t.Fatal("malformed cache accepted")
	}
}

func TestBindTypedSubscriptionCacheRespectsOverride(t *testing.T) {
	raw := func(context.Context, string) (json.RawMessage, error) { return nil, nil }
	typed := func(context.Context, string) (*subs.SubscriptionCache, error) { return &subs.SubscriptionCache{}, nil }
	var env MihomoEnvironment
	env.BindTypedSubscriptionCache(raw, typed)
	if env.activeTypedSubscriptionCache() == nil {
		t.Fatal("bound typed reader was not active")
	}
	env.LoadSubscriptionCache = func(context.Context, string) (json.RawMessage, error) { return []byte(`{}`), nil }
	if env.activeTypedSubscriptionCache() != nil {
		t.Fatal("overriding LoadSubscriptionCache did not disable typed fast path")
	}
}
