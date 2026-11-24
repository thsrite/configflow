package gateway_test

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// snapshot is test_linux_gateway.py's snapshot(): one selected inline rule set.
func snapshot(enabled bool) map[string]any {
	return map[string]any{
		"profile_id":    "home",
		"system_config": map[string]any{"server_domain": "https://config.example", "config_token": "a+b&c"},
		"mihomo": map[string]any{
			"custom_config": "mixed-port: 7890\n",
			"linux_gateway": map[string]any{"enabled": enabled, "rule_set_ids": []any{"r"}},
		},
		"rule_configs": []any{map[string]any{
			"id": "r", "itemType": "ruleset", "name": "Proxy IPs",
			"library_rule_id": "lib", "behavior": "ipcidr", "policy": "DIRECT", "enabled": true,
		}},
		"rule_library": []any{map[string]any{
			"id": "lib", "name": "Proxy IPs", "behavior": "ipcidr",
			"source_type": "content", "content": "1.1.1.0/24\n8.8.8.8", "enabled": true,
		}},
	}
}

// TestDefaultContentReader covers test_empty_stable_id_cache_survives_source_rename:
// an empty remote source is accepted only while its cache file exists, and
// without the C32 reader the gateway fails closed.
func TestDefaultContentReader(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	config := snapshot(true)
	source := config["rule_library"].([]any)[0].(map[string]any)
	source["source_type"] = "url"
	source["url"] = "https://rules.example/ip.txt"
	delete(source, "content")

	if _, err := gateway.NewFixtureConfig(config).ProviderPayload(ctx, "r", nil); !errors.Is(err, gateway.ErrRulesetContentUnavailable) {
		t.Fatalf("without a reader: %v", err)
	}
	var calls int
	t.Cleanup(gateway.SetRuleSourceReader(func(_ context.Context, item model.RuleConfig, library *model.RuleLibraryItem, cfg *rulefetch.SourceConfig) (string, error) {
		calls++
		if item.ID != "r" || library == nil || library.ID != "lib" || cfg == nil || cfg.Settings.ServerDomain != "https://config.example" {
			t.Errorf("reader arguments: %+v %+v %+v", item, library, cfg)
		}
		return "", nil
	}))
	rulesDir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(rulesDir, rules.CacheFilename(testjson.Decode[model.RuleConfig](t, source)))
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source["name"] = "Renamed source"
	payload, err := gateway.NewFixtureConfig(config).ProviderPayload(ctx, "r", nil)
	if err != nil || len(payload) != 0 {
		t.Fatalf("empty cached source: %v %v", payload, err)
	}
	if err := os.Remove(filepath.Join(rulesDir, rules.CacheFilename(testjson.Decode[model.RuleConfig](t, source)))); err != nil {
		t.Fatal(err)
	}
	_, err = gateway.NewFixtureConfig(config).ProviderPayload(ctx, "r", nil)
	var gwErr *gateway.Error
	if !errors.As(err, &gwErr) || !strings.Contains(gwErr.Message, "无法读取") {
		t.Fatalf("missing cache: %v", err)
	}
	// The overlap check treats an empty remote set as unreadable, without the
	// cache requirement.
	warnings, err := gateway.NewFixtureConfig(config).DirectOverlapWarnings(ctx, nil)
	if err != nil || len(warnings) != 1 || !strings.HasPrefix(warnings[0], "未能读取「Renamed source」") {
		t.Fatalf("overlap: %q %v", warnings, err)
	}
	if calls != 3 {
		t.Fatalf("reader calls = %d", calls)
	}
}

