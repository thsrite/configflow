package convert

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func nativeGeneralReplay(t *testing.T, row corpusRow, result, mutated any, err error) bool {
	t.Helper()
	if row.fn != "set_general_option" || row.args[0] != "[General]\nloglevel=x\u001c\u001d" {
		return false
	}
	wants := map[string]string{
		"ipv6":       "[General]\nloglevel=x\u001c\u001d\nipv6 = v",
		"a+b":        "[General]\nloglevel=x\u001c\u001d\na+b = v",
		"(x)":        "[General]\nloglevel=x\u001c\u001d\n(x) = v",
		"ı":          "[General]\nloglevel=x\u001c\u001d\nı = v",
		"skip-proxy": "[General]\nloglevel=x\u001c\u001d\nskip-proxy = v",
	}
	key, ok := row.args[1].(string)
	if !ok || row.args[2] != "v" {
		t.Fatal("unclassified native whitespace case")
	}
	want, ok := wants[key]
	if !ok || err != nil || result != want || mutated != nil {
		t.Fatalf("native whitespace %q: %q / %v", key, result, err)
	}
	return true
}

func TestCommonFullUnicodeUppercase(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"ß", "SS"}, {"ǰ", "J̌"}, {"ﬀ", "FF"}, {"ΐ", "Ϊ́"}, {"ᾳ", "ΑΙ"},
		{"ᾀ", "ἈΙ"}, {"ŉ", "ʼN"}, {"ﬆudp", "STUDP"}, {"ﬓ", "ՄՆ"}, {"ffiﬃ", "FFIFFI"},
	} {
		kind, value := NetworkAsProtocol("NETWORK", tt.value)
		if kind != "PROTOCOL" || value != tt.want {
			t.Fatalf("%q -> %q,%q want %q", tt.value, kind, value, tt.want)
		}
	}
}

// CommonReplayIssue classifies malformed stored members owned by the migrated
// common helpers. Only normal rows remain Python byte oracles, per N8-8a's
// approved boundary; malformed shapes have explicit typed assertions below.
// This symbol exists only in the test build, including external-package tests.
func CommonReplayIssue(config *jsonx.Object) string {
	if config == nil {
		return "config object"
	}
	for _, key := range []string{"rule_configs", "rule_library"} {
		raw, present := config.Get(key)
		if !present {
			continue
		}
		items, ok := raw.([]any)
		if !ok {
			return key + " array"
		}
		for _, raw := range items {
			item, ok := raw.(*jsonx.Object)
			if !ok || item == nil {
				return key + " object"
			}
			for _, field := range []string{"id", "name", "itemType", "rule_type", "value", "policy", "udp_fallback", "library_rule_id", "format", "url", "content", "source_type"} {
				if value, ok := item.Get(field); ok {
					if _, ok := value.(string); !ok {
						return key + "." + field + " string"
					}
				}
			}
			for _, field := range []string{"enabled", "ntp_direct"} {
				if value, ok := item.Get(field); ok {
					if _, ok := value.(bool); !ok {
						return key + "." + field + " bool"
					}
				}
			}
		}
	}
	if raw, ok := config.Get("profile_id"); ok {
		if _, ok := raw.(string); !ok {
			return "profile_id string"
		}
	}
	return ""
}

func commonRowIssue(row corpusRow) string {
	switch row.fn {
	case "udp_fallback", "ntp_direct", "emitted_rules":
		return CommonReplayIssue(obj(row.args[0]))
	case "after_match_comment":
		return CommonReplayIssue(jsonx.Obj("rule_configs", []any{row.args[0]}))
	case "ruleset_format":
		config := obj(row.args[1]).DeepCopy()
		config.Set("rule_configs", []any{row.args[0]})
		return CommonReplayIssue(config)
	}
	return ""
}

func TestCommonTypedDefaultsAndMalformedStorage(t *testing.T) {
	for _, tt := range []struct {
		raw, fallback, comment string
		ntp                    bool
		count                  int
	}{
		{`{"rule_configs":[{"itemType":"rule","rule_type":"MATCH","enabled":false,"udp_fallback":"REJECT"},{"itemType":"rule","rule_type":"MATCH","udp_fallback":"DIRECT","ntp_direct":true}]}`, "DIRECT", "", true, 3},
		{`{"rule_configs":"bad"}`, "", "", false, 0},
		{`{"rule_configs":[1]}`, "", "", false, 0},
		{`{"rule_configs":[{"itemType":"rule","rule_type":"MATCH","enabled":0,"udp_fallback":"DIRECT"}]}`, "", "", false, 1},
	} {
		config, err := rulesConfigFromLegacy(mustJSON(t, tt.raw))
		if err != nil {
			t.Fatal(err)
		}
		if FallbackPolicy(config) != tt.fallback || DirectNTP(config) != tt.ntp || len(RulesForOutput(config)) != tt.count {
			t.Fatalf("%s: unexpected typed defaults", tt.raw)
		}
	}
	var malformed model.RuleConfig
	if err := json.Unmarshal([]byte(`{"itemType":"rule","rule_type":"MATCH","value":5,"policy":null}`), &malformed); err != nil {
		t.Fatal(err)
	}
	if got, want := RuleComment(malformed), "# "+AfterMatchNote+": MATCH,5,"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	for _, raw := range []string{`null`, `1`, `false`, `[]`, `"bad"`} {
		config, err := ruleLibraryConfigFromLegacy(mustJSON(t, `{"rule_library":[`+raw+`]}`))
		if err != nil {
			t.Fatal(err)
		}
		// R5 discards the malformed list as a typed field, retaining its raw
		// bytes in the model. It is never traversed with dynamic .get calls.
		if len(config.RuleLibrary) != 0 {
			t.Fatalf("%s: bad library retained as typed entry", raw)
		}
	}
}

