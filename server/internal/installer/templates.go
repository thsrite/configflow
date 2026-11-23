package installer

import (
	"configflow/internal/textx"
	"embed"
	"strings"
	"unicode/utf8"

	"unicode"
)

// scriptFiles holds the installer shell templates (originally copied from the
// Python tree's backend/agents/scripts; this embedded copy is now the source).
//
//go:embed scripts/*.sh
var scriptFiles embed.FS

func script(name string) string {
	data, err := scriptFiles.ReadFile("scripts/" + name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// forwardingHookJournal saves the IPv4 forwarding hook before the installer
// writes it, so compensation restores (or removes) it with the other managed
// files. Emitted only when forwarding is managed (L1 review R3).
const forwardingHookJournal = "agent_rollback_save \"$AGENT_DIR/enable-ipv4-forwarding.sh\"\n"

var (
	cliOutputHelpers           = script("cli-output.sh")
	installOutputHelpers       = script("install-output.sh")
	agentInstallRollback       = script("agent-install-rollback.sh")
	installGoScriptTemplate    = script("install-go.sh")
	installGoAIOScriptTemplate = script("install-go-aio.sh")
	installGoAIOForwarding     = script("aio-ipv4-forwarding.sh")
	// AgentInstallDependencies is AGENT_INSTALL_DEPENDENCIES.
	AgentInstallDependencies = script("agent-install-dependencies.sh")
	dnsHAPreparation         = script("prepare-dns-ha.sh")
	installCoresTemplate     = script("install-cores.sh")
	installGatewayTemplate   = script("install-gateway.sh")
	// GatewayNetworkProbe is detect-gateway-network.sh, served as is.
	GatewayNetworkProbe = script("detect-gateway-network.sh")
)

// Script returns an embedded template by file name (for tests and routes).
func Script(name string) (string, bool) {
	data, err := scriptFiles.ReadFile("scripts/" + name)
	return string(data), err == nil
}

// shlexQuote is shlex.quote: the ASCII-only safe set [\w@%+=:,./-].
func shlexQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_@%+=:,./-", c) >= 0) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// ShlexQuote is shlex.quote (exported for the routes and tests).
func ShlexQuote(s string) string { return shlexQuote(s) }

// substitute is re.sub(r"\{(k1|k2|...)\}", lambda m: values[m.group(1)], text):
// one pass, replaced values are not scanned again. Keys never contain braces,
// so a brace pair matches exactly when its content is a key.
func substitute(text string, values map[string]string) string {
	var out strings.Builder
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			out.WriteString(text)
			return out.String()
		}
		out.WriteString(text[:open])
		rest := text[open+1:]
		if end := strings.IndexByte(rest, '}'); end >= 0 {
			if value, ok := values[rest[:end]]; ok {
				out.WriteString(value)
				text = rest[end+1:]
				continue
			}
		}
		out.WriteByte('{')
		text = rest
	}
}

// trimScriptTail removes native Unicode whitespace from a generated script.
func trimScriptTail(text string) string { return strings.TrimRightFunc(text, unicode.IsSpace) }

// codePoints is len(str) of a Python str.
func codePoints(s string) int { return utf8.RuneCountInString(s) }

// replaceOnce is str.replace(old, new, 1).
func replaceOnce(s, old, replacement string) string { return strings.Replace(s, old, replacement, 1) }

// SplitLines is str.splitlines(keepends=True) (exported for the routes).
func SplitLines(text string) []string { return textx.SplitLines(text, true) }
