package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// MihomoReplayIssue only classifies the shapes owned by N8-8b. This is test-only:
// normal archived outputs keep their original Python and byte oracles.
func MihomoReplayIssue(profile *jsonx.Object) string {
	if profile == nil {
		return "config object"
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return "nonfinite stored config"
	}
	var p model.Profile
	var shared model.Shared
	if json.Unmarshal(data, &p) != nil || json.Unmarshal(data, &shared) != nil {
		return "config object"
	}
	for _, issue := range append(model.Issues(p), model.Issues(shared)...) {
		for _, prefix := range []string{"mihomo", "mosdns.custom_hosts", "proxy_groups", "subscriptions", "subscription_aggregations", "domain_discovery"} {
			if issue.Path == prefix || strings.HasPrefix(issue.Path, prefix+".") || strings.HasPrefix(issue.Path, prefix+"[") {
				return "R5 " + issue.Path
			}
		}
	}
	for _, key := range []string{"subscriptions", "subscription_aggregations"} {
		for _, raw := range profile.GetList(key) {
			if item, ok := raw.(*jsonx.Object); ok {
				if value, present := item.Get("health_check"); present && value == nil {
					return "R5 " + key + ".health_check null"
				}

			}
		}
	}
	if settings := profile.GetObject("mihomo"); settings != nil {
		if n, ok := settings.Value("provider_update_interval").(jsonx.Number); ok && !n.IsInt() {
			return "R8 provider_update_interval integer"
		}
	}
	return ""
}

func TestNativeMihomoStoredMemberFallbacks(t *testing.T) {
	cases := []struct{ source, path string }{
		{`{"mihomo":{"custom_config":5}}`, "mihomo.custom_config"},
		{`{"mihomo":{"provider_update_interval":"300"}}`, "mihomo.provider_update_interval"},
		{`{"proxy_groups":[{"id":"g","type":"select","interval":"300"}]}`, "proxy_groups[0].interval"},
		{`{"subscriptions":[{"id":"s","health_check":"x"}]}`, "subscriptions[0].health_check"},
		{`{"subscription_aggregations":[{"id":"a","health_check_url":5}]}`, "subscription_aggregations[0].health_check_url"},
		{`{"mosdns":{"custom_hosts":false}}`, "mosdns.custom_hosts"},
		{`{"domain_discovery":{"region_probe_port":"300"}}`, "domain_discovery.region_probe_port"},
	}
	for _, tt := range cases {
		t.Run(tt.path, func(t *testing.T) {
			var profile model.Profile
			var shared model.Shared
			if err := json.Unmarshal([]byte(tt.source), &profile); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.source), &shared); err != nil {
				t.Fatal(err)
			}
			issues := append(model.Issues(profile), model.Issues(shared)...)
			if !model.IssueUnder(issues, tt.path) {
				t.Fatalf("missing typed diagnostic: %v", issues)
			}
			switch tt.path {
			case "mihomo.custom_config":
				if profile.Mihomo.CustomConfig != "" {
					t.Fatal("non-text base accepted")
				}
			case "mihomo.provider_update_interval":
				if profile.Mihomo.ProviderUpdateInterval != nil {
					t.Fatal("non-number interval accepted")
				}
			case "proxy_groups[0].interval":
				if profile.ProxyGroups[0].Interval != nil {
					t.Fatal("non-number group interval accepted")
				}
			case "subscriptions[0].health_check":
				if shared.Subscriptions[0].HealthCheck != nil {
					t.Fatal("non-object health accepted")
				}
			case "subscription_aggregations[0].health_check_url":
				if shared.Aggregations[0].HealthCheckURL != nil {
					t.Fatal("non-text health URL accepted")
				}
			case "mosdns.custom_hosts":
				if profile.MosDNS.CustomHosts != "" {
					t.Fatal("non-text hosts accepted")
				}
			case "domain_discovery.region_probe_port":
				if profile.DomainDiscovery.RegionProbePort != nil {
					t.Fatal("non-number probe port accepted")
				}
			}
			raw, err := model.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			var original, roundtrip map[string]json.RawMessage
			json.Unmarshal([]byte(tt.source), &original)
			json.Unmarshal(raw, &roundtrip)
			for key, value := range original {
				if key == "subscriptions" || key == "subscription_aggregations" {
					continue
				}
				if string(value) != string(roundtrip[key]) {
					t.Fatalf("stored field %s rewritten: %s", key, raw)
				}
			}
		})
	}
	var agent model.Agent
	if err := json.Unmarshal([]byte(`{"id":"agent","host":"192.0.2.1","port":8080.0}`), &agent); err != nil {
		t.Fatal(err)
	}
	ports, _, _ := AgentPorts([]model.Agent{agent})
	if ports[8080] != fmt.Sprintf(msgBaseAgentPort, "Mihomo", "agent") {
		t.Fatal(ports)
	}
}

// NativeBaseControllerMessage exposes the shared message to archived boundary tests.
const NativeBaseControllerMessage = msgBaseController

// NativeMihomoRulesMessage exposes the typed rule-list diagnostic for old client boundary tests.
const NativeMihomoRulesMessage = msgMihomoRules
