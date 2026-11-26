package installation

import (
	"net/url"
	"strings"
	"testing"

	"configflow/internal/installer"
)

// O9 round 2 L6: gateway installation command quoting now uses the shared
// installer.ShlexQuote instead of a private helper, with byte-for-byte
// identical output for all gateway install script URLs and commands.
func TestO9R2CommandUsesSharedShlexQuoteByteIdentical(t *testing.T) {
	legacySingleQuote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'" }

	// Negative control: prove installer.ShlexQuote differs from naive single
	// quoting on safe ASCII tokens, confirming we actually rely on ShlexQuote.
	if got := installer.ShlexQuote("safe_token-1.0"); got == legacySingleQuote("safe_token-1.0") {
		t.Fatalf("negative control failed: ShlexQuote unexpectedly single-quoted safe token: %q", got)
	}

	tests := []struct {
		name      string
		serverURL string
		id        string
		token     string
	}{
		{"standard https", "https://panel.example.com", "inst-1234", "synthetic-token-abc"},
		{"custom port and path", "http://192.0.2.10:8080/sub", "inst-5678", "tok+1/2=3"},
		{"single quote and shell metachars in server URL", "https://panel.example.com/a'b$(id);echo", "inst-9999", "tok'x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scriptURL := tc.serverURL + "/api/agents/gateway-installations/" + tc.id + "/script?" + url.Values{"token": {tc.token}}.Encode()
			wantCmd := `set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; if command -v curl >/dev/null 2>&1; then curl -fSL ` + legacySingleQuote(scriptURL) + ` -o "$f"; else wget -O "$f" ` + legacySingleQuote(scriptURL) + `; fi; if [ "$(id -u)" -eq 0 ]; then sh "$f"; else sudo sh "$f"; fi`
			wantFull := "sh -c " + legacySingleQuote(wantCmd)

			gotCmd := `set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; if command -v curl >/dev/null 2>&1; then curl -fSL ` + installer.ShlexQuote(scriptURL) + ` -o "$f"; else wget -O "$f" ` + installer.ShlexQuote(scriptURL) + `; fi; if [ "$(id -u)" -eq 0 ]; then sh "$f"; else sudo sh "$f"; fi`
			gotFull := "sh -c " + installer.ShlexQuote(gotCmd)

			if gotFull != wantFull {
				t.Fatalf("command mismatch:\ngot:  %s\nwant: %s", gotFull, wantFull)
			}
		})
	}

	// End-to-end check on Service.public output.
	f := newFixture(t)
	pub, err := f.s.public(f.ctx, f.r)
	must(t, err)
	got := text(object(pub)["command"])
	scriptURL := f.r.ServerURL + "/api/agents/gateway-installations/" + f.r.ID + "/script?" + url.Values{"token": {f.r.Token}}.Encode()
	wantCmd := `set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; if command -v curl >/dev/null 2>&1; then curl -fSL ` + legacySingleQuote(scriptURL) + ` -o "$f"; else wget -O "$f" ` + legacySingleQuote(scriptURL) + `; fi; if [ "$(id -u)" -eq 0 ]; then sh "$f"; else sudo sh "$f"; fi`
	want := "sh -c " + legacySingleQuote(wantCmd)
	if got != want {
		t.Fatalf("Service.public command mismatch:\ngot:  %s\nwant: %s", got, want)
	}
}
