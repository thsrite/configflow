// Package pystr reproduces CPython 3.11 string operations whose result depends
// on the Unicode database or on CPython's own parsing rules: str.lower(),
// str.upper(), str.isspace()/strip(), str.isidentifier(), int(str, base) and
// float(str).
//
// Go's unicode package carries a newer Unicode version than CPython 3.11
// (Unicode 14.0.0), and str.lower() uses the full case mapping plus the
// Final_Sigma rule, so the tables in tables.go are generated from CPython
// itself (testdata/gen_tables.py).
package pystr

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
)

func inRanges(table [][2]rune, r rune) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i][1] >= r })
	return i < len(table) && table[i][0] <= r
}

// IsSpace is str.isspace() for one character (Py_UNICODE_ISSPACE).
func IsSpace(r rune) bool {
	if r < 128 {
		return r == ' ' || (r >= '\t' && r <= '\r') || (r >= 0x1c && r <= 0x1f)
	}
	return inRanges(spaceRanges, r)
}

// Strip is str.strip() without arguments.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// Decimal is Py_UNICODE_TODECIMAL: the value of a decimal digit (category Nd
// in Unicode 14.0.0), or -1.
func Decimal(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	if r < 128 {
		return -1
	}
	i := sort.Search(len(decimalRuns), func(i int) bool { return decimalRuns[i][1] >= r })
	if i < len(decimalRuns) && decimalRuns[i][0] <= r {
		return int(decimalRuns[i][2] + r - decimalRuns[i][0])
	}
	return -1
}

func lowerOf(r rune) (string, bool) {
	i := sort.Search(len(lowerTable), func(i int) bool { return lowerTable[i].r >= r })
	if i < len(lowerTable) && lowerTable[i].r == r {
		return lowerTable[i].s, true
	}
	return "", false
}

// Lower is str.lower(): full case mapping (U+0130 becomes "i̇") and the
// Final_Sigma rule for U+03A3, with CPython 3.11's tables.
func Lower(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r == 0x3A3 {
			if finalSigma(runes, i) {
				b.WriteRune(0x3C2)
			} else {
				b.WriteRune(0x3C3)
			}
			continue
		}
		if lower, ok := lowerOf(r); ok {
			b.WriteString(lower)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Upper is str.upper(): the full case mapping (U+00DF becomes "SS", U+0390
// three characters) with CPython 3.11's tables.
func Upper(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	for _, r := range s {
		i := sort.Search(len(upperTable), func(i int) bool { return upperTable[i].r >= r })
		if i < len(upperTable) && upperTable[i].r == r {
			b.WriteString(upperTable[i].s)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsIdentifier is str.isidentifier(): the first character is XID_Start or
// "_", the others XID_Continue (Unicode 14.0.0).
func IsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && !inRanges(identStartRanges, r) {
				return false
			}
		} else if !inRanges(identContinueRanges, r) {
			return false
		}
	}
	return true
}

func finalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && inRanges(caseIgnorableRanges, runes[j]) {
		j--
	}
	if j < 0 || !inRanges(casedRanges, runes[j]) {
		return false
	}
	j = i + 1
	for j < len(runes) && inRanges(caseIgnorableRanges, runes[j]) {
		j++
	}
	return j == len(runes) || !inRanges(casedRanges, runes[j])
}

// transform is _PyUnicode_TransformDecimalAndSpaceToASCII: characters below
// U+007F stay, other white space becomes " ", decimal digits become ASCII
// digits; the first other character becomes "?" and ends the text.
func transform(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 127:
			b.WriteRune(r)
		case IsSpace(r):
			b.WriteByte(' ')
		default:
			d := Decimal(r)
			if d < 0 {
				b.WriteByte('?')
				return b.String()
			}
			b.WriteByte(byte('0' + d))
		}
	}
	return b.String()
}

func asciiSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// ValueError is a Python ValueError raised by int() or float().
type ValueError struct{ Msg string }

func (e *ValueError) Error() string { return e.Msg }

// truncatedRepr is %.200R: repr() cut to 200 characters.
func truncatedRepr(s string) string {
	repr := jsonx.Repr(s)
	if utf8.RuneCountInString(repr) > 200 {
		repr = string([]rune(repr)[:200])
	}
	return repr
}

// MaxStrDigits is sys.get_int_max_str_digits() (the default limit).
const MaxStrDigits = 4300

