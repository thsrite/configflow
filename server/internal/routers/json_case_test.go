package routers

import (
	"configflow/internal/jsondecode"
	"encoding/json"
	"testing"
)

func TestAuditReturnProofExactMembers(t *testing.T) {
	want := &ReturnSpec{Table: 100, Priority: 200, Mark: "0x10"}
	for _, tc := range []struct {
		name, raw string
		ok        bool
	}{
		{"exact", `{"table":100,"priority":200,"mark":"0x10/0x10"}`, true},
		{"case-only", `{"Table":100,"Priority":200,"Mark":"0x10/0x10"}`, false},
		{"case-conflict", `{"table":100,"priority":200,"mark":"0x10/0x10","TABLE":101}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := returnRuleMatches(json.RawMessage(tc.raw), want); got != tc.ok {
				t.Fatalf("proof=%v want %v", got, tc.ok)
			}
		})
	}
}

func TestAuditManifestExactMembers(t *testing.T) {
	var got ReturnSpec
	if err := jsondecode.Unmarshal([]byte(`{"table":100,"TABLE":101,"safety_version":1,"SAFETY_VERSION":2}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Table != 100 || got.SafetyVersion != 1 {
		t.Fatalf("manifest overwritten: %#v", got)
	}
}
