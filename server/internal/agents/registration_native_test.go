package agents

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestRegistrationClaimUsesProtocolTypes(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		valid bool
		port  model.Int
	}{
		{`{"name":"device","port":8080}`, true, 8080},
		{`{"name":"device","port":8080.0}`, true, 8080},
		{`{"name":"device","port":8.08e3}`, true, 8080},
		{`{"name":"device"}`, true, 8080},
		{`{"name":"device","port":"8080"}`, false, 8080},
		{`{"name":"device","port":true}`, false, 8080},
		{`{"name":7,"port":8080}`, false, 8080},
		{`{"name":null,"port":8080}`, false, 8080},
	} {
		claim := decodeRegistrationClaim([]byte(tt.raw))
		if !claim.invalid != tt.valid || tt.valid && claim.Port != tt.port {
			t.Fatalf("input=%s valid=%v port=%v", tt.raw, !claim.invalid, claim.Port)
		}
	}
}

func TestRegistrationSessionR5ValuesAreNotProofs(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		valid bool
	}{
		{`{"hash":"synthetic-hash","port":8080,"expires":9999999999}`, true},
		{`{"hash":"synthetic-hash","port":"8080","expires":"9999999999"}`, true},
		{`{"hash":"synthetic-hash","name":7,"port":8080,"expires":9999999999}`, false},
		{`{"hash":"synthetic-hash","name":null,"port":8080,"expires":9999999999}`, false},
		{`{"hash":"synthetic-hash","host":null,"port":8080,"expires":9999999999}`, false},
		{`{"hash":"synthetic-hash","port":false,"expires":9999999999}`, false},
		{`{"hash":"synthetic-hash","port":null,"expires":9999999999}`, false},
		{`{"hash":"synthetic-hash","expires":9999999999}`, false},
	} {
		var session model.AgentRegistrationSession
		if err := json.Unmarshal([]byte(tt.raw), &session); err != nil {
			t.Fatal(err)
		}
		if got := admissionSessionIdentity(session); got != tt.valid {
			t.Fatalf("input=%s valid=%v want=%v", tt.raw, got, tt.valid)
		}
	}
}

func TestRegistrationClaimIgnoresCaseAliases(t *testing.T) {
	claim := decodeRegistrationClaim([]byte(`{"name":"device","Name":"forged","port":8080,"PORT":true,"Service_Type":"other"}`))
	if claim.invalid || claim.Name != "device" || claim.Port != 8080 || claim.ServiceType != "mihomo" {
		t.Fatalf("case alias changed protocol identity: %+v", claim)
	}
	var session model.AgentRegistrationSession
	if err := json.Unmarshal([]byte(`{"hash":"synthetic-hash","port":8080,"expires":9999999999,"name":null,"Name":"forged"}`), &session); err != nil {
		t.Fatal(err)
	}
	if admissionSessionIdentity(session) {
		t.Fatal("case alias repaired invalid stored identity proof")
	}
}
