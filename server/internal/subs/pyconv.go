package subs

import (
	"math"
	"math/big"
	"sort"
	"strings"
	"unicode"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
)

// errPy stands for any Python exception that the Python callers swallow or
// wrap; the text is Python's str(exception) where it matters.
type errPy struct{ msg string }

func (e *errPy) Error() string { return e.msg }

func pyErr(msg string) error { return &errPy{msg} }

var errValue = pyErr("ValueError")

// unpack2 is `a, b = s.split(sep, 1)`.
func unpack2(s, sep string) (string, string, error) {
	a, b, ok := strings.Cut(s, sep)
	if !ok {
		return "", "", errValue
	}
	return a, b, nil
}

// pyIsSpace is str.isspace for one character.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pyStrip is str.strip().
func pyStrip(s string) string {
	return strings.TrimFunc(s, pyIsSpace)
}

// digitValue returns the decimal value of a Unicode Nd character.
func digitValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	if r < 128 || !unicode.Is(unicode.Nd, r) {
		return 0, false
	}
	// Nd characters come in contiguous runs 0..9; adjacent runs (the
	// mathematical digits U+1D7CE..U+1D7FF are five) start a block at a 0,
	// so the value is the distance from the block start modulo ten.
	lo := r
	for unicode.Is(unicode.Nd, lo-1) {
		lo--
	}
	return int(r-lo) % 10, true
}

// pyIntStr is int(s) for a str.
func pyIntStr(s string) (jsonx.Number, error) {
	// int() normalises Unicode whitespace to ASCII space but only strips the
	// ASCII whitespace characters (not \x1c-\x1f).
	t := strings.TrimFunc(s, func(r rune) bool {
		if r < 127 {
			return r == ' ' || (r >= '\t' && r <= '\r')
		}
		return unicode.IsSpace(r)
	})
	neg := false
	if strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-") {
		neg = t[0] == '-'
		t = t[1:]
	}
	if t == "" {
		return "", errValue
	}
	var digits []byte
	prevUnderscore := true
	for _, r := range t {
		if r == '_' {
			if prevUnderscore {
				return "", errValue
			}
			prevUnderscore = true
			continue
		}
		d, ok := digitValue(r)
		if !ok {
			return "", errValue
		}
		digits = append(digits, byte('0'+d))
		prevUnderscore = false
	}
	if prevUnderscore {
		return "", errValue
	}
	if len(digits) > 4300 {
		return "", errValue
	}
	n, ok := new(big.Int).SetString(string(digits), 10)
	if !ok {
		return "", errValue
	}
	if neg {
		n.Neg(n)
	}
	return jsonx.BigInt(n), nil
}

// pyInt is int(v) for a JSON/YAML value.
func pyInt(v any) (jsonx.Number, error) {
	switch x := v.(type) {
	case string:
		return pyIntStr(x)
	case bool:
		if x {
			return jsonx.Int(1), nil
		}
		return jsonx.Int(0), nil
	case jsonx.Number:
		if x.IsInt() {
			return x, nil
		}
		f := x.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", pyErr("cannot convert float to integer")
		}
		b, _ := new(big.Float).SetFloat64(math.Trunc(f)).Int(nil)
		return jsonx.BigInt(b), nil
	}
	return "", pyErr("int() argument must be a string, a bytes-like object or a real number")
}

// pyTypeName is type(v).__name__.
func pyTypeName(v any) string {
	switch x := v.(type) {
	case nil:
		return "NoneType"
	case string:
		return "str"
	case bool:
		return "bool"
	case jsonx.Number:
		if x.IsInt() {
			return "int"
		}
		return "float"
	case *jsonx.Object:
		return "dict"
	case []any:
		return "list"
	}
	return "object"
}

// pyLower is str.lower() (full case mapping of the few characters where it
// differs from Go's simple mapping).
func pyLower(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
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
		switch r {
		case 0x130:
			b.WriteString("i̇")
		case 'Σ':
			if finalSigma(runes, i) {
				b.WriteRune('ς')
			} else {
				b.WriteRune('σ')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isCased(r rune) bool { return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) }

func isCaseIgnorable(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r) || r == '\'' || r == '.' || r == ':' ||
		r == '^' || r == '`' || r == 0xb7 || r == 0x2019
}

func finalSigma(runes []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		if isCaseIgnorable(runes[j]) {
			continue
		}
		before = isCased(runes[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(runes); j++ {
		if isCaseIgnorable(runes[j]) {
			continue
		}
		return !isCased(runes[j])
	}
	return true
}

// pyIsDigit is str.isdigit() for a non-empty-or-empty string (empty is false).
func pyIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(unicode.IsDigit(r) || unicode.Is(unicode.No, r) && isDigitProperty(r)) {
			return false
		}
	}
	return true
}

// isDigitProperty covers the No-category characters Python counts as digits
// (superscripts, circled digits ...), i.e. those with a digit value.
func isDigitProperty(r rune) bool {
	switch {
	case r == 0xb2 || r == 0xb3 || r == 0xb9:
		return true
	case r >= 0x2070 && r <= 0x2079 && r != 0x2071 && r != 0x2072 && r != 0x2073:
		return true
	case r >= 0x2080 && r <= 0x2089:
		return true
	case r >= 0x2460 && r <= 0x2468, r >= 0x2474 && r <= 0x247c, r >= 0x2488 && r <= 0x2490,
		r >= 0x24ea && r <= 0x24ea, r >= 0x24f5 && r <= 0x24fd, r == 0x24ff,
		r >= 0x2776 && r <= 0x277e, r >= 0x2780 && r <= 0x2788, r >= 0x278a && r <= 0x2792,
		r >= 0x10a40 && r <= 0x10a43, r >= 0x1f100 && r <= 0x1f10a:
		return true
	}
	return false
}

// ParseQS is urllib.parse.parse_qs(query): blank values are dropped and the
// values of a repeated key are collected in order.
func ParseQS(query string) (keys []string, values map[string][]string) {
	values = map[string][]string{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value, ok := strings.Cut(pair, "=")
		if !ok || value == "" {
			continue
		}
		name = weburl.UnquotePlus(name)
		value = weburl.UnquotePlus(value)
		if _, seen := values[name]; !seen {
			keys = append(keys, name)
		}
		values[name] = append(values[name], value)
	}
	return keys, values
}

// ---------------------------------------------------------------- ipaddress

type ipInfo struct {
	text        string
	unspecified bool
	multicast   bool
	v6          bool
	loopback    bool
}

// pyIPAddress is the temporary old view for unported callers. Parsing,
// canonical text and address flags all come from the native address owner.
func pyIPAddress(text string) (ipInfo, bool) {
	addr, ok := parseHostIP(text)
	if !ok {
		return ipInfo{}, false
	}
	flags := addr.WithZone("").Unmap()
	return ipInfo{text: addr.String(), unspecified: flags.IsUnspecified(), multicast: flags.IsMulticast(), v6: addr.Is6(), loopback: flags.IsLoopback()}, true
}

func sortStrings(s []string) { sort.Strings(s) }

func getOr(o *jsonx.Object, key string, fallback any) any {
	if v, ok := o.Get(key); ok {
		return v
	}
	return fallback
}
func isString(v any) bool { _, ok := v.(string); return ok }
