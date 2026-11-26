package oxidns

import (
	"strconv"
	"strings"
	"testing"

	"configflow/internal/devicenotes"
)

// Typed device policy rows are validated again before they reach the DNS
// draft, as the JSON rows were (batch-52 review).
func TestApplyPoliciesValidatesTypedRows(t *testing.T) {
	tooMany := make([]devicenotes.Policy, 513)
	for i := range tooMany {
		tooMany[i] = devicenotes.Policy{IP: "192.0.2." + strconv.Itoa(i%250+1), Mode: "direct"}
	}
	tests := []struct {
		name string
		rows []devicenotes.Policy
		want string
	}{
		{"address", []devicenotes.Policy{{IP: "not-an-address", Mode: "direct"}}, devicenotes.AddressMessage},
		{"mode", []devicenotes.Policy{{IP: "192.0.2.10", Mode: "bogus"}}, devicenotes.PolicyModeMessage},
		{"name", []devicenotes.Policy{{IP: "192.0.2.10", Name: "a\nb", Mode: "direct"}}, devicenotes.NameMessage},
		{"limit", tooMany, devicenotes.PoliciesMessage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ApplyPolicies(M(), emptyProfile(), tt.rows, "")
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// The template branch of ApplyRuleDNS keeps normalizeRuleDNS's action check:
// an action that normalizeAction rejects is not written (batch-52 review).
func TestValidateRuleDNSNormalizesActions(t *testing.T) {
	tests := []struct {
		name  string
		build func(v *ruleDNSValues)
		want  string
	}{
		{"empty upstream addr", func(v *ruleDNSValues) {
			v.Actions.set("r1", RuleAction{Action: "upstream", Source: "local", Upstream: M("addr", "")})
		}, "指定上游需要非空的 addr 地址"},
		{"list route in rule_actions", func(v *ruleDNSValues) {
			v.Actions.set("r1", RuleAction{Action: "direct"})
		}, "规则 DNS 处理无效"},
		{"unknown parked", func(v *ruleDNSValues) {
			v.Parked.set("r1", RuleAction{Action: "bogus"})
		}, "规则 DNS 处理无效"},
		{"valid", func(v *ruleDNSValues) {
			v.Actions.set("r1", RuleAction{Action: "refused"})
			v.Parked.set("r2", RuleAction{Action: "direct"})
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newRuleDNSValues()
			tt.build(v)
			err := validateRuleDNS(v)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// A rule_actions member with a null value still removes the rule from the
// selector lists of template inputs, as Python's `identity in actions` did.
func TestRestoreParkedNullActionLeavesLists(t *testing.T) {
	inputs, err := FromJSON([]byte(`{"direct_rules":["r1","r2"],"rule_actions":{"r1":null},"rule_dns_parked":{"r3":{"action":"proxy"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	out := restoreParked(inputs, nil)
	if got, want := string(ToJSON(get(out, "direct_rules"))), `["r2"]`; got != want {
		t.Fatalf("direct_rules = %s, want %s", got, want)
	}
	if got, want := string(ToJSON(get(out, "proxy_rules"))), `["r3"]`; got != want {
		t.Fatalf("proxy_rules = %s, want %s", got, want)
	}
}
