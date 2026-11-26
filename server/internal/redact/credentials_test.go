package redact

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/testjson"
)

// TestPublicMetadataCredentials checks opt-in redaction and known registration keys.
func TestPublicMetadataCredentials(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-env-registration")
	input := map[string]any{
		"id": "device", "message": "old synthetic-registration",
		"registration_key": "synthetic-registration", "password": "synthetic-password",
		"future":       map[string]any{"keep": true, "literal": `ordinary \u2028 text`},
		"journal":      []any{map[string]any{"url": "https://operator:synthetic-userinfo@example.com/path", "api_key": "synthetic-api"}},
		"encoded":      "synthetic%2Dregistration",
		"header":       "Bearer synthetic-auth",
		"legacy_error": "old synthetic-env-registration",
	}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	publicBytes := SanitizeCredentials(input, nil)
	public := parseRedactedObject(t, publicBytes)
	if strings.Contains(string(publicBytes), "synthetic") {
		t.Fatalf("credential escaped: %s", publicBytes)
	}
	if !testjson.Equal(json.RawMessage(before), input) || !testjson.Equal(public["future"], input["future"]) {
		t.Fatal("source or unrelated public metadata changed")
	}
	if again := parseRedactedObject(t, SanitizeCredentials(public, nil)); !testjson.Equal(again, public) {
		t.Fatal("public redaction is not idempotent")
	}
	// Credential-key policy is opt-in, leaving configuration/data redaction intact.
	if original := parseRedactedObject(t, encodedPublic(SanitizeWithSecrets(input, nil, false))); original["password"] != "synthetic-password" {
		t.Fatal("global token redactor changed credential policy")
	}
}

func parseRedactedObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatalf("redacted root null")
	}
	return out
}

type testLegacyNumber string

func (n testLegacyNumber) Float64() float64 {
	v, _ := strconv.ParseFloat(string(n), 64)
	return v
}

func (n testLegacyNumber) MarshalJSON() ([]byte, error) { return []byte(n), nil }

func TestLegacyNumericRedactionPreservesNativeJSONLiterals(t *testing.T) {
	for _, literal := range []string{"1e400", "-1e400", "1e-400", strings.Repeat("9", 4301), "NaN", "Infinity", "-Infinity"} {
		input := map[string]any{"counter": testLegacyNumber(literal), "ordinary": "kept"}
		data := SanitizeCredentials(input, nil)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("%s: %v", literal, err)
		}
		want := literal
		switch literal {
		case "NaN", "Infinity", "-Infinity":
			want = "null"
		}
		if string(fields["counter"]) != want || string(fields["ordinary"]) != `"kept"` {
			t.Fatalf("numeric redaction changed a valid literal: %s", data)
		}
	}
}
