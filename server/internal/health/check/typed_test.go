package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func TestCheckCoreHasNoLegacyOrUntypedDependency(t *testing.T) {
	coreFiles := []string{"messages.go", "values.go", "healthcheck.go", "probe.go", "region.go"}
	forbiddenNames := map[string]bool{
		"any":                      true,
		"raise":                    true,
		"catch":                    true,
		"PyError":                  true,
		"KeyError":                 true,
		"decodeLegacy":             true,
		"encodeLegacyValue":        true,
		"encodeLegacyObject":       true,
		"encodeLegacySlice":        true,
		"replaceObjectInPlace":     true,
		"ProviderDefaults":         true,
		"GroupDefaults":            true,
		"InitializeGroupHealth":    true,
		"InitializeProviderHealth": true,
		"ValidateHealth":           true,
		"ProviderHealthCheck":      true,
		"ControlTargets":           true,
		"Judge":                    true,
		"LatestProbes":             true,
		"ExitRequest":              true,
		"ServiceRequests":          true,
		"DomainRequest":            true,
		"PublicServices":           true,
		"PickTargets":              true,
		"ExitInfo":                 true,
		"EvaluateServices":         true,
		"ClassifyDomainResult":     true,
		"SummarizeDomain":          true,
	}
	for _, name := range coreFiles {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "jsonx") || strings.Contains(imp.Path.Value, "pystr") {
				t.Errorf("%s imports legacy %s", name, imp.Path.Value)
			}
		}
		selected := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				selected[sel.Sel] = true
			}
			if _, ok := n.(*ast.InterfaceType); ok {
				t.Errorf("%s contains an interface{} type literal", name)
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && !selected[id] && forbiddenNames[id.Name] {
				t.Errorf("%s references forbidden identifier %s", name, id.Name)
			}
			return true
		})
	}
}

func TestCheckLegacyWrappersOnlyCallTypedEntries(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "legacy.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	expectedTargets := map[string]string{
		"ProviderDefaults":         "DefaultProviderHealth",
		"GroupDefaults":            "DefaultGroupHealth",
		"InitializeGroupHealth":    "InitGroupHealth",
		"InitializeProviderHealth": "InitProviderHealth",
		"ValidateHealth":           "ValidateHealthJSON",
		"ProviderHealthCheck":      "ResolveProviderHealth",
		"ControlTargets":           "DefaultControlTargets",
		"Judge":                    "JudgePath",
		"Save":                     "SaveEntries",
		"Load":                     "", // method name overloaded across ProbeStore and RegionStore
		"LatestProbes":             "LatestProbeEntries",
		"ExitRequest":              "DefaultExitRequest",
		"ServiceRequests":          "BuildServiceRequests",
		"DomainRequest":            "BuildDomainRequest",
		"PublicServices":           "PublicServiceList",
		"PickTargets":              "ChooseTargets",
		"ExitInfo":                 "SummarizeExit",
		"EvaluateServices":         "EvaluateServiceList",
		"ClassifyDomainResult":     "ClassifyDomain",
		"SummarizeDomain":          "SummarizeTargets",
		"Update":                   "UpdateData",
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		target, tracked := expectedTargets[fn.Name.Name]
		if !tracked {
			continue
		}
		called := target == ""
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.ForStmt); ok {
				t.Errorf("legacy %s contains a for loop", fn.Name.Name)
			}
			if _, ok := n.(*ast.RangeStmt); ok {
				t.Errorf("legacy %s contains a range loop", fn.Name.Name)
			}
			call, ok := n.(*ast.CallExpr)
			if !ok || target == "" {
				return true
			}
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				if callee.Name == target {
					called = true
				}
			case *ast.SelectorExpr:
				if callee.Sel.Name == target {
					called = true
				}
			}
			return true
		})
		if !called {
			t.Errorf("legacy %s does not call typed target %s", fn.Name.Name, target)
		}
	}
}

