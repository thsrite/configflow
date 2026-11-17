package subs

import (
	"encoding/hex"
	"fmt"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
)

func TestParserCorpus(t *testing.T) {
	rows := loadCorpus(t, "parser.jsonl.gz")
	oracle := nativeReaderCases(t)
	counts := map[string]int{}
	for i, row := range rows {
		counts[row.Fn]++
		switch row.Fn {
		case "parse_uri_list":
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/parser/%d", i)); changed {
				if expected.Has("error") {
					t.Fatalf("unexpected native oracle rejection %d", i)
				}
				row.Out = expected.Value("value")
			}
			nodes := ParseURIList(row.In.(string))
			got := make([]any, len(nodes))
			for j, n := range nodes {
				got[j] = n
			}
			if g, w := compact(t, got), compact(t, row.Out); !yamlxtest.EqualJSON(got, row.Out) {
				t.Fatalf("row %d parse_uri_list(%q)\n got  %s\n want %s", i, row.In, g, w)
			}
		case "b64decode":
			out := row.Out.(*jsonx.Object)
			b, err := b64decode(row.In.(string))
			if out.Has("error") {
				if err == nil {
					t.Fatalf("row %d b64decode(%q): want error %v, got %x", i, row.In, out.Value("error"), b)
				}
			} else if err != nil || hex.EncodeToString(b) != out.GetString("hex") {
				t.Fatalf("row %d b64decode(%q): got %x (%v), want %s", i, row.In, b, err, out.GetString("hex"))
			}
		case "int":
			n, err := pyIntStr(row.In.(string))
			if msg, isErr := isErrorOut(row.Out); isErr {
				if err == nil {
					t.Fatalf("row %d int(%q): want %s, got %s", i, row.In, msg, n)
				}
			} else if err != nil || compact(t, n) != compact(t, row.Out) {
				t.Fatalf("row %d int(%q): got %s (%v), want %s", i, row.In, n, err, compact(t, row.Out))
			}
		}
	}
	t.Logf("%v", counts)
}
