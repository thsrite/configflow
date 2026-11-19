package monitor

// Text/query compatibility retained from the existing input contract; N10 owns
// Unicode table simplification. These helpers do not interpret JSON values.
import (
	"sort"
	"strings"
	"unicode"
)

// pyDecimalValue is the decimal value of a str.isdecimal() character of
// CPython 3.11 (Unicode 14), -1 for any other character.
func pyDecimalValue(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	if r < 0x660 {
		return -1
	}
	i := sort.Search(len(pyDecimalStarts), func(i int) bool { return pyDecimalStarts[i] > r }) - 1
	if i >= 0 && r-pyDecimalStarts[i] < 10 {
		return int(r - pyDecimalStarts[i])
	}
	return -1
}

// pyIsSpace is str.isspace() for one character (also re's \s for str
// patterns).
func pyIsSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// pyStrip is str.strip().
func pyStrip(s string) string { return strings.TrimFunc(s, pyIsSpace) }

// pySplitLines is str.splitlines().
func pySplitLines(s string) []string {
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

// runeLen is len(str).
func runeLen(s string) int { return len([]rune(s)) }

// pySlice is s[:limit] by code points.
func pySlice(s string, limit int) string {
	n := 0
	for i := range s {
		if n == limit {
			return s[:i]
		}
		n++
	}
	return s
}
