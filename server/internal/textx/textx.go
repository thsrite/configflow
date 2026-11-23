// Package textx holds small text helpers shared by several packages: digit
// checks and line splitting with the rules the web editor uses. It does not
// parse any data format.
package textx

import "strings"

// ASCIIDigits reports whether s is a nonempty run of ASCII digits 0-9.
func ASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// SplitLines splits text at "\n", "\r\n" and "\r", the line breaks the web
// editor (CodeMirror) counts, so line numbers match what the user sees. Other
// Unicode separators stay inside a line. A final line break does not start an
// empty last line, and "" has no lines. keepEnds keeps each line's break.
func SplitLines(text string, keepEnds bool) []string {
	var lines []string
	for len(text) > 0 {
		i := strings.IndexAny(text, "\r\n")
		if i < 0 {
			lines = append(lines, text)
			break
		}
		width := 1
		if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			width = 2
		}
		if keepEnds {
			lines = append(lines, text[:i+width])
		} else {
			lines = append(lines, text[:i])
		}
		text = text[i+width:]
	}
	return lines
}
