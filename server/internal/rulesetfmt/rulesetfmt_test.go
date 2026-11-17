package rulesetfmt

import "testing"

func TestYAMLPayloadHintAllowsLeadingCommentsAndDocumentMarker(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   bool
	}{
		{"# header\n{payload: [example.com]}\n", true},
		{"\ufeff\r\n# header\r\n---\r\n{payload: [example.com]}\r\n", true},
		{"# header\npayload: [example.com]\n", true},
		{"# payload: [not a document]\nDOMAIN,example.com\n", false},
		{"DOMAIN,example.com\n{other text}\n", false},
		{"DOMAIN-REGEX,foo: bar\nDOMAIN-REGEX,foo: baz\n", false},
	} {
		if got := HasYAMLPayload(tc.source); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.source, got, tc.want)
		}
	}
}
