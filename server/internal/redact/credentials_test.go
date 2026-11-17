package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestPublicMetadataCredentials checks opt-in redaction and known registration keys.
func TestPublicMetadataCredentials(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-env-registration")
	input := jsonx.Obj("id", "device", "message", "old synthetic-registration",
		"registration_key", "synthetic-registration", "password", "synthetic-password",
		"future", jsonx.Obj("keep", true, "literal", `ordinary \u2028 text`),
		"journal", []any{jsonx.Obj("url", "https://operator:synthetic-userinfo@example.com/path", "api_key", "synthetic-api")},
		"encoded", "synthetic%2Dregistration")
	input.Set("header", "Bearer synthetic-auth")
	input.Set("legacy_error", "old synthetic-env-registration")
	before := input.DeepCopy()
	public := legacyRedacted(t, SanitizeCredentials(input, nil))
	text, err := jsonx.Compact(public)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "synthetic") {
		t.Fatalf("credential escaped: %s", text)
	}
	if !jsonx.Equal(before, input) || !jsonx.Equal(public.GetObject("future"), input.GetObject("future")) {
		t.Fatal("source or unrelated public metadata changed")
	}
	if again := legacyRedacted(t, SanitizeCredentials(public, nil)); !jsonx.Equal(again, public) {
		t.Fatal("public redaction is not idempotent")
	}
	// Credential-key policy is opt-in, leaving configuration/data redaction intact.
	if original := legacyRedacted(t, encodedPublic(SanitizeWithSecrets(input, nil, false))); original.GetString("password") != "synthetic-password" {
		t.Fatal("global token redactor changed credential policy")
	}
}

func legacyRedacted(t *testing.T, data []byte) *jsonx.Object {
	t.Helper()
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := value.(*jsonx.Object)
	if !ok {
		t.Fatalf("redacted root %T", value)
	}
	return out
}

func TestLegacyNumericRedactionPreservesNativeJSONLiterals(t *testing.T) {
	for _, literal := range []string{"1e400", "-1e400", "1e-400", strings.Repeat("9", 4301), "NaN", "Infinity", "-Infinity"} {
		input := jsonx.Obj("counter", jsonx.Number(literal), "ordinary", "kept")
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
