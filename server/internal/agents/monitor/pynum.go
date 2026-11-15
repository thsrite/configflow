package monitor

// Python numbers and strings as oxidns_metrics.py, oxidns_dashboard_state.py
// and oxidns_monitor.py use them: ints are exact (any size), floats float64,
// mixed arithmetic converts the int with correct rounding (OverflowError
// beyond the float range), comparisons between int and float are exact.

import (
	"errors"
	"math"
	"math/big"
	"sort"
	"strings"
	"unicode"

	"configflow/internal/jsonx"
)

// errOverflow is Python's OverflowError (int too large to convert to float,
// or a true division result too large for a float).
var errOverflow = errors.New("OverflowError")

// num is a Python int (exact) or float.
type num struct {
	isFloat bool
	i       *big.Int
	f       float64
}

// asNum is a jsonx.Number as a num; ok false for anything else (bool too:
// callers check bool where Python does).
func asNum(value any) (num, bool) {
	n, ok := value.(jsonx.Number)
	if !ok {
		return num{}, false
	}
	if n.IsInt() {
		i, ok := n.BigInt()
		if !ok {
			return num{}, false
		}
		return num{i: i}, true
	}
	return num{isFloat: true, f: n.Float64()}, true
}

func intNum(i int64) num { return num{i: big.NewInt(i)} }

// value is the num as a jsonx value.
func (n num) value() any {
	if n.isFloat {
		return jsonx.Float(n.f)
	}
	return jsonx.BigInt(new(big.Int).Set(n.i))
}

// toFloat is float(n): PyLong_AsDouble for an int.
func (n num) toFloat() (float64, error) {
	if n.isFloat {
		return n.f, nil
	}
	f, _ := new(big.Float).SetInt(n.i).Float64()
	if math.IsInf(f, 0) {
		return 0, errOverflow
	}
	return f, nil
}

// isFinite is math.isfinite(n): an int beyond the float range is an
// OverflowError.
func (n num) isFinite() (bool, error) {
	f, err := n.toFloat()
	if err != nil {
		return false, err
	}
	return !math.IsInf(f, 0) && !math.IsNaN(f), nil
}

func (n num) isNaN() bool { return n.isFloat && math.IsNaN(n.f) }

// rat is the exact value of a finite num.
func (n num) rat() *big.Rat {
	if n.isFloat {
		return new(big.Rat).SetFloat64(n.f)
	}
	return new(big.Rat).SetInt(n.i)
}

// cmp compares exactly like Python (no NaN: callers exclude it). Infinite
// floats are beyond every int.
func (n num) cmp(m num) int {
	if !n.isFloat && !m.isFloat {
		return n.i.Cmp(m.i)
	}
	if n.isFloat && m.isFloat {
		switch {
		case n.f < m.f:
			return -1
		case n.f > m.f:
			return 1
		}
		return 0
	}
	if n.isFloat && math.IsInf(n.f, 0) {
		if n.f > 0 {
			return 1
		}
		return -1
	}
	if m.isFloat && math.IsInf(m.f, 0) {
		if m.f > 0 {
			return -1
		}
		return 1
	}
	return n.rat().Cmp(m.rat())
}

// less is n < m (False with a NaN).
func (n num) less(m num) bool { return !n.isNaN() && !m.isNaN() && n.cmp(m) < 0 }

// equal is n == m (False with a NaN).
func (n num) equal(m num) bool { return !n.isNaN() && !m.isNaN() && n.cmp(m) == 0 }

// add is n + m.
func (n num) add(m num) (num, error) {
	if !n.isFloat && !m.isFloat {
		return num{i: new(big.Int).Add(n.i, m.i)}, nil
	}
	a, err := n.toFloat()
	if err != nil {
		return num{}, err
	}
	b, err := m.toFloat()
	if err != nil {
		return num{}, err
	}
	return num{isFloat: true, f: a + b}, nil
}

// sub is n - m.
func (n num) sub(m num) (num, error) {
	if !n.isFloat && !m.isFloat {
		return num{i: new(big.Int).Sub(n.i, m.i)}, nil
	}
	a, err := n.toFloat()
	if err != nil {
		return num{}, err
	}
	b, err := m.toFloat()
	if err != nil {
		return num{}, err
	}
	return num{isFloat: true, f: a - b}, nil
}

// trueDiv is n / m for m != 0: an int quotient is correctly rounded
// (long_true_divide), OverflowError when it does not fit a float.
func (n num) trueDiv(m num) (float64, error) {
	if !n.isFloat && !m.isFloat {
		f, _ := new(big.Rat).SetFrac(n.i, m.i).Float64()
		if math.IsInf(f, 0) {
			return 0, errOverflow
		}
		if f == 0 && (n.i.Sign() < 0) != (m.i.Sign() < 0) && n.i.Sign() != 0 {
			f = math.Copysign(0, -1)
		} else if f == 0 && n.i.Sign() == 0 && m.i.Sign() < 0 {
			f = math.Copysign(0, -1)
		}
		return f, nil
	}
	a, err := n.toFloat()
	if err != nil {
		return 0, err
	}
	b, err := m.toFloat()
	if err != nil {
		return 0, err
	}
	return a / b, nil
}

// pyNumberEqual is a == b for two JSON values: numbers by value (1 == 1.0),
// None == None, anything else by jsonx.Equal.
func pyNumberEqual(a, b any) bool {
	x, okA := asNum(a)
	y, okB := asNum(b)
	if okA && okB {
		return x.equal(y)
	}
	return jsonx.Equal(a, b)
}

// isPyInt is type(v) is int.
func isPyInt(v any) bool {
	n, ok := v.(jsonx.Number)
	return ok && n.IsInt()
}

// pyIntIn is type(v) is int and lo <= v <= hi.
func pyIntIn(v any, lo, hi int64) bool {
	n, ok := v.(jsonx.Number)
	if !ok || !n.IsInt() {
		return false
	}
	i, ok := n.BigInt()
	return ok && i.Cmp(big.NewInt(lo)) >= 0 && i.Cmp(big.NewInt(hi)) <= 0
}

// pyInt64 is the value of an int already checked with pyIntIn.
func pyInt64(v any) int64 {
	i, _ := v.(jsonx.Number).Int64()
	return i
}

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