func TestWrongTypedOutputIdentitiesFailWithOffendingValue(t *testing.T) {
	t.Run("RegionResult.ID", func(t *testing.T) {
		tests := []struct {
			name string
			raw  string
			want string
		}{
			{"missing", `[{"status":200}]`, msgRegionResultIDMissing},
			{"null", `[{"id":null,"status":200}]`, fmt.Sprintf(msgRegionResultIDInvalid, "null")},
			{"bool", `[{"id":true,"status":200}]`, fmt.Sprintf(msgRegionResultIDInvalid, "true")},
			{"array", `[{"id":["x"],"status":200}]`, fmt.Sprintf(msgRegionResultIDInvalid, `["x"]`)},
			{"object", `[{"id":{"k":1},"status":200}]`, fmt.Sprintf(msgRegionResultIDInvalid, `{"k":1}`)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var results []RegionResult
				err := json.Unmarshal([]byte(tt.raw), &results)
				if err == nil {
					_, err = EvaluateServiceList(results)
				}
				var ve *ValueError
				if !errors.As(err, &ve) || ve.Message != tt.want {
					t.Fatalf("EvaluateServiceList(%s): want ValueError %q, got %v", tt.raw, tt.want, err)
				}

				var exitResults []RegionResult
				err = json.Unmarshal([]byte(tt.raw), &exitResults)
				if err == nil {
					_, err = SummarizeExit(exitResults)
				}
				if !errors.As(err, &ve) || ve.Message != tt.want {
					t.Fatalf("SummarizeExit(%s): want ValueError %q, got %v", tt.raw, tt.want, err)
				}
			})
		}
	})

	t.Run("AvailableTarget.Name", func(t *testing.T) {
		tests := []struct {
			name string
			raw  string
			want string
		}{
			{"missing", `[{"type":"Selector"}]`, msgTargetNameMissing},
			{"null", `[{"name":null,"type":"Selector"}]`, fmt.Sprintf(msgTargetNameInvalid, "null")},
			{"bool", `[{"name":false,"type":"Selector"}]`, fmt.Sprintf(msgTargetNameInvalid, "false")},
			{"array", `[{"name":["HK"],"type":"Selector"}]`, fmt.Sprintf(msgTargetNameInvalid, `["HK"]`)},
			{"object", `[{"name":{"a":1},"type":"Selector"}]`, fmt.Sprintf(msgTargetNameInvalid, `{"a":1}`)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var targets []AvailableTarget
				err := json.Unmarshal([]byte(tt.raw), &targets)
				var ve *ValueError
				if !errors.As(err, &ve) || ve.Message != tt.want {
					t.Fatalf("Unmarshal AvailableTarget(%s): want ValueError %q, got %v", tt.raw, tt.want, err)
				}
			})
		}
	})
}

