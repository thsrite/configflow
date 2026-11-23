package aggregations

import (
	"testing"

	"configflow/internal/provider"
)

// O10 R9: remembered source nodes are handed out as copies.
func TestO10SourceNodesAreCopies(t *testing.T) {
	text := "proxies:\n  - {name: A, type: ss, server: a.example.com, port: 443, cipher: aes-128-gcm, password: x}\n"
	proxies, err := provider.ParseNodes(text, "Feed", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := sourceNodes("sub-1", text, proxies)
	if err != nil || len(first) != 1 {
		t.Fatalf("%v %v", first, err)
	}
	on := false
	first[0].Name, first[0].Enabled, first[0].SubscriptionID = "changed", &on, "s"
	second, err := sourceNodes("sub-1", text, proxies)
	if err != nil || len(second) != 1 || second[0].Name != "A" || second[0].Enabled != nil || second[0].SubscriptionID != "" {
		t.Fatalf("remembered nodes changed through a caller: %+v %v", second, err)
	}
	// Updating a subscription replaces its previous generation immediately.
	updatedText := "proxies:\n  - {name: B, type: ss, server: b.example.com, port: 443, cipher: aes-128-gcm, password: y}\n"
	updatedProxies, err := provider.ParseNodes(updatedText, "Feed", "")
	if err != nil {
		t.Fatal(err)
	}
	third, err := sourceNodes("sub-1", updatedText, updatedProxies)
	if err != nil || len(third) != 1 || third[0].Name != "B" {
		t.Fatalf("updated subscription returned %+v %v", third, err)
	}
	sourceNodeMemo.mu.Lock()
	entry := sourceNodeMemo.entries["sub-1"].Value.(*sourceNodeEntry)
	gotText := entry.text
	sourceNodeMemo.mu.Unlock()
	if gotText != updatedText {
		t.Fatalf("superseded text was not replaced for sub-1")
	}
}
