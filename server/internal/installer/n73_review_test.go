package installer

import (
	"testing"
)

func TestN73ComposeHashOnlyReviewCases(t *testing.T) {
	checked := 0
	for _, row := range readComposeLines(t, "testdata/docker_ha_compose_review.jsonl.gz") {
		name := mapStr(row, "test")
		if name != "review-a10/guide/1315" && name != "review-a10/guide/2501" && name != "review-a10/guide/2590" {
			continue
		}
		args := mapList(row, "args")
		var enrollment *Enrollment
		if value, ok := args[1].(map[string]any); ok && value != nil {
			enrollment = &Enrollment{URL: mapStr(value, "url"), Token: mapStr(value, "token"), ExpiresAt: value["expires_at"]}
		}
		result, err := GenerateGuide(args[0], enrollment)
		if err != nil {
			t.Fatal(err)
		}
		expected := row["out"]
		if value, present, oracleErr := writerExpected("generate_guide", row["args"]); oracleErr != nil {
			t.Fatal(oracleErr)
		} else if present {
			expected = value
		}
		if !nativeOutcomeEqual(expected, l1GuideBaseline(t, result), "") {
			t.Errorf("%s: generated guide/bundle changed meaning", name)
		}
		checked++
	}
	if checked != 3 {
		t.Fatalf("regression rows: got %d, want 3", checked)
	}
}
