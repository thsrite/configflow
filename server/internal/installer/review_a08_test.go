package installer

import (
	"strings"
	"testing"
)

// Regression cases of the A08 review (expectations from CPython 3.11.17; the
// full sets are in testdata/fuzz.jsonl.gz).
func TestReviewIntOfPortFollowsCPython(t *testing.T) {
	// Unicode 15 digits are not digits for CPython 3.11 (Unicode 14).
	for port, repr := range map[string]string{"\U0001e4f1": `'\U0001e4f1'`, "\U00011f52": `'\U00011f52'`} {
		_, err := GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "http://s", AgentName: "n", ServiceType: "oxidns", AgentPort: port})
		if err == nil || err.Error() != "invalid literal for int() with base 10: "+repr {
			t.Errorf("%q: %v", port, err)
		}
	}
	// The ValueError carries repr() cut to 200 characters (%.200R).
	_, err := GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "http://s", AgentName: "n", ServiceType: "mihomo", AgentPort: strings.Repeat("x", 300)})
	if want := "invalid literal for int() with base 10: '" + strings.Repeat("x", 199); err == nil || err.Error() != want {
		t.Errorf("long port: %v", err)
	}
}

func TestReviewBracketedScopeIDFollowsIPAddress(t *testing.T) {
	_, err := GenerateGatewayInstallScript(GatewayOptions{ServerURL: "HTTP://[::ffff:1.2.3.4%%]::///", AgentName: "gw",
		AgentChecksums: map[string]string{"linux-amd64": strings.Repeat("a", 64)}})
	if err == nil || err.Error() != "'::ffff:1.2.3.4%%' does not appear to be an IPv4 or IPv6 address" {
		t.Errorf("%v", err)
	}
	_, err = GenerateGatewayInstallScript(GatewayOptions{ServerURL: "http://[zz]/", AgentName: "gw",
		AgentChecksums: map[string]string{"linux-amd64": strings.Repeat("a", 64)}})
	if err == nil || err.Error() != "'zz' does not appear to be an IPv4 or IPv6 address" {
		t.Errorf("%v", err)
	}
}
