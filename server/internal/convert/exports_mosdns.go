package convert

import (
	"net/netip"
	"regexp"
	"strings"

	"configflow/internal/netipx"
)

// AppendQuery appends ordered query pairs to a panel URL, preserving its path.
func AppendQuery(address string, pairs [][2]string) (string, error) {
	return appendProfileQuery(address, pairs)
}

var ruleLineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\v", "\n", "\f", "\n", "\x1c", "\n", "\x1d", "\n", "\x1e", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")

// RuleLines splits text accepted by the rule-file editors into logical lines.
func RuleLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(ruleLineBreaks.Replace(text), "\n"), "\n")
}

var ruleLiteralPunctuation = strings.NewReplacer("-", `\-`, "&", `\&`, "~", `\~`, "#", `\#`, " ", `\ `, "\t", "\\\t", "\n", "\\\n", "\r", "\\\r", "\v", "\\\v", "\f", "\\\f")

// RuleLiteral quotes literals for portable rule-file regular expressions,
// including extended-mode whitespace and character-class punctuation.
func RuleLiteral(text string) string { return ruleLiteralPunctuation.Replace(regexp.QuoteMeta(text)) }

// IsIPNetwork recognizes supported host, prefix, IPv4 netmask and hostmask forms.
func IsIPNetwork(text string) bool {
	address, mask, hasMask := strings.Cut(text, "/")
	if strings.Contains(address, "%") {
		parsed, err := netip.ParseAddr(address)
		if err != nil || !parsed.Is6() || parsed.Zone() == "" || strings.Contains(parsed.Zone(), "%") {
			return false
		}
		// Rule files keep scoped literals verbatim; the shared network parser only
		// needs the address/prefix to decide whether this is an IP rule.
		text = parsed.WithZone("").String()
		if hasMask {
			text += "/" + mask
		}
	}
	_, ok := netipx.ParseNetwork(text, false)
	return ok
}
