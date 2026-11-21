package agents

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzRegistrationInputOwnershipBoundary(f *testing.F) {
	f.Add([]byte(`{"name":"device","host":"192.0.2.40","port":8080,"profile_id":null}`))
	f.Add([]byte(`{"name":null,"port":true,"profile_id":""}`))
	f.Add([]byte(`{"name":{"raw":2.00},"version":7}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var members map[string]json.RawMessage
		if json.Unmarshal(data, &members) != nil || members == nil {
			return
		}
		input, err := DecodeRegistrationInput(data)
		if err != nil {
			return
		}
		members["token"] = json.RawMessage(`"forged"`)
		members["gateway_installation_id"] = json.RawMessage(`"forged"`)
		members["Name"] = json.RawMessage(`"forged"`)
		members["PORT"] = json.RawMessage(`true`)
		members["Profile_ID"] = json.RawMessage(`7`)
		mutated, err := json.Marshal(members)
		if err != nil {
			t.Fatal(err)
		}
		other, err := DecodeRegistrationInput(mutated)
		if err != nil || other.claim != input.claim || other.profile != input.profile {
			t.Fatal("unknown fields changed identity", err)
		}
		for _, key := range []string{"token", "gateway_installation_id"} {
			if raw, present, err := other.record.MemberJSON(key); err != nil || present {
				t.Fatalf("request crossed ownership boundary: %s=%s err=%v", key, raw, err)
			}
		}
		for _, key := range []string{"name", "host", "port", "profile_id", "service_type", "deployment_method", "version"} {
			a, aPresent, aErr := input.record.MemberJSON(key)
			b, bPresent, bErr := other.record.MemberJSON(key)
			if aErr != nil || bErr != nil || aPresent != bPresent {
				t.Fatal("presence changed")
			}
			if !aPresent || bytes.Equal(a, b) {
				continue
			}
			// Out-of-range numbers (1E700) do not decode into any; equal bytes
			// above already prove those unchanged.
			var left, right any
			if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil || !reflect.DeepEqual(left, right) {
				t.Fatalf("field=%s changed from %s to %s", key, a, b)
			}
		}
	})
}
