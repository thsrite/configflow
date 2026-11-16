package convert_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/rules"
)

// Hand-ported Python tests that the recorded replay (mihomo_recorded_test.go)
// cannot cover because they monkeypatch generator internals, plus the Go-side
// wiring (hooks, environment). Everything else of the C18 test files is in the
// replay, named by Python test id.

// countingConverter is a Sub-Store stand-in that records every conversion.
type countingConverter struct {
	calls []string
	fail  map[string]bool
}

func (c *countingConverter) ConvertProxyString(_ context.Context, s string) *jsonx.Object {
	c.calls = append(c.calls, s)
	if c.fail[s] {
		return nil
	}
	return jsonx.Obj("name", "x", "type", "http", "server", s[strings.Index(s, "@")+1:strings.LastIndex(s, ":")], "port", jsonx.Int(80))
}

func uriNode(id string) *jsonx.Object {
	return jsonx.Obj("id", id, "name", strings.ToUpper(id), "proxy_string", "http://u@"+id+".test:80")
}

func chainGroup(id string, entry, exit any) *jsonx.Object {
	if entry == nil {
		entry = jsonx.Obj("type", "node", "id", "a")
	}
	if s, ok := exit.(string); ok {
		exit = jsonx.Obj("type", "node", "id", s)
	}
	name := strings.ToUpper(id[:1]) + id[1:]
	return jsonx.Obj("id", id, "name", name, "type", "chain", "chain", jsonx.Obj("entry", entry, "exit", exit))
}

func chainProfile() *jsonx.Object {
	return jsonx.Obj("profile_id", "default", "nodes", []any{uriNode("a"), uriNode("b")},
		"proxy_groups", []any{chainGroup("chain", nil, "b")}, "rule_configs", []any{})
}

func withEnv(t *testing.T, converter convert.ProxyStringConverter) {
	t.Helper()
	env := corpusEnv(nil, nil)
	env.Converter = converter
	t.Cleanup(convert.SetMihomoEnv(env))
}

// test_proxy_chains.py::test_invalid_chain_metadata_is_rejected_before_conversion
// (the monkeypatched convert_node_to_mihomo becomes a counting Sub-Store).
func TestInvalidChainMetadataIsRejectedBeforeConversion(t *testing.T) {
	cases := map[string]func(config, current *jsonx.Object){
		"missing_exit":  func(_, c *jsonx.Object) { c.GetObject("chain").GetObject("exit").Set("id", "missing") },
		"missing_entry": func(_, c *jsonx.Object) { c.GetObject("chain").GetObject("entry").Set("id", "missing") },
		"disabled_exit": func(p, _ *jsonx.Object) { p.GetList("nodes")[1].(*jsonx.Object).Set("enabled", false) },
		"disabled_entry": func(p, _ *jsonx.Object) {
			p.GetList("nodes")[0].(*jsonx.Object).Set("enabled", false)
		},
		"duplicate_name": func(_, c *jsonx.Object) { c.Set("name", "B") },
		"duplicate_id": func(p, _ *jsonx.Object) {
			other := chainGroup("chain", nil, "b")
			other.Set("name", "Other")
			p.Set("proxy_groups", append(p.GetList("proxy_groups"), other))
		},
		"reserved_name":      func(_, c *jsonx.Object) { c.Set("name", "DIRECT") },
		"self_entry":         func(_, c *jsonx.Object) { c.GetObject("chain").GetObject("entry").Set("id", "b") },
		"subscription_exit":  func(p, _ *jsonx.Object) { p.GetList("nodes")[1].(*jsonx.Object).Set("subscription_id", "feed") },
		"subscription_entry": func(p, _ *jsonx.Object) { p.GetList("nodes")[0].(*jsonx.Object).Set("subscription_id", "feed") },
		"malformed_entry":    func(_, c *jsonx.Object) { c.GetObject("chain").Set("entry", nil) },
		"malformed_chain":    func(_, c *jsonx.Object) { c.Set("chain", []any{}) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			converter := &countingConverter{}
			withEnv(t, converter)
			config := chainProfile()
			change(config, config.GetList("proxy_groups")[0].(*jsonx.Object))
			_, err := convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{SkipProviderPreflight: true})
			var dialer *convert.DialerReferenceError
			if !errors.As(err, &dialer) {
				t.Fatalf("want DialerReferenceError, got %v", err)
			}
			if len(converter.calls) != 0 {
				t.Fatalf("invalid stored metadata reached node conversion: %v", converter.calls)
			}
		})
	}
}

