package pushdiff

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/redact"
)

func TestReviewStoredCredentialMetadataFallback(t *testing.T) {
	const stored = "synthetic-stored-key"
	find := func(text string) []redact.Range {
		i := strings.Index(text, stored)
		if i < 0 {
			return nil
		}
		start := utf8.RuneCountInString(text[:i])
		return []redact.Range{{Start: start, End: start + len(stored)}}
	}
	result := Config([]byte("mode: rule\n"), []byte("hosts:\n  中-"+stored+".example.com: 192.0.2.1\npassword: synthetic-user-password\n"), "", "")
	public := WithoutStored(map[string]any{"key-" + stored: result, "files": []any{map[string]any{"path": "rules/" + stored + ".list", "name": "visible-" + stored + "-tail"}}}, find)
	raw, err := json.Marshal(public)
	if err != nil || strings.Contains(string(raw), stored) {
		t.Errorf("stored credential survived in metadata: %s (%v)", raw, err)
	}
	if !strings.Contains(string(raw), "visible-") || !strings.Contains(string(raw), "-tail") || !strings.Contains(string(raw), "synthetic-user-password") {
		t.Error("partial context or revealable user credentials lost")
	}
	hidden, err := json.Marshal(WithoutSecrets(public))
	if err != nil || strings.Contains(string(hidden), "synthetic-user-password") {
		t.Error("user password escaped the hidden view")
	}
	before, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(before), stored) {
		t.Error("source comparison was mutated")
	}
}
