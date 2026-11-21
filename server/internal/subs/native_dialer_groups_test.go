package subs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func groupGenerationFixture(n, unrelated int) (*yamlx.Node, *model.Config, map[string]*SubscriptionCache) {
	providers := yamlx.Map()
	config := &model.Config{Profile: &model.Profile{}}
	caches := map[string]*SubscriptionCache{}
	if unrelated > 0 {
		raw, _ := json.Marshal(strings.Repeat("x", unrelated))
		_ = config.Profile.SetJSON("description", raw)
	}
	for i := 0; i < n; i++ {
		id, name := strconv.Itoa(i), fmt.Sprintf("Air %d", i)
		config.Subscriptions = append(config.Subscriptions, model.Subscription{ID: id, Name: name})
		providers.Content = append(providers.Content, yamlx.Str(name), yamlx.Map(yamlx.Str("url"), yamlx.Str("https://panel.example/api/subscriptions/"+id+"/proxies")))
		var cache SubscriptionCache
		_ = json.Unmarshal([]byte(`{"nodes":[{"name":"start","type":"ss","params":{"dialer-proxy":"R"}},{"name":"relay","type":"ss"}],"metadata":{"dialers":{"targets":{"R":{"type":"select","members":["relay"]}},"unresolved":[]}}}`), &cache)
		caches[id] = &cache
	}
	return yamlx.Map(yamlx.Str("proxy-providers"), providers), config, caches
}
func TestN89SourceDialerPresence(t *testing.T) {
	for _, field := range []string{`"dialers":null`, `"dialers":false`, `"dialers":{}`} {
		t.Run(field, func(t *testing.T) {
			var cache SubscriptionCache
			if err := json.Unmarshal([]byte(`{"metadata":{`+field+`}}`), &cache); err != nil {
				t.Fatal(err)
			}
			calls := 0
			_, err := SourceDialers(context.Background(), model.Subscription{URL: "https://sub.example/feed"}, &cache, func(context.Context, string) (string, bool) { calls++; return "", true })
			if err != nil || calls != 0 {
				t.Fatalf("stored presence became fallback calls=%d err=%v", calls, err)
			}
		})
	}
	calls := 0
	_, err := SourceDialers(context.Background(), model.Subscription{URL: "https://sub.example/feed"}, &SubscriptionCache{Metadata: &SourceMetadata{}}, func(context.Context, string) (string, bool) { calls++; return "proxies: []", true })
	if err != nil || calls != 1 {
		t.Fatalf("missing record did not use original fallback %d %v", calls, err)
	}
}
func TestN89AddGroupsLoadsEachSourceOnceAndKeepsInputs(t *testing.T) {
	main, config, caches := groupGenerationFixture(8, 0)
	before, err := model.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	report, err := AddGroups(context.Background(), main, config, DialerInputs{LoadCache: func(_ context.Context, id string) (*SubscriptionCache, error) { calls[id]++; return caches[id], nil }})
	if err != nil || report == nil || len(report.Groups) != 8 || report.SkippedCount != 0 {
		t.Fatalf("report %#v err %v", report, err)
	}
	for id, count := range calls {
		if count != 1 {
			t.Fatalf("cache %s loaded %d times", id, count)
		}
	}
	if len(calls) != 8 {
		t.Fatalf("loaded %d sources", len(calls))
	}
	after, err := model.Marshal(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("input config changed", err)
	}
}
func TestN89AddGroupsKeepsPrefixOnCollision(t *testing.T) {
	main, config, caches := groupGenerationFixture(1, 0)
	config.Aggregations = []model.Aggregation{{ID: "aggregate", Name: "Aggregate", Subscriptions: []string{"0"}}}
	yamlx.Set(main, "proxy-providers", yamlx.Map(yamlx.Str("Aggregate"), yamlx.Map(yamlx.Str("url"), yamlx.Str("https://panel.example/api/aggregations/aggregate/proxies"))))
	yamlx.Set(main, "proxies", yamlx.Seq(yamlx.Map(yamlx.Str("name"), yamlx.Str("Air 0/relay-2"))))
	var cache SubscriptionCache
	if err := json.Unmarshal([]byte(`{"nodes":[{"name":"start","params":{"dialer-proxy":"R"}},{"name":"relay-1"},{"name":"relay-2"}],"metadata":{"dialers":{"targets":{"R":{"type":"select","members":["relay-1","relay-2"]}}}}}`), &cache); err != nil {
		t.Fatal(err)
	}
	caches["0"] = &cache
	converted := 0
	report, err := AddGroups(context.Background(), main, config, DialerInputs{LoadCache: func(_ context.Context, id string) (*SubscriptionCache, error) { return caches[id], nil }, Convert: func(_ context.Context, node model.Node) (*yamlx.Node, error) {
		converted++
		return yamlx.Map(yamlx.Str("name"), yamlx.Str(node.Name), yamlx.Str("type"), yamlx.Str("ss")), nil
	}})
	var conflict *DialerReferenceError
	if report != nil || !errors.As(err, &conflict) || converted != 2 {
		t.Fatalf("collision report %#v err %v converted=%d", report, err, converted)
	}
	proxies := yamlx.Get(main, "proxies")
	if len(proxies.Content) != 2 || proxyName(proxies.Content[1]) != "Air 0/relay-1" {
		t.Fatal("successful prefix lost")
	}
	if len(yamlx.Get(main, "proxy-groups").Content) != 0 {
		t.Fatal("failed group was emitted")
	}
}
func TestN89AddGroupsDoesNotEmitUnknownEmptyGroup(t *testing.T) {
	for _, target := range []string{`{"members":[]}`, `{"members":false}`, `false`} {
		t.Run(target, func(t *testing.T) {
			main, config, caches := groupGenerationFixture(1, 0)
			var cache SubscriptionCache
			if err := json.Unmarshal([]byte(`{"nodes":[{"name":"start","params":{"dialer-proxy":"R"}}],"metadata":{"dialers":{"targets":{"R":`+target+`}}}}`), &cache); err != nil {
				t.Fatal(err)
			}
			caches["0"] = &cache
			report, err := AddGroups(context.Background(), main, config, DialerInputs{LoadCache: func(_ context.Context, id string) (*SubscriptionCache, error) { return caches[id], nil }})
			if err != nil || report == nil || len(report.Groups) != 0 || report.SkippedCount != 1 || report.Skipped[0].Reason != "missing" {
				t.Fatalf("unknown relay emitted %#v %v", report, err)
			}
			if len(yamlx.Get(main, "proxy-groups").Content) != 0 {
				t.Fatal("unknown group written")
			}
		})
	}
}
func TestN89DialerGenerationDoesNotDecodeConfigPerSource(t *testing.T) {
	measure := func(size int) int64 {
		base, config, caches := groupGenerationFixture(16, size)
		result := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				main := yamlx.Clone(base)
				_, err := AddGroups(context.Background(), main, config, DialerInputs{LoadCache: func(_ context.Context, id string) (*SubscriptionCache, error) { return caches[id], nil }})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	small, large := measure(0), measure(1<<20)
	if large > small+32768 {
		t.Fatalf("dialers decoded unrelated config per source: empty=%d 1MiB=%d B/op", small, large)
	}
}
func BenchmarkN89DialerGeneration(b *testing.B) {
	for _, n := range []int{8, 32, 128} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			base, config, caches := groupGenerationFixture(n, 1<<20)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				main := yamlx.Clone(base)
				_, err := AddGroups(context.Background(), main, config, DialerInputs{LoadCache: func(_ context.Context, id string) (*SubscriptionCache, error) { return caches[id], nil }})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
