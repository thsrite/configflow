package aggregations

import (
	"fmt"
	"strings"
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
	// A text too large to keep still drops the superseded generation
	// (batch-47 review).
	var big strings.Builder
	big.WriteString("proxies:\n")
	for i := 0; big.Len() <= sourceNodeRetainedLimit/2/sourceNodeRetainedPerByte; i++ {
		fmt.Fprintf(&big, "  - {name: N%06d, type: ss, server: n%06d.example.com, port: 443, cipher: aes-128-gcm, password: x}\n", i, i)
	}
	bigProxies, err := provider.ParseNodes(big.String(), "Feed", "")
	if err != nil {
		t.Fatal(err)
	}
	if nodes, err := sourceNodes("sub-1", big.String(), bigProxies); err != nil || len(nodes) != len(bigProxies) {
		t.Fatalf("large text: %d nodes, %v", len(nodes), err)
	}
	sourceNodeMemo.mu.Lock()
	_, kept := sourceNodeMemo.entries["sub-1"]
	sourceNodeMemo.mu.Unlock()
	if kept {
		t.Fatal("superseded generation kept after an oversized update")
	}
}
