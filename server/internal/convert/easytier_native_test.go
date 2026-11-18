package convert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestTypedEasyTierValidation(t *testing.T) {
	for _, tt := range []struct{ input, message string }{
		{`{"enabled":0}`, "enabled"},
		{`{"name":false}`, msgETName},
		{`{"keepalive":1}`, msgETKeepObject},
		{`{"options":false}`, msgETOptionsObject},
		{`{"options":{"udp":"yes"}}`, "udp"},
		{`{"options":{"mtu":"1500.0"}}`, "mtu"},
		{`{"options":{"peers":false}}`, "peers"},
		{`{"routes":[1]}`, msgETRoutes},
	} {
		t.Run(tt.input, func(t *testing.T) {
			var input model.EasyTierSettings
			if err := json.Unmarshal([]byte(tt.input), &input); err != nil {
				t.Fatal(err)
			}
			_, err := NormalizeEasyTier(&input)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("want validation %q: %v", tt.message, err)
			}
		})
	}
	// Integral float notation is R8, independent of Python's strict int test.
	var input model.EasyTierSettings
	if err := json.Unmarshal([]byte(`{"options":{"mtu":1500.0,"routing-mark":6e1},"keepalive":{"interval":60.0}}`), &input); err != nil {
		t.Fatal(err)
	}
	settings, err := NormalizeEasyTier(&input)
	if err != nil || settings.Options.MTU == nil || *settings.Options.MTU != 1500 || *settings.Options.RoutingMark != 60 || *settings.Keepalive.Interval != 60 {
		t.Fatalf("R8: %#v %v", settings, err)
	}
}

func TestTypedEasyTierQuotedMTU(t *testing.T) {
	var input model.EasyTierSettings
	if err := json.Unmarshal([]byte(`{"options":{"mtu":"1500"}}`), &input); err != nil {
		t.Fatal(err)
	}
	settings, err := NormalizeEasyTier(&input)
	if err != nil || settings.Options.MTU == nil || *settings.Options.MTU != 1500 {
		t.Fatalf("quoted MTU: %#v %v", settings, err)
	}
	normalized, err := easyTierSettingsNode(settings)
	if err != nil {
		t.Fatal(err)
	}
	mtu := yamlx.Get(yamlx.Get(normalized, "options"), "mtu")
	if mtu == nil || mtu.ShortTag() != "!!int" || mtu.Value != "1500" {
		t.Fatalf("MTU was not normalized: %+v", mtu)
	}
}

func TestTypedEasyTierNetworksAndRedaction(t *testing.T) {
	for _, value := range []string{"192.0.2.129/24", "192.0.2.129/00000024", "192.0.2.129/255.255.255.0", "192.0.2.129/0.0.0.255"} {
		got, err := easyTierNetwork(value, msgETRoutes)
		if err != nil || got != "192.0.2.0/24" {
			t.Fatalf("%q: %q %v", value, got, err)
		}
	}
	var input model.EasyTierSettings
	if err := json.Unmarshal([]byte(`{"options":{"network-secret":5,"local-private-key":false,"future":"keep"}}`), &input); err != nil {
		t.Fatal(err)
	}
	redacted, err := RedactEasyTier(&input)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := model.Marshal(redacted)
	if string(data) != `{"options":{"network-secret":"[REDACTED]","local-private-key":"[REDACTED]","future":"keep"}}` {
		t.Fatalf("secret shape: %s", data)
	}
	if EasyTierGroupName(nil) != msgETKeepSuffix {
		t.Fatal("empty typed name was not safe")
	}
	var profile model.Profile
	if err := json.Unmarshal([]byte(`{"mihomo":{"easytier":5}}`), &profile); err != nil {
		t.Fatal(err)
	}
	settings, err := ConfigEasyTier(&model.Config{Profile: &profile})
	if err != nil || EasyTierFor(settings, false) {
		t.Fatalf("R5 malformed overlay: %v", err)
	}
}

func TestTypedEasyTierUsesAliasedSections(t *testing.T) {
	var settings model.EasyTierSettings
	if err := json.Unmarshal([]byte(`{"enabled":true,"magic_dns":true,"options":{"network-name":"test","peers":["tcp://192.0.2.1:11010"]}}`), &settings); err != nil {
		t.Fatal(err)
	}
	document, err := yamlx.Parse([]byte("dns-base: &dns {nameserver-policy: {}}\ndns: *dns\nproxies: &proxies []\nproxy-copy: *proxies\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyEasyTierNode(document, &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{EasyTier: &settings}}}, false, nil); err != nil {
		t.Fatal(err)
	}
	if scalarText(yamlx.Get(yamlx.Get(yamlx.Get(document, "dns"), "nameserver-policy"), "+.et.net")) != "et://EasyTier" {
		t.Fatal("alias DNS policy was skipped")
	}
	output, err := yamlx.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := yamlx.Parse(output)
	if err != nil || !yamlx.Equal(document, parsed) {
		t.Fatalf("aliased output changed: %s %v", output, err)
	}
}

func FuzzTypedEasyTier(f *testing.F) {
	for _, source := range []string{
		`{}`, `{"options":{"udp":false}}`, `{"options":{"mtu":1500.0}}`,
		`{"enabled":true,"options":{"network-name":"test","peers":["tcp://192.0.2.1:11010"]},"magic_dns":true,"routes":["192.0.2.7/24"]}`,
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		var input model.EasyTierSettings
		if json.Unmarshal(source, &input) != nil {
			return
		}
		before, err := model.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := NormalizeEasyTier(&input)
		after, _ := model.Marshal(input)
		if !bytes.Equal(before, after) {
			t.Fatal("normalization changed its input")
		}
		if err != nil {
			return
		}
		again, err := NormalizeEasyTier(normalized)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := model.Marshal(normalized)
		second, _ := model.Marshal(again)
		if !bytes.Equal(first, second) {
			t.Fatalf("normalization not idempotent: %s -> %s", first, second)
		}
		document := yamlx.Map()
		config := &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{EasyTier: normalized}}}
		if _, err := ApplyEasyTierNode(document, config, false, nil); err != nil {
			t.Fatal(err)
		}
		output, err := yamlx.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := yamlx.Parse(output)
		if err != nil || !yamlx.Equal(document, parsed) {
			t.Fatalf("generated overlay is not closed: %s %v", output, err)
		}
	})
}