// test_proxy_chains.py::test_conversion_failure_cannot_silently_remove_chain_topology
// (conversion of the entry A, or of the exit the Chain copy is made from, fails).
func TestConversionFailureCannotSilentlyRemoveChainTopology(t *testing.T) {
	for name, failing := range map[string]string{"A": "http://u@a.test:80", "Chain": "http://u@b.test:80"} {
		t.Run(name, func(t *testing.T) {
			withEnv(t, &countingConverter{fail: map[string]bool{failing: true}})
			_, err := convert.GenerateMihomoConfig(context.Background(), chainProfile(), convert.GenerateOptions{SkipProviderPreflight: true})
			var dialer *convert.DialerReferenceError
			if !errors.As(err, &dialer) {
				t.Fatalf("want DialerReferenceError, got %v", err)
			}
		})
	}
}

func simpleProfile() *jsonx.Object {
	return jsonx.Obj("nodes", []any{jsonx.Obj("id", "n", "name", "N", "type", "http", "server", "n.example.test", "port", jsonx.Int(80))},
		"proxy_groups", []any{jsonx.Obj("id", "g", "name", "G", "type", "select", "manual_nodes", []any{"n"})})
}

func TestLinuxGatewayHookDefaults(t *testing.T) {
	withEnv(t, fakeSubStore{})
	ctx := context.Background()
	opts := convert.GenerateOptions{SkipProviderPreflight: true}
	// Absent or null setting, no router split marker: Python's exact no-op.
	for _, mihomo := range []*jsonx.Object{nil, jsonx.Obj("linux_gateway", nil)} {
		config := simpleProfile()
		if mihomo != nil {
			config.Set("mihomo", mihomo)
		}
		if _, err := convert.GenerateMihomoConfig(ctx, config, opts); err != nil {
			t.Fatalf("%v: %v", mihomo, err)
		}
		if _, err := convert.GetMihomoRulesetDownloads(ctx, config, ""); err != nil {
			t.Fatalf("downloads %v: %v", mihomo, err)
		}
		if mode, err := convert.TakeoverMode(config); err != nil || mode != "" {
			t.Fatalf("takeover %q %v", mode, err)
		}
	}
	// Anything else needs the agents/routers providers: fail closed.
	for _, mihomo := range []*jsonx.Object{jsonx.Obj("linux_gateway", jsonx.Obj("enabled", false)),
		jsonx.Obj("router_split", jsonx.Obj("version", jsonx.Int(1)))} {
		config := simpleProfile()
		config.Set("mihomo", mihomo)
		var missing *convert.HookMissingError
		if _, err := convert.GenerateMihomoConfig(ctx, config, opts); !errors.As(err, &missing) {
			t.Fatalf("%v: want HookMissingError, got %v", mihomo, err)
		}
		if _, err := convert.TakeoverMode(config); !errors.As(err, &missing) {
			t.Fatalf("takeover %v: want HookMissingError, got %v", mihomo, err)
		}
	}
	// Base settings checks that need another domain fail closed too.
	var missing *convert.HookMissingError
	if err := convert.ValidateBaseSettings(jsonx.Obj("dns", jsonx.Obj("fake-ip-range", "198.18.0.1/16")), nil, nil, nil); !errors.As(err, &missing) {
		t.Fatalf("fake-ip-range: %v", err)
	}
	if err := convert.ValidateBaseSettings(jsonx.Obj("dns", jsonx.Obj("nameserver", []any{"1.1.1.1"})), nil, nil, nil); !errors.As(err, &missing) {
		t.Fatalf("dns loop: %v", err)
	}
	// ... but only when the value is checked.
	previous := jsonx.Obj("dns", jsonx.Obj("fake-ip-range", "198.18.0.1/16", "nameserver", []any{"1.1.1.1"}))
	if err := convert.ValidateBaseSettings(previous.DeepCopy(), previous, nil, nil); err != nil {
		t.Fatalf("unchanged: %v", err)
	}
}

