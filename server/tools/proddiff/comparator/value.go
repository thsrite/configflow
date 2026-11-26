package main

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// The comparator works on one dynamic value model shared by JSON, YAML, ZIP
// and text bodies. It keeps the distinctions the previous comparator drew:
//
//	nil      null / None
//	bool     booleans (never equal in type to int)
//	int64, *big.Int   integers ("int")
//	float64  floats ("float")
//	string   text
//	[]byte   undecodable non-YAML bodies ("bytes")
//	[]any    lists
//	*Map     mappings in document order
//
// Numbers compare like Python: 1 == 1.0 == true for equality and mapping keys,
// while differences() still reports a separate type class for them.

// Map is an insertion-ordered mapping. Keys are hashable scalars.
type Map struct {
	keys  []any
	vals  []any
	index map[string]int
}

func newMap() *Map { return &Map{index: map[string]int{}} }

// Len returns the number of entries.
func (m *Map) Len() int { return len(m.keys) }

// Get returns the value stored under key.
func (m *Map) Get(key any) (any, bool) {
	h, ok := hashKey(key)
	if !ok {
		return nil, false
	}
	i, ok := m.index[h]
	if !ok {
		return nil, false
	}
	return m.vals[i], true
}

// GetString is Get for string keys.
func (m *Map) GetString(key string) (any, bool) { return m.Get(key) }

// Has reports whether key is present.
func (m *Map) Has(key any) bool {
	_, ok := m.Get(key)
	return ok
}

// Set replaces the value of an existing key in place or appends a new entry.
func (m *Map) Set(key, value any) {
	h, ok := hashKey(key)
	if !ok {
		return
	}
	if i, ok := m.index[h]; ok {
		m.vals[i] = value
		return
	}
	m.index[h] = len(m.keys)
	m.keys = append(m.keys, key)
	m.vals = append(m.vals, value)
}

// Delete removes key, keeping the order of the remaining entries.
func (m *Map) Delete(key any) bool {
	h, ok := hashKey(key)
	if !ok {
		return false
	}
	i, ok := m.index[h]
	if !ok {
		return false
	}
	m.keys = append(m.keys[:i], m.keys[i+1:]...)
	m.vals = append(m.vals[:i], m.vals[i+1:]...)
	delete(m.index, h)
	for j := i; j < len(m.keys); j++ {
		hj, _ := hashKey(m.keys[j])
		m.index[hj] = j
	}
	return true
}

// Keys returns the keys in order.
func (m *Map) Keys() []any { return m.keys }

// hashKey maps equal Python keys (1, 1.0, True) to one string. Lists and
// mappings are unhashable.
func hashKey(key any) (string, bool) {
	switch k := key.(type) {
	case nil:
		return "z", true
	case string:
		return "s" + k, true
	case bool:
		if k {
			return "n1", true
		}
		return "n0", true
	case int64:
		return "n" + strconv.FormatInt(k, 10), true
	case *big.Int:
		return "n" + k.String(), true
	case float64:
		if i, ok := floatInt(k); ok {
			return "n" + i.String(), true
		}
		return "f" + strconv.FormatFloat(k, 'g', -1, 64), true
	case []byte:
		return "b" + string(k), true
	}
	return "", false
}

// floatInt returns the exact integer value of an integral finite float.
func floatInt(f float64) (*big.Int, bool) {
	if math.IsInf(f, 0) || math.IsNaN(f) || f != math.Trunc(f) {
		return nil, false
	}
	i, _ := new(big.Float).SetFloat64(f).Int(nil)
	return i, true
}

func bigOf(v any) (*big.Int, bool) {
	switch n := v.(type) {
	case int64:
		return big.NewInt(n), true
	case *big.Int:
		return n, true
	}
	return nil, false
}

// pyType names a value's class the way the old comparator distinguished types.
func pyType(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case int64, *big.Int:
		return "int"
	case float64:
		return "float"
	case string:
		return "str"
	case []byte:
		return "bytes"
	case []any:
		return "list"
	case *Map:
		return "dict"
	}
	return fmt.Sprintf("%T", v)
}

func isNumber(v any) bool {
	switch v.(type) {
	case int64, *big.Int, float64:
		return true
	}
	return false
}

