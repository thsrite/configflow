package convert_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func batch6GenerationProfile(tb testing.TB, count int) *model.Config {
	tb.Helper()
	nodes, ids := make([]any, count), make([]any, count)
	for i := range nodes {
		id := fmt.Sprintf("node-%d", i)
		nodes[i] = convert.JObjKV("id", id, "name", fmt.Sprintf("Node%d", i), "type", "http", "server", "192.0.2.1", "port", 443)
		ids[i] = id
	}
	rules := make([]any, 512)
	for i := range rules {
		rules[i] = convert.JObjKV("id", fmt.Sprintf("rule-%d", i), "itemType", "rule", "rule_type", "DOMAIN", "value", fmt.Sprintf("d%d.example", i), "policy", "DIRECT")
	}
	cfg, err := convert.DecodeConfigJ(convert.JObjKV(
		"nodes", nodes,
		"rule_configs", rules,
		"proxy_groups", []any{convert.JObjKV("id", "group", "name", "Example", "type", "select", "manual_nodes", ids)},
		"subscriptions", []any{},
		"rule_library", []any{},
	))
	if err != nil {
		tb.Fatal(err)
	}
	return cfg
}

// BenchmarkMihomoGenerationProfile keeps a provider-sized render independent of
// wall-clock pass/fail thresholds. The entire public generation path is timed.
func BenchmarkMihomoGenerationProfile(b *testing.B) {
	b.Cleanup(convert.SetMihomoEnvironment(corpusEnv(nil, nil)))
	for _, count := range []int{32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			cfg := batch6GenerationProfile(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				text, err := convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{SkipProviderPreflight: true})
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
	t.Cleanup(convert.SetMihomoEnvironment(corpusEnv(nil, nil)))
	cfg := batch6GenerationProfile(t, 128)
	before, err := model.Marshal(cfg.Profile)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := convert.MihomoDocument(context.Background(), cfg, convert.MihomoOptions{SkipProviderPreflight: true})
	if err != nil {
		t.Fatal(err)
	}
	nodes := yamlx.Get(doc, "proxies").Content
	if len(nodes) != 128 {
		t.Fatalf("got %d proxies", len(nodes))
	}
	for i, node := range nodes {
		if yamlx.Get(node, "name").Value != fmt.Sprintf("Node%d", i) {
			t.Fatalf("proxy %d changed order/name", i)
		}
	}
	rules := yamlx.Get(doc, "rules").Content
	if len(rules) != 512 {
		t.Fatalf("got %d rules", len(rules))
	}
	for i, rule := range rules {
		if rule.Value != fmt.Sprintf("DOMAIN,d%d.example,DIRECT", i) {
			t.Fatalf("rule %d changed order/content", i)
		}
	}
	after, err := model.Marshal(cfg.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("generation changed its input")
	}
}
