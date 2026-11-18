package convert_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
)

func batch6GenerationProfile(count int) *jsonx.Object {
	nodes, ids := make([]any, count), make([]any, count)
	for i := range nodes {
		id := fmt.Sprintf("node-%d", i)
		nodes[i] = jsonx.Obj("id", id, "name", fmt.Sprintf("Node%d", i), "type", "http", "server", "192.0.2.1", "port", 443)
		ids[i] = id
	}
	rules := make([]any, 512)
	for i := range rules {
		rules[i] = jsonx.Obj("id", fmt.Sprintf("rule-%d", i), "itemType", "rule", "rule_type", "DOMAIN", "value", fmt.Sprintf("d%d.example", i), "policy", "DIRECT")
	}
	return jsonx.Obj("nodes", nodes, "rule_configs", rules, "proxy_groups", []any{jsonx.Obj("id", "group", "name", "Example", "type", "select", "manual_nodes", ids)}, "subscriptions", []any{}, "rule_library", []any{})
}

// BenchmarkMihomoGenerationProfile keeps a provider-sized render independent of
// wall-clock pass/fail thresholds. The entire public generation path is timed;
// each node must not transcode the full profile again at the legacy boundary.
func BenchmarkMihomoGenerationProfile(b *testing.B) {
	b.Cleanup(convert.SetMihomoEnv(corpusEnv(nil, nil)))
	for _, count := range []int{32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			cfg := batch6GenerationProfile(count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				text, err := convert.GenerateMihomoConfig(context.Background(), cfg, convert.GenerateOptions{SkipProviderPreflight: true})
				if err != nil {
					b.Fatal(err)
				}
				if !strings.Contains(text, fmt.Sprintf("Node%d", count-1)) {
					b.Fatal("missing final selected node")
				}
			}
		})
	}
}

func TestGenerationRetainsAllNodesAndSource(t *testing.T) {
	t.Cleanup(convert.SetMihomoEnv(corpusEnv(nil, nil)))
	cfg := batch6GenerationProfile(128)
	before, err := jsonx.Compact(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := convert.GenerateMihomoDocument(context.Background(), cfg, convert.GenerateOptions{SkipProviderPreflight: true})
	if err != nil {
		t.Fatal(err)
	}
	nodes := doc.GetList("proxies")
	if len(nodes) != 128 {
		t.Fatalf("got %d proxies", len(nodes))
	}
	for i, node := range nodes {
		if node.(*jsonx.Object).GetString("name") != fmt.Sprintf("Node%d", i) {
			t.Fatalf("proxy %d changed order/name", i)
		}
	}
	rules := doc.GetList("rules")
	if len(rules) != 512 {
		t.Fatalf("got %d rules", len(rules))
	}
	for i, rule := range rules {
		if rule != fmt.Sprintf("DOMAIN,d%d.example,DIRECT", i) {
			t.Fatalf("rule %d changed order/content", i)
		}
	}

	after, err := jsonx.Compact(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("generation changed its input")
	}
}
