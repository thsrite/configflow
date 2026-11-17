package difftest

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// nativeQueryVariant changes only invalid UTF-8 query components to the native
// U+FFFD policy. The independent Flask server then generates the entire expected
// output for that explicit alternative input; no script lines are masked.
func nativeQueryVariant(path string) (string, bool) {
	u, err := url.Parse(path)
	if err != nil {
		return path, false
	}
	changed := false
	parts := strings.Split(u.RawQuery, "&")
	hex := func(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }
	part := func(raw string) string {
		var protected strings.Builder
		for i := 0; i < len(raw); i++ {
			if raw[i] == '%' && (i+2 >= len(raw) || !hex(raw[i+1]) || !hex(raw[i+2])) {
				protected.WriteString("%25")
			} else {
				protected.WriteByte(raw[i])
			}
		}
		decoded, err := url.QueryUnescape(protected.String())
		if err != nil || utf8.ValidString(decoded) {
			return raw
		}
		changed = true
		return url.QueryEscape(strings.ToValidUTF8(decoded, "\ufffd"))
	}
	for i, p := range parts {
		k, v, eq := strings.Cut(p, "=")
		k = part(k)
		if eq {
			parts[i] = k + "=" + part(v)
		} else {
			parts[i] = k
		}
	}
	if !changed {
		return path, false
	}
	u.RawQuery = strings.Join(parts, "&")
	return u.String(), true
}

func assertNativeOutputSyntax(t *testing.T, kind string, body []byte) {
	t.Helper()
	if kind == "compose" {
		var document yaml.Node
		if err := yaml.Unmarshal(body, &document); err != nil {
			t.Fatalf("invalid Compose YAML: %v", err)
		}
		return
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(string(body))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unsafe shell syntax: %v: %s", err, output)
	}
}

func TestInvalidUTF8InstallerQueryRemainsShellSafe(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	base := t.TempDir()
	seed(t, base)
	server, stop := startGo(t, base)
	defer stop()
	marker := filepath.Join(t.TempDir(), "must-not-execute")
	value := "https://panel.example/'$(touch " + marker + ")/"
	result := sendInstaller(t, server, installerRequest{method: "GET", path: "/api/agents/install-script?agent_type=go&server_url=" + url.QueryEscape(value) + "%ff"}, "")
	if result.status != 200 {
		t.Fatalf("status %d: %s", result.status, result.body)
	}
	assertNativeOutputSyntax(t, "install-script", result.body)
	var assignment string
	for _, line := range strings.Split(string(result.body), "\n") {
		if strings.HasPrefix(line, "BINARY_BASE=") {
			assignment = line
			break
		}
	}
	if assignment == "" {
		t.Fatal("binary URL assignment missing")
	}
	cmd := exec.Command("bash", "-c", "set -eu\n"+assignment+"\nprintf '%s' \"$BINARY_BASE\"")
	actual, err := cmd.CombinedOutput()
	if err != nil || string(actual) != value+"\ufffd/api/agents/download" {
		t.Fatalf("shell assignment changed URL: %q %v", actual, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("query text executed a command")
	}
	_, tail, ok := strings.Cut(string(result.body), "<<'CONFIGFLOW_JSON_EOF'\n")
	if !ok {
		t.Fatal("config is not in a literal heredoc")
	}
	config, _, ok := strings.Cut(tail, "\nCONFIGFLOW_JSON_EOF")
	if !ok || !json.Valid([]byte(config)) {
		t.Fatal("invalid literal config JSON")
	}
}