// mihomo_base_settings.takeover_mode with registered hooks.
func TestTakeoverMode(t *testing.T) {
	config := simpleProfile()
	cases := []struct {
		gateway func(*jsonx.Object) (bool, error)
		full    bool
		want    string
	}{
		{func(*jsonx.Object) (bool, error) { return true, nil }, true, "linux_gateway"},
		{func(*jsonx.Object) (bool, error) { return false, nil }, true, "full_tun"},
		{func(*jsonx.Object) (bool, error) { return false, errors.New("Linux 网关设置必须是对象") }, true, "full_tun"},
		{func(*jsonx.Object) (bool, error) { return false, errors.New("enabled 必须是布尔值") }, false, ""},
		{func(*jsonx.Object) (bool, error) { return false, nil }, false, ""},
	}
	for _, c := range cases {
		full := c.full
		restore := convert.RegisterMihomoHooks(convert.MihomoHooks{GatewayEnabled: c.gateway,
			IsFullTun: func(*jsonx.Object) (bool, error) { return full, nil }})
		mode, err := convert.TakeoverMode(config)
		restore()
		if err != nil || mode != c.want {
			t.Fatalf("got %q %v, want %q", mode, err, c.want)
		}
	}
}

// generate_mihomo_config(preflight_providers=True) hands the logical profile
// (before chain lowering) and the finished document to prepare_provider_bundle,
// after the emitted graph was validated; its errors abort generation.
func TestProviderPreflightGetsLogicalProfileAndFinishedDocument(t *testing.T) {
	env := corpusEnv(nil, nil)
	var gotConfig, gotMain *jsonx.Object
	env.PrepareProviderBundle = func(_ context.Context, config, main *jsonx.Object) error {
		gotConfig, gotMain = config, main
		return nil
	}
	defer convert.SetMihomoEnv(env)()
	config := chainProfile()
	config.Set("nodes", []any{
		jsonx.Obj("id", "a", "name", "A", "type", "http", "server", "a.test", "port", jsonx.Int(80)),
		jsonx.Obj("id", "b", "name", "B", "type", "http", "server", "b.test", "port", jsonx.Int(80)),
	})
	if _, err := convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{}); err != nil {
		t.Fatal(err)
	}
	if gotConfig != config {
		t.Fatal("preflight did not receive the logical profile")
	}
	if gotMain == nil || len(gotMain.GetList("proxies")) == 0 || gotMain.Value("rules") == nil {
		t.Fatalf("preflight did not receive the finished document: %v", gotMain)
	}
	env.PrepareProviderBundle = func(context.Context, *jsonx.Object, *jsonx.Object) error {
		return errors.New("订阅 Feed 无法交付")
	}
	defer convert.SetMihomoEnv(env)()
	if _, err := convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{}); err == nil || err.Error() != "订阅 Feed 无法交付" {
		t.Fatalf("preflight error not returned: %v", err)
	}
	// Without the provider wired, preflight fails closed; skipping it needs nothing.
	env.PrepareProviderBundle = nil
	defer convert.SetMihomoEnv(env)()
	var missing *convert.HookMissingError
	if _, err := convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{}); !errors.As(err, &missing) {
		t.Fatalf("want HookMissingError, got %v", err)
	}
	if _, err := convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{SkipProviderPreflight: true}); err != nil {
		t.Fatal(err)
	}
}