// numericEqual compares two numbers (bools count as 0/1) exactly.
func numericEqual(a, b any) bool {
	if x, ok := a.(bool); ok {
		a = boolInt(x)
	}
	if x, ok := b.(bool); ok {
		b = boolInt(x)
	}
	af, aFloat := a.(float64)
	bf, bFloat := b.(float64)
	switch {
	case aFloat && bFloat:
		return af == bf
	case aFloat:
		ai, ok := floatInt(af)
		bi, _ := bigOf(b)
		return ok && ai.Cmp(bi) == 0
	case bFloat:
		bi, ok := floatInt(bf)
		ai, _ := bigOf(a)
		return ok && ai.Cmp(bi) == 0
	}
	ai, _ := bigOf(a)
	bi, _ := bigOf(b)
	return ai.Cmp(bi) == 0
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func isNumeric(v any) bool {
	_, ok := v.(bool)
	return ok || isNumber(v)
}

// pyEqual is Python ==: numbers across int/float/bool, order-free mappings.
func pyEqual(a, b any) bool {
	if isNumeric(a) && isNumeric(b) {
		return numericEqual(a, b)
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []byte:
		y, ok := b.([]byte)
		return ok && bytes.Equal(x, y)
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !pyEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case *Map:
		y, ok := b.(*Map)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for i, k := range x.keys {
			other, ok := y.Get(k)
			if !ok || !pyEqual(x.vals[i], other) {
				return false
			}
		}
		return true
	}
	return false
}

// pyStr is Python str() for the value model.
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(x, 10)
	case *big.Int:
		return x.String()
	case float64:
		return pyFloatRepr(x)
	case string:
		return x
	}
	return pyRepr(v)
}

// pyRepr approximates Python repr() for containers (only used for odd inputs).
func pyRepr(v any) string {
	switch x := v.(type) {
	case string:
		return "'" + strings.ReplaceAll(strings.ReplaceAll(x, `\`, `\\`), "'", `\'`) + "'"
	case []byte:
		return "b" + pyRepr(string(x))
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = pyRepr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Map:
		parts := make([]string, x.Len())
		for i, k := range x.keys {
			parts[i] = pyRepr(k) + ": " + pyRepr(x.vals[i])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return pyStr(v)
}

// pyFloatRepr is Python repr(float): shortest round-trip digits, fixed
// notation for exponents in [-4, 16), always with a fractional part.
func pyFloatRepr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	sign := ""
	if s[0] == '-' {
		sign, s = "-", s[1:]
	}
	mantissa, expText, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mantissa, ".", "", 1)
	if exp < -4 || exp >= 16 {
		m := digits[:1]
		if len(digits) > 1 {
			m += "." + digits[1:]
		}
		es := "+"
		if exp < 0 {
			es, exp = "-", -exp
		}
		return fmt.Sprintf("%s%se%s%02d", sign, m, es, exp)
	}
	point := exp + 1
	var out string
	switch {
	case point <= 0:
		out = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		out = digits + strings.Repeat("0", point-len(digits)) + ".0"
	default:
		out = digits[:point] + "." + digits[point:]
	}
	return sign + out
}

// deepCopy copies lists and mappings; scalars are immutable.
func deepCopy(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = deepCopy(item)
		}
		return out
	case *Map:
		out := newMap()
		for i, k := range x.keys {
			out.Set(k, deepCopy(x.vals[i]))
		}
		return out
	}
	return v
}

// sortedKeys returns the union of both mappings' keys, ordered by str(key).
func unionKeys(a, b *Map) []any {
	seen := map[string]bool{}
	var keys []any
	for _, m := range []*Map{a, b} {
		for _, k := range m.keys {
			h, _ := hashKey(k)
			if !seen[h] {
				seen[h] = true
				keys = append(keys, k)
			}
		}
	}
	sort.SliceStable(keys, func(i, j int) bool { return pyStr(keys[i]) < pyStr(keys[j]) })
	return keys
}

// truthy is Python truthiness.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case *big.Int:
		return x.Sign() != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []byte:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case *Map:
		return x.Len() > 0
	}
	return true
}

// get returns m[key] when v is a mapping holding key.
func get(v any, key string) (any, bool) {
	m, ok := v.(*Map)
	if !ok {
		return nil, false
	}
	return m.Get(key)
}

// getOr is Python dict.get(key, fallback) on a value that should be a mapping.
func getOr(v any, key string, fallback any) any {
	if value, ok := get(v, key); ok {
		return value
	}
	return fallback
}
