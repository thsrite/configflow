package metrics

// Python arithmetic on JSON values, for the statistics of metrics_history.py:
// ints are exact (any size), floats are float64, bools count as ints, and
// anything else fails with CPython's TypeError text. Only the operations the
// statistics use are provided.

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"configflow/internal/jsonx"
)

// numeric is a Python int (exact) or float.
type numeric struct {
	isFloat bool
	i       *big.Int
	f       float64
}

func asNumeric(value any) (numeric, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return numeric{i: big.NewInt(1)}, true
		}
		return numeric{i: big.NewInt(0)}, true
	case jsonx.Number:
		if v.IsInt() {
			i, ok := v.BigInt()
			if !ok {
				return numeric{}, false
			}
			return numeric{i: i}, true
		}
		return numeric{isFloat: true, f: v.Float64()}, true
	}
	return numeric{}, false
}

func (n numeric) value() any {
	if n.isFloat {
		return jsonx.Float(n.f)
	}
	return jsonx.BigInt(n.i)
}

var errIntTooLarge = &OverflowError{"int too large to convert to float"}

// intToFloat is PyLong_AsDouble: correctly rounded, OverflowError beyond
// the float range.
func intToFloat(i *big.Int) (float64, error) {
	f, _ := new(big.Float).SetInt(i).Float64()
	if math.IsInf(f, 0) {
		return 0, errIntTooLarge
	}
	return f, nil
}

func (n numeric) float() (float64, error) {
	if n.isFloat {
		return n.f, nil
	}
	return intToFloat(n.i)
}

func operandError(op string, left, right any) error {
	return &TypeError{fmt.Sprintf("unsupported operand type(s) for %s: '%s' and '%s'", op, pyTypeName(left), pyTypeName(right))}
}

// pyAdd and pySub are a + b and a - b.
func pyAdd(a, b any) (any, error) { return arith("+", a, b) }
func pySub(a, b any) (any, error) { return arith("-", a, b) }

func arith(op string, a, b any) (any, error) {
	x, okA := asNumeric(a)
	y, okB := asNumeric(b)
	if !okA || !okB {
		return nil, operandError(op, a, b)
	}
	if !x.isFloat && !y.isFloat {
		if op == "+" {
			return jsonx.BigInt(new(big.Int).Add(x.i, y.i)), nil
		}
		return jsonx.BigInt(new(big.Int).Sub(x.i, y.i)), nil
	}
	fx, err := x.float()
	if err != nil {
		return nil, err
	}
	fy, err := y.float()
	if err != nil {
		return nil, err
	}
	if op == "+" {
		return jsonx.Float(fx + fy), nil
	}
	return jsonx.Float(fx - fy), nil
}

// pyTrueDivFloat is a / d for a float d.
func pyTrueDivFloat(a any, d float64) (any, error) {
	x, ok := asNumeric(a)
	if !ok {
		return nil, &TypeError{fmt.Sprintf("unsupported operand type(s) for /: '%s' and 'float'", pyTypeName(a))}
	}
	fx, err := x.float()
	if err != nil {
		return nil, err
	}
	return jsonx.Float(fx / d), nil
}

// compareKind is the result of comparing two Python numbers.
type compareKind int

const (
	less compareKind = iota - 1
	equal
	greater
	unordered // NaN
)

func ratOf(f float64) *big.Rat { return new(big.Rat).SetFloat64(f) }

// compareNumeric compares exactly, as Python compares ints and floats.
func compareNumeric(x, y numeric) compareKind {
	if x.isFloat && math.IsNaN(x.f) || y.isFloat && math.IsNaN(y.f) {
		return unordered
	}
	if x.isFloat && y.isFloat {
		switch {
		case x.f < y.f:
			return less
		case x.f > y.f:
			return greater
		}
		return equal
	}
	inf := func(n numeric) int {
		if n.isFloat && math.IsInf(n.f, 1) {
			return 1
		}
		if n.isFloat && math.IsInf(n.f, -1) {
			return -1
		}
		return 0
	}
	if ix, iy := inf(x), inf(y); ix != 0 || iy != 0 {
		switch {
		case ix < iy:
			return less
		case ix > iy:
			return greater
		}
		return equal
	}
	rx := new(big.Rat)
	if x.isFloat {
		rx = ratOf(x.f)
	} else {
		rx.SetInt(x.i)
	}
	ry := new(big.Rat)
	if y.isFloat {
		ry = ratOf(y.f)
	} else {
		ry.SetInt(y.i)
	}
	return compareKind(rx.Cmp(ry))
}