func TestMihomoEnvProviderInterval(t *testing.T) {
	interval := mihomoenv.Env().ProviderInterval
	if v, err := interval(jsonx.NewObject()); err != nil || v != refresh.DefaultProviderInterval {
		t.Fatalf("default: %d %v", v, err)
	}
	if v, err := interval(jsonx.Obj("provider_update_interval", jsonx.Int(0))); err != nil || v != 0 {
		t.Fatalf("zero: %d %v", v, err)
	}
	_, err := interval(jsonx.Obj("provider_update_interval", "3600"))
	if err == nil || err.Error() != "Mihomo 订阅同步间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数" {
		t.Fatalf("invalid: %v", err)
	}
}

// GenerateMihomoDocument is yaml.safe_load(generate_mihomo_config(...)).
func TestGenerateMihomoDocumentLoadsTheGeneratedText(t *testing.T) {
	withEnv(t, fakeSubStore{})
	ctx := context.Background()
	opts := convert.GenerateOptions{SkipProviderPreflight: true, BaseURL: "http://cf.example.test"}
	text, err := convert.GenerateMihomoConfig(ctx, simpleProfile(), opts)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := convert.GenerateMihomoDocument(ctx, simpleProfile(), opts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := convert.GenerateMihomoConfig(ctx, simpleProfile(), opts)
	if err != nil || again != text {
		t.Fatal("generation is not deterministic")
	}
	if doc.GetList("proxies")[0].(*jsonx.Object).GetString("name") != "N" ||
		doc.GetList("proxy-groups")[0].(*jsonx.Object).GetString("name") != "G" {
		t.Fatalf("unexpected document: %v", doc)
	}
	// Each call of the template returns a fresh value.
	a, b := convert.DefaultMihomoTemplate(), convert.DefaultMihomoTemplate()
	a.GetObject("dns").Set("enable", false)
	if b.GetObject("dns").Value("enable") != true {
		t.Fatal("template values are shared")
	}
}

// test_dynamic_proxy_chains.py::test_native_override_cycle_is_not_hidden_by_original_provider_graph
func TestNativeOverrideCycleIsNotHiddenByOriginalProviderGraph(t *testing.T) {
	main := jsonx.Obj("proxies", []any{},
		"proxy-groups", []any{jsonx.Obj("name", "Entry", "type", "select", "use", []any{"Private"})},
		"proxy-providers", jsonx.Obj("Private", jsonx.Obj("override", jsonx.Obj("additional-prefix", "private::", "dialer-proxy", "Entry"))))
	err := convert.ValidateEmitted(main, convert.EmitOptions{
		ProviderProxies:  jsonx.Obj("Private", []any{jsonx.Obj("name", "remote", "type", "http")}),
		RequireProviders: true,
	})
	var dialer *convert.DialerReferenceError
	if !errors.As(err, &dialer) || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("want a cycle DialerReferenceError, got %v", err)
	}
}

// test_group_exclusion_generation.py::test_candidate_filter_preserves_records_order_and_empty_include
func TestCandidateFilterPreservesRecordsOrderAndEmptyInclude(t *testing.T) {
	nodes := []any{jsonx.Obj("name", "US-new", "params", jsonx.Obj("password", "unchanged")), jsonx.Obj("name", "US-expired"),
		jsonx.Obj("name", "HK-new")}
	got, err := rules.FilterCandidates(nodes, nil, "expired")
	if err != nil || len(got) != 2 || got[0] != nodes[0] || got[1] != nodes[2] {
		t.Fatalf("exclude: %v %v", got, err)
	}
	got, err = rules.FilterCandidates(nodes, "^US", "expired")
	if err != nil || len(got) != 1 || got[0] != nodes[0] {
		t.Fatalf("include+exclude: %v %v", got, err)
	}
	var candidate *rules.CandidateValidationError
	if _, err := rules.FilterCandidates(nodes, nil, "["); !errors.As(err, &candidate) {
		t.Fatalf("invalid exclude: %v", err)
	}
}
