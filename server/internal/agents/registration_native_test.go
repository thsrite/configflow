package agents

import (
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
