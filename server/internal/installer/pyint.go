package installer

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/agents/diag"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
)

// maxStrDigits is CPython 3.11's default sys.get_int_max_str_digits().
const maxStrDigits = 4300

// pyInt is int(value) for the values the generators receive: Go integers,
// *big.Int and str. A str follows int(str): the ValueError texts are
// CPython's, including the digit limit (checked on the leading run of digits
// before trailing characters are rejected, as CPython does).
func pyInt(value any) (*big.Int, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case int:
		return big.NewInt(int64(v)), nil
	case int64:
		return big.NewInt(v), nil
	case *big.Int:
		return new(big.Int).Set(v), nil
	case string:
		if digits := leadingDigitRun(v); digits > maxStrDigits {
			return nil, valueError(fmt.Sprintf("Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", maxStrDigits, digits))
		}
		n, ok := httpx.PyParseInt(v)
		if !ok {
			return nil, valueError("invalid literal for int() with base 10: " + jsonx.Repr(v))
		}
		return n, nil
	}
	panic(fmt.Sprintf("pyInt: unsupported %T", value))
}

// leadingDigitRun counts the digits CPython scans before the limit check:
// after whitespace and a sign, digits with single underscores between them.
func leadingDigitRun(text string) int {
	s := strings.TrimLeftFunc(text, func(r rune) bool {
		return r == ' ' || (r >= '\t' && r <= '\r') || (r >= 127 && diag.IsPySpace(r))
	})
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	digits, previous := 0, rune(0)
	for _, r := range s {
		if r == '_' {
			if previous == '_' {
				return 0
			}
		} else if unicode.IsDigit(r) {
			digits++
		} else {
			break
		}
		previous = r
	}
	if previous == '_' {
		return 0
	}
	return digits
}

// pyStr is str(value) for the same values.
func pyStr(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case *big.Int:
		return v.String()
	case string:
		return v
	}
	panic(fmt.Sprintf("pyStr: unsupported %T", value))
}

func inRange(n *big.Int, low, high int64) bool {
	return n.Cmp(big.NewInt(low)) >= 0 && n.Cmp(big.NewInt(high)) <= 0
}

// asciiDigits is re.fullmatch(r"[0-9]+", text).
func asciiDigits(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// matchesClass is re.fullmatch(r"[<class>]+", text) for an ASCII class given
// as the set of allowed bytes besides letters and digits.
func matchesClass(text, extra string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(extra, c) >= 0) {
			return false
		}
	}
	return true
}

// isUnitName is re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.@-]*", unit).
func isUnitName(unit string) bool {
	return unit != "" && matchesClass(unit[:1], "_") && (len(unit) == 1 || matchesClass(unit[1:], "_.@-"))
}
