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
	first, err := sourceNodes(text, proxies)
	if err != nil || len(first) != 1 {
		t.Fatalf("%v %v", first, err)
	}
	on := false
	first[0].Name, first[0].Enabled, first[0].SubscriptionID = "changed", &on, "s"
	second, err := sourceNodes(text, proxies)
	if err != nil || len(second) != 1 || second[0].Name != "A" || second[0].Enabled != nil || second[0].SubscriptionID != "" {
		t.Fatalf("remembered nodes changed through a caller: %+v %v", second, err)
	}
}