func compareError(op string, a, b any) error {
	return &TypeError{fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, pyTypeName(a), pyTypeName(b))}
}

// pyLess is a < b; pyGreater is a > b; pyGreaterEqual is a >= b.
func pyLess(a, b any) (bool, error) {
	x, okA := asNumeric(a)
	y, okB := asNumeric(b)
	if !okA || !okB {
		return false, compareError("<", a, b)
	}
	return compareNumeric(x, y) == less, nil
}

func pyGreater(a, b any) (bool, error) {
	x, okA := asNumeric(a)
	y, okB := asNumeric(b)
	if !okA || !okB {
		return false, compareError(">", a, b)
	}
	return compareNumeric(x, y) == greater, nil
}

func pyGreaterEqual(a, b any) (bool, error) {
	x, okA := asNumeric(a)
	y, okB := asNumeric(b)
	if !okA || !okB {
		return false, compareError(">=", a, b)
	}
	kind := compareNumeric(x, y)
	return kind == greater || kind == equal, nil
}

// pySum is sum(values) (CPython 3.11: exact int accumulation until the
// first float, then float64 additions; no compensated summation).
func pySum(values []any) (any, error) {
	var result any = jsonx.Int(0)
	for _, item := range values {
		next, err := pyAdd(result, item)
		if err != nil {
			return nil, err
		}
		result = next
	}
	return result, nil
}

// pyMax and pyMin are max(values) / min(values) of a non-empty list: the
// first item that no later item exceeds (or undercuts), as the original
// object.
func pyMax(values []any) (any, error) {
	best := values[0]
	for _, item := range values[1:] {
		bigger, err := pyGreater(item, best)
		if err != nil {
			return nil, err
		}
		if bigger {
			best = item
		}
	}
	return best, nil
}

func pyMin(values []any) (any, error) {
	best := values[0]
	for _, item := range values[1:] {
		smaller, err := pyLess(item, best)
		if err != nil {
			return nil, err
		}
		if smaller {
			best = item
		}
	}
	return best, nil
}

// pyMean is sum(values) / len(values) for a non-empty list.
func pyMean(values []any) (any, error) {
	total, err := pySum(values)
	if err != nil {
		return nil, err
	}
	n, _ := asNumeric(total)
	count := int64(len(values))
	if n.isFloat {
		return jsonx.Float(n.f / float64(count)), nil
	}
	quotient, _ := new(big.Rat).SetFrac(n.i, big.NewInt(count)).Float64()
	if math.IsInf(quotient, 0) {
		return nil, &OverflowError{"integer division result too large for a float"}
	}
	return jsonx.Float(quotient), nil
}

// pyGet is value.get(key, default) for a value that should be a dict.
func pyGet(value any, key string, def any) (any, error) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, &AttributeError{fmt.Sprintf("'%s' object has no attribute 'get'", pyTypeName(value))}
	}
	if v, present := object.Get(key); present {
		return v, nil
	}
	return def, nil
}

// pyContains is `key in container`.
func pyContains(container any, key string) (bool, error) {
	switch v := container.(type) {
	case *jsonx.Object:
		if v != nil {
			return v.Has(key), nil
		}
	case string:
		return strings.Contains(v, key), nil
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok && text == key {
				return true, nil
			}
		}
		return false, nil
	}
	return false, &TypeError{fmt.Sprintf("argument of type '%s' is not iterable", pyTypeName(container))}
}

// pyIndex is container[key] after `key in container` was true.
func pyIndex(container any, key string) (any, error) {
	switch v := container.(type) {
	case *jsonx.Object:
		return v.Value(key), nil
	case string:
		return nil, &TypeError{"string indices must be integers, not 'str'"}
	case []any:
		return nil, &TypeError{"list indices must be integers or slices, not str"}
	}
	return nil, &TypeError{fmt.Sprintf("'%s' object is not subscriptable", pyTypeName(container))}
}
