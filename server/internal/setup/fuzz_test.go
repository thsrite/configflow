package setup

import (
	"encoding/json"
	"testing"
)

func FuzzSetupRecord(f *testing.F) {
	for _, seed := range []string{`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_id":"synthetic","prepared":false,"_future":[null,0]}`, `{"router_draft":{"password":"synthetic-private"},"domain_rules":{"direct":[],"proxy":[]}}`, `{"owned_digest":null,"profile_revision":1234567890123456789}`, `[]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 {
			return
		}
		var record Record
		if json.Unmarshal(raw, &record) != nil {
			return
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		var again Record
		if err := json.Unmarshal(encoded, &again); err != nil {
			t.Fatal(err)
		}
		var before, after any
		json.Unmarshal(raw, &before)
		json.Unmarshal(encoded, &after)
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		if string(a) != string(b) {
			t.Fatalf("stored fields changed: %s / %s", a, b)
		}
	})
}
