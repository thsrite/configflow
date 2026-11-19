package monitor

// Metric arithmetic retains exact integral counters and finite float64 samples.
// It does not convert an integral counter through float64 for comparisons.

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// errOverflow identifies arithmetic outside the finite float64 range.
var errOverflow = errors.New("metric number is outside float64 range")

// num is an exact integral counter or a floating sample.
type num struct {
	isFloat bool
	i       *big.Int
	f       float64
}

// toFloat converts a counter to a finite floating value.
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

// isFinite checks a value against the finite float64 range.
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

// cmp compares exact values; callers exclude NaN.
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

// trueDiv rounds an exact rational quotient to float64 and checks its range.
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

// MetricNumber retains exact integral counters and finite floating samples.
// A nil pointer denotes an unknown observation, distinct from zero.
type MetricNumber struct{ num }

// MarshalJSON preserves integral counters and floating sample kinds.
func (n MetricNumber) MarshalJSON() ([]byte, error) {
	if n.isFloat {
		if math.IsNaN(n.f) || math.IsInf(n.f, 0) {
			return nil, errOverflow
		}
		text := strconv.FormatFloat(n.f, 'g', -1, 64)
		if !strings.ContainsAny(text, ".eE") {
			text += ".0"
		}
		return []byte(text), nil
	}
	if n.i == nil {
		return []byte("null"), nil
	}
	return []byte(n.i.String()), nil
}

// UnmarshalJSON accepts only native JSON number tokens.
func (n *MetricNumber) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return errors.New("metric value must be a JSON number")
	}
	var value json.Number
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	text := value.String()
	if !strings.ContainsAny(text, ".eE") {
		i, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return errors.New("invalid metric integer")
		}
		n.num = num{i: i}
		return nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return errOverflow
	}
	n.num = num{isFloat: true, f: f}
	return nil
}
func intNum(i int64) num              { return num{i: big.NewInt(i)} }
func metricInt(i int64) *MetricNumber { return &MetricNumber{num: intNum(i)} }
func metricEqual(a, b *MetricNumber) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.num.equal(b.num)
}
func metricSum(values ...*MetricNumber) *MetricNumber {
	if len(values) == 0 {
		return nil
	}
	total := intNum(0)
	for _, value := range values {
		if value == nil {
			return nil
		}
		var err error
		total, err = total.add(value.num)
		if err != nil {
			return nil
		}
	}
	finite, err := total.isFinite()
	if err != nil || !finite {
		return nil
	}
	return &MetricNumber{num: total}
}
func metricRatio(n, d *MetricNumber, percentage bool) *MetricNumber {
	if n == nil || d == nil || d.num.equal(intNum(0)) || percentage && d.num.less(n.num) {
		return nil
	}
	value, err := n.num.trueDiv(d.num)
	if err != nil {
		return nil
	}
	if percentage {
		value *= 100
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &MetricNumber{num: num{isFloat: true, f: value}}
}

func cloneMetric(n *MetricNumber) *MetricNumber {
	if n == nil {
		return nil
	}
	out := *n
	if n.i != nil {
		out.i = new(big.Int).Set(n.i)
	}
	return &out
}
