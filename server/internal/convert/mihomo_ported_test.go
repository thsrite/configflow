package convert_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// Hand-ported Python tests that the recorded replay (mihomo_recorded_test.go)
// cannot cover because they monkeypatch generator internals, plus the Go-side
// wiring (integrations, environment). Everything else of the C18 test files is
// in the replay, named by Python test id.

// countingConverter is a Sub-Store stand-in that records every conversion.
type countingConverter struct {
	calls []string
	fail  map[string]bool
}

func (c *countingConverter) ConvertProxy(_ context.Context, s string) (*yamlx.Node, error) {
	c.calls = append(c.calls, s)
	if c.fail[s] {
		return nil, nil
	}
	return yamlx.Map(
		yamlx.Str("name"), yamlx.Str("x"),
		yamlx.Str("type"), yamlx.Str("http"),
		yamlx.Str("server"), yamlx.Str(s[strings.Index(s, "@")+1:strings.LastIndex(s, ":")]),
		yamlx.Str("port"), yamlx.Int(80),
	), nil
}

func uriNode(id string) *convert.JObj {
	return convert.JObjKV("id", id, "name", strings.ToUpper(id), "proxy_string", "http://u@"+id+".test:80")
}

func chainGroup(id string, entry, exit any) *convert.JObj {
	if entry == nil {
		entry = convert.JObjKV("type", "node", "id", "a")
	}
	if s, ok := exit.(string); ok {
		exit = convert.JObjKV("type", "node", "id", s)
	}
	name := strings.ToUpper(id[:1]) + id[1:]
	return convert.JObjKV("id", id, "name", name, "type", "chain", "chain", convert.JObjKV("entry", entry, "exit", exit))
}

func chainProfile() *convert.JObj {
	return convert.JObjKV("profile_id", "default", "nodes", []any{uriNode("a"), uriNode("b")},
		"proxy_groups", []any{chainGroup("chain", nil, "b")}, "rule_configs", []any{})
}

func withEnv(t *testing.T, converter convert.NodeConverter) {
	t.Helper()
	env := corpusEnv(nil, nil)
	env.Converter = converter
	t.Cleanup(convert.OverrideMihomoEnvironmentForTest(t, env))
}

func mustDecodeConfig(t *testing.T, obj *convert.JObj) *model.Config {
	t.Helper()
	cfg, err := convert.DecodeConfigJ(obj)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// test_proxy_chains.py::test_invalid_chain_metadata_is_rejected_before_conversion
// (the monkeypatched convert_node_to_mihomo becomes a counting Sub-Store).
func TestInvalidChainMetadataIsRejectedBeforeConversion(t *testing.T) {
	cases := map[string]func(config, current *convert.JObj){
		"missing_exit":  func(_, c *convert.JObj) { c.GetObject("chain").GetObject("exit").Set("id", "missing") },
		"missing_entry": func(_, c *convert.JObj) { c.GetObject("chain").GetObject("entry").Set("id", "missing") },
		"disabled_exit": func(p, _ *convert.JObj) { p.GetList("nodes")[1].(*convert.JObj).Set("enabled", false) },
		"disabled_entry": func(p, _ *convert.JObj) {
			p.GetList("nodes")[0].(*convert.JObj).Set("enabled", false)
		},
		"duplicate_name": func(_, c *convert.JObj) { c.Set("name", "B") },
		"duplicate_id": func(p, _ *convert.JObj) {
			other := chainGroup("chain", nil, "b")
			other.Set("name", "Other")
			p.Set("proxy_groups", append(p.GetList("proxy_groups"), other))
		},
		"reserved_name":      func(_, c *convert.JObj) { c.Set("name", "DIRECT") },
		"self_entry":         func(_, c *convert.JObj) { c.GetObject("chain").GetObject("entry").Set("id", "b") },
		"subscription_exit":  func(p, _ *convert.JObj) { p.GetList("nodes")[1].(*convert.JObj).Set("subscription_id", "feed") },
		"subscription_entry": func(p, _ *convert.JObj) { p.GetList("nodes")[0].(*convert.JObj).Set("subscription_id", "feed") },
		"malformed_entry":    func(_, c *convert.JObj) { c.GetObject("chain").Set("entry", nil) },
		"malformed_chain":    func(_, c *convert.JObj) { c.Set("chain", []any{}) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			converter := &countingConverter{}
			withEnv(t, converter)
			config := chainProfile()
			change(config, config.GetList("proxy_groups")[0].(*convert.JObj))
			_, err := convert.MihomoConfig(context.Background(), mustDecodeConfig(t, config), convert.MihomoOptions{SkipProviderPreflight: true})
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
			_, err := convert.MihomoConfig(context.Background(), mustDecodeConfig(t, chainProfile()), convert.MihomoOptions{SkipProviderPreflight: true})
			var dialer *convert.DialerReferenceError
			if !errors.As(err, &dialer) {
				t.Fatalf("want DialerReferenceError, got %v", err)
			}
		})
	}
}

