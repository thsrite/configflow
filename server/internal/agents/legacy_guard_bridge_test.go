package agents

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

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
			var record model.Agent
			if err := json.Unmarshal([]byte(tc.raw), &record); err != nil {
				t.Fatal(err)
			}
			if got := DNSFollowInProgress([]model.Agent{record}, "7"); got != tc.busy {
				t.Fatalf("got %v want %v", got, tc.busy)
			}
		})
	}
}
