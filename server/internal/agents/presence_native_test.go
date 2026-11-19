package agents

import (
	"testing"

	"configflow/internal/redact"
)

func TestTypedPresenceRetainsOnlySanitizedMetadata(t *testing.T) {
	resetPresence()
	t.Cleanup(resetPresence)
	secrets := redact.NewSecrets([]string{"synthetic-presence-secret"})
	records := []PresenceObservation{{ID: "device", Status: "online", Name: "edge synthetic-presence-secret", LastHeartbeat: "synthetic-presence-secret"}}
	ObserveAgentPresence(records, secrets)
	stored := presenceKnown["device"]
	if stored.name != "edge [REDACTED]" || stored.heartbeat != "[REDACTED]" {
		t.Fatal("presence retained credential", stored)
	}
	if records[0].Name != "edge synthetic-presence-secret" {
		t.Fatal("observation mutated")
	}
	ObserveAgentPresence([]PresenceObservation{{ID: "device", Status: "offline", Name: "edge"}}, secrets)
	if presenceKnown["device"].status != "offline" {
		t.Fatal("known offline record was dropped")
	}
	ObserveAgentPresence(nil, secrets)
	if len(presenceKnown) != 0 {
		t.Fatal("deleted record retained")
	}
	ObserveAgentPresence([]PresenceObservation{{ID: "cold", Status: "offline"}}, secrets)
	if len(presenceKnown) != 0 {
		t.Fatal("cold offline record logged/followed")
	}
}
