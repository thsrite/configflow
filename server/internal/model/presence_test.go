package model

import (
	"encoding/json"
	"testing"
)

func TestHARecordPresenceMatchesNativeEncoding(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enable":null}`, `{"enable":{}}`, `{"enable":{"peer_id":"peer"}}`, `{"enable":7}`, `{"status":null}`, `{"status":0}`, `{"updated_at":0}`, `{"future":{"source":"opaque"}}`} {
		t.Run(raw, func(t *testing.T) {
			var record HAJournal
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatal(err)
			}
			check := func() {
				data, err := Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err = json.Unmarshal(data, &fields); err != nil {
					t.Fatal(err)
				}
				if record.HasMembers() != (len(fields) > 0) {
					t.Fatalf("presence mismatch: %s", data)
				}
			}
			check()
			record.Enable = nil
			check()
			for _, key := range []string{"status", "enable", "updated_at"} {
				if err := record.Clear(key); err != nil {
					t.Fatal(err)
				}
			}
			record.Fields.Delete("future")
			check()
			if record.HasMembers() {
				t.Fatal("cleared record still has members")
			}
			record.Status = "preparing"
			check()
		})
	}
}
