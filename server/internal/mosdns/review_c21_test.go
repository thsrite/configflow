package mosdns

// Regression tests of the adversarial review of C21 (PROGRESS-config.md,
// section C21 "Review C21"). Python references: testdata/review_c21_ref.py →
// testdata/review_c21_ref.json.

import (
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// YAML values jsonx cannot hold (!!set, !!omap/!!pairs, recursive aliases)
// that Python drops (log/api/plugins/servers/data_providers, a non-dict
// root) or skips (non-string payload items, non-dict/str upstream items) no
// longer make Go refuse or read the text differently, and mappings with keys
// that are not strings (also at the root of custom_config) are written like
// PyYAML: the output is Python's.
func TestReviewUnholdableYAMLOutsideTheOutputMatchesPython(t *testing.T) {
	data, err := os.ReadFile("testdata/review_c21_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range ref.(*jsonx.Object).All() {
		kind, rest, _ := strings.Cut(key, ":")
		var got any
		switch kind {
		case "generate":
			out, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", rest)), "")
			if err != nil {
				got = []any{errClass(err), err.Error()}
			} else {
				got = []any{"ok", out}
			}
		case "generate-nolog":
			out, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", rest,
				"log_enabled", false, "api_enabled", false)), "")
			if err != nil {
				got = []any{errClass(err), err.Error()}
			} else {
				got = []any{"ok", out}
			}
		case "convert":
			partText, text, _ := strings.Cut(rest, ":")
			var out string
			if partText == "None" {
				out, err = ConvertRuleContentForMosDNS(text)
			} else {
				out, err = ConvertRuleContentForMosDNS(text, partText)
			}
			if err != nil {
				t.Fatalf("%q: %v", key, err)
			}
			got = out
		case "upstreams":
			out, err := ParseDNSUpstreams(rest)
			if err != nil {
				t.Fatalf("%q: %v", key, err)
			}
			got = out
		}
		if !jsonx.Equal(want, got) {
			t.Errorf("%q:\nwant %s\n got %s", key, compact(want), compact(got))
		}
	}
}
