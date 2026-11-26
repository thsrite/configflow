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
		_, err := parseInstallInteger(port)
		if !nativeIntegerDiagnostic(port, "invalid literal for int() with base 10: "+repr, err) && (err == nil || err.Error() != "invalid literal for int() with base 10: "+repr) {
			t.Errorf("%q: %v", port, err)
		}
	}
	// The ValueError carries repr() cut to 200 characters (%.200R).
	_, err := parseInstallInteger(strings.Repeat("x", 300))
	if want := "invalid literal for int() with base 10: '" + strings.Repeat("x", 199); !nativeIntegerDiagnostic(strings.Repeat("x", 300), want, err) && (err == nil || err.Error() != want) {
		t.Errorf("long port: %v", err)
	}
}

// Bracketed hosts that are not IPv6 addresses are refused (the error text is
// net/url's since N2).
func TestReviewBracketedScopeIDFollowsIPAddress(t *testing.T) {
	_, err := RenderGatewayInstall(GatewayInstallOptions{ServerURL: "HTTP://[::ffff:1.2.3.4%%]::///", AgentName: "gw",
		MihomoAgentPort: 8080, OxidnsAgentPort: 8082,
		AgentChecksums: map[string]string{"linux-amd64": strings.Repeat("a", 64)}})
	if err == nil {
		t.Errorf("%v", err)
	}
	_, err = RenderGatewayInstall(GatewayInstallOptions{ServerURL: "http://[zz]/", AgentName: "gw",
		MihomoAgentPort: 8080, OxidnsAgentPort: 8082,
		AgentChecksums: map[string]string{"linux-amd64": strings.Repeat("a", 64)}})
	if err == nil {
		t.Errorf("%v", err)
	}
}
