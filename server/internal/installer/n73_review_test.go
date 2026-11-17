package installer

import (
	"testing"

	"configflow/internal/jsonx"
)

func TestN73ComposeHashOnlyReviewCases(t *testing.T) {
	checked := 0
	for _, row := range readComposeLines(t, "testdata/docker_ha_compose_review.jsonl.gz") {
		name := row.GetString("test")
		if name != "review-a10/guide/1315" && name != "review-a10/guide/2501" && name != "review-a10/guide/2590" {
			continue
		}
		args := row.GetList("args")
		var enrollment *Enrollment
		if value, ok := args[1].(*jsonx.Object); ok && value != nil {
			enrollment = &Enrollment{URL: value.GetString("url"), Token: value.GetString("token"), ExpiresAt: value.Value("expires_at")}
		}
		result, err := GenerateGuide(args[0], enrollment)
		if err != nil {
			t.Fatal(err)
		}
		expected := row.Value("out")
		if value, present, oracleErr := writerExpected("generate_guide", row.Value("args")); oracleErr != nil {
			t.Fatal(oracleErr)
		} else if present {
			expected = value
		}
		if !nativeOutcomeEqual(expected, result, "") {
			t.Errorf("%s: generated guide/bundle changed meaning", name)
		}
		checked++
	}
	if checked != 3 {
		t.Fatalf("regression rows: got %d, want 3", checked)
	}
}