// TestApplyKeepsUntouchedValues checks the YAML node boundary: values the patch
// does not replace keep their original form (number forms, timestamps, big
// integers), and a disabled gateway leaves the document untouched.
func TestApplyKeepsUntouchedValues(t *testing.T) {
	rawDoc := `mixed-port: 7890
proxies:
  - name: p
    port: 80
    ratio: 1.0
    big: 123456789012345678901234567890
    when: "2024-01-02 03:04:05"
    none: null
    中文: 值
tun:
  mtu: 1400
  route-address:
    - 198.18.0.0/16
rule-providers:
  Proxy IPs:
    type: http
    interval: 86400
`
	build := func() *yamlx.Node {
		n, err := yamlx.Parse([]byte(rawDoc))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	off := build()
	before, _ := yamlx.Marshal(off)
	if err := gateway.NewFixtureConfig(snapshot(false)).Apply(off, ""); err != nil {
		t.Fatal(err)
	}
	if after, _ := yamlx.Marshal(off); string(after) != string(before) {
		t.Fatalf("disabled gateway changed the document: %s", after)
	}

	on := build()
	if err := gateway.NewFixtureConfig(snapshot(true)).Apply(on, ""); err != nil {
		t.Fatal(err)
	}
	proxy := yamlx.Get(on, "proxies").Content[0]
	for k, want := range map[string]string{"name": "p", "port": "80", "ratio": "1.0", "big": "123456789012345678901234567890", "when": "2024-01-02 03:04:05", "中文": "值"} {
		if got := yamlx.Get(proxy, k); got == nil || got.Value != want {
			t.Errorf("proxies.0.%s = %v, want %q", k, got, want)
		}
	}
	if ratio := yamlx.Get(proxy, "ratio"); ratio == nil || ratio.ShortTag() != "!!float" {
		t.Errorf("ratio lost its float form: %#v", ratio)
	}
	newTun := yamlx.Get(on, "tun")
	if yamlx.Get(newTun, "route-address") != nil || yamlx.Get(newTun, "mtu").Value != "1400" {
		t.Errorf("patched TUN: %v", newTun)
	}
	original := yamlx.Get(yamlx.Get(on, "rule-providers"), "Proxy IPs")
	if yamlx.Get(original, "interval").Value != strconv.Itoa(gateway.RefreshInterval) || yamlx.Get(original, "format").Value != "yaml" ||
		!strings.HasPrefix(yamlx.Get(original, "url").Value, "https://config.example/api/profiles/home/linux-gateway/source/r?token=") {
		t.Errorf("original provider not re-pointed: %v", original)
	}
	var keys []string
	for k := range yamlx.Pairs(on) {
		keys = append(keys, k)
	}
	if strings.Join(keys, ",") != "mixed-port,proxies,tun,rule-providers,ipv6,mode,dns,"+gateway.MarkerKey {
		t.Errorf("key order %v", keys)
	}
}

// TestSettingsDefaults covers the editor/settings values the corpus cannot
// produce from Python input shapes: an absent setting, raw JSON forms.
func TestSettingsDefaults(t *testing.T) {
	tests := []struct {
		name, raw string
		enabled   bool
		ids       []string
		err       string
	}{
		{name: "absent", raw: ``},
		{name: "null", raw: ` null `},
		{name: "object", raw: `{"enabled": true, "rule_set_ids": ["a", "b", "a"]}`, enabled: true, ids: []string{"a", "b"}},
		{name: "array", raw: `[]`, err: "Linux 网关设置必须是对象"},
		{name: "enabled null", raw: `{"enabled": null}`, err: "enabled 必须是布尔值"},
		{name: "ids null", raw: `{"rule_set_ids": null}`, err: "请选择有效的 IP 规则集，最多 1024 个"},
		{name: "surrogate pair", raw: `{"rule_set_ids": ["\ud83d\ude00"]}`, ids: []string{"\U0001F600"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings, err := gateway.NormalizeSettings([]byte(tt.raw))
			if tt.err != "" {
				var gwErr *gateway.Error
				if !errors.As(err, &gwErr) || gwErr.Message != tt.err {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || settings.Enabled != tt.enabled || settings.RuleSetIDs == nil ||
				strings.Join(settings.RuleSetIDs, "|") != strings.Join(tt.ids, "|") {
				t.Fatalf("got %+v, %v", settings, err)
			}
		})
	}
}

// FuzzExtractCIDRs checks the capture's safety invariants on any input: the
// networks are IPv4, /8 or longer, sorted and disjoint, never all of IPv4.
func FuzzExtractCIDRs(f *testing.F) {
	for _, seed := range []string{"1.1.1.0/24\n8.8.8.8", "payload: [10.0.0.0/8]", "IP-CIDR,1.1.1.7/24,no-resolve",
		"0.0.0.0/1\n128.0.0.0/1", "- 2001:db8::/32", "1.2.3.4/255.255.255.0", "fe80::1%eth0", "\x00"} {
		f.Add(seed, true)
		f.Add(seed, false)
	}
	f.Fuzz(func(t *testing.T, content string, classical bool) {
		behavior := "ipcidr"
		if classical {
			behavior = "classical"
		}
		networks, err := gateway.ExtractCIDRs(content, behavior)
		if err != nil {
			var gwErr *gateway.Error
			if !errors.As(err, &gwErr) {
				t.Fatalf("not a gateway error: %v", err)
			}
			return
		}
		var previous netip.Prefix
		for i, n := range networks {
			if !n.Addr().Is4() || n.Bits() < 8 || n != n.Masked() {
				t.Fatalf("bad network %v", n)
			}
			if i > 0 && (previous.Overlaps(n) || !previous.Addr().Less(n.Addr())) {
				t.Fatalf("not sorted/disjoint: %v %v", previous, n)
			}
			previous = n
		}
	})
}

// TestHooks covers the convert hooks Wire registers: the base settings'
// fake-ip-range check (test_mihomo_base_settings.py values) and the gateway
// switch read by takeover_mode / get_mihomo_ruleset_downloads.
func TestHooks(t *testing.T) {
	hooks := gateway.Hooks()
	for _, value := range []string{"10.10.0.1/16", "198.18.0.1/15", "100.64.0.0/10"} {
		if err := hooks.FakeIPNetworkError(value); err != nil {
			t.Errorf("%s refused: %v", value, err)
		}
	}
	for _, value := range []string{"198.18.0.1/31", "10.0.0.0/7", "127.0.0.1/16", "224.0.0.1/8", "169.254.0.1/16",
		"0.0.0.0/8", "fc00::1/64", " 198.18.0.1/16"} {
		if err := hooks.FakeIPNetworkError(value); err == nil {
			t.Errorf("%s accepted", value)
		}
	}
	tests := []struct {
		name    string
		config  map[string]any
		enabled bool
		wantErr bool
	}{
		{"absent", map[string]any{"mihomo": map[string]any{}}, false, false},
		{"no mihomo", map[string]any{}, false, false},
		{"on", snapshot(true), true, false},
		{"off", snapshot(false), false, false},
		{"malformed", map[string]any{"mihomo": map[string]any{"linux_gateway": map[string]any{"enabled": "yes"}}}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, err := hooks.GatewayEnabled(gateway.NewFixtureModel(tt.config))
			if enabled != tt.enabled || (err != nil) != tt.wantErr {
				t.Fatalf("got %v, %v", enabled, err)
			}
		})
	}
}

func TestN811RuleReaderRestoreFailsClosed(t *testing.T) {
	typedCalls := 0
	t.Cleanup(gateway.SetRuleSourceReader(nil))
	restore := gateway.SetRuleSourceReader(func(ctx context.Context, item model.RuleConfig, library *model.RuleLibraryItem, config *rulefetch.SourceConfig) (string, error) {
		typedCalls++
		if item.ID != "r" || library == nil || library.ID != "lib" || config.Settings.ServerDomain != "https://config.example" {
			t.Fatal("typed snapshot lost source identity")
		}
		return "198.51.100.0/24", nil
	})
	raw := snapshot(true)
	source := raw["rule_library"].([]any)[0].(map[string]any)
	source["source_type"] = "url"
	source["url"] = "https://example.com/rules"
	delete(source, "content")
	config := gateway.NewFixtureConfig(raw)
	if _, err := config.ProviderPayload(t.Context(), "r", nil); err != nil {
		t.Fatal(err)
	}
	if typedCalls != 1 {
		t.Fatalf("readers called typed=%d", typedCalls)
	}
	restore()
	if _, err := config.ProviderPayload(t.Context(), "r", nil); !errors.Is(err, gateway.ErrRulesetContentUnavailable) {
		t.Fatalf("after restore expected ErrRulesetContentUnavailable, got %v", err)
	}
}

func TestN811ReaderProjectionExcludesUnconsumedContent(t *testing.T) {
	raw := snapshot(true)
	delete(raw, "system_config")
	source := raw["rule_library"].([]any)[0].(map[string]any)
	source["source_type"] = "url"
	source["url"] = "https://example.com/rules"
	delete(source, "content")
	raw["rule_library"] = append(raw["rule_library"].([]any), map[string]any{"id": "unused", "name": "Unused", "content": strings.Repeat("x", 1<<20)})
	raw["nodes"] = []any{map[string]any{"id": "unused", "params": map[string]any{"password": strings.Repeat("x", 1<<20)}}}
	calls := 0
	t.Cleanup(gateway.SetRuleSourceReader(func(ctx context.Context, item model.RuleConfig, library *model.RuleLibraryItem, config *rulefetch.SourceConfig) (string, error) {
		calls++
		if len(config.Library) != 2 || config.Library[1].ID != "unused" || config.Library[1].Name != "Unused" || config.Library[1].Content != nil {
			t.Fatal("reader did not receive only cache identity metadata")
		}
		return "198.51.100.0/24", nil
	}))
	if _, err := gateway.NewFixtureConfig(raw).ProviderPayload(t.Context(), "r", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("reader called %d times", calls)
	}
}

func TestReviewN811ReaderProjectionPreservesFormatInferenceWithoutBody(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	raw := snapshot(true)
	raw["system_config"].(map[string]any)["rule_proxy_token"] = "synthetic-secret-token"
	// Standalone ruleset referencing a library entry only by name (no library_rule_id, no format, no .yaml suffix).
	standalone := map[string]any{
		"id": "r", "itemType": "ruleset", "name": "Proxy IPs",
		"source_type": "url", "url": "https://rules.example/ip", "behavior": "ipcidr", "policy": "DIRECT", "enabled": true,
	}
	raw["rule_configs"] = []any{standalone}
	lib := raw["rule_library"].([]any)[0].(map[string]any)
	lib["source_type"] = "url"
	lib["url"] = "https://rules.example/ip.yaml"
	lib["format"] = "yaml"
	lib["content"] = strings.Repeat("x", 1<<20)
	raw["nodes"] = []any{map[string]any{"id": "unused", "params": map[string]any{"password": strings.Repeat("y", 1<<20)}}}

	rulesDir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(rulesDir, rules.CacheFilename(testjson.Decode[model.RuleConfig](t, standalone)))
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, []byte("payload:\n  - 198.51.100.0/24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	t.Cleanup(gateway.SetRuleSourceReader(func(ctx context.Context, item model.RuleConfig, library *model.RuleLibraryItem, config *rulefetch.SourceConfig) (string, error) {
		calls++
		if library != nil {
			t.Fatalf("expected standalone ruleset without library_rule_id, got %+v", library)
		}
		if config.Settings.ServerDomain != "https://config.example" || config.Settings.ConfigToken != "" || config.Settings.RuleProxyToken != "" {
			t.Fatalf("unexpected settings projection: %+v", config.Settings)
		}
		if len(config.Library) != 1 || config.Library[0].Format == nil || *config.Library[0].Format != "yaml" || config.Library[0].Content != nil {
			t.Fatalf("unexpected library projection: %+v", config.Library)
		}
		data, err := rulefetch.ReadCache(ctx, r, item, config)
		return string(data), err
	}))

	payload, err := gateway.NewFixtureConfig(raw).ProviderPayload(ctx, "r", nil)
	if err != nil {
		t.Fatalf("ProviderPayload failed format inference on cached YAML: %v", err)
	}
	if calls != 1 || len(payload) != 1 || payload[0] != "198.51.100.0/24" {
		t.Fatalf("unexpected payload=%v calls=%d", payload, calls)
	}
}
