package gateway_test

import (
	"context"
	"errors"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

// snapshot is test_linux_gateway.py's snapshot(): one selected inline rule set.
func snapshot(enabled bool) *jsonx.Object {
	return jsonx.Obj(
		"profile_id", "home",
		"system_config", jsonx.Obj("server_domain", "https://config.example", "config_token", "a+b&c"),
		"mihomo", jsonx.Obj("custom_config", "mixed-port: 7890\n",
			"linux_gateway", jsonx.Obj("enabled", enabled, "rule_set_ids", []any{"r"})),
		"rule_configs", []any{jsonx.Obj("id", "r", "itemType", "ruleset", "name", "Proxy IPs",
			"library_rule_id", "lib", "behavior", "ipcidr", "policy", "DIRECT", "enabled", true)},
		"rule_library", []any{jsonx.Obj("id", "lib", "name", "Proxy IPs", "behavior", "ipcidr",
			"source_type", "content", "content", "1.1.1.0/24\n8.8.8.8", "enabled", true)},
	)
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
	source := config.GetList("rule_library")[0].(*jsonx.Object)
	source.Set("source_type", "url")
	source.Set("url", "https://rules.example/ip.txt")
	source.Delete("content")

	if _, err := gateway.NewConfig(config).ProviderPayload(ctx, "r", nil); !errors.Is(err, gateway.ErrRulesetContentUnavailable) {
		t.Fatalf("without a reader: %v", err)
	}
	var calls int
	t.Cleanup(gateway.SetRulesetContent(func(_ context.Context, item, library, cfg *jsonx.Object) (string, error) {
		calls++
		if item.GetString("id") != "r" || library.GetString("id") != "lib" || cfg != config {
			t.Errorf("reader arguments: %v %v", item, library)
		}
		return "", nil
	}))
	rulesDir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(rulesDir, rules.RuleCacheFilename(source))
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source.Set("name", "Renamed source")
	payload, err := gateway.NewConfig(config).ProviderPayload(ctx, "r", nil)
	if err != nil || len(payload) != 0 {
		t.Fatalf("empty cached source: %v %v", payload, err)
	}
	if err := os.Remove(filepath.Join(rulesDir, rules.RuleCacheFilename(source))); err != nil {
		t.Fatal(err)
	}
	_, err = gateway.NewConfig(config).ProviderPayload(ctx, "r", nil)
	var gwErr *gateway.Error
	if !errors.As(err, &gwErr) || !strings.Contains(gwErr.Message, "无法读取") {
		t.Fatalf("missing cache: %v", err)
	}
	// The overlap check treats an empty remote set as unreadable, without the
	// cache requirement.
	warnings, err := gateway.NewConfig(config).DirectOverlapWarnings(ctx, nil)
	if err != nil || len(warnings) != 1 || !strings.HasPrefix(warnings[0], "未能读取「Renamed source」") {
		t.Fatalf("overlap: %q %v", warnings, err)
	}
	if calls != 3 {
		t.Fatalf("reader calls = %d", calls)
	}
}

// TestApplyKeepsUntouchedValues checks the jsonx boundary: values the patch
// does not replace come back as they were (number forms, timestamps, big
// integers), and a disabled gateway leaves the document untouched.
func TestApplyKeepsUntouchedValues(t *testing.T) {
	big, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	stamp := "2024-01-02 03:04:05"
	proxies := []any{jsonx.Obj("name", "p", "port", jsonx.Int(80), "ratio", jsonx.Float(1.0),
		"big", jsonx.BigInt(big), "when", stamp, "raw", []byte("x"), "none", nil, "中文", "值")}
	tun := jsonx.Obj("mtu", jsonx.Int(1400), "route-address", []any{"198.18.0.0/16"})
	build := func() *jsonx.Object {
		return jsonx.Obj("mixed-port", jsonx.Int(7890), "proxies", proxies, "tun", tun,
			"rule-providers", jsonx.Obj("Proxy IPs", jsonx.Obj("type", "http", "interval", jsonx.Int(86400))))
	}

	off := build()
	before, _ := jsonx.Compact(off)
	if err := gateway.ApplyJSONX(off, snapshot(false), ""); err != nil {
		t.Fatal(err)
	}
	if after, _ := jsonx.Compact(off); string(after) != string(before) || off.Value("tun") != tun {
		t.Fatalf("disabled gateway changed the document: %s", after)
	}

	on := build()
	if err := gateway.ApplyJSONX(on, snapshot(true), ""); err != nil {
		t.Fatal(err)
	}
	got := on.GetList("proxies")[0].(*jsonx.Object)
	want := proxies[0].(*jsonx.Object)
	for _, key := range want.Keys() {
		if !reflect.DeepEqual(got.Value(key), want.Value(key)) {
			t.Errorf("proxies.0.%s = %#v, want %#v", key, got.Value(key), want.Value(key))
		}
	}
	if _, isFloat := got.Value("ratio").(jsonx.Number); !isFloat || !got.Value("ratio").(jsonx.Number).IsFloat() {
		t.Errorf("ratio lost its float form: %#v", got.Value("ratio"))
	}
	if tun.Has("route-address-set") || !tun.Has("route-address") {
		t.Errorf("the input TUN object was modified: %v", tun)
	}
	newTun := on.GetObject("tun")
	if newTun.Has("route-address") || newTun.GetInt("mtu") != 1400 {
		t.Errorf("patched TUN: %v", newTun)
	}
	original := on.GetObject("rule-providers").GetObject("Proxy IPs")
	if original.GetInt("interval") != gateway.RefreshInterval || original.GetString("format") != "yaml" ||
		!strings.HasPrefix(original.GetString("url"), "https://config.example/api/profiles/home/linux-gateway/source/r?token=") {
		t.Errorf("original provider not re-pointed: %v", original)
	}
	if keys := on.Keys(); strings.Join(keys, ",") != "mixed-port,proxies,tun,rule-providers,ipv6,mode,dns,"+gateway.MarkerKey {
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
		config  *jsonx.Object
		enabled bool
		wantErr bool
	}{
		{"absent", jsonx.Obj("mihomo", jsonx.NewObject()), false, false},
		{"no mihomo", jsonx.NewObject(), false, false},
		{"on", snapshot(true), true, false},
		{"off", snapshot(false), false, false},
		{"malformed", jsonx.Obj("mihomo", jsonx.Obj("linux_gateway", jsonx.Obj("enabled", "yes"))), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, err := hooks.GatewayEnabled(tt.config)
			if enabled != tt.enabled || (err != nil) != tt.wantErr {
				t.Fatalf("got %v, %v", enabled, err)
			}
		})
	}
}
