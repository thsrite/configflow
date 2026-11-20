package provider

import (
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/testjson"
)

// These four frozen rows have explicit per-outcome native expectations. Only
// Python's AttributeError becomes a typed validation error; every business
// rejection and successful header remains the original complete outcome.
func nativeSnapshotExpectations(t *testing.T, rows []map[string]any) map[int]map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/n88a-snapshot-native.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Row    int             `json:"row"`
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{366: 70, 369: 80, 375: 70, 378: 80}
	if len(fixtures) != len(counts) {
		t.Fatal("native snapshot fixture row count changed")
	}
	result := map[int]map[string]any{}
	for _, fixture := range fixtures {
		count, ok := counts[fixture.Row]
		if !ok || result[fixture.Row] != nil {
			t.Fatalf("unexpected/duplicate native snapshot row %d", fixture.Row)
		}
		input, err := decodeAny(fixture.Input)
		if err != nil || !testjson.Equal(input, rows[fixture.Row]["input"]) || mapStr(rows[fixture.Row], "kind") != "snapshot" {
			t.Fatalf("native snapshot row %d input changed: %v", fixture.Row, err)
		}
		output, err := decodeAny(fixture.Output)
		if err != nil {
			t.Fatal(err)
		}
		native, ok := output.([]any)
		python, _ := rows[fixture.Row]["output"].([]any)
		if !ok || len(native) != 152 || len(native) != len(python) {
			t.Fatalf("native snapshot row %d outcome count changed", fixture.Row)
		}
		changed := 0
		for index, original := range python {
			if testjson.Equal(original, native[index]) {
				continue
			}
			if !testjson.Equal(original, map[string]any{"error": "AttributeError"}) ||
				!testjson.Equal(native[index], map[string]any{"error": "DialerReferenceError", "message": "拨号拓扑字段类型无效"}) {
				t.Fatalf("native snapshot row %d outcome %d changes a business result", fixture.Row, index)
			}
			changed++
		}
		if changed != count {
			t.Fatalf("native snapshot row %d: changed %d outcomes, want %d", fixture.Row, changed, count)
		}
		result[fixture.Row] = map[string]any{"output": native}
	}
	return result
}
