package installer

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"configflow/internal/pystr"
)

// pyInt is int(value) for the values the generators receive: Go integers,
// *big.Int and str. A str follows int(str) (shared pystr port: CPython 3.11
// Unicode digits and white space, the 4300 digit limit, ValueError texts with
// the %.200R repr).
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
		n, err := pystr.Int(v, 10)
		if err != nil {
			var pyErr *pystr.ValueError
			if errors.As(err, &pyErr) {
				return nil, valueError(pyErr.Msg)
			}
			return nil, err
		}
		return n, nil
	}
	panic(fmt.Sprintf("pyInt: unsupported %T", value))
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

// PyInt is int(value) (exported for the routes' type=int arguments).
func PyInt(value any) (*big.Int, error) { return pyInt(value) }

// PyStr is str(value) of an int or str argument.
func PyStr(value any) string { return pyStr(value) }