// easyTierRowIssue limits the Python internal replay to well-typed inputs.
// Malformed members and R8 integer notation have explicit native assertions.
func easyTierRowIssue(row corpusRow) string {
	var value any
	switch row.fn {
	case "normalize_settings", "redact_settings":
		value = row.args[0]
	case "apply_easytier":
		config, ok := row.args[1].(*jsonx.Object)
		if !ok && row.args[1] != nil {
			return "easytier.config object"
		}
		if config != nil {
			mihomo, present := config.Get("mihomo")
			if present && mihomo != nil {
				obj, ok := mihomo.(*jsonx.Object)
				if !ok {
					return "easytier.mihomo object"
				}
				value = obj.Value("easytier")
			}
		}
		if row.args[3] != nil {
			if _, ok := row.args[3].(string); !ok {
				return "easytier.version string"
			}
		}
		doc, ok := row.args[0].(*jsonx.Object)
		if !ok {
			return "easytier.document object"
		}
		for _, key := range []string{"proxies", "proxy-groups"} {
			if raw, present := doc.Get(key); present {
				items, ok := raw.([]any)
				if !ok {
					return "easytier.document." + key + " array"
				}
				for _, item := range items {
					obj, ok := item.(*jsonx.Object)
					if !ok {
						return "easytier.document.resource object"
					}
					if name, present := obj.Get("name"); present && name != nil {
						if _, ok := name.(string); !ok {
							return "easytier.document.name string"
						}
					}
				}
			}
		}
		if raw, present := doc.Get("rules"); present && raw != nil {
			if _, ok := raw.([]any); !ok {
				return "easytier.document.rules array"
			}
		}
	default:
		return ""
	}
	if value == nil {
		return ""
	}
	settings, ok := value.(*jsonx.Object)
	if !ok {
		return "easytier.settings object"
	}
	check := func(obj *jsonx.Object, key, kind string, nullable bool) string {
		raw, present := obj.Get(key)
		if !present || raw == nil && nullable {
			return ""
		}
		valid := false
		switch kind {
		case "string":
			_, valid = raw.(string)
		case "bool":
			_, valid = raw.(bool)
		case "list":
			items, ok := raw.([]any)
			valid = ok
			for _, item := range items {
				if _, ok := item.(string); !ok {
					valid = false
				}
			}
		case "int":
			number, ok := raw.(jsonx.Number)
			valid = ok && number.IsInt()
		}
		if !valid {
			return "easytier." + key + " " + kind
		}
		return ""
	}
	for _, key := range []string{"enabled", "magic_dns"} {
		if issue := check(settings, key, "bool", false); issue != "" {
			return issue
		}
	}
	for _, key := range []string{"name", "apply_to"} {
		if issue := check(settings, key, "string", false); issue != "" {
			return issue
		}
	}
	if issue := check(settings, "routes", "list", true); issue != "" {
		return issue
	}
	if raw, present := settings.Get("keepalive"); present && raw != nil {
		keep, ok := raw.(*jsonx.Object)
		if !ok {
			return "easytier.keepalive object"
		}
		for _, test := range []struct{ key, kind string }{{"enabled", "bool"}, {"url", "string"}, {"interval", "int"}} {
			if issue := check(keep, test.key, test.kind, false); issue != "" {
				return issue
			}
		}
	}
	if raw, present := settings.Get("options"); present {
		options, ok := raw.(*jsonx.Object)
		if !ok {
			return "easytier.options object"
		}
		for _, field := range EasyTierFields {
			kind := field.Kind
			if kind == "text" || kind == "secret" || kind == "choice" {
				kind = "string"
			}
			if issue := check(options, field.Key, kind, true); issue != "" {
				return issue
			}
		}
	}
	return ""
}

func TestN89AgentVersionFailsClosedBeforeChangingDocument(t *testing.T) {
	var settings model.EasyTierSettings
	if err := json.Unmarshal([]byte(`{"enabled":true,"apply_to":"all","options":{"network-name":"test","peers":["tcp://192.0.2.1:11010"]}}`), &settings); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name           string
		version        json.RawMessage
		agent, invalid bool
	}{
		{"missing", nil, true, false}, {"null", json.RawMessage(`null`), true, false}, {"empty", json.RawMessage(`""`), true, false},
		{"valid", json.RawMessage(`"v1.19.31"`), true, false}, {"fullwidth", json.RawMessage(`"v１.19.30"`), true, true},
		{"unknown", json.RawMessage(`"unrecognized"`), true, true}, {"non-agent", json.RawMessage(`"unrecognized"`), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := yamlx.Map()
			_, err := ApplyEasyTierNode(document, &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{EasyTier: &settings}}}, test.agent, test.version)
			if test.invalid {
				var version string
				_ = json.Unmarshal(test.version, &version)
				if err == nil || err.Error() != fmt.Sprintf(msgETAgentVersionUnknown, version) || len(document.Content) != 0 {
					t.Fatalf("invalid Agent version changed document: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
