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
func TestTypedMosDNSR5(t *testing.T) {
	config, err := ConfigFromJSON([]byte(`{"mosdns":{"cache_size":true,"cache_lazy_ttl":1.5,"cache_enabled":{},"log_level":7},"rule_configs":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Profile.MosDNS.CacheSize != nil || config.Profile.MosDNS.CacheLazyTTL != nil || config.Profile.MosDNS.CacheEnabled != nil {
		t.Fatal("shared R5 fallback not applied")
	}
	output, err := Generate(config, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "size: 10240") || !strings.Contains(output, "lazy_cache_ttl: 21600") {
		t.Fatal(output)
	}
	encoded, _ := json.Marshal(config.Profile.MosDNS)
	if !strings.Contains(string(encoded), `"cache_size":true`) {
		t.Fatal("consumption changed storage", string(encoded))
	}
}

func BenchmarkTypedMosDNSUnrelatedFields(b *testing.B) {
	for _, size := range []int{0, 1 << 20} {
		b.Run(string(rune('0'+size/(1<<20))), func(b *testing.B) {
			config := &model.Config{Profile: &model.Profile{}}
			if err := config.Profile.Fields.Set("unused", json.RawMessage(`"`+strings.Repeat("x", size)+`"`)); err != nil {
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
