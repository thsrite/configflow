package aggregations

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func aggregationDeliveryFixture(count, unrelated int) (model.Aggregation, *model.Config, *yamlx.Node) {
	config := &model.Config{ProfileID: "default", Profile: &model.Profile{}}
	agg := model.Aggregation{ID: "agg", Name: "Aggregate"}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("n%d", i)
		config.Nodes = append(config.Nodes, model.Node{ID: id, Name: fmt.Sprintf("Node%d", i), Type: "http", Server: "example.com", Port: 80})
		agg.Nodes = append(agg.Nodes, id)
	}
	if unrelated > 0 {
		_ = config.Profile.Fields.Set("unrelated", []byte(`"`+strings.Repeat("x", unrelated)+`"`))
	}
	main := yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(), yamlx.Str("proxy-groups"), yamlx.Seq())
	return agg, config, main
}
func TestN88dAggregationDoesNotDecodeConfigPerNode(t *testing.T) {
	measure := func(size int) int64 {
		agg, config, main := aggregationDeliveryFixture(32, size)
		result := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				artifact, err := RenderProvider(convert.EnsureConversions(context.Background()), agg, config, main, nil, nil)
				if err != nil || artifact == nil {
					b.Fatalf("%v", err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	base, large := measure(0), measure(1<<20)
	if large > base+base/50+65536 {
		t.Fatalf("unrelated configuration copied per node: %d/%d B/op", base, large)
	}
	t.Logf("unrelated 0/1MiB: %d/%d B/op", base, large)
}
func BenchmarkN88dAggregationDelivery(b *testing.B) {
	for _, count := range []int{8, 32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			agg, config, main := aggregationDeliveryFixture(count, 1<<20)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := RenderProvider(convert.EnsureConversions(context.Background()), agg, config, main, nil, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
