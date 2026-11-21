package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/logx"
	"configflow/internal/redact"
)

func TestTypedPresenceRetainsOnlySanitizedMetadata(t *testing.T) {
	resetPresence()
	t.Cleanup(resetPresence)
	secrets := redact.NewSecrets([]string{"synthetic-presence-secret"})
	records := []PresenceObservation{{ID: "device", Status: "online", Name: "edge synthetic-presence-secret", LastHeartbeat: "synthetic-presence-secret"}}
	ObserveAgentPresence(records, secrets)
	if stored := presenceKnown["device"]; stored != "online" {
		t.Fatal("presence retained more than the status", stored)
	}
	if records[0].Name != "edge synthetic-presence-secret" {
		t.Fatal("observation mutated")
	}
	ObserveAgentPresence([]PresenceObservation{{ID: "device", Status: "offline", Name: "edge"}}, secrets)
	if presenceKnown["device"] != "offline" {
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

func TestPresenceSecretsOnlyForLoggedTransitions(t *testing.T) {
	resetPresence()
	t.Cleanup(resetPresence)
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Info, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	record := func(status string) []PresenceRecord {
		return []PresenceRecord{{ID: "device", ValidID: true, Status: status,
			NameRaw: json.RawMessage(`"edge synthetic-presence-secret"`), HeartbeatRaw: json.RawMessage(`"2026-01-02T03:04:05Z"`)}}
	}
	calls := 0
	secrets := func() (redact.Secrets, error) {
		calls++
		return redact.NewSecrets([]string{"synthetic-presence-secret"}), nil
	}
	tests := []struct {
		name   string
		status string
		calls  int
		log    string
	}{
		{"first online scan follows silently", "online", 0, ""},
		{"unchanged scan", "online", 0, ""},
		{"transition renders once", "offline", 1, "edge [REDACTED]"},
		{"unchanged offline", "offline", 1, ""},
		{"recovery", "online", 2, "已恢复在线"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			ObservePresenceRecords(record(tt.status), secrets)
			if calls != tt.calls {
				t.Fatalf("secrets calls = %d, want %d", calls, tt.calls)
			}
			if out := logs.String(); tt.log == "" && out != "" || !strings.Contains(out, tt.log) || strings.Contains(out, "synthetic-presence-secret") {
				t.Fatalf("log %q, want %q", out, tt.log)
			}
		})
	}

	logs.Reset()
	ObservePresenceRecords(record("offline"), func() (redact.Secrets, error) { return nil, errors.New("synthetic failure") })
	if out := logs.String(); !strings.Contains(out, "已离线") || !strings.Contains(out, redact.Redacted) || strings.Contains(out, "edge") {
		t.Fatalf("failed secrets must hide the display values: %q", out)
	}
}
