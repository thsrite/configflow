package rulefetch

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"configflow/internal/yamlx"
)

func TestCFG03PhysicalMultilinePreservesOriginal(t *testing.T) {
	data, err := os.ReadFile("testdata/cfg03.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Output string
		Warnings             []any
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 16 {
		t.Fatal("missing physical-source controls")
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			out, warnings := NormalizeYAML(tt.Source)
			raw, err := json.Marshal(warnings)
			if err != nil {
				t.Fatal(err)
			}
			var actual []any
			if err := json.Unmarshal(raw, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tt.Warnings) {
				t.Fatalf("delivery warnings differ from Python: got %s want %v", raw, tt.Warnings)
			}
			if len(tt.Warnings) > 0 {
				if out != tt.Source || out != tt.Output {
					t.Fatal("physical multiline source rewritten")
				}
				return
			}
			want, err := yamlx.Parse([]byte(tt.Output))
			if err != nil {
				t.Fatal(err)
			}
			got, err := yamlx.Parse([]byte(out))
			if err != nil || !yamlx.Equal(want, got) {
				t.Fatalf("valid flow changed: %v", err)
			}
		})
	}
}
