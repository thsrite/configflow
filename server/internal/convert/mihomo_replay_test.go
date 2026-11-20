package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// MihomoReplayIssue only classifies the shapes owned by N8-8b. This is test-only:
// normal archived outputs keep their original Python and byte oracles.
func MihomoReplayIssue(profile *JObj) string {
	if profile == nil {
		return "config object"
	}
	data, err := CompactJ(profile)
	if err != nil || !json.Valid(data) {
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
			if item, ok := raw.(*JObj); ok {
				if value, present := item.Get("health_check"); present && value == nil {
					return "R5 " + key + ".health_check null"
				}

			}
		}
	}
	if settings := profile.GetObject("mihomo"); settings != nil {
		if _, ok := settings.Value("provider_update_interval").(string); ok {
			return "R8 quoted provider interval"
		}

		if n, ok := settings.Value("provider_update_interval").(json.Number); ok && !IsIntNumber(n) {
			return "R8 provider_update_interval integer"
		}
	}

	for _, key := range []string{"subscriptions", "subscription_aggregations"} {
		for _, item := range profile.GetList(key) {
			if row, ok := item.(*JObj); ok {
				if _, numeric := row.Value("health_check_url").(json.Number); numeric {
					return "shared numeric health URL text"
				}
			}
		}
	}
	return ""
}

func TestNativeMihomoStoredMemberFallbacks(t *testing.T) {
	cases := []struct {
		source, path string
		compatible   bool
	}{
		{`{"mihomo":{"custom_config":5}}`, "mihomo.custom_config", true},
		{`{"mihomo":{"provider_update_interval":"300"}}`, "mihomo.provider_update_interval", true},
		{`{"proxy_groups":[{"id":"g","type":"select","interval":"300"}]}`, "proxy_groups[0].interval", true},
		{`{"subscriptions":[{"id":"s","health_check":"x"}]}`, "subscriptions[0].health_check", false},
		{`{"subscription_aggregations":[{"id":"a","health_check_url":5}]}`, "subscription_aggregations[0].health_check_url", true},
		{`{"mosdns":{"custom_hosts":false}}`, "mosdns.custom_hosts", false},
		{`{"domain_discovery":{"region_probe_port":"300"}}`, "domain_discovery.region_probe_port", true},
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
			if model.IssueUnder(issues, tt.path) == tt.compatible {
				t.Fatalf("shared scalar compatibility=%v diagnostics=%v", tt.compatible, issues)
			}
			switch tt.path {
			case "mihomo.custom_config":
				if profile.Mihomo.CustomConfig != "5" {
					t.Fatal("numeric base text not decoded")
				}
			case "mihomo.provider_update_interval":
				if profile.Mihomo.ProviderUpdateInterval == nil || *profile.Mihomo.ProviderUpdateInterval != 300 {
					t.Fatal("quoted provider interval not decoded")
				}
			case "proxy_groups[0].interval":
				if profile.ProxyGroups[0].Interval == nil || *profile.ProxyGroups[0].Interval != 300 {
					t.Fatal("quoted group interval not decoded")
				}
			case "subscriptions[0].health_check":
				if shared.Subscriptions[0].HealthCheck != nil {
					t.Fatal("non-object health accepted")
				}
			case "subscription_aggregations[0].health_check_url":
				if shared.Aggregations[0].HealthCheckURL == nil || *shared.Aggregations[0].HealthCheckURL != "5" {
					t.Fatal("numeric health URL text not decoded")
				}
			case "mosdns.custom_hosts":
				if profile.MosDNS.CustomHosts != "" {
					t.Fatal("non-text hosts accepted")
				}
			case "domain_discovery.region_probe_port":
				if profile.DomainDiscovery.RegionProbePort == nil || *profile.DomainDiscovery.RegionProbePort != 300 {
					t.Fatal("quoted probe port not decoded")
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

func TestSharedScalarMihomoOutputBoundaries(t *testing.T) {
	env := nativeGenerationEnv(t)
	t.Cleanup(SetMihomoEnvironment(env))
	config, err := DecodeConfigJ(mustJSON(t, `{"mihomo":{"provider_update_interval":"3600"},"proxy_groups":[{"id":"g","name":"G","type":"url-test","interval":"300","manual_nodes":["DIRECT"]}],"subscriptions":[{"id":"s","name":"S","health_check_url":5}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Profile.Mihomo.ProviderUpdateInterval == nil || *config.Profile.Mihomo.ProviderUpdateInterval != 3600 {
		t.Fatalf("quoted interval: %v", config.Profile.Mihomo.ProviderUpdateInterval)
	}
	doc, err := MihomoDocument(t.Context(), config, MihomoOptions{SkipProviderPreflight: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, group := range yamlx.Get(doc, "proxy-groups").Content {
		if yamlx.Get(group, "name").Value == "G" {
			interval := yamlx.Get(group, "interval")
			found = interval != nil && interval.ShortTag() == "!!str" && interval.Value == "300"
		}
	}
	if !found {
		data, _ := yamlx.Marshal(doc)
		t.Fatalf("quoted group interval lost its stored YAML override: %s", data)
	}
	address := config.Subscriptions[0].HealthCheckURL
	health, err := providerHealthNode(nil, address, env)
	if err != nil || yamlx.Get(health, "url").Value != "5" {
		t.Fatalf("numeric URL text: %v", err)
	}
}

func TestMihomoFollowKeepsLocalQuotedInterval(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(nativeGenerationEnv(t)))
	config, err := DecodeConfigJ(mustJSON(t, `{"proxy_groups":[{"id":"f","name":"Follower","type":"url-test","follow_group":"s","interval":"300"},{"id":"s","name":"Source","type":"url-test","manual_nodes":["DIRECT"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := MihomoDocument(t.Context(), config, MihomoOptions{SkipProviderPreflight: true})
	if err != nil {
		t.Fatal(err)
	}
	groups := yamlx.Get(doc, "proxy-groups")
	if len(groups.Content) != 2 {
		t.Fatalf("groups: %v", groups)
	}
	interval := yamlx.Get(groups.Content[0], "interval")
	if interval == nil || interval.ShortTag() != "!!str" || interval.Value != "300" {
		t.Fatalf("interval: %v", interval)
	}
}
