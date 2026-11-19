package rules

import (
	"encoding/json"
	"testing"
)

func TestN811IdentifierAndRejectedValueMessages(t *testing.T) {
	if got := IdentifierList([]string{"id", "a'b", `c\d`}); got != `['id', 'a\'b', 'c\\d']` {
		t.Fatalf("identifier list %s", got)
	}
	for _, raw := range []string{"null", "true", "42", `{"id":1}`} {
		if got := InvalidValueText(json.RawMessage(raw)); got != raw {
			t.Fatalf("rejected value %s -> %s", raw, got)
		}
	}
}