func simpleProfile() *convert.JObj {
	return convert.JObjKV("nodes", []any{convert.JObjKV("id", "n", "name", "N", "type", "http", "server", "n.example.test", "port", convert.JInt(80))},
		"proxy_groups", []any{convert.JObjKV("id", "g", "name", "G", "type", "select", "manual_nodes", []any{"n"})})
}

func TestLinuxGatewayHookDefaults(t *testing.T) {
	withEnv(t, fakeSubStore{})
	ctx := context.Background()
	opts := convert.MihomoOptions{SkipProviderPreflight: true}
	// Absent or null setting, no router split marker: Python's exact no-op.
	for _, mihomo := range []*convert.JObj{nil, convert.JObjKV("linux_gateway", nil)} {
		config := simpleProfile()
		if mihomo != nil {
			config.Set("mihomo", mihomo)
		}
		cfg := mustDecodeConfig(t, config)
		if _, err := convert.MihomoConfig(ctx, cfg, opts); err != nil {
			t.Fatalf("%v: %v", mihomo, err)
		}
		if _, err := convert.MihomoRulesetDownloads(ctx, cfg, ""); err != nil {
			t.Fatalf("downloads %v: %v", mihomo, err)
		}
		if mode, err := convert.GatewayMode(cfg); err != nil || mode != "" {
			t.Fatalf("takeover %q %v", mode, err)
		}
	}
	// Anything else needs the agents/routers providers: fail closed.
	for _, mihomo := range []*convert.JObj{convert.JObjKV("linux_gateway", convert.JObjKV("enabled", false)),
		convert.JObjKV("router_split", convert.JObjKV("version", convert.JInt(1)))} {
		config := simpleProfile()
		config.Set("mihomo", mihomo)
		cfg := mustDecodeConfig(t, config)
		var missing *convert.HookMissingError
		if _, err := convert.MihomoConfig(ctx, cfg, opts); !errors.As(err, &missing) {
			t.Fatalf("%v: want HookMissingError, got %v", mihomo, err)
		}
		if _, err := convert.GatewayMode(cfg); !errors.As(err, &missing) {
			t.Fatalf("takeover %v: want HookMissingError, got %v", mihomo, err)
		}
	}
	// Base settings checks that need another domain fail closed too.
	var missing *convert.HookMissingError
	if err := validateBaseFromJ(convert.JObjKV("dns", convert.JObjKV("fake-ip-range", "198.18.0.1/16")), nil, nil, nil); !errors.As(err, &missing) {
		t.Fatalf("fake-ip-range: %v", err)
	}
	if err := validateBaseFromJ(convert.JObjKV("dns", convert.JObjKV("nameserver", []any{"1.1.1.1"})), nil, nil, nil); !errors.As(err, &missing) {
		t.Fatalf("dns loop: %v", err)
	}
	// ... but only when the value is checked.
	previous := convert.JObjKV("dns", convert.JObjKV("fake-ip-range", "198.18.0.1/16", "nameserver", []any{"1.1.1.1"}))
	if err := validateBaseFromJ(previous.DeepCopy(), previous, nil, nil); err != nil {
		t.Fatalf("unchanged: %v", err)
	}
}

// mihomo_base_settings.takeover_mode with registered typed integrations.
func TestTakeoverMode(t *testing.T) {
	config := mustDecodeConfig(t, simpleProfile())
	cases := []struct {
		gateway func(*model.Config) (bool, error)
		full    bool
		want    string
	}{
		{func(*model.Config) (bool, error) { return true, nil }, true, "linux_gateway"},
		{func(*model.Config) (bool, error) { return false, nil }, true, "full_tun"},
		{func(*model.Config) (bool, error) { return false, errors.New("Linux 网关设置必须是对象") }, true, "full_tun"},
		{func(*model.Config) (bool, error) { return false, errors.New("enabled 必须是布尔值") }, false, ""},
		{func(*model.Config) (bool, error) { return false, nil }, false, ""},
	}
	for _, c := range cases {
		full := c.full
		restore := convert.OverrideMihomoIntegrationsForTest(t, convert.MihomoIntegrations{
			GatewayEnabled: c.gateway,
			FullTun:        func(*model.Config) (bool, error) { return full, nil },
		})
		mode, err := convert.GatewayMode(config)
		restore()
		if err != nil || mode != c.want {
			t.Fatalf("got %q %v, want %q", mode, err, c.want)
		}
	}
}

