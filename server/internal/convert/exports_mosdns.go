package convert

import (
	"iter"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/netipx"
)

// AppendQuery appends ordered query pairs to a panel URL, preserving its path.
func AppendQuery(address string, pairs [][2]string) (string, error) {
	return appendProfileQuery(address, pairs)
}

// RuleLines splits text accepted by the rule-file editors into logical lines.
func RuleLines(text string) []string {
	return slices.Collect(RuleLineSeq(text))
}

// RuleLineSeq visits the same logical lines without allocating a list. A
// trailing separator terminates the preceding line; CRLF is one separator.
func RuleLineSeq(text string) iter.Seq[string] {
	return func(yield func(string) bool) {
		start := 0
		for i, r := range text {
			switch r {
			case '\n':
				if i > 0 && text[i-1] == '\r' {
					start = i + 1
					continue
				}
			case '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
			default:
				continue
			}
			if !yield(text[start:i]) {
				return
			}
			start = i + utf8.RuneLen(r)
		}
		if start < len(text) {
			yield(text[start:])
		}
	}
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
