package pushdiff

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"
)

func TestReviewN73DPythonRuleListings(t *testing.T) {
	file, err := os.Open("testdata/review_n73d.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stream, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var rows []struct {
		Name, Source string
		Entries      []string
		Expected     json.RawMessage
	}
	if err := json.NewDecoder(stream).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 14 {
		t.Fatal("missing lexical oracle cases")
	}
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			entries, err := RuleEntries([]byte(row.Source))
			if err != nil {
				t.Fatalf("lexical listing parsed YAML: %v", err)
			}
			compareJSON(t, entries, row.Entries)
			summary, details := ruleChange(nil, []byte(row.Source), &detailBudget{left: MaxDetailBytes}, nil)
			var want any
			if err := json.Unmarshal(row.Expected, &want); err != nil {
				t.Fatal(err)
			}
			compareJSON(t, []any{summary, details}, want)
		})
	}
}