// MihomoConfig with provider preflight hands the logical profile (before chain
// lowering) and the finished document to Preflight; its errors abort generation.
func TestProviderPreflightGetsLogicalProfileAndFinishedDocument(t *testing.T) {
	env := corpusEnv(nil, nil)
	var gotConfig *model.Config
	var gotMain *yamlx.Node
	env.Preflight = func(_ context.Context, config *model.Config, main *yamlx.Node) error {
		gotConfig, gotMain = config, main
		return nil
	}
	defer convert.OverrideMihomoEnvironmentForTest(t, env)()
	config := chainProfile()
	config.Set("nodes", []any{
		convert.JObjKV("id", "a", "name", "A", "type", "http", "server", "a.test", "port", convert.JInt(80)),
		convert.JObjKV("id", "b", "name", "B", "type", "http", "server", "b.test", "port", convert.JInt(80)),
	})
	cfg := mustDecodeConfig(t, config)
	if _, err := convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{}); err != nil {
		t.Fatal(err)
	}
	if gotConfig == nil || gotConfig.ProfileID != "default" || len(gotConfig.Nodes) != 2 || gotConfig.Profile == nil ||
		len(gotConfig.Profile.ProxyGroups) != 1 || gotConfig.Profile.ProxyGroups[0].Type != "chain" {
		t.Fatalf("preflight did not receive logical profile before chain lowering: %+v", gotConfig)
	}
	if gotMain == nil || yamlx.Get(gotMain, "proxies") == nil || len(yamlx.Get(gotMain, "proxies").Content) == 0 || yamlx.Get(gotMain, "rules") == nil {
		t.Fatalf("preflight did not receive the finished document: %v", gotMain)
	}
	env.Preflight = func(context.Context, *model.Config, *yamlx.Node) error {
		return errors.New("订阅 Feed 无法交付")
	}
	defer convert.OverrideMihomoEnvironmentForTest(t, env)()
	if _, err := convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{}); err == nil || err.Error() != "订阅 Feed 无法交付" {
		t.Fatalf("preflight error not returned: %v", err)
	}
	// Without the provider wired, preflight fails closed; skipping it needs nothing.
	env.Preflight = nil
	defer convert.OverrideMihomoEnvironmentForTest(t, env)()
	var missing *convert.HookMissingError
	if _, err := convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{}); !errors.As(err, &missing) {
		t.Fatalf("want HookMissingError, got %v", err)
	}
	if _, err := convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{SkipProviderPreflight: true}); err != nil {
		t.Fatal(err)
	}
}

func TestMihomoEnvProviderInterval(t *testing.T) {
	interval := mihomoenv.NativeEnvFor(nil, nil, nil).ProviderInterval
	if v, err := interval(model.MihomoSettings{}); err != nil || v != refresh.DefaultProviderInterval {
		t.Fatalf("default: %d %v", v, err)
	}
	zero := model.Int(0)
	if v, err := interval(model.MihomoSettings{ProviderUpdateInterval: &zero}); err != nil || v != 0 {
		t.Fatalf("zero: %d %v", v, err)
	}
	// A stored interval that is not an integer reads as missing (plain
	// records, O18 T1c), so the default applies; Python refused it.
	var bad model.MihomoSettings
	if err := json.Unmarshal([]byte(`{"provider_update_interval":"bad"}`), &bad); err != nil {
		t.Fatal(err)
	}
	if v, err := interval(bad); err != nil || v != refresh.DefaultProviderInterval {
		t.Fatalf("unreadable stored interval: %d %v", v, err)
	}
	short := model.Int(30)
	_, err := interval(model.MihomoSettings{ProviderUpdateInterval: &short})
	if err == nil || err.Error() != "Mihomo 订阅同步间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数" {
		t.Fatalf("invalid: %v", err)
	}
}

