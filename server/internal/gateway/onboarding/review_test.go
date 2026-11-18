package onboarding

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
)

// nativeProfile returns the test service snapshot with its OxiDNS draft in
// native mode, after edit has changed the parsed native document.
func nativeProfile(t *testing.T, edit func(document *yamlx.Node)) (Document, string) {
	t.Helper()
	s := testService(t)
	raw, _, err := s.Repository.GetPublishSnapshot(context.Background(), "default", true)
	if err != nil {
		t.Fatal(err)
	}
	config, err := fromLegacy(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := oxidns.ProfileFromJSON(encode(config))
	if err != nil {
		t.Fatal(err)
	}
	settings := yamlx.Clone(p.Settings())
	yamlx.Set(settings, "fallback_dns", yamlx.Str("https://192.0.2.54/dns-query"))
	settings, err = oxidns.PrepareEditor(settings, p, "")
	if err != nil {
		t.Fatal(err)
	}
	document, err := yamlx.Parse([]byte(text(field(settings, "native_config"))))
	if err != nil {
		t.Fatal(err)
	}
	edit(document)
	source, err := yamlx.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(settings, "native_config", yamlx.Str(string(source)))
	data, err := yamlx.ToJSON(settings)
	if err != nil {
		t.Fatal(err)
	}
	config["oxidns"] = data
	return config, string(source)
}

func dropPlugins(document *yamlx.Node, tags ...string) {
	plugins := field(document, "plugins")
	plugins.Content = slices.DeleteFunc(plugins.Content, func(item *yamlx.Node) bool {
		return slices.Contains(tags, text(field(item, "tag")))
	})
}

// TestConversionBackupKeepsOriginalNative covers review A21 F2: when the
// recognized disabled fallback was restored but the common editor still could
// not apply the gateway fields, the explicit template conversion must back up
// the user's original native document, not the partially restored projection.
func TestConversionBackupKeepsOriginalNative(t *testing.T) {
	config, original := nativeProfile(t, func(document *yamlx.Node) {
		dropPlugins(document, "forward_fallback", "fallback_dns", "fallback", "server_udp", "server_tcp")
		yamlx.Set(pluginIndex(document)["proxy_dns_seq"], "args", yamlx.Seq(step("goto remote_dns")))
	})
	form, err := ParseRequest(encode(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query", "confirm_template_conversion": true}))
	if err != nil {
		t.Fatal(err)
	}
	candidate, report, err := Prepare(context.Background(), config, form, Options{BaseURL: "https://panel.example", DNSLoop: noLoop})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || !slices.Contains(report.Warnings, msgConvertedUpdate) {
		t.Fatalf("conversion was not prepared: %+v", report)
	}
	settings, err := yamlx.FromJSON(candidate["oxidns"])
	if err != nil {
		t.Fatal(err)
	}
	if got := text(field(settings, "gateway_onboarding_native_backup", "native_config")); got != original {
		t.Fatalf("backup is not the original native document:\n%s", got)
	}
}

// TestAssertReadyClassifiesValidationErrors covers review A21 F1: Python
// assert_gateway_onboarding_ready wraps every validation class in
// GatewayOnboardingError (A22 answers it with 400). Saved drafts with
// unreadable OxiDNS upstreams or Mihomo YAML must be input errors with the
// original message, not infrastructure errors.
func TestAssertReadyClassifiesValidationErrors(t *testing.T) {
	s := testService(t)
	if _, err := s.Execute(context.Background(), testOperation()); err != nil {
		t.Fatal(err)
	}
	raw, _, err := s.Repository.GetPublishSnapshot(context.Background(), "default", true)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := fromLegacy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = AssertReady(context.Background(), saved, Options{BaseURL: "https://panel.example", DNSLoop: noLoop}); err != nil {
		t.Fatal("saved draft not ready:", err)
	}
	tests := []struct {
		name string
		edit func(config Document)
	}{
		{"broken remote upstream YAML", func(config Document) {
			settings, _ := yamlx.FromJSON(config["oxidns"])
			yamlx.Set(settings, "remote_dns", yamlx.Str("- addr: [broken"))
			config["oxidns"], _ = yamlx.ToJSON(settings)
		}},
		{"broken Mihomo base YAML", func(config Document) {
			mihomo := object(config["mihomo"])
			mihomo["custom_config"] = encode("dns: [broken\n")
			config["mihomo"] = encode(mihomo)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := clone(saved)
			tc.edit(config)
			_, err := AssertReady(context.Background(), config, Options{BaseURL: "https://panel.example", DNSLoop: noLoop})
			if err == nil || !IsInputError(err) || err.Error() == "" {
				t.Fatalf("validation failure is not an input error: %T %v", err, err)
			}
		})
	}
}

// TestShortCredentialDoesNotCorruptWizardFields covers review A21 F4: a
// profile credential that is a short common word (here a node password "dns")
// must not mask the wizard's keys and form values, which Python returns
// verbatim. Diagnostics still mask configuration credentials, and system
// secrets remain masked everywhere.
func TestShortCredentialDoesNotCorruptWizardFields(t *testing.T) {
	config := Document{
		"nodes":         encode([]map[string]any{{"name": "Short", "type": "ss", "password": "dns"}, {"name": "Long", "type": "ss", "password": "synthetic-node-secret"}}),
		"system_config": encode(map[string]any{"rule_proxy_token": "synthetic-rule-token"}),
	}
	state := State{Settings: FormState{LocalDNS: "https://192.0.2.53/dns-query"}, OxiDNSMode: encode("template"), ServerURL: "https://config.example/synthetic-rule-token",
		Warnings: []string{"rule synthetic-node-secret"}, Blockers: []string{}, FieldWarnings: map[string]string{}}
	raw, err := sanitize(encode(state), config)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Settings struct {
			LocalDNS string `json:"local_dns"`
		} `json:"settings"`
		OxiDNSMode string   `json:"oxidns_mode"`
		ServerURL  string   `json:"server_url"`
		Warnings   []string `json:"warnings"`
	}
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Settings.LocalDNS != state.Settings.LocalDNS || got.OxiDNSMode != "template" {
		t.Fatalf("short credential corrupted wizard fields: %s", raw)
	}
	if got.ServerURL != redact.Redacted || len(got.Warnings) != 1 || got.Warnings[0] != redact.Redacted {
		t.Fatalf("secret not masked: %s", raw)
	}
	message, err := sanitize(encode("bad synthetic-node-secret"), config)
	if err != nil || stringValue(message) != redact.Redacted {
		t.Fatalf("diagnostic error leaked: %s %v", message, err)
	}
}

func TestMappingEditsKeepAliasGraphAndInput(t *testing.T) {
	leaf := yamlx.Seq(yamlx.Str("kept"))
	// A very small physical graph with exponentially many expanded values.
	// Editing its parent must neither visit nor expand the shared children.
	for range 40 {
		leaf = yamlx.Seq(leaf, leaf)
	}
	source := yamlx.Map(yamlx.Str("graph"), leaf, yamlx.Str("enabled"), yamlx.Bool(false))
	candidate := editMapping(source)
	yamlx.Set(candidate, "enabled", yamlx.Bool(true))
	if field(candidate, "graph") != leaf || field(source, "graph") != leaf || boolean(field(source, "enabled")) {
		t.Fatal("mapping edit expanded, truncated or mutated the source graph")
	}
}

func TestConversionBackupKeepsOriginalNestedSettings(t *testing.T) {
	config, _ := nativeProfile(t, func(document *yamlx.Node) {
		dropPlugins(document, "forward_fallback", "fallback_dns", "fallback", "server_udp", "server_tcp")
		yamlx.Set(pluginIndex(document)["proxy_dns_seq"], "args", yamlx.Seq(step("goto remote_dns")))
	})
	settings, err := yamlx.FromJSON(config["oxidns"])
	if err != nil {
		t.Fatal(err)
	}
	yamlx.Set(settings, "api", yamlx.Map(yamlx.Str("enabled"), yamlx.Bool(false), yamlx.Str("username"), yamlx.Str("synthetic-user")))
	yamlx.Set(settings, "query_recorder", yamlx.Map(yamlx.Str("enabled"), yamlx.Bool(false)))
	config["oxidns"], err = yamlx.ToJSON(settings)
	if err != nil {
		t.Fatal(err)
	}
	expectedBackup, err := oxidns.NormalizeSettings(settings, false)
	if err != nil {
		t.Fatal(err)
	}
	before := string(encode(config))
	form, err := ParseRequest(encode(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query", "confirm_template_conversion": true, "query_recorder": true}))
	if err != nil {
		t.Fatal(err)
	}
	candidate, _, err := Prepare(context.Background(), config, form, Options{DNSLoop: noLoop})
	if err != nil {
		t.Fatal(err)
	}
	result, err := yamlx.FromJSON(candidate["oxidns"])
	if err != nil {
		t.Fatal(err)
	}
	backup := field(result, "gateway_onboarding_native_backup")
	if backup == nil || !yamlx.Equal(field(backup, "api"), field(expectedBackup, "api")) || !yamlx.Equal(field(backup, "query_recorder"), field(expectedBackup, "query_recorder")) {
		t.Fatal("template API/recorder edits modified the retained native backup")
	}
	if !boolean(field(result, "api", "enabled")) || !boolean(field(result, "query_recorder", "enabled")) || string(encode(config)) != before {
		t.Fatal("candidate did not change independently of its input")
	}
}
