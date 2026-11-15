package check

import (
	"math"
	"math/big"

	"configflow/internal/jsonx"
)

// pnum is a Python int (exact) or float.
type pnum struct {
	isInt bool
	i     *big.Int
	f     float64
}

func numberOf(n jsonx.Number) pnum {
	if n.IsInt() {
		i, _ := n.BigInt()
		return pnum{isInt: true, i: i}
	}
	return pnum{f: n.Float64()}
}

func intNum(i int64) pnum { return pnum{isInt: true, i: big.NewInt(i)} }

func floatNum(f float64) pnum { return pnum{f: f} }

// asNumber is a value used in arithmetic: ints, floats and bools (True is
// 1); anything else raises the TypeError of the operator.
func asNumber(v any, op string, other any) pnum {
	switch value := v.(type) {
	case bool:
		if value {
			return intNum(1)
		}
		return intNum(0)
	case jsonx.Number:
		return numberOf(value)
	}
	raise("TypeError", "unsupported operand type(s) for "+op+": '"+TypeName(v)+"' and '"+TypeName(other)+"'")
	return pnum{}
}

// toFloat is float(n), correctly rounded; OverflowError beyond the range.
func (n pnum) toFloat() float64 {
	if !n.isInt {
		return n.f
	}
	f, _ := new(big.Float).SetInt(n.i).Float64()
	if math.IsInf(f, 0) {
		raise("OverflowError", "int too large to convert to float")
	}
	return f
}

// cmpNum compares exactly like Python (int vs float without rounding); ok
// is false when a NaN takes part (every comparison is False).
func cmpNum(a, b pnum) (int, bool) {
	if a.isInt && b.isInt {
		return a.i.Cmp(b.i), true
	}
	if (!a.isInt && math.IsNaN(a.f)) || (!b.isInt && math.IsNaN(b.f)) {
		return 0, false
	}
	return toBigFloat(a).Cmp(toBigFloat(b)), true
}

func toBigFloat(n pnum) *big.Float {
	if n.isInt {
		return new(big.Float).SetInt(n.i)
	}
	return new(big.Float).SetFloat64(n.f) // ±Inf allowed
}

func less(a, b pnum) bool          { c, ok := cmpNum(a, b); return ok && c < 0 }
func lessEqual(a, b pnum) bool     { c, ok := cmpNum(a, b); return ok && c <= 0 }
func equalNum(a, b pnum) bool      { c, ok := cmpNum(a, b); return ok && c == 0 }
func greaterEq(a, b pnum) bool     { c, ok := cmpNum(a, b); return ok && c >= 0 }
func isZeroNum(a pnum) bool        { return equalNum(a, intNum(0)) }
func compareNumbers(a, b pnum) int { c, _ := cmpNum(a, b); return c }

// trueDiv is a / b (Python true division: int / int correctly rounded).
func trueDiv(a, b pnum) pnum {
	if isZeroNum(b) {
		raise("ZeroDivisionError", "division by zero")
	}
	if a.isInt && b.isInt {
		f, _ := new(big.Rat).SetFrac(a.i, b.i).Float64()
		if math.IsInf(f, 0) {
			raise("OverflowError", "integer division result too large for a float")
		}
		return floatNum(f)
	}
	return floatNum(a.toFloat() / b.toFloat())
}

func add(a, b pnum) pnum {
	if a.isInt && b.isInt {
		return pnum{isInt: true, i: new(big.Int).Add(a.i, b.i)}
	}
	return floatNum(a.toFloat() + b.toFloat())
}

func mul(a, b pnum) pnum {
	if a.isInt && b.isInt {
		return pnum{isInt: true, i: new(big.Int).Mul(a.i, b.i)}
	}
	return floatNum(a.toFloat() * b.toFloat())
}

// round is round(n) with no ndigits: ints unchanged, floats to the nearest
// int with ties to even (ValueError for NaN, OverflowError for infinity).
func round(n pnum) *big.Int {
	if n.isInt {
		return n.i
	}
	if math.IsNaN(n.f) {
		raise("ValueError", "cannot convert float NaN to integer")
	}
	if math.IsInf(n.f, 0) {
		raise("OverflowError", "cannot convert float infinity to integer")
	}
	i, _ := new(big.Float).SetFloat64(math.RoundToEven(n.f)).Int(nil)
	return i
}

func (n pnum) str() string {
	if n.isInt {
		return n.i.String()
	}
	return jsonx.Float(n.f).String()
}
