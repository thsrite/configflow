package convert_test

import (
	"encoding/json"
	"os"
	"testing"

	"configflow/internal/convert"
)

func TestN88ReviewByteGateCoverage(t *testing.T) {
	data, err := os.ReadFile("testdata/n88_generation_bytes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture generationByteFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	rows := map[string][]*convert.JObj{}
	for name := range fixture.Sources {
		rows[name] = loadJSONLines(t, "testdata/"+name)
	}
	if err := validateGenerationByteCoverage(fixture, rows); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"ten Mihomo only", "duplicate row"} {
		t.Run(kind, func(t *testing.T) {
			shortened := fixture
			shortened.Cases = append([]generationByteCase{}, fixture.Cases...)
			if kind == "ten Mihomo only" {
				shortened.Cases = shortened.Cases[:10]
			} else {
				shortened.Cases[1] = shortened.Cases[0]
			}
			if err := validateGenerationByteCoverage(shortened, rows); err == nil {
				t.Fatal("incomplete byte gate accepted")
			}
		})
	}
}
