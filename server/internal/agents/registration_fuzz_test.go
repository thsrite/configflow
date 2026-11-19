package agents

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzRegistrationClaimExactFields(f *testing.F) {
	f.Add([]byte(`{"name":"device","host":"192.0.2.40","port":8080,"service_type":"mihomo"}`))
	f.Add([]byte(`{"name":null,"port":true,"Name":"other"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var record Record
		if json.Unmarshal(data, &record) != nil {
			t.Skip()
		}
		before, err := json.Marshal(&record)
		if err != nil {
			t.Skip()
		}
		claim := legacyRegistrationClaim(&record)
		after, err := json.Marshal(&record)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("claim decoder mutated request", err)
		}
		aliases := record.DeepCopy()
		aliases.Set("Name", "synthetic-alias")
		aliases.Set("Host", "198.51.100.80")
		aliases.Set("PORT", false)
		aliases.Set("Service_Type", "synthetic-service")
		aliases.Set("Profile_ID", "synthetic-profile")
		if changed := legacyRegistrationClaim(aliases); changed != claim {
			t.Fatal("unknown case alias changed exact identity")
		}
	})
}
