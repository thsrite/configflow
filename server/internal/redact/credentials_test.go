package redact

import (
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
	public := SanitizeCredentials(input, nil)
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
	if again := SanitizeCredentials(public, nil); !jsonx.Equal(again, public) {
		t.Fatal("public redaction is not idempotent")
	}
	// Credential-key policy is opt-in, leaving configuration/data redaction intact.
	if original := SanitizeWithSecrets(input, nil, false).(*jsonx.Object); original.GetString("password") != "synthetic-password" {
		t.Fatal("global token redactor changed credential policy")
	}
}
