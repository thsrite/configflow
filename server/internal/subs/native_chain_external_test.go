package subs_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	_ "configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

func chainFixture(count, unrelated int) *model.Config {
	config := &model.Config{Profile: &model.Profile{}}
	ids := []string{}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("node-%d", i)
		ids = append(ids, id)
		config.Nodes = append(config.Nodes, model.Node{ID: id, Name: id, Type: "http", Server: "192.0.2.1", Port: 443})
	}
	config.Nodes = append(config.Nodes, model.Node{ID: "relay", Name: "Relay", Type: "http", Server: "192.0.2.2", Port: 443})
	config.Profile.ProxyGroups = []model.ProxyGroup{
		{ID: "exit", Name: "Exit", Type: "select", ManualNodes: &ids},
		{ID: "chain", Name: "Chain", Type: "chain", Chain: &model.ProxyChain{Entry: model.ResourceRef{Type: "node", ID: "relay"}, Exit: model.ResourceRef{Type: "group", ID: "exit"}}},
	}
	if unrelated > 0 {
		raw, _ := json.Marshal(strings.Repeat("x", unrelated))
		_ = config.Profile.SetJSON("description", raw)
	}
	return config
}

func TestN89LowerChainsOwnsOnlyChangedRecords(t *testing.T) {
	config := chainFixture(3, 0)
	before, err := model.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := subs.LowerChains(config)
	if err != nil {
		t.Fatal(err)
	}
	if runtime == config || len(runtime.Nodes) != 7 || len(runtime.Profile.ProxyGroups) != 2 {
		t.Fatalf("lowering incomplete: %#v", runtime)
	}
	var generated *model.ProxyGroup
	for i := range runtime.Profile.ProxyGroups {
		if runtime.Profile.ProxyGroups[i].Name == "Chain" {
			generated = &runtime.Profile.ProxyGroups[i]
		}
	}
	if generated == nil || generated.ManualNodes == nil || len(*generated.ManualNodes) != 3 {
		t.Fatal("missing generated exit")
	}
	for _, id := range *generated.ManualNodes {
		if !strings.HasPrefix(id, "__chain__") {
			t.Fatal("unstable generated ID", id)
		}
	}
	if err := generated.SetJSON("manual_nodes", json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	_ = runtime.Nodes[4].Fields.Set("owned", json.RawMessage(`true`))
	after, err := model.Marshal(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("lowering or editing runtime changed input", err)
	}
}

func TestN89ChainsDoNotDecodeUnrelatedConfig(t *testing.T) {
	measure := func(size int) int64 {
		config := chainFixture(16, size)
		result := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := subs.LowerChains(config); err != nil {
					b.Fatal(err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	small, large := measure(0), measure(1<<20)
	if large > small+32768 {
		t.Fatalf("chain lowering decoded unrelated config: %d -> %d B/op", small, large)
	}
}

func BenchmarkN89ChainGeneration(b *testing.B) {
	for _, count := range []int{8, 32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			config := chainFixture(count, 1<<20)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := subs.LowerChains(config); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestN89ChainProviderFailureKeepsOnlySuccessfulPrefix(t *testing.T) {
	config := chainFixture(0, 0)
	group := model.ProxyGroup{ID: "generated", Name: "Generated", Type: "select"}
	if err := group.Fields.Set("_chain_dialer", json.RawMessage(`{"type":"node","id":"relay"}`)); err != nil {
		t.Fatal(err)
	}
	config.Profile.ProxyGroups = []model.ProxyGroup{group}
	original := yamlx.Map(yamlx.Str("type"), yamlx.Str("http"), yamlx.Str("url"), yamlx.Str("https://sub.example/feed"))
	main := yamlx.Map(yamlx.Str("proxy-providers"), yamlx.Map(yamlx.Str("Air"), original), yamlx.Str("proxy-groups"), yamlx.Seq(yamlx.Map(yamlx.Str("name"), yamlx.Str("Generated"), yamlx.Str("use"), yamlx.Seq(yamlx.Str("Air"), yamlx.Str("Missing")))))
	err := subs.ChainProviders(config, main)
	if err == nil {
		t.Fatal("missing provider accepted")
	}
	providers := yamlx.Get(main, "proxy-providers")
	generated := yamlx.Get(providers, "Generated_Air")
	if generated == nil || yamlx.Get(yamlx.Get(generated, "override"), "dialer-proxy").Value != "Relay" {
		t.Fatal("successful provider prefix lost")
	}
	if yamlx.Get(original, "override") != nil || yamlx.Get(original, "path") != nil {
		t.Fatal("source provider changed")
	}
}
