package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/store"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func deliveryFixture(count, unrelated int) (*model.Config, *yamlx.Node, DeliveryOptions) {
	config := &model.Config{ProfileID: "default", Profile: &model.Profile{}}
	definitions := yamlx.Map()
	options := DeliveryOptions{RenderAll: true, Prepared: map[string]*Artifact{}}
	for i := 0; i < count; i++ {
		id, name := fmt.Sprintf("s%d", i), fmt.Sprintf("Feed%d", i)
		config.Subscriptions = append(config.Subscriptions, model.Subscription{ID: id, Name: name})
		yamlx.Set(definitions, name, yamlx.Map(yamlx.Str("url"), yamlx.Str("/subscriptions/"+id+"/proxies"), yamlx.Str("path"), yamlx.Str("./providers/"+id+".yaml")))
		options.Prepared[name] = &Artifact{Content: fmt.Sprintf("proxies: [{name: Node%d, type: http, server: example.com, port: 80}]", i)}
	}
	main := yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(), yamlx.Str("proxy-groups"), yamlx.Seq(), yamlx.Str("proxy-providers"), definitions)
	if unrelated > 0 {
		value := []byte(`"` + strings.Repeat("x", unrelated) + `"`)
		_ = config.Profile.SetJSON("description", value)
		yamlx.Set(main, "unrelated", yamlx.Str(strings.Repeat("y", unrelated)))
	}
	return config, main, options
}
func TestN88dDeliveryRetainsInputsAndSnapshot(t *testing.T) {
	config, main, opts := deliveryFixture(8, 1<<20)
	before, _ := model.Marshal(config)
	graphBefore, _ := yamlx.ToJSON(main)
	snapshot, err := CaptureDelivery("default", main, config.Profile)
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(snapshot.MainNode(), "unrelated", yamlx.Str("changed"))
	pv := New(Options{})
	artifacts, err := pv.PrepareBundle(context.Background(), config, main, opts)
	if err != nil || len(artifacts) != 8 {
		t.Fatalf("%d %v", len(artifacts), err)
	}
	after, _ := model.Marshal(config)
	graphAfter, _ := yamlx.ToJSON(main)
	if !bytes.Equal(before, after) || !bytes.Equal(graphBefore, graphAfter) {
		t.Fatal("preparation mutated its input")
	}
	if memberText(snapshot.main, "unrelated") != strings.Repeat("y", 1<<20) {
		t.Fatal("snapshot mutation escaped")
	}
}
func TestN88dNoConfigDecodePerProvider(t *testing.T) {
	pv := New(Options{})
	measure := func(size int) int64 {
		config, main, opts := deliveryFixture(16, size)
		result := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				if _, err := pv.PrepareBundle(context.Background(), config, main, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	base, large := measure(0), measure(1<<20)
	if large > base+base/50+65536 {
		t.Fatalf("unrelated configuration copied per provider: base=%d large=%d", base, large)
	}
	t.Logf("unrelated 0/1MiB: %d/%d B/op", base, large)
}
func BenchmarkN88dDelivery(b *testing.B) {
	pv := New(Options{})
	for _, count := range []int{8, 32, 128} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			config, main, opts := deliveryFixture(count, 1<<20)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := pv.PrepareBundle(context.Background(), config, main, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestN88dShutdownBetweenCacheAndArtifact(t *testing.T) {
	base, r := repoContext(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(base)
	defer runtime.Shutdown(context.Background())
	ctx := runtime.BindRequest(base)
	var hosts subs.HostsData
	if err := json.Unmarshal([]byte(`{"entries":{"example.com":["192.0.2.1"]}}`), &hosts); err != nil {
		t.Fatal(err)
	}
	sub := substore.NewService(substore.NewEnvFor(r))
	t.Cleanup(sub.OnContentChange(func(context.Context) error { runtime.Shutdown(context.Background()); return nil }))
	pv := New(Options{Repo: r, SubStore: sub})
	bundle := []*Artifact{{AggregationID: "agg", Content: "proxies: []", CacheUpdates: []CacheWrite{{SubID: "first", Nodes: []model.Node{{Name: "Node"}}, Metadata: &substore.CacheMetadata{Hosts: &hosts}}, {SubID: "second", Nodes: []model.Node{{Name: "Other"}}}}}}
	err := pv.CommitArtifacts(ctx, "default", bundle)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	first, _ := r.SharedPath("subscribes/first.json")
	second, _ := r.SharedPath("subscribes/second.json")
	artifact, _ := r.ProfilePath("default", "providers/agg.yaml")
	if _, err := os.Stat(first); err != nil {
		t.Fatal("first commit never completed", err)
	}
	for _, path := range []string{second, artifact} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("post-shutdown write", path, err)
		}
	}
}
func TestN88dRunFlightShutdownAfterCallback(t *testing.T) {
	base, r := repoContext(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(base)
	defer runtime.Shutdown(context.Background())
	ctx := runtime.BindRequest(base)
	_, err := runFlight(ctx, r, true, "default", "subscription", "s", func(context.Context) (*httpx.Response, error) {
		runtime.Shutdown(context.Background())
		return httpx.Bytes(200, "text/yaml", []byte("proxies: []")), nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	path, _ := r.ProfilePath("default", lastGoodPath("subscription", "s"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("shutdown published last-good", err)
	}
}
func TestFlightKeyFollowsCommittedHead(t *testing.T) {
	head := &store.Head{StoreID: "s", Generation: 3, CommitID: "c3"}
	same := *head
	next := &store.Head{StoreID: "s", Generation: 4, CommitID: "c4"}
	key := func(head *store.Head) flightKey { return newFlightKey("aggregation", "alpha", "a", "", "", "", head) }
	tests := []struct {
		name  string
		a, b  flightKey
		share bool
	}{
		{"same head shares", key(head), key(&same), true},
		{"new commit renders anew", key(head), key(next), false},
		{"other filter renders anew", key(head), newFlightKey("aggregation", "alpha", "a", "", "x", "", head), false},
		{"working copy never shares", key(nil), key(nil), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a == tt.b; got != tt.share {
				t.Fatalf("shared = %v, want %v", got, tt.share)
			}
		})
	}
}
func FuzzN88dDelivery(f *testing.F) {
	f.Add("proxies: [{name: Node, type: http}]", false)
	f.Add("proxies: &p [*p]", true)
	f.Fuzz(func(t *testing.T, text string, force bool) {
		nodes, err := ParseNodes(text, "Feed", "")
		if err != nil {
			return
		}
		config, main, _ := deliveryFixture(1, 0)
		snapshot, err := CaptureDelivery("default", main, config.Profile)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := yamlx.ToJSON(main)
		_ = snapshot.Validate(nodes, force, nil)
		after, _ := yamlx.ToJSON(main)
		if !bytes.Equal(before, after) {
			t.Fatal("validation mutated main")
		}
	})
}

var _ convert.NodeConverter = nilNodeConverter{}

func TestN88dProviderPathsUseSharedURLCompatibility(t *testing.T) {
	pv := New(Options{})
	for _, address := range []string{"/subscriptions/s0/proxies?label=50%", "https://user:synthetic^value@example.com/subscriptions/s0/proxies?label=50%", "http://[2001:db8::1%test]/subscriptions/s0/proxies"} {
		config, main, options := deliveryFixture(1, 0)
		yamlx.Set(yamlx.Get(yamlx.Get(main, "proxy-providers"), "Feed0"), "url", yamlx.Str(address))
		bundle, err := pv.PrepareBundle(context.Background(), config, main, options)
		if err != nil || len(bundle) != 1 || bundle[0].URL != address {
			t.Fatalf("shared URL compatibility lost: %v", err)
		}
	}
}
