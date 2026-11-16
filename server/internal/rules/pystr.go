package rules

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file holds the few Python str behaviours the rule engine depends on and
// that internal/pystr does not provide: str.strip(chars), ASCII digit checks,
// str.splitlines() separators and the \w class. str.lower(), str.upper(),
// str.strip(), str.isspace() and str.isidentifier() come from internal/pystr
// (CPython 3.11 tables).

// pyStripChars is s.strip(chars) for a set of characters.
func pyStripChars(s, chars string) string {
	return strings.Trim(s, chars)
}

// pyIsASCIIDigits is s.isascii() and s.isdigit() for a non-empty string.
func pyIsASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isLineEnd reports the separators of the Python regex
// `\r\n|[\n\r\v\f\x1c-\x1e\x85  ]`; width is the byte length.
func lineEndAt(s string, i int) (width int) {
	c := s[i]
	switch c {
	case '\r':
		if i+1 < len(s) && s[i+1] == '\n' {
			return 2
		}
		return 1
	case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e:
		return 1
	}
	if c >= utf8.RuneSelf {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == 0x85 || r == 0x2028 || r == 0x2029 {
			return size
		}
	}
	return 0
}

// iterRuleLines is _iter_rule_lines: str.splitlines() semantics, yielding
// slices of s (no copies).
func iterRuleLines(s string, yield func(string) bool) {
	start := 0
	for i := 0; i < len(s); {
		if w := lineEndAt(s, i); w > 0 {
			if !yield(s[start:i]) {
				return
			}
			i += w
			start = i
			continue
		}
		i++
	}
	if start < len(s) {
		yield(s[start:])
	}
}

// pyWordRune reports whether r matches the Unicode \w of Python's re.
func pyWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}
