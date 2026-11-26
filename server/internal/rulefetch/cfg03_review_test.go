package rulefetch

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"configflow/internal/yamlx"
)

// TestCFG03ReviewPhysicalLines replays testdata/cfg03_review.json (real
// Python normalize_mihomo_yaml): block payloads, Unicode line breaks, CR,
// tags and quote edge cases beyond the original CFG-03 vectors. A refused
// source keeps every byte with one line-0 warning of the same text; an
// accepted one must expand to the same YAML.
func TestCFG03ReviewPhysicalLines(t *testing.T) {
	data, err := os.ReadFile("testdata/cfg03_review.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Output string
		Warnings             []map[string]any
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 20 {
		t.Fatal("missing review vectors")
	}
	// PyYAML's parser error text is not a CFG-03 message; only its position
	// and the preserved bytes are compared.
	parserError := map[string]bool{"multiline-key": true}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			out, warnings := NormalizeYAML(tt.Source)
			raw, err := json.Marshal(warnings)
			if err != nil {
				t.Fatal(err)
			}
			var got []map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if len(tt.Warnings) > 0 {
				if out != tt.Source || tt.Output != tt.Source {
					t.Fatalf("refused source rewritten: %q", out)
				}
				if len(got) != 1 || got[0]["line"] != float64(0) {
					t.Fatalf("want one line-0 warning, got %s", raw)
				}
				if !parserError[tt.Name] && !reflect.DeepEqual(got, tt.Warnings) {
					t.Fatalf("warning differs from Python: got %s want %v", raw, tt.Warnings)
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("unexpected warning %s", raw)
			}
			want, err := yamlx.Parse([]byte(tt.Output))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := yamlx.Parse([]byte(out))
			if err != nil || !yamlx.Equal(want, parsed) {
				t.Fatalf("expanded YAML differs from Python: %v\n%s", err, out)
			}
		})
	}
}
