package rules

import (
	"configflow/internal/model"
	"encoding/json"
	"os"
	"testing"
)

func TestHistoricalMigrationFilenameReference(t *testing.T) {
	data, err := os.ReadFile("testdata/n89_migration_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Input    string `json:"input"`
		Filename string `json:"filename"`
	}
	if json.Unmarshal(data, &rows) != nil || len(rows) != 6 {
		t.Fatal("independent migration evidence missing")
	}
	for _, row := range rows {
		var rule model.RuleConfig
		if err := json.Unmarshal([]byte(row.Input), &rule); err != nil {
			t.Fatal(err)
		}
		name, ok := LegacyCacheFilename(rule)
		if !ok || name != row.Filename {
			t.Fatalf("historical address got %q/%v want %q", name, ok, row.Filename)
		}
		// Equivalent spelling is ownership evidence; numeric coercions are not.
		var canonical model.RuleConfig
		var fields map[string]json.RawMessage
		_ = json.Unmarshal([]byte(row.Input), &fields)
		for _, key := range []string{"behavior", "source_type", "url"} {
			var text string
			if json.Unmarshal(fields[key], &text) == nil && text != "" {
				fields[key], _ = json.Marshal(text)
			}
		}
		raw, _ := json.Marshal(fields)
		_ = json.Unmarshal(raw, &canonical)
		if !SameSourceDeclaration(rule, canonical) {
			t.Fatal("escaped string lost current ownership")
		}
	}
}
