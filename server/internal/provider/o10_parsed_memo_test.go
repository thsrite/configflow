package provider

import (
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

// O10 R9: remembered parses give every owner its own nodes and the same
// result as a fresh parse.
func TestO10ParsedProvidersAreCopies(t *testing.T) {
	text := "proxies:\n  - {name: A, type: ss, server: a.example.com, port: 443, cipher: aes-128-gcm, password: x}\n  - &b {name: B, type: http, server: b.example.com, port: 80}\n"
	first, err := ParseNodes(text, "Feed", "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := yamlx.Marshal(yamlx.Seq(first...))
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(first[0], "name", yamlx.Str("changed"))
	second, err := ParseNodes(text, "Feed", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := yamlx.Marshal(yamlx.Seq(second...)); string(got) != string(want) {
		t.Fatalf("remembered parse changed through a caller:\n%s\nwant\n%s", got, want)
	}
	if second[0] == first[0] {
		t.Fatal("same node handed out twice")
	}

	own, err := readProviderNodes(text, "Feed", "", msgSubscription, msgSubscriptionSource)
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(own[1], "port", yamlx.Int(1))
	again, err := readProviderNodes(text, "Feed", "", msgSubscription, msgSubscriptionSource)
	if err != nil {
		t.Fatal(err)
	}
	if port := yamlx.Get(again[1], "port"); port == nil || port.Value != "80" {
		t.Fatalf("remembered provider list changed through a caller: %v", port)
	}
	// Errors are not remembered and keep their context.
	for range 2 {
		if _, err := ParseNodes("proxies: [1", "Feed", ""); err == nil {
			t.Fatal("malformed text accepted")
		}
	}
	empty, err := ParseNodes("proxies: []\n", "Feed", "")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list: %#v %v", empty, err)
	}
	if empty, err = ParseNodes("proxies: []\n", "Feed", ""); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("remembered empty list: %#v %v", empty, err)
	}
}

// The tables are bounded by the heap they are estimated to keep alive
// (parsedRetainedPerByte per byte of text), not by the length of the texts.
func TestO10ParsedMemoBoundedByRetainedEstimate(t *testing.T) {
	memo := &parsedMemo{limitBytes: 50 * parsedRetainedPerByte, limitEntries: 32}
	memo.keep(strings.Repeat("x", 26), nil) // above half the budget
	if len(memo.entries) != 0 {
		t.Fatal("an entry above half the budget was kept")
	}
	texts := []string{"a" + strings.Repeat("-", 19), "b" + strings.Repeat("-", 19), "c" + strings.Repeat("-", 19)}
	for i, text := range texts {
		memo.keep(text, nil)
		if want := []int{1, 2, 2}[i]; len(memo.entries) != want || memo.bytes != want*20*parsedRetainedPerByte {
			t.Fatalf("after %d: %d entries, %d estimated bytes", i+1, len(memo.entries), memo.bytes)
		}
	}
	if _, ok := memo.get(texts[0]); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if _, ok := memo.get(texts[1]); !ok {
		t.Fatal("second entry was evicted prematurely")
	}
	if _, ok := memo.get(texts[2]); !ok {
		t.Fatal("newest entry missing")
	}
}