func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 99
}

// Int is int(s, base) for a str (base 0 or 2..36): Unicode digits and
// white space as CPython transforms them, single underscores between digits,
// a base prefix where the base allows it, the 4300 digit limit for bases
// that are not powers of two, and CPython's ValueError texts.
func Int(s string, base int) (*big.Int, error) {
	invalid := func() error {
		return &ValueError{fmt.Sprintf("invalid literal for int() with base %d: %s", base, truncatedRepr(s))}
	}
	text := transform(s)
	i, n := 0, len(text)
	for i < n && asciiSpace(text[i]) {
		i++
	}
	negative := false
	if i < n && (text[i] == '+' || text[i] == '-') {
		negative = text[i] == '-'
		i++
	}
	b := base
	prefixed := false
	if i+1 < n && text[i] == '0' {
		p := text[i+1] | 0x20
		switch {
		case p == 'x' && (b == 0 || b == 16):
			b, prefixed = 16, true
		case p == 'o' && (b == 0 || b == 8):
			b, prefixed = 8, true
		case p == 'b' && (b == 0 || b == 2):
			b, prefixed = 2, true
		}
	}
	if prefixed {
		i += 2
	}
	// Base 0 with a leading zero is decimal that must come out as zero
	// ("0", "00", "0_0"; "010" is refused).
	errorIfNonzero := false
	if b == 0 {
		b = 10
		errorIfNonzero = i < n && text[i] == '0'
	}
	var digits strings.Builder
	// After a base prefix one underscore may come before the first digit.
	previousUnderscore := !prefixed
	for i < n {
		c := text[i]
		if c == '_' {
			if previousUnderscore {
				return nil, invalid()
			}
			previousUnderscore = true
			i++
			continue
		}
		if digitValue(c) >= b {
			break
		}
		digits.WriteByte(c)
		previousUnderscore = false
		i++
	}
	if previousUnderscore && digits.Len() > 0 {
		return nil, invalid()
	}
	if b&(b-1) != 0 && digits.Len() > MaxStrDigits {
		return nil, &ValueError{fmt.Sprintf("Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", MaxStrDigits, digits.Len())}
	}
	if digits.Len() == 0 {
		return nil, invalid()
	}
	for i < n && asciiSpace(text[i]) {
		i++
	}
	if i != n {
		return nil, invalid()
	}
	value, ok := new(big.Int).SetString(digits.String(), b)
	if !ok || (errorIfNonzero && value.Sign() != 0) {
		return nil, invalid()
	}
	if negative {
		value.Neg(value)
	}
	return value, nil
}

// Float is float(s) for a str: Unicode digits and white space as CPython
// transforms them, single underscores between digits, inf/infinity/nan in
// any case, no hexadecimal; an out-of-range exponent gives ±inf or ±0.
func Float(s string) (float64, error) {
	invalid := func() error {
		return &ValueError{"could not convert string to float: " + jsonx.Repr(s)}
	}
	text := strings.TrimFunc(transform(s), func(r rune) bool { return r < 128 && asciiSpace(byte(r)) })
	if strings.Contains(text, "_") {
		// _Py_string_to_number_with_underscores: only between two digits.
		for i := 0; i < len(text); i++ {
			if text[i] == '_' && (i == 0 || i+1 == len(text) || !isDigit(text[i-1]) || !isDigit(text[i+1])) {
				return 0, invalid()
			}
		}
		text = strings.ReplaceAll(text, "_", "")
	}
	body := text
	sign := 1.0
	if body != "" && (body[0] == '+' || body[0] == '-') {
		if body[0] == '-' {
			sign = -1
		}
		body = body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		return sign * math.Inf(1), nil
	case "nan":
		return math.Copysign(math.NaN(), sign), nil
	}
	if !decimalFloat(body) {
		return 0, invalid()
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return value, nil
		}
		return 0, invalid()
	}
	return value, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// decimalFloat is the grammar of _Py_dg_strtod without sign: digits with an
// optional fraction (at least one digit in total) and an optional exponent.
func decimalFloat(s string) bool {
	i, n := 0, len(s)
	digits := 0
	for i < n && isDigit(s[i]) {
		i++
		digits++
	}
	if i < n && s[i] == '.' {
		i++
		for i < n && isDigit(s[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return false
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < n && isDigit(s[i]) {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == n
}
