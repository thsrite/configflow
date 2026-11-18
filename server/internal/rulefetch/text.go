package rulefetch

import (
	"strings"
	"unicode/utf8"

	"unicode"
)

// Rule text accepts Unicode line separators in both text and YAML delivery.

// lineBreak reports the width (in runes) of a str.splitlines() line break
// starting at runes[i], 0 when there is none.
func lineBreak(runes []rune, i int) int {
	switch runes[i] {
	case '\r':
		if i+1 < len(runes) && runes[i+1] == '\n' {
			return 2
		}
		return 1
	case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return 1
	}
	return 0
}

// splitLines retains source line boundaries for editor offsets and diagnostics.
func splitLines(text string, keepends bool) []string {
	runes := []rune(text)
	var lines []string
	start := 0
	for i := 0; i < len(runes); {
		width := lineBreak(runes, i)
		if width == 0 {
			i++
			continue
		}
		end := i
		if keepends {
			end = i + width
		}
		lines = append(lines, string(runes[start:end]))
		i += width
		start = i
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

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
