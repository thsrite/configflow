package subs

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/testjson"
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
			wantRaw := row.OutRaw
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/parser/%d", i)); changed {
				if expected.HasError {
					t.Fatalf("unexpected native oracle rejection %d", i)
				}
				wantRaw = expected.Value
			}
			if expected, ok := native[fmt.Sprintf("parser/%d", i)]; ok {
				wantRaw = expected.Output
			}
			gotRaw := parsedListJSON(t, ParseList(row.In.(string)))
			if !testjson.Equal(wantRaw, gotRaw) && o18T1cSameParsed(wantRaw, gotRaw) {
				counts["O18 T1c 接受: empty server/port omitted"]++
				continue
			}
			if !testjson.Equal(wantRaw, gotRaw) {
				t.Fatalf("row %d parse_uri_list(%q)\n got  %s\n want %s", i, row.In, gotRaw, wantRaw)
			}
		case "b64decode":
			var out map[string]json.RawMessage
			if err := json.Unmarshal(row.OutRaw, &out); err != nil {
				t.Fatal(err)
			}
			b, err := decodeSubscriptionBase64(row.In.(string))
			if errVal, hasErr := out["error"]; hasErr {
				if err == nil {
					t.Fatalf("row %d decodeSubscriptionBase64(%q): want error %s, got %x", i, row.In, errVal, b)
				}
			} else {
				var wantHex string
				_ = json.Unmarshal(out["hex"], &wantHex)
				if err != nil || hex.EncodeToString(b) != wantHex {
					t.Fatalf("row %d decodeSubscriptionBase64(%q): got %x (%v), want %s", i, row.In, b, err, wantHex)
				}
			}
		case "int":
			n, err := parseURIPort(row.In.(string))
			expected, ok := native[fmt.Sprintf("parser/%d", i)]
			if !ok {
				t.Fatalf("missing native URI port helper %d", i)
			}
			if expected.Reject {
				if err == nil {
					t.Fatalf("native URI port %d accepted invalid text", i)
				}
			} else if err != nil || !testjson.Equal(expected.Output, int64(n)) {
				t.Fatalf("native URI port %d got %d %v want %s", i, n, err, expected.Output)
			}
		}
	}
	t.Logf("%v", counts)
}
