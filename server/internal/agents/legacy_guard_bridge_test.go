package agents

import (
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
)

func nativeTestLegacyObject(t *testing.T, raw string) *jsonx.Object {
	t.Helper()
	var out jsonx.Object
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func TestLegacyDNSGuardUsesOriginalIdentity(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		busy bool
	}{
		{`{"id":"7","dns_follow_state":{"status":"publishing"}}`, true},
		{`{"id":7,"dns_follow_state":{"status":"publishing"}}`, false},
		{`{"id":"other","dns_follow":{"source_agent_id":"7"},"dns_follow_state":{"status":"preparing"}}`, true},
		{`{"id":"other","dns_follow":{"source_agent_id":7},"dns_follow_state":{"status":"preparing"}}`, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			record, err := jsonx.Parse([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got := DNSFollowBusy([]any{record}, "7"); got != tc.busy {
				t.Fatalf("got %v want %v", got, tc.busy)
			}
		})
	}
}
