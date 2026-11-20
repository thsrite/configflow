package mosdns

// Regression tests of the adversarial review of C21 (PROGRESS-config.md,
// section C21 "Review C21"). Python references: testdata/review_c21_ref.py →
// testdata/review_c21_ref.json.

import (
	"strings"
	"testing"

	"configflow/internal/testgolden"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

// Old review values are compared structurally; unsafe graphs now use the
// explicitly approved refusal path, including graphs in replaced sections.
func TestReviewUnholdableYAMLOutsideTheOutputMatchesPython(t *testing.T) {
	t.Parallel()
	data, err := testgolden.ReadFile(t, "testdata/review_c21_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := mosParse(data)
	if err != nil {
		t.Fatal(err)
	}
	oracle := newGoldenOracle(t, "server/internal/mosdns/testdata/n73_reference.py")
	for key, want := range value.(*mosObj).All() {
		kind, source, _ := strings.Cut(key, ":")
		row := newMosObj()
		switch kind {
		case "generate", "generate-nolog":
			settings := mosKV("custom_config", source)
			if kind == "generate-nolog" {
				settings.Set("log_enabled", false)
				settings.Set("api_enabled", false)
			}
			row.Set("fn", "generate")
			row.Set("args", []any{mosKV("mosdns", settings), ""})
			output := want.([]any)
			if output[0] == "ok" {
				row.Set("out", output[1])
			} else {
				row.Set("err", output)
			}
		case "convert":
			part, text, _ := strings.Cut(source, ":")
			args := []any{text}
			if part != "None" {
				args = append(args, part)
			}
			row.Set("fn", "convert")
			row.Set("args", args)
			row.Set("out", want)
		case "upstreams":
			node, err := yamlbridge.Encode(want)
			if err != nil {
				t.Fatal(err)
			}
			text, err := yamlx.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			row.Set("fn", "upstreams")
			row.Set("args", []any{source})
			row.Set("out", mosKV("dump", string(text)))
		}
		if msg, _, ok := compareRow(t, oracle, row); !ok || msg != "" {
			t.Errorf("%q: %s", key, msg)
		}
	}
}
