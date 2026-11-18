package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
)

func subscriptionGenerationProfile(count, unrelated int) *model.Config {
	config := &model.Config{Profile: &model.Profile{}}
	ids := []string{}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("node-%d", i)
		ids = append(ids, id)
		config.Nodes = append(config.Nodes, model.Node{ID: id, Name: id, Type: "http", Server: "192.0.2.1", Port: 443})
	}
	config.Nodes = append(config.Nodes, model.Node{ID: "relay", Name: "Relay", Type: "http", Server: "192.0.2.2", Port: 443})
	config.Profile.ProxyGroups = []model.ProxyGroup{{ID: "exit", Name: "Exit", Type: "select", ManualNodes: &ids}, {ID: "chain", Name: "Chain", Type: "chain", Chain: &model.ProxyChain{Entry: model.ResourceRef{Type: "node", ID: "relay"}, Exit: model.ResourceRef{Type: "group", ID: "exit"}}}}
	for i := 0; i < 512; i++ {
		config.Profile.RuleConfigs = append(config.Profile.RuleConfigs, model.RuleConfig{ID: fmt.Sprint(i), ItemType: "rule", RuleType: "DOMAIN", Value: fmt.Sprintf("d%d.example", i), Policy: new("DIRECT")})
	}
	if unrelated > 0 {
		raw, _ := json.Marshal(strings.Repeat("x", unrelated))
		_ = config.Profile.Fields.Set("unrelated", raw)
	}
	return config
}

func BenchmarkN89MihomoSubscriptionPipeline(b *testing.B) {
	b.Cleanup(SetMihomoEnvironment(nativeGenerationEnv(b)))
	for _, count := range []int{32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			config := subscriptionGenerationProfile(count, 1<<20)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				output, err := MihomoConfig(context.Background(), config, MihomoOptions{SkipProviderPreflight: true})
				if err != nil {
					b.Fatal(err)
				}
				if !strings.Contains(output, "Chain_") {
					b.Fatal("chain path not exercised")
				}
			}
		})
	}
}

func TestN89MihomoPipelineDoesNotDecodeUnrelatedConfigPerNode(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(nativeGenerationEnv(t)))
	measure := func(size int) int64 {
		config := subscriptionGenerationProfile(16, size)
		result := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := MihomoConfig(context.Background(), config, MihomoOptions{SkipProviderPreflight: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	small, large := measure(0), measure(1<<20)
	// Race instrumentation and pooled output buffers add small sampling noise.
	// A 2% margin still rejects even one extra 1MiB config decode here, far
	// below the 16MiB minimum introduced by decoding it for every node.
	if large > small+small/50+65536 {
		t.Fatalf("generation decoded unrelated config per node: %d -> %d B/op", small, large)
	}
}
