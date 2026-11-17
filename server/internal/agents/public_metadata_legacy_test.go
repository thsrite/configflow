package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
)

func TestPublicMetadataBridgeKeepsNativeNumberLiterals(t *testing.T) {
	for _, literal := range []string{"1e400", "-1e400", "1e-400", strings.Repeat("9", 4301), "NaN", "Infinity", "-Infinity"} {
		record := jsonx.Obj("counter", jsonx.Number(literal), "ordinary", "kept")
		public := SanitizePublicMetadata(record, nil)
		encoded, err := jsonresp.Encode(public)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		want := literal
		switch literal {
		case "NaN", "Infinity", "-Infinity":
			want = "null"
		}
		if string(fields["counter"]) != want || string(fields["ordinary"]) != `"kept"` {
			t.Fatalf("legacy projection lost native number: %s", encoded)
		}
	}
}
