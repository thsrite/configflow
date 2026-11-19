package agents

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

func n810UnitCanonical(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func nativeQueryUnit(t *testing.T, row *jsonx.Object) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_query_unit_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Original map[string]any `json:"original"`
		SHA      string         `json:"sha256"`
		Native   string         `json:"native"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, rule := range rows {
		if fmt.Sprintf("%x", sha256.Sum256(n810UnitCanonical(t, rule.Original))) != rule.SHA {
			t.Fatal("original query unit row changed")
		}
		if bytes.Equal(n810UnitCanonical(t, row), n810UnitCanonical(t, rule.Original)) {
			return rule.Native
		}
	}
	return row.GetString("lines")
}
func TestN810RegistryUnitPolicyDoesNotMatchChangedInput(t *testing.T) {
	old := jsonx.Obj("value", "١٢", "lines", "12")
	if nativeQueryUnit(t, old) != "100" {
		t.Fatal("native row was ignored")
	}
	old.Set("lines", "13")
	if nativeQueryUnit(t, old) != "13" {
		t.Fatal("changed original row matched policy")
	}
}
