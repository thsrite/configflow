package agents

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/redact"
)

func TestPublicMetadataRedactionKeepsNativeNumberLiterals(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"1e400", json.Number("1e400"), "1e400"},
		{"-1e400", json.Number("-1e400"), "-1e400"},
		{"1e-400", json.Number("1e-400"), "1e-400"},
		{"4301-digits", json.Number(strings.Repeat("9", 4301)), strings.Repeat("9", 4301)},
		{"NaN", math.NaN(), "null"},
		{"Infinity", math.Inf(1), "null"},
		{"-Infinity", math.Inf(-1), "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := map[string]any{"counter": tt.value, "ordinary": "kept"}
			public := redact.SanitizeCredentials(record, nil)
			encoded, err := jsonresp.Encode(public)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatalf("unmarshal %s: %v", encoded, err)
			}
			if string(fields["counter"]) != tt.want || string(fields["ordinary"]) != `"kept"` {
				t.Fatalf("native projection lost number literal: %s", encoded)
			}
		})
	}
}
