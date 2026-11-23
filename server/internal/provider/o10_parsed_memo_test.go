package provider

import (
	"strings"
	"sync"
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

// batch-47 review: ValidateArtifacts reads provider texts with own=false, so
// the fresh parse is remembered without a copy and shared by later readers.
// Validating (one after another and concurrently) never modifies it.
func TestO10SharedProviderParseStaysReadOnly(t *testing.T) {
	repoContext(t)
	main := mustParseYAML(t, `proxies:
  - {name: Exit, type: http, server: exit.example.com, port: 80}
proxy-groups:
  - {name: Group, type: select, use: [Feed]}
proxy-providers:
  Feed: {type: http, url: https://example.test/sub, path: ./providers/feed.yaml}
`)
	content := `x-opts: &opts {path: /ws, headers: {Host: cdn.example.com}}
proxies:
  - {name: Hop, type: vmess, server: hop.example.com, port: 443, uuid: 00000000-0000-4000-8000-000000000001, alterId: 0, cipher: auto, network: ws, ws-opts: *opts}
  - {name: Entry, type: http, server: entry.example.com, port: 80, dialer-proxy: Exit}
`
	artifacts := func() []*Artifact {
		return []*Artifact{{Name: "Feed", HasName: true, LocalPath: "./providers/feed.yaml", Content: content}}
	}
	snapshot := emptySnapshot(t, main)
	if _, err := ValidateArtifacts(snapshot, artifacts()); err != nil {
		t.Fatal(err)
	}
	kept, ok := parsedProviders.get(content)
	if !ok {
		t.Fatal("validated provider parse was not remembered")
	}
	encode := func(nodes []*yamlx.Node) string {
		t.Helper()
		var out []string
		for _, node := range nodes {
			data, err := yamlx.ToJSON(node)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, string(data))
		}
		return strings.Join(out, "\n")
	}
	before := encode(kept)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := ValidateArtifacts(emptySnapshot(t, main), artifacts()); err != nil {
				t.Error(err)
			}
			own, err := readProviderNodes(content, "Feed", "", msgNodeSource, msgPreparedSource)
			if err != nil {
				t.Error(err)
				return
			}
			yamlx.Set(own[0], "server", yamlx.Str("changed.example.com"))
		})
	}
	wg.Wait()
	again, ok := parsedProviders.get(content)
	if !ok || len(again) != len(kept) || again[0] != kept[0] {
		t.Fatal("remembered parse was replaced")
	}
	if after := encode(again); after != before {
		t.Fatalf("shared provider parse changed:\n%s\nwant\n%s", after, before)
	}
}
