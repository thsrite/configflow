package spec

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// HR O18 T1c: a guard switch stored with another type than a boolean reads
// as off (device-write gate), but off withdraws the guard on the next
// publication. A switch Python read as on ("x", 1) keeps the guard whole and
// device management refuses it (fail closed, as before O18 T1c); a falsy one
// (0, "") is off in Python too and may be withdrawn.
func TestReviewO18T1cDamagedGuardSwitchFailsClosed(t *testing.T) {
	tests := []struct {
		raw     string
		blocked bool
	}{
		{`{"ipv6_pd_guard":{"enabled":"x"},"ipv6_pd_guard_state":{"resources":[]}}`, true},
		{`{"ipv6_pd_guard":{"enabled":1}}`, true},
		{`{"dns_guard":{"enabled":"yes","bypass":[]},"dns_guard_state":{"resources":[]}}`, true},
		{`{"dns_guard":{"enabled":[1]}}`, true},
		{`{"dns_guard":{"enabled":0},"dns_guard_state":{"resources":[]}}`, false},
		{`{"ipv6_pd_guard":{"enabled":""},"ipv6_pd_guard_state":{"resources":[]}}`, false},
		{`{"ipv6_pd_guard":{"enabled":null}}`, false},
		{`{"dns_guard":{"enabled":true,"bypass":[],"future":1}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			var r Router
			if err := json.Unmarshal([]byte(tt.raw), &r); err != nil {
				t.Fatal(err)
			}
			err := r.ValidateForDevice()
			if tt.blocked != (err != nil) {
				t.Fatalf("blocked=%v, want %v (%v)", err != nil, tt.blocked, err)
			}
			if tt.blocked && err.Error() != MessageStoredGuardInvalid {
				t.Fatalf("error %v", err)
			}
		})
	}
}

// HR O18 T1c: runtime routers come from the plain record (routerFromModel),
// not from stored text. A rule-set selection stored as another JSON type used
// to be refused by validateIdentity on the raw record; on the plain record it
// read as an empty selection, and the next publication would withdraw every
// route or policy the selection had put on the router. It now stays damaged
// and device management refuses it; a selection stored as null is no
// selection, as in Python (`or []`).
func TestReviewO18T1cDamagedSelectionFailsClosed(t *testing.T) {
	for _, member := range []string{"ip_rule_sets", "ros_policy_rule_sets"} {
		for _, value := range []string{`"abc"`, `5`, `true`, `{"a":1}`, `null`, `["a"]`} {
			t.Run(member+value, func(t *testing.T) {
				raw := `{"id":"r","name":"n","type":"routeros","endpoint":"http://192.0.2.1","username":"u","password":"p","` + member + `":` + value + `}`
				var stored model.Router
				if _, err := model.DecodeStored([]byte(raw), &stored, ""); err != nil {
					t.Fatal(err)
				}
				r := routerFromModel(stored)
				err := r.ValidateForDevice()
				fine := value == `null` || value == `["a"]`
				if fine != (err == nil) {
					t.Fatalf("refused=%v (%v)", err != nil, err)
				}
				// Written back (an unrelated router write), the damage stays.
				out, err := model.Marshal(&stored)
				if err != nil {
					t.Fatal(err)
				}
				var again model.Router
				if _, err := model.DecodeStored(out, &again, ""); err != nil {
					t.Fatal(err)
				}
				rewritten := routerFromModel(again)
				if err := rewritten.ValidateForDevice(); fine != (err == nil) {
					t.Fatalf("after rewrite %s: refused=%v", out, err != nil)
				}
			})
		}
	}
}
