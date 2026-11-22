package agents

import (
	"encoding/json"
	"testing"
)

func TestNativeProtocolDecodersReadOnlyExactKeys(t *testing.T) {
	claim := decodeRegistrationClaim(json.RawMessage(`{"name":"device","Name":"forged","port":8080,"PORT":9090,"service_type":"mihomo","Service_Type":"oxidns"}`))
	if claim.invalid || claim.Name != "device" || claim.Port != 8080 || claim.ServiceType != "mihomo" {
		t.Fatalf("alias changed claim: %+v", claim)
	}
	input, err := DecodeRegistrationInput(json.RawMessage(`{"name":"device","Name":"forged","profile_id":"default","Profile_ID":"forged"}`))
	if err != nil || input.profile != "default" || input.record.Name != "device" {
		t.Fatal("alias changed registration", input, err)
	}
	edits, err := DecodeAgentEdits(json.RawMessage(`{"name":"device","Name":"forged","profile_id":"default","Profile_ID":"forged"}`))
	if err != nil || edits.record.Name != "device" || edits.record.ProfileID != "default" {
		t.Fatal("alias changed edits", edits, err)
	}
	patch, err := DecodeDeploymentUpdate(json.RawMessage(`{"status":"checking","Status":"succeeded","success":false,"Success":true,"profile_id":"forged"}`))
	if err != nil || patch.record.Status != "checking" || flagSet(patch.record.Success) || patch.record.ProfileID != "" {
		t.Fatal("alias or proof entered update", patch, err)
	}
}

func TestNativeProtocolDecodersRejectRawDuplicateMembers(t *testing.T) {
	for _, raw := range []string{
		`{"name":"device","name":"forged"}`,
		`{"name":"device","n\u0061me":"forged"}`,
		`{"profile_id":"default","profile_id":"other"}`,
		`{"status":"checking","status":"succeeded"}`,
		`{"future":{"x":1,"x":2}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			data := json.RawMessage(raw)
			if _, err := DecodeRegistrationInput(data); err == nil {
				t.Fatal("registration accepted duplicate")
			}
			if _, err := DecodeAgentEdits(data); err == nil {
				t.Fatal("edits accepted duplicate")
			}
			if _, err := DecodeDeploymentUpdate(data); err == nil {
				t.Fatal("journal patch accepted duplicate")
			}
			if !decodeRegistrationClaim(data).invalid {
				t.Fatal("claim accepted duplicate")
			}
		})
	}
}
