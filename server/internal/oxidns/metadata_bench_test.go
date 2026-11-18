package oxidns_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/oxidns"
)

func metadataProfile(t testing.TB, count, unusedBytes int) *oxidns.Profile {
	t.Helper()
	type rule struct {
		ID       string `json:"id"`
		Kind     string `json:"itemType"`
		Name     string `json:"name"`
		Library  string `json:"library_rule_id"`
		Behavior string `json:"behavior"`
		URL      string `json:"url"`
	}
	type library struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Source   string `json:"source_type"`
		Behavior string `json:"behavior"`
		Content  string `json:"content"`
	}
	rules := make([]rule, count)
	libraries := make([]library, count)
	ids := make([]string, count)
	for i := range rules {
		id := fmt.Sprintf("set-%d", i)
		ids[i] = id
		rules[i] = rule{id, "ruleset", id, id, "classical", "https://rules.example/" + id}
		libraries[i] = library{id, id, "content", "classical", "DOMAIN-SUFFIX,example.com"}
	}
	value := struct {
		Rules   []rule    `json:"rule_configs"`
		Library []library `json:"rule_library"`
		System  struct {
			Token string `json:"rule_proxy_token"`
		} `json:"system_config"`
		Settings struct {
			Proxy  []string `json:"proxy_rulesets"`
			Local  string   `json:"local_dns"`
			Remote string   `json:"remote_dns"`
		} `json:"oxidns"`
		Unrelated string `json:"unrelated"`
	}{Rules: rules, Library: libraries, Unrelated: strings.Repeat("x", unusedBytes)}
	value.System.Token = "synthetic-capability"
	value.Settings.Proxy = ids
	value.Settings.Local = "udp://192.0.2.53"
	value.Settings.Remote = "udp://192.0.2.54"
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := oxidns.ProfileFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

// BenchmarkOxiDNSMetadataChain exercises Generate -> IP-presence -> rulefetch /
// RulesetFormat using the real registered offline reader. Profile parsing stays
// outside the timed region. Unrelated data must not be decoded inside the chain.
func BenchmarkOxiDNSMetadataChain(b *testing.B) {
	for _, width := range []int{0, 2 << 20} {
		b.Run(fmt.Sprintf("rules128/unused%d", width), func(b *testing.B) {
			profile := metadataProfile(b, 128, width)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := oxidns.Generate(profile, "http://panel.example"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
