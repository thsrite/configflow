package rules

// The archived converter corpus also exercises the typed rulesetfmt owner.
// Rows where Python raises are skipped here because this historical projection
// has no error return; the owner has separate malformed-input coverage.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/rulesetfmt"
)

func TestRulesetFormatDoesNotDriftFromConvertCorpus(t *testing.T) {
	file, err := os.Open("../convert/testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	checked, skipped := 0, 0
	for scanner.Scan() {
		if !bytes.Contains(scanner.Bytes(), []byte(`"ruleset_format"`)) {
			continue
		}
		var row struct {
			Fn   string            `json:"fn"`
			Err  json.RawMessage   `json:"err"`
			Out  json.RawMessage   `json:"out"`
			Args []json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Fn != "ruleset_format" {
			continue
		}
		if len(row.Err) > 0 && string(row.Err) != "null" {
			skipped++
			continue
		}
		var want string
		if err := json.Unmarshal(row.Out, &want); err != nil {
			t.Fatal(err)
		}
		var rule model.RuleConfig
		got := "text"
		if jsondecode.Unmarshal(row.Args[0], &rule) == nil {
			var cfg struct {
				RuleLibrary []json.RawMessage `json:"rule_library"`
			}
			_ = json.Unmarshal(row.Args[1], &cfg)
			entries := make([]model.RuleLibraryItem, 0, len(cfg.RuleLibrary))
			ok := true
			for _, raw := range cfg.RuleLibrary {
				var item model.RuleLibraryItem
				if jsondecode.Unmarshal(raw, &item) != nil {
					ok = false
					break
				}
				entries = append(entries, item)
			}
			var sourceURL string
			_ = json.Unmarshal(row.Args[2], &sourceURL)
			if ok {
				if result, _ := rulesetfmt.RuleFormat(rule, entries, sourceURL); result != "" {
					got = result
				}
			}
		}
		if got != want {
			t.Errorf("ruleset_format %s: got %s want %s", compact(t, row.Args), got, want)
		}
		checked++
	}
	if checked < 200 {
		t.Fatalf("only %d rows", checked)
	}
	t.Logf("checked %d, skipped %d rows where Python raises", checked, skipped)
}