func TestStringFieldsAcceptLosslessNumericLiterals(t *testing.T) {
	// 9007199254740993 = 2^53 + 1: rounds to 9007199254740992 if passed through float64.
	const exactBigInt = "9007199254740993"

	var result RegionResult
	if err := json.Unmarshal([]byte(`{"id":`+exactBigInt+`,"status":200,"markers":{"gl":`+exactBigInt+`}}`), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != exactBigInt {
		t.Fatalf("RegionResult.ID = %q, want %q", result.ID, exactBigInt)
	}
	gl, ok := result.Markers.Get("gl")
	if !ok || !gl.Valid || gl.Value != exactBigInt {
		t.Fatalf("marker gl = %+v, want %q", gl, exactBigInt)
	}

	var target AvailableTarget
	if err := json.Unmarshal([]byte(`{"name":`+exactBigInt+`,"type":"Selector"}`), &target); err != nil {
		t.Fatal(err)
	}
	if target.Name != exactBigInt {
		t.Fatalf("AvailableTarget.Name = %q, want %q", target.Name, exactBigInt)
	}
	picked := ChooseTargets([]AvailableTarget{target}, exactBigInt)
	if len(picked) != 1 || picked[0].Name != exactBigInt || picked[0].Kind != "group" {
		t.Fatalf("ChooseTargets numeric group = %+v", picked)
	}
}

func TestR5FallbacksAndStoreValidation(t *testing.T) {
	// PathStats R5: wrong-typed members read as zero/empty and retain raw bytes when re-encoded.
	const rawStats = `{"ok":"bad","total":2,"delays":["a"],"errors":null,"extra":1}`
	var ps PathStats
	if err := json.Unmarshal([]byte(rawStats), &ps); err != nil {
		t.Fatal(err)
	}
	if ps.OK != 0 || ps.Total != 2 || len(ps.Delays) != 0 || PathHealthy(&ps) {
		t.Fatalf("unexpected decoded PathStats: %+v", ps)
	}
	encoded, err := json.Marshal(ps)
	if err != nil || string(encoded) != rawStats {
		t.Fatalf("R5 round-trip mismatch: got %s, want %s (%v)", encoded, rawStats, err)
	}

	// ProbeStore rejects invalid checked_at on write and resets non-object hosts on read.
	dir := t.TempDir()
	psStore := NewProbeStore(dir)
	now := time.Date(2026, 3, 29, 2, 30, 0, 0, time.UTC)
	if err := psStore.Save("bad-int", jsonx.Obj("x", jsonx.Obj("checked_at", jsonx.Int(5))), now); err == nil || err.Error() != msgInvalidProbeEntry {
		t.Fatalf("want %q, got %v", msgInvalidProbeEntry, err)
	}
	if err := psStore.Save("bad-str", jsonx.Obj("x", "y"), now); err == nil || err.Error() != msgInvalidProbeEntry {
		t.Fatalf("want %q, got %v", msgInvalidProbeEntry, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "keep.json"), []byte(`{"hosts":{"a":{"checked_at":"2026-03-01T00:00:00"}},"extra":"😀"}`), 0o644)
	if err := psStore.Save("keep", jsonx.Obj("b", jsonx.Obj("checked_at", "2026-03-29T00:00:00")), now); err != nil {
		t.Fatal(err)
	}
	savedBytes, _ := os.ReadFile(psStore.Path("keep"))
	if !strings.Contains(string(savedBytes), `"extra":"😀"`) {
		t.Fatalf("lost top-level extra field: %s", savedBytes)
	}

	// RegionStore rejects invalid checked_at / non-object domains on update.
	rsStore := NewRegionStore(t.TempDir())
	err = rsStore.Update("r-bad-1", func(data *jsonx.Object) error {
		data.GetObject("domains").Set("x", jsonx.Obj("checked_at", nil))
		return nil
	}, now)
	if err == nil || err.Error() != msgInvalidRegionStore {
		t.Fatalf("want %q, got %v", msgInvalidRegionStore, err)
	}
	err = rsStore.Update("r-bad-2", func(data *jsonx.Object) error {
		data.Set("domains", []any{})
		return nil
	}, now)
	if err == nil || err.Error() != msgInvalidRegionStore {
		t.Fatalf("want %q, got %v", msgInvalidRegionStore, err)
	}
}

func TestValidateModelRecords(t *testing.T) {
	var validHC model.ProviderHealthCheck
	if err := json.Unmarshal([]byte(`{"enable":true,"url":"https://probe.example.test/check","interval":120}`), &validHC); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProviderHealth(&validHC); err != nil {
		t.Fatalf("valid provider health failed: %v", err)
	}

	var invalidHC model.ProviderHealthCheck
	if err := json.Unmarshal([]byte(`{"interval":"bad"}`), &invalidHC); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProviderHealth(&invalidHC); err == nil || err.Error() != fmt.Sprintf(msgHealthIntRange, "interval", 1, 86400) {
		t.Fatalf("expected interval range error on R5-preserved raw JSON, got %v", err)
	}

	var group model.ProxyGroup
	if err := json.Unmarshal([]byte(`{"id":"g1","name":"G","type":"url-test","tolerance":-1}`), &group); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGroupHealth(&group); err == nil || err.Error() != fmt.Sprintf(msgHealthIntRange, "tolerance", 0, 600000) {
		t.Fatalf("expected tolerance range error, got %v", err)
	}
}