func TestCommonCoreHasNoLegacyDependency(t *testing.T) {
	for _, name := range []string{"common.go", "mihomo_nodes.go", "proxyyaml.go", "easytier.go", "dialers.go", "dialers_api.go", "base_settings.go", "mihomo_generate.go", "mihomo_generate_env.go", "mihomo_environment.go", "mihomo_integrations.go", "mihomo_downloads.go", "mihomo_graph.go", "mihomo_urls.go", "client_common.go", "client_values.go", "client_nodes.go", "client_groups.go", "client_rules.go", "client_generate.go", "client_delivery.go", "surge.go", "loon.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "jsonx") || strings.Contains(imp.Path.Value, "pystr") {
				t.Errorf("%s imports legacy %s", name, imp.Path.Value)
			}
		}
		forbidden := map[string]bool{"legacyDialerView": true, "clientConfigFromLegacy": true, "validateLegacyResourceArrays": true, "rulesConfigFromLegacy": true, "ruleLibraryConfigFromLegacy": true, "githubConfigFromLegacy": true, "dialerConfigFromLegacy": true, "easyTierProfileFromLegacy": true, "any": true, "dictGet": true, "raise": true, "catch": true, "must": true, "pyError": true, "decodeLegacy": true, "configFromLegacy": true, "encodeLegacy": true, "FixProxyFields": true, "ApplyGithubProxyDomain": true, "MihomoUsedPorts": true, "MihomoListenPorts": true, "ParseStructuredProxyString": true, "InspectStructuredProxyString": true, "ConvertNodeToMihomo": true, "ProxyStringConverter": true, "ConversionResults": true, "WithNodeConversions": true, "EnsureNodeConversions": true, "readDocumentYAML": true, "ValidateShapes": true, "ValidateDialers": true, "DependencyGraph": true, "RawDialer": true, "DialerTarget": true, "GroupMembers": true, "GroupDependencies": true, "CheckCycles": true, "IncomingDialers": true, "ResolveDialer": true, "OverlayDialer": true, "FilterProviderCandidates": true, "ProviderProxyName": true, "ValidateEmitted": true, "RejectSurgeDialers": true, "DependencyNodeIDs": true, "CurrentMihomoEnv": true, "SetMihomoEnv": true, "RegisterMihomoHooks": true, "applyLinuxGateway": true, "gatewayEnabled": true, "isFullTun": true, "ValidateBaseSettings": true, "SplitHostPort": true, "SniffPortError": true, "AgentContext": true, "TakeoverMode": true, "GenerateMihomoConfig": true, "GenerateMihomoDocument": true, "GetMihomoProviderDownloads": true, "GetMihomoRulesetDownloads": true, "DefaultMihomoTemplate": true, "ValidateIPv6Flags": true, "NormalizeFindProcessMode": true, "ParseMosdnsCustomHosts": true, "SyncMosdnsHosts": true, "InjectRegionProbe": true, "BuildProviderHealthCheck": true, "SplitRulesAndRulesets": true, "splitRulesAndRulesets": true, "appendURLQuery": true, "profileAPIPath": true, "loadCustomConfig": true, "legacyProxy": true, "replaceLegacyDocument": true, "adaptDocument": true, "MihomoEnvironmentFromLegacy": true, "subsError": true, "MihomoEnv": true, "MihomoHooks": true, "GenerateOptions": true, "Download": true, "AgentSource": true, "PortOwner": true}
		// Selector names belong to their imported owner (e.g. net.SplitHostPort),
		// not to an unqualified compatibility entry in this package.
		selected := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if selector, ok := n.(*ast.SelectorExpr); ok {
				selected[selector.Sel] = true
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && !selected[id] && forbidden[id.Name] {
				t.Errorf("%s references %s", name, id.Name)
			}
			return true
		})
	}
}

func TestCommonLegacyOnlyCallsTypedEntries(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "legacy.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{"NTPDirectRule": "BuiltinNTPRule", "UDPFallback": "FallbackPolicy", "NTPDirect": "DirectNTP", "EmittedRules": "emittedRules", "emittedRules": "RulesForOutput", "AfterMatchComment": "RuleComment", "RulesetFormat": "rulesetFormat", "rulesetFormat": "RuleFormat"}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		target, ok := entries[function.Name.Name]
		if !ok {
			continue
		}
		called := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if _, ok := node.(*ast.ForStmt); ok {
				t.Errorf("%s contains business iteration", function.Name.Name)
			}
			if _, ok := node.(*ast.RangeStmt); ok {
				t.Errorf("%s contains business iteration", function.Name.Name)
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := call.Fun
			if generic, ok := callee.(*ast.IndexExpr); ok {
				callee = generic.X
			}
			name, ok := callee.(*ast.Ident)
			if !ok {
				t.Errorf("%s calls a non-typed boundary", function.Name.Name)
				return true
			}
			if name.Name == target {
				called = true
			} else if name.Name != "rulesConfigFromLegacy" && name.Name != "ruleLibraryConfigFromLegacy" && name.Name != "decodeLegacy" && name.Name != "encodeLegacy" {
				t.Errorf("%s calls %s", function.Name.Name, name.Name)
			}
			return true
		})
		if !called {
			t.Errorf("%s does not call typed entry %s", function.Name.Name, target)
		}
	}
}
