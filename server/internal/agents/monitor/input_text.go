package monitor

// Text boundaries retain the monitoring protocol's whitespace, line separators
// and character limits. Numeric tokens are validated separately as ASCII.
import (
	"strings"
	"unicode"
)

// textSpace recognizes Unicode whitespace and the legacy record separators.
func textSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// trimText removes surrounding protocol whitespace.
func trimText(s string) string { return strings.TrimFunc(s, textSpace) }

// splitMetricLines separates metrics records, treating CRLF as one separator.
func splitMetricLines(s string) []string {
	var lines []string
	start := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\n', '\x0b', '\x0c', '\x1c', '\x1d', '\x1e', '\x85', ' ', ' ':
		case '\r':
			lines = append(lines, string(runes[start:i]))
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			start = i + 1
			continue
		default:
			continue
		}
		lines = append(lines, string(runes[start:i]))
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// runeLen measures user-visible limits in code points.
func runeLen(s string) int { return len([]rune(s)) }

// truncateText bounds display text by code points.
func truncateText(s string, limit int) string {
	n := 0
	for i := range s {
		if n == limit {
			return s[:i]
		}
		n++
	}
	return s
}
