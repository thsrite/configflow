package mosdns

// Regression tests of the adversarial review of C21 (PROGRESS-config.md,
// section C21 "Review C21"). Python references: testdata/review_c21_ref.py →
// testdata/review_c21_ref.json.

import (
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// Old review values are compared structurally; unsafe graphs now use the
// explicitly approved refusal path, including graphs in replaced sections.
func TestReviewUnholdableYAMLOutsideTheOutputMatchesPython(t *testing.T) {
	data, err := os.ReadFile("testdata/review_c21_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	oracle := yamlxtest.NewOracle(t, "server/internal/mosdns/testdata/n73_reference.py")
	for key, want := range value.(*jsonx.Object).All() {
		kind, source, _ := strings.Cut(key, ":")
		row := jsonx.Obj()
		switch kind {
		case "generate", "generate-nolog":
			settings := jsonx.Obj("custom_config", source)
			if kind == "generate-nolog" {
				settings.Set("log_enabled", false)
				settings.Set("api_enabled", false)
			}
			row.Set("fn", "generate")
			row.Set("args", []any{jsonx.Obj("mosdns", settings), ""})
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
			row.Set("out", jsonx.Obj("dump", string(text)))
		}
		if msg, _, ok := compareRow(t, oracle, row); !ok || msg != "" {
			t.Errorf("%q: %s", key, msg)
		}
	}
}
