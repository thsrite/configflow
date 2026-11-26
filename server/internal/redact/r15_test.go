package redact

import (
	"encoding/json"
	"testing"
)

func TestR15PasswordPresenceIsPublicButValuesArePrivate(t *testing.T) {
	for _, flag := range []bool{false, true} {
		raw := SanitizeCredentials(map[string]any{"has_password": flag, "password": "synthetic-secret", "message": "reference synthetic-secret"}, nil)
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["has_password"] != flag || fields["password"] != Redacted || fields["message"] != Redacted {
			t.Fatalf("bad public metadata: %s", raw)
		}
	}
	raw := SanitizeCredentials(json.RawMessage(`{"has_password":"synthetic-secret"}`), nil)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if fields["has_password"] != Redacted {
		t.Fatal("string credential escaped")
	}
}
