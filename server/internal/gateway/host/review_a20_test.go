package host

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
)

// A20 review: the persisted-forwarding entry is built from the Agent's
// forwarding reply. When that reply has no "managed" member, Python reads
// None as no gateway record; the Go entry must not invent an unreadable one.
func TestReviewA20PersistEntryWithoutManagedHasNoRecord(t *testing.T) {
	tests := []struct {
		name, reply string
		record      bool
	}{
		{"absent", `{"success":true,"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`, false},
		{"null", `{"success":true,"managed":null,"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`, false},
		{"inactive", `{"success":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`, false},
		{"active", `{"success":true,"managed":{"active":true},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`, true},
		{"malformed", `{"success":true,"managed":"broken","ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, client := newBridge(t)
			client.call = func(context.Context, agents.Request) (*agents.Response, error) {
				return &agents.Response{StatusCode: 200, Body: []byte(tc.reply)}, nil
			}
			data, err := b.PersistForwarding(context.Background(), "gw", []RouterRef{{ID: "r", Name: "r"}})
			if err != nil {
				t.Fatal(err)
			}
			var entry ForwardingEntry
			if err := json.Unmarshal(data["gateway"], &entry); err != nil {
				t.Fatal(err)
			}
			if entry.GatewayRecord != tc.record || entry.State != "persisted" {
				t.Fatalf("entry %+v, want record=%v", entry, tc.record)
			}
		})
	}
	if HasRecord(Reply{"managed": nil}) {
		t.Fatal("an absent managed value counted as a record")
	}
}
