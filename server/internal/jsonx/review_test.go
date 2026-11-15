package jsonx

import "testing"

// TestReviewCorpus runs testdata/review.jsonl, the edge cases added by the
// C01 adversarial review (testdata/review_gen.py, CPython 3.11 + Flask 3.0):
// correctly rounded floats with more than 800 significant digits (exact
// ties), huge and tiny exponents, sort order across the BMP/astral boundary,
// U+2028/2029 and other format characters, full-width and other non-ASCII
// digits, nested duplicate keys, deep and wide documents, and error
// positions after CRLF, astral characters and deep nesting.
func TestReviewCorpus(t *testing.T) {
	values, errors := 0, 0
	for _, item := range loadCorpusFile(t, "testdata/review.jsonl") {
		switch item.Kind {
		case "value":
			values++
			checkValueCase(t, item)
		case "error":
			errors++
			checkErrorCase(t, item)
		}
	}
	if values < 35 || errors < 30 {
		t.Fatalf("review corpus has %d values and %d errors", values, errors)
	}
}
