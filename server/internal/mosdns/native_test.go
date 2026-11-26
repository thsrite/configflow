package mosdns

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestTypedGenerationUsesConsumedFields(t *testing.T) {
	raw := []byte(`{"mosdns":{"direct_rules":["one"],"custom_hosts":"192.0.2.1 host.example","local_dns":"192.0.2.53","remote_dns":"https://dns.example/dns-query"},"rule_configs":[{"id":"one","itemType":"rule","rule_type":"DOMAIN-SUFFIX","value":"example.com"}],"unrelated":{"name":7}}`)
	config, err := ConfigFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	before, err := model.Marshal(config.Profile)
	if err != nil {
		t.Fatal(err)
	}
	text, err := Generate(config, "http://panel.example")
	if err != nil {
		t.Fatal(err)
	}
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Get(root, "plugins") == nil || !strings.Contains(text, "qname $direct_rules") {
		t.Fatal(text)
	}
	files, err := CustomFiles(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[1].Content != "domain:example.com" {
		t.Fatalf("%+v", files)
	}
	after, _ := model.Marshal(config.Profile)
	if string(before) != string(after) {
		t.Fatal("generation mutated source")
	}
}

// TestTypedMosDNSStoredValuesOfAnotherType: mosdns settings are plain
// records (O18 T1c). A stored cache size that is not an integer (true, 1.5)
// reads as missing, so generation uses the defaults; a stored switch of
// another type keeps its truth value ({} is off). Wrong-typed members are not
// written back (R5 kept their original values).
func TestTypedMosDNSStoredValuesOfAnotherType(t *testing.T) {
	config, err := ConfigFromJSON([]byte(`{"mosdns":{"cache_size":true,"cache_lazy_ttl":1.5,"cache_enabled":{},"log_level":7},"rule_configs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Profile.MosDNS
	if settings.CacheSize != nil || settings.CacheLazyTTL != nil {
		t.Fatalf("wrong-typed integers must read as missing: %v %v", settings.CacheSize, settings.CacheLazyTTL)
	}
	if settings.CacheEnabled != nil && *settings.CacheEnabled {
		t.Fatal("cache_enabled {} must read as off")
	}
	output, err := Generate(config, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "tag: lazy_cache") {
		t.Fatalf("empty object cache_enabled must disable cache: %s", output)
	}
	enabledConfig, err := ConfigFromJSON([]byte(`{"mosdns":{"cache_size":true,"cache_lazy_ttl":1.5,"log_level":7},"rule_configs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	enabledOutput, err := Generate(enabledConfig, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enabledOutput, "size: 10240") || !strings.Contains(enabledOutput, "lazy_cache_ttl: 21600") {
		t.Fatal(enabledOutput)
	}
	encoded, _ := json.Marshal(config.Profile.MosDNS)
	if strings.Contains(string(encoded), `"cache_size"`) || strings.Contains(string(encoded), `"cache_lazy_ttl"`) {
		t.Fatal("wrong-typed members must not be written back", string(encoded))
	}
}

func BenchmarkTypedMosDNSUnrelatedFields(b *testing.B) {
	for _, size := range []int{0, 1 << 20} {
		b.Run(string(rune('0'+size/(1<<20))), func(b *testing.B) {
			config := &model.Config{Profile: &model.Profile{}}
			if err := config.Profile.SetJSON("description", json.RawMessage(`"`+strings.Repeat("x", size)+`"`)); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Generate(config, ""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestProxyCredentialEvidenceStaysLiteral(t *testing.T) {
	for _, source := range []string{`{"rule_proxy_token":5}`, `{"rule_proxy_token":true}`, `{"rule_proxy_token":null}`, `{"rule_proxy_token":[]}`} {
		var system model.SystemConfig
		if err := json.Unmarshal([]byte(source), &system); err != nil {
			t.Fatal(err)
		}
		if got := ProxyCredentials(system); got.Current != "" {
			t.Fatal("R5 fallback became credential evidence")
		}
	}
	var system model.SystemConfig
	if err := json.Unmarshal([]byte(`{"rule_proxy_token":"current","retired_rule_proxy_tokens":["retired",5,null,{},true]}`), &system); err != nil {
		t.Fatal(err)
	}
	got := ProxyCredentials(system)
	if got.Current != "current" || len(got.Retired) != 1 || got.Retired[0] != "retired" {
		t.Fatalf("literal evidence=%#v", got)
	}
}
