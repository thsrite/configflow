package agents

import (
	"bytes"
	"encoding/json"
	"testing"
	"unicode/utf8"
)

func FuzzRegistrationClaimExactFields(f *testing.F) {
	f.Add([]byte(`{"name":"device","host":"192.0.2.40","port":8080,"service_type":"mihomo"}`))
	f.Add([]byte(`{"name":null,"port":true,"Name":"other"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(data, &raw) != nil || raw == nil {
			t.Skip()
		}
		before, err := json.Marshal(raw)
		if err != nil || !utf8.Valid(before) {
			// Invalid UTF-8 makes the whole claim invalid; overwriting an alias
			// key below could remove the only invalid member and is no alias test.
			t.Skip()
		}
		claim := decodeRegistrationClaim(before)
		after, err := json.Marshal(raw)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("claim decoder mutated request", err)
		}
		aliases := make(map[string]json.RawMessage, len(raw)+5)
		for k, v := range raw {
			aliases[k] = v
		}
		aliases["Name"] = json.RawMessage(`"synthetic-alias"`)
		aliases["Host"] = json.RawMessage(`"198.51.100.80"`)
		aliases["PORT"] = json.RawMessage(`false`)
		aliases["Service_Type"] = json.RawMessage(`"synthetic-service"`)
		aliases["Profile_ID"] = json.RawMessage(`"synthetic-profile"`)
		aliasBytes, err := json.Marshal(aliases)
		if err != nil {
			t.Fatal(err)
		}
		if changed := decodeRegistrationClaim(aliasBytes); changed != claim {
			t.Fatal("unknown case alias changed exact identity")
		}
	})
}
