package aggregations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/aggregations"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func TestBoundedCollectionDetachesCancellationAndDoesNotMutate(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{})
	subs := make([]map[string]any, 24)
	ids := make([]string, len(subs))
	for i := range subs {
		ids[i] = fmt.Sprint(i)
		subs[i] = map[string]any{"id": ids[i], "name": "Feed" + ids[i], "url": "http://127.0.0.1:9/" + ids[i]}
	}
	config, _ := json.Marshal(map[string]any{"profile_id": "default", "subscriptions": subs, "nodes": []any{}, "proxy_groups": []any{}, "rule_configs": []any{}})
	agg, _ := json.Marshal(map[string]any{"id": "a", "name": "Aggregate", "subscriptions": ids})
	beforeConfig, beforeAgg := string(config), string(agg)
	ctx, cancel := context.WithCancel(srv.Context())
	cancel()
	var active, maximum, calls atomic.Int32
	result, err := aggregations.Generate(ctx, agg, config, aggregations.Options{Main: json.RawMessage(`{"proxies":[],"proxy-groups":[]}`), SourcePrepared: true, Fetch: func(ctx context.Context, id, url string) (*refresh.SourceResult, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		calls.Add(1)
		now := active.Add(1)
		defer active.Add(-1)
		for prior := maximum.Load(); now > prior; prior = maximum.Load() {
			if maximum.CompareAndSwap(prior, now) {
				break
			}
		}
		// Different completion orders must not reorder configured subscription ids.
		time.Sleep(time.Duration(len(id)+1) * time.Millisecond)
		return &refresh.SourceResult{Text: fmt.Sprintf("proxies: [{name: Node%s, type: http, server: example.test, port: 80}]", id), Source: "prepared_yaml"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 24 || maximum.Load() > 8 || active.Load() != 0 {
		t.Fatalf("calls=%d max=%d active=%d", calls.Load(), maximum.Load(), active.Load())
	}
	if result.TotalCount != 24 {
		t.Fatalf("count=%d", result.TotalCount)
	}
	for i, proxy := range result.Proxies {
		var name string
		json.Unmarshal(proxy["name"], &name)
		if name != "Node"+ids[i] {
			t.Fatalf("out of order %d: %s", i, name)
		}
	}
	if string(config) != beforeConfig || string(agg) != beforeAgg {
		t.Fatal("caller document mutated")
	}
	// A dry generator must leave the entire repository unchanged.
	shared, err := srv.Repo.Shared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.Subscriptions) != 0 {
		t.Fatal("dry generator committed sources")
	}
}

type converterHTTP struct{ calls *atomic.Int32 }

func (f converterHTTP) Do(_ context.Context, r *substore.Request) (*substore.Response, error) {
	text := "{}"
	for _, p := range r.Params {
		if p.Key == "content" {
			f.calls.Add(1)
			text = `proxies: [{name: Converted, type: http, server: example.test, port: 80, dialer-proxy: relay}]`
		}
	}
	return &substore.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(text)}, nil
}
func TestOneOpaqueMaterializationForMainAndAggregation(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{})
	aggregations.Wire()
	mihomoenv.Wire()
	var calls atomic.Int32
	env := substore.NewEnv()
	env.HTTP = converterHTTP{&calls}
	env.LookupEnv = func(key string) (string, bool) {
		if key == "SUB_STORE_URL" {
			return "http://127.0.0.1:9", true
		}
		if key == "SUB_STORE_BUILTIN_DIR" {
			return "/nonexistent", true
		}
		return "", false
	}
	env.RandomHex = func() string { return "00000001" }
	t.Cleanup(substore.SetDefault(substore.NewService(env)))
	config, err := model.DecodeConfig([]byte(`{"profile_id":"default","system_config":{"config_token":"synthetic"},"nodes":[{"id":"relay","name":"relay","type":"http","server":"relay.test","port":80},{"id":"exit","name":"Exit","type":"http","proxy_string":"opaque://exit"}],"subscriptions":[],"subscription_aggregations":[{"id":"a","name":"Aggregate","nodes":["exit"],"subscriptions":[]}],"proxy_groups":[{"id":"g","name":"Entry","type":"select","manual_nodes":["relay"],"aggregations":["a"]}],"rule_configs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	main, err := convert.MihomoConfig(srv.Context(), config, convert.MihomoOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("conversion count=%d; main=%s", calls.Load(), main)
	}
	// The converter's preflight uses the same materialization in the bundle.
	var document map[string]any
	if err := yamlx.Unmarshal([]byte(main), &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document["proxy-providers"].(map[string]any)["Aggregate"]; !ok {
		t.Fatal("aggregation absent from main")
	}
}

func FuzzAggregationDocument(f *testing.F) {
	f.Add([]byte(`{"id":"a","name":"Aggregate","nodes":[],"subscriptions":[]}`))
	f.Add([]byte(`{"id":"a","name":"Aggregate","regex_filter":"["}`))
	f.Add([]byte(`null`))
	f.Add([]byte{})
	// Input parsing, regex and serialization are exercised through the exported
	// generator. Supplied main and empty catalog guarantee no upstream I/O.
	srv := httpxtest.New(f, httpxtest.Options{})
	config := json.RawMessage(`{"profile_id":"default","nodes":[],"subscriptions":[],"proxy_groups":[],"rule_configs":[]}`)
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<16 {
			return
		}
		before := append([]byte(nil), raw...)
		_, _ = aggregations.Generate(srv.Context(), raw, config, aggregations.Options{Main: json.RawMessage(`{"proxies":[],"proxy-groups":[]}`)})
		if !bytes.Equal(before, raw) {
			t.Fatal("input mutated")
		}
	})
}

func TestPublishedSnapshotSurvivesArtifactReplacement(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{})
	aggregations.Wire()
	mihomoenv.Wire()
	agg := json.RawMessage(`{"id":"a","name":"Aggregate","nodes":["n"],"subscriptions":[]}`)
	cfg := json.RawMessage(`{"profile_id":"default","nodes":[{"id":"n","name":"Original","type":"http","server":"original.test","port":80}],"subscriptions":[],"subscription_aggregations":[{"id":"a","name":"Aggregate","nodes":["n"],"subscriptions":[]}],"proxy_groups":[],"rule_configs":[]}`)
	result, err := aggregations.Generate(srv.Context(), agg, cfg, aggregations.Options{Main: json.RawMessage(`{"proxies":[],"proxy-groups":[]}`), Persist: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.FilePath == nil || result.TotalCount != 1 || len(result.Proxies) != 1 {
		t.Fatalf("incomplete publication: %+v", result)
	}
	before := result.Content
	_, err = srv.Repo.WriteProfileText(srv.Context(), "default", "providers/a.yaml", []byte("proxies: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	var name string
	json.Unmarshal(result.Proxies[0]["name"], &name)
	if result.Content != before || name != "Original" || result.TotalCount != 1 {
		t.Fatal("published result re-read a concurrently replaced artifact")
	}
}
