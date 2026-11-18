package provider

import (
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

// These four frozen rows have explicit per-outcome native expectations. Only
// Python's AttributeError becomes a typed validation error; every business
// rejection and successful header remains the original complete outcome.
func nativeSnapshotExpectations(t *testing.T, rows []*jsonx.Object) map[int]*jsonx.Object {
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
	result := map[int]*jsonx.Object{}
	for _, fixture := range fixtures {
		count, ok := counts[fixture.Row]
		if !ok || result[fixture.Row] != nil {
			t.Fatalf("unexpected/duplicate native snapshot row %d", fixture.Row)
		}
		input, err := jsonx.Parse(fixture.Input)
		if err != nil || !jsonx.Equal(input, rows[fixture.Row].Value("input")) || rows[fixture.Row].GetString("kind") != "snapshot" {
			t.Fatalf("native snapshot row %d input changed: %v", fixture.Row, err)
		}
		output, err := jsonx.Parse(fixture.Output)
		if err != nil {
			t.Fatal(err)
		}
		native, ok := output.([]any)
		python := rows[fixture.Row].GetList("output")
		if !ok || len(native) != 152 || len(native) != len(python) {
			t.Fatalf("native snapshot row %d outcome count changed", fixture.Row)
		}
		changed := 0
		for index, original := range python {
			if jsonx.Equal(original, native[index]) {
				continue
			}
			if !jsonx.Equal(original, jsonx.Obj("error", "AttributeError")) ||
				!jsonx.Equal(native[index], jsonx.Obj("error", "DialerReferenceError", "message", "拨号拓扑字段类型无效")) {
				t.Fatalf("native snapshot row %d outcome %d changes a business result", fixture.Row, index)
			}
			changed++
		}
		if changed != count {
			t.Fatalf("native snapshot row %d: changed %d outcomes, want %d", fixture.Row, changed, count)
		}
		result[fixture.Row] = jsonx.Obj("output", native)
	}
	return result
}
