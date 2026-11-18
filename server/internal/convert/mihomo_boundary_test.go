package convert

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The public compatibility entries may transcode values, but every business
// decision must reach the sole typed implementation. No dynamic dispatch is
// allowed to grow back into these boundaries during N8-8c/d and N8-9 removal.
func TestNativeMihomoLegacyAdaptersOnlyTranscode(t *testing.T) {
	entries := map[string]string{
		"ValidateBaseSettings": "ValidateBaseNode", "SplitHostPort": "ParseListener", "SniffPortError": "SniffPortInvalid", "TakeoverMode": "GatewayMode", "AgentContext": "AgentPorts",
		"GenerateMihomoConfig": "MihomoConfig", "GenerateMihomoDocument": "MihomoConfig", "DefaultMihomoTemplate": "MihomoTemplate",
		"ValidateIPv6Flags": "ValidateIPv6Node", "NormalizeFindProcessMode": "NormalizeProcessMode", "ParseMosdnsCustomHosts": "mosdnsHosts", "SyncMosdnsHosts": "syncMihomoHosts", "InjectRegionProbe": "addRegionProbe", "BuildProviderHealthCheck": "providerHealthNode", "SplitRulesAndRulesets": "MihomoRuleLists", "GetMihomoProviderDownloads": "MihomoProviderDownloads", "GetMihomoRulesetDownloads": "MihomoRulesetDownloads",
	}
	file, err := parser.ParseFile(token.NewFileSet(), "legacy.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		target, ok := entries[fn.Name.Name]
		if !ok {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if id.Name == target {
					found = true
				}
				switch id.Name {
				case "dictGet", "iterate", "raise", "catch", "must", "mustValue", "pyError", "Truthy":
					t.Errorf("%s calls dynamic business %s", fn.Name.Name, id.Name)
				}
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == target {
					found = true
				}
			}
			return true
		})
		if !found {
			t.Errorf("%s does not reach typed entry %s", fn.Name.Name, target)
		}
		delete(entries, fn.Name.Name)
	}
	if len(entries) != 0 {
		t.Fatalf("missing adapters %v", entries)
	}
}