// MihomoDocument produces the same document structure as MihomoConfig.
func TestGenerateMihomoDocumentLoadsTheGeneratedText(t *testing.T) {
	withEnv(t, fakeSubStore{})
	ctx := context.Background()
	opts := convert.MihomoOptions{SkipProviderPreflight: true, BaseURL: "http://cf.example.test"}
	cfg := mustDecodeConfig(t, simpleProfile())
	text, err := convert.MihomoConfig(ctx, cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := convert.MihomoDocument(ctx, mustDecodeConfig(t, simpleProfile()), opts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := convert.MihomoConfig(ctx, mustDecodeConfig(t, simpleProfile()), opts)
	if err != nil || again != text {
		t.Fatal("generation is not deterministic")
	}
	if yamlx.Get(yamlx.Get(doc, "proxies").Content[0], "name").Value != "N" ||
		yamlx.Get(yamlx.Get(doc, "proxy-groups").Content[0], "name").Value != "G" {
		t.Fatalf("unexpected document: %v", doc)
	}
	// Each call of the template returns a fresh value.
	a, b := convert.MihomoTemplate(), convert.MihomoTemplate()
	yamlx.Set(yamlx.Get(a, "dns"), "enable", yamlx.Bool(false))
	if yamlx.Get(yamlx.Get(b, "dns"), "enable").Value != "true" {
		t.Fatal("template values are shared")
	}
}

// test_dynamic_proxy_chains.py::test_native_override_cycle_is_not_hidden_by_original_provider_graph
func TestNativeOverrideCycleIsNotHiddenByOriginalProviderGraph(t *testing.T) {
	main, err := convert.YAMLNodeFromJ(convert.JObjKV("proxies", []any{},
		"proxy-groups", []any{convert.JObjKV("name", "Entry", "type", "select", "use", []any{"Private"})},
		"proxy-providers", convert.JObjKV("Private", convert.JObjKV("override", convert.JObjKV("additional-prefix", "private::", "dialer-proxy", "Entry")))))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := convert.YAMLNodeFromJ(convert.JObjKV("name", "remote", "type", "http"))
	if err != nil {
		t.Fatal(err)
	}
	err = convert.DialerFailure(convert.ValidateTopology(main, convert.TopologyOptions{
		ProviderProxies:  map[string][]*yamlx.Node{"Private": {remote}},
		RequireProviders: true,
	}))
	var dialer *convert.DialerReferenceError
	if !errors.As(err, &dialer) || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("want a cycle DialerReferenceError, got %v", err)
	}
}

// test_group_exclusion_generation.py::test_candidate_filter_preserves_records_order_and_empty_include
func TestCandidateFilterPreservesRecordsOrderAndEmptyInclude(t *testing.T) {
	type cand struct {
		name     string
		password string
	}
	nodes := []cand{{name: "US-new", password: "unchanged"}, {name: "US-expired"}, {name: "HK-new"}}
	nameOf := func(c cand) string { return c.name }
	exclude := "expired"
	got, err := rules.Filter(nodes, nil, &exclude, nameOf)
	if err != nil || len(got) != 2 || got[0] != nodes[0] || got[1] != nodes[2] {
		t.Fatalf("exclude: %v %v", got, err)
	}
	include := "^US"
	got, err = rules.Filter(nodes, &include, &exclude, nameOf)
	if err != nil || len(got) != 1 || got[0] != nodes[0] {
		t.Fatalf("include+exclude: %v %v", got, err)
	}
	bad := "["
	var candidate *rules.CandidateValidationError
	if _, err := rules.Filter(nodes, nil, &bad, nameOf); !errors.As(err, &candidate) {
		t.Fatalf("invalid exclude: %v", err)
	}
}

func TestTypedMihomoProviderInterval(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want         int64
		invalid      bool
	}{
		{"quoted", `{"provider_update_interval":"3600"}`, 3600, false},
		{"zero", `{"provider_update_interval":0}`, 0, false},
		{"below", `{"provider_update_interval":59}`, 0, true},
		{"one", `{"provider_update_interval":1}`, 0, true},
		{"above", `{"provider_update_interval":604801}`, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var settings model.MihomoSettings
			if err := json.Unmarshal([]byte(tt.source), &settings); err != nil {
				t.Fatal(err)
			}
			got, err := mihomoenv.NativeEnvFor(nil, nil, nil).ProviderInterval(settings)
			if (err != nil) != tt.invalid || (!tt.invalid && got != tt.want) {
				t.Fatalf("got %d %v", got, err)
			}
		})
	}
}
