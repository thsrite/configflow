package rulefetch

import (
	"strings"
	"unicode/utf8"

	"unicode"
)

func pyStrip(s string) string  { return strings.TrimFunc(s, ruleSpace) }
func pyLstrip(s string) string { return strings.TrimLeftFunc(s, ruleSpace) }
func runeLen(s string) int     { return utf8.RuneCountInString(s) }

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func ruleSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }
