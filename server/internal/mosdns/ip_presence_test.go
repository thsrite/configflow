package mosdns

import "testing"

func TestHasIPRulesMatchesServedPart(t *testing.T) {
	tests := []struct {
		name, content string
		want, wantErr bool
	}{
		{"comments and domains", "# 192.0.2.1\n// ip:192.0.2.1\nDOMAIN,example.com\nfull:192.0.2.1", false, false},
		{"last bare prefix", "example.com\r\ninvalid\u2028192.0.2.0/24", true, false},
		{"mixed case native", "\ufeffIp: 2001:db8::1", true, false},
		{"classical IPv6", "ip-cidr6,2001:db8::/48,no-resolve", true, false},
		{"invalid IP", "ip:no-address\nIP-CIDR,192.0.2.1/33", false, false},
		{"hostname in explicit IP rule", "IP-CIDR,example.com", false, false},
		{"YAML sequence", "payload: [false, 123, '192.0.2.9']", true, false},
		{"YAML mapping keys", "payload: {'192.0.2.7': ignored}", true, false},
		{"YAML mapping values ignored", "payload: {unrelated: '192.0.2.1'}", false, false},
		{"YAML scalar text iterates characters", "payload: '192.0.2.1'", false, false},
		{"YAML false", "payload: false", false, false},
		{"YAML number", "payload: 2", false, true},
		{"YAML policy after first IP", "payload: ['192.0.2.1', &self [*self]]", false, true},
		{"malformed YAML text fallback", "payload: [\n192.0.2.1\n", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HasIPRules(tc.content)
			text, serveErr := ConvertRuleContentForMosDNS(tc.content, PartIP)
			if (err != nil) != tc.wantErr || (serveErr != nil) != tc.wantErr {
				t.Fatalf("presence error %v, conversion error %v", err, serveErr)
			}
			if err != nil {
				if err.Error() != serveErr.Error() {
					t.Fatalf("different errors: %v, %v", err, serveErr)
				}
				return
			}
			if got != tc.want || got != (text != EmptyRulePart(PartIP)) {
				t.Fatalf("presence %t, served %q", got, text)
			}
		})
	}
}
