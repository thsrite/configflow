package model

import (
	"encoding/json"
	"testing"
)

// TestHARecordPresence: HasMembers reports a journal holding any value; a
// zero journal (decoded from {}, null members, wrong types or unknown
// members) has none.
func TestHARecordPresence(t *testing.T) {
	for _, tt := range []struct {
		raw     string
		members bool
	}{
		{`{}`, false}, {`{"enable":null}`, false}, {`{"enable":{}}`, true}, {`{"enable":{"peer_id":"peer"}}`, true},
		{`{"enable":7}`, false}, {`{"status":null}`, false}, {`{"status":0}`, true}, {`{"updated_at":0}`, false},
		{`{"future":{"source":"opaque"}}`, false},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			var record HAJournal
			if err := json.Unmarshal([]byte(tt.raw), &record); err != nil {
				t.Fatal(err)
			}
			if record.HasMembers() != tt.members {
				t.Fatalf("HasMembers = %v, want %v", record.HasMembers(), tt.members)
			}
			for _, key := range []string{"status", "enable", "updated_at"} {
				if err := ZeroMember(&record, key); err != nil {
					t.Fatal(err)
				}
			}
			if record.HasMembers() {
				t.Fatal("cleared record still has members")
			}
			record.Status = "preparing"
			if !record.HasMembers() {
				t.Fatal("record with a status has no members")
			}
		})
	}
}
