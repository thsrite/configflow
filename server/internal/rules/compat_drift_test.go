package rules

// The archived converter corpus also exercises the typed rulesetfmt owner
// through the test-only fixture adapter. Rows where Python raises are skipped
// here because this historical projection has no error return; the owner has
// separate malformed-input coverage.

import (
	"bufio"
	"compress/gzip"
	"os"
	"testing"

	"configflow/internal/jsonx"
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
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		if row.GetString("fn") != "ruleset_format" {
			continue
		}
		if row.Has("err") {
			skipped++
			continue
		}
		args := row.GetList("args")
		rule, _ := args[0].(*jsonx.Object)
		library := args[1].(*jsonx.Object).GetList("rule_library")
		sourceURL, _ := args[2].(string)
		if got, want := RulesetFormat(rule, library, sourceURL), row.GetString("out"); got != want {
			t.Errorf("ruleset_format %s: got %s want %s", compact(t, args), got, want)
		}
		checked++
	}
	if checked < 200 {
		t.Fatalf("only %d rows", checked)
	}
	t.Logf("checked %d, skipped %d rows where Python raises", checked, skipped)
}
