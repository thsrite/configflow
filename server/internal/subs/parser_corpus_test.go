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
	native := nativeParserExpectations(t)
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
			if expected := native.GetObject(fmt.Sprintf("parser/%d", i)); expected != nil {
				row.Out = expected.Value("output")
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
			b, err := decodeSubscriptionBase64(row.In.(string))
			if out.Has("error") {
				if err == nil {
					t.Fatalf("row %d decodeSubscriptionBase64(%q): want error %v, got %x", i, row.In, out.Value("error"), b)
				}
			} else if err != nil || hex.EncodeToString(b) != out.GetString("hex") {
				t.Fatalf("row %d decodeSubscriptionBase64(%q): got %x (%v), want %s", i, row.In, b, err, out.GetString("hex"))
			}
		case "int":
			n, err := parseURIPort(row.In.(string))
			expected := native.GetObject(fmt.Sprintf("parser/%d", i))
			if expected == nil {
				t.Fatalf("missing native URI port helper %d", i)
			}
			if expected.GetBool("reject") {
				if err == nil {
					t.Fatalf("native URI port %d accepted invalid text", i)
				}
			} else if err != nil || !jsonx.Equal(jsonx.Int(int64(n)), expected.Value("output")) {
				t.Fatalf("native URI port %d got %d %v want %v", i, n, err, expected.Value("output"))
			}
		}
	}
	t.Logf("%v", counts)
}
