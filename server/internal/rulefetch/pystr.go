package rulefetch

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

// Python str helpers on code points.

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

// splitLines is str.splitlines(keepends).
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

func pyStrip(s string) string  { return rules.PyStrip(s) }
func pyLstrip(s string) string { return strings.TrimLeftFunc(s, rules.PyIsSpace) }
func runeLen(s string) int     { return utf8.RuneCountInString(s) }

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// jsonDumps is json.dumps(value, ensure_ascii=ascii) for a str.
func jsonDumps(value string, ascii bool) string {
	out, _ := jsonx.Encode(value, jsonx.Options{EnsureASCII: ascii})
	return string(out)
}

// bytesRepr is repr(bytes).
func bytesRepr(b []byte) string {
	quote := byte('\'')
	if strings.IndexByte(string(b), '\'') >= 0 && strings.IndexByte(string(b), '"') < 0 {
		quote = '"'
	}
	var out strings.Builder
	out.WriteString("b")
	out.WriteByte(quote)
	for _, c := range b {
		switch {
		case c == quote || c == '\\':
			out.WriteByte('\\')
			out.WriteByte(c)
		case c == '\t':
			out.WriteString(`\t`)
		case c == '\n':
			out.WriteString(`\n`)
		case c == '\r':
			out.WriteString(`\r`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&out, `\x%02x`, c)
		default:
			out.WriteByte(c)
		}
	}
	out.WriteByte(quote)
	return out.String()
}

// utf8ErrorRange is the (start, end) of the first UnicodeDecodeError of
// bytes.decode('utf-8'), CPython's ranges; ok false for valid UTF-8.
func utf8ErrorRange(b []byte) (int, int, bool) {
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			i++
			continue
		}
		var need int
		var lo, hi byte = 0x80, 0xBF
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lo = 0xA0
			} else if c == 0xED {
				hi = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lo = 0x90
			} else if c == 0xF4 {
				hi = 0x8F
			}
		default:
			return i, i + 1, true
		}
		for j := 1; j <= need; j++ {
			if i+j >= len(b) {
				return i, i + j, true
			}
			low, high := byte(0x80), byte(0xBF)
			if j == 1 {
				low, high = lo, hi
			}
			if d := b[i+j]; d < low || d > high {
				return i, i + j, true
			}
		}
		i += need + 1
	}
	return 0, 0, false
}
