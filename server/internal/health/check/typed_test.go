package check

import (
	"context"
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
		"Save":                     "SaveContext",
		"SaveContext":              "SaveEntriesContext",
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
		"Update":                   "UpdateContext",
		"UpdateContext":            "UpdateDataContext",
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

// Archived fixture adapters call the same native owners after production callers migrated.
// ControlTargets returns DefaultControlTargets as a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls DefaultControlTargets directly.
func ControlTargets() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultControlTargets())
	return obj
}

// Judge adapts legacy jsonx path statistics to JudgePath.
// Removed in N8-11 when discovery/bridge.go calls JudgePath directly.
func Judge(direct, proxy any, options JudgeOptions) (*jsonx.Object, error) {
	var d, p *PathStats
	if direct != nil {
		var parsed PathStats
		if err := decodeLegacy(direct, &parsed); err != nil {
			return nil, err
		}
		d = &parsed
	}
	if proxy != nil {
		var parsed PathStats
		if err := decodeLegacy(proxy, &parsed); err != nil {
			return nil, err
		}
		p = &parsed
	}
	return encodeLegacyObject(JudgePath(d, p, options))
}

// Save adapts legacy jsonx probe results to ProbeStore.SaveEntries.
// Removed in N8-11 when discovery/bridge.go calls SaveEntries directly.
func (s *ProbeStore) Save(agentID string, results *jsonx.Object, now time.Time) error {
	return s.SaveContext(context.Background(), agentID, results, now)
}

// SaveContext adapts legacy probe entries while preserving the owner's lifetime.
func (s *ProbeStore) SaveContext(ctx context.Context, agentID string, results *jsonx.Object, now time.Time) error {
	if results == nil {
		return valueError(msgInvalidProbeEntry)
	}
	var entries model.Map[TimedRecord]
	if err := decodeLegacy(results, &entries); err != nil {
		return err
	}
	return s.SaveEntriesContext(ctx, agentID, entries, now)
}

// Load adapts ProbeStore.LoadEntries to a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls LoadEntries directly.
func (s *ProbeStore) Load(agentID string, now time.Time) (*jsonx.Object, error) {
	return encodeLegacyObject(s.LoadEntries(agentID, now))
}

// LatestProbes adapts LatestProbeEntries to a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls LatestProbeEntries directly.
func LatestProbes(agentIDs []string, store *ProbeStore) (*jsonx.Object, error) {
	entries, err := LatestProbeEntries(agentIDs, store)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(entries)
}

// ExitRequest returns DefaultExitRequest as a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls DefaultExitRequest directly.
func ExitRequest() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultExitRequest())
	return obj
}

// ServiceRequests returns BuildServiceRequests as a legacy []any.
// Removed in N8-11 when discovery/bridge.go calls BuildServiceRequests directly.
func ServiceRequests() []any {
	list, _ := encodeLegacySlice(BuildServiceRequests())
	return list
}

// DomainRequest returns BuildDomainRequest as a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls BuildDomainRequest directly.
func DomainRequest(requestID, url string) *jsonx.Object {
	obj, _ := encodeLegacyObject(BuildDomainRequest(requestID, url))
	return obj
}

// PublicServices returns PublicServiceList as a legacy []any.
// Removed in N8-11 when discovery/bridge.go calls PublicServiceList directly.
func PublicServices() []any {
	list, _ := encodeLegacySlice(PublicServiceList())
	return list
}

// PickTargets adapts legacy jsonx targets to ChooseTargets.
// Removed in N8-11 when discovery/bridge.go calls ChooseTargets directly.
func PickTargets(targets []any, preferredGroup any) ([]any, error) {
	var typedTargets []AvailableTarget
	if err := decodeLegacy(targets, &typedTargets); err != nil {
		return nil, err
	}
	var preferred Text
	if preferredGroup != nil {
		if err := decodeLegacy(preferredGroup, &preferred); err != nil {
			return nil, err
		}
	}
	return encodeLegacySlice(ChooseTargets(typedTargets, string(preferred)))
}

// ExitInfo adapts legacy jsonx results to SummarizeExit.
// Removed in N8-11 when discovery/bridge.go calls SummarizeExit directly.
func ExitInfo(results []any) (*jsonx.Object, error) {
	var typedResults []RegionResult
	if err := decodeLegacy(results, &typedResults); err != nil {
		return nil, err
	}
	summary, err := SummarizeExit(typedResults)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(summary)
}

// EvaluateServices adapts legacy jsonx results to EvaluateServiceList.
// Removed in N8-11 when discovery/bridge.go calls EvaluateServiceList directly.
func EvaluateServices(results []any) (*jsonx.Object, error) {
	var typedResults []RegionResult
	if err := decodeLegacy(results, &typedResults); err != nil {
		return nil, err
	}
	verdicts, err := EvaluateServiceList(typedResults)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(verdicts)
}

// ClassifyDomainResult adapts a legacy jsonx result to ClassifyDomain.
// Removed in N8-11 when discovery/bridge.go calls ClassifyDomain directly.
func ClassifyDomainResult(result any, originalURL string) (*jsonx.Object, error) {
	var typedResult *RegionResult
	if result != nil {
		var parsed RegionResult
		if err := decodeLegacy(result, &parsed); err != nil {
			return nil, err
		}
		typedResult = &parsed
	}
	classified, err := ClassifyDomain(typedResult, originalURL)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(classified)
}

// SummarizeDomain adapts legacy jsonx per-target states to SummarizeTargets.
// Removed in N8-11 when discovery/bridge.go calls SummarizeTargets directly.
func SummarizeDomain(perTarget *jsonx.Object) (*jsonx.Object, error) {
	var typedTargets model.Map[TargetState]
	if perTarget != nil {
		if err := decodeLegacy(perTarget, &typedTargets); err != nil {
			return nil, err
		}
	}
	return encodeLegacyObject(SummarizeTargets(typedTargets))
}

// Load adapts RegionStore.LoadData to a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls LoadData directly.
func (s *RegionStore) Load(agentID string) *jsonx.Object {
	obj, _ := encodeLegacyObject(s.LoadData(agentID))
	return obj
}

// Update adapts a legacy *jsonx.Object mutator to RegionStore.UpdateData.
// Removed in N8-11 when discovery/bridge.go calls UpdateData directly.
func (s *RegionStore) Update(agentID string, mutate func(*jsonx.Object) error, now time.Time) error {
	return s.UpdateContext(context.Background(), agentID, mutate, now)
}

// UpdateContext adapts a legacy region mutator while preserving the owner's lifetime.
func (s *RegionStore) UpdateContext(ctx context.Context, agentID string, mutate func(*jsonx.Object) error, now time.Time) error {
	return s.UpdateDataContext(ctx, agentID, func(data *RegionData) error {
		obj, err := encodeLegacyObject(data)
		if err != nil {
			return err
		}
		if err := mutate(obj); err != nil {
			return err
		}
		var rawCheck rawJSONObject
		if err := decodeLegacy(obj, &rawCheck); err != nil {
			return err
		}
		if !rawCheck.Fields.Has("domains") || !rawCheck.Fields.Has("websites") {
			return valueError(msgInvalidRegionStore)
		}
		var next RegionData
		if err := decodeLegacy(obj, &next); err != nil {
			return err
		}
		*data = next
		return nil
	}, now)
}
