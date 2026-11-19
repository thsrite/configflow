package agents

import (
	"configflow/internal/redact"
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

// ObservePresence adapts the legacy registry observation API.
func ObservePresence(records []*jsonx.Object) {
	observations := make([]PresenceObservation, 0, len(records))
	tokens := make([]string, 0, len(records))
	for _, record := range records {
		id, _ := record.Value("id").(string)
		status, _ := record.Value("status").(string)
		name := presenceDisplayJSON(record.Value("name"))
		heartbeat := presenceDisplayJSON(record.Value("last_heartbeat"))
		token, _ := record.Value("token").(string)
		tokens = append(tokens, token)
		observations = append(observations, PresenceObservation{id, status, name, heartbeat})
	}
	ObserveAgentPresence(observations, redact.NewSecrets(tokens))
}

// Rich legacy display metadata is JSON, never a private Python repr renderer.
func presenceDisplayJSON(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, err := jsonresp.EncodeUTF8(value)
	if err != nil {
		return redact.Redacted
	}
	return string(raw)
}
