package rules

// compat.go keeps a copy of converters/common.ruleset_format (RulesetFormat)
// because its port, internal/convert, imports this package. So the two cannot
// drift, the copy runs over convert's Python corpus
// (../convert/testdata/gen.py). Rows where Python raises (malformed library
// items, URLs urlsplit refuses, non-string URLs) are skipped: the copy has no
// error path and returns a format there.

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
