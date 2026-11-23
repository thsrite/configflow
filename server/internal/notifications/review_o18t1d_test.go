package notifications

import "testing"

// TestReviewO18T1dStoredTextWithoutHTMLEscapes: text members kept as JSON
// (profile_id/profile_name/checked_at/evidence) are written as Python's
// json.dump writes them, without \u003c/\u003e/\u0026.
func TestReviewO18T1dStoredTextWithoutHTMLEscapes(t *testing.T) {
	name := "A&B <c>"
	if got := string(jsonString(name)); got != `"A&B <c>"` {
		t.Fatalf("jsonString = %s", got)
	}
	if got := string(nullableString(&name)); got != `"A&B <c>"` {
		t.Fatalf("nullableString = %s", got)
	}
	if got := string(nullableString(nil)); got != "null" {
		t.Fatalf("nullableString(nil) = %s", got)
	}
}
