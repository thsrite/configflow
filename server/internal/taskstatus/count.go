package taskstatus

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var decimalCounter = regexp.MustCompile(`^[+-]?[0-9]+(_[0-9]+)*$`)

// Count accepts legacy decimal strings without losing large counters.
// Floating values use the native IEEE-754 value and truncate toward zero.
func Count(raw json.RawMessage) (*big.Int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("false")) || bytes.Equal(raw, []byte(`""`)) || bytes.Equal(raw, []byte("[]")) || bytes.Equal(raw, []byte("{}")) {
		return new(big.Int), nil
	}
	if bytes.Equal(raw, []byte("true")) {
		return big.NewInt(1), nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
		if !decimalCounter.MatchString(text) {
			return nil, errors.New("counter must contain a decimal integer")
		}
		if count, ok := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 10); ok {
			return count, nil
		}
		return nil, errors.New("counter must contain a decimal integer")
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, err
	}
	if !strings.ContainsAny(number.String(), ".eE") {
		if count, ok := new(big.Int).SetString(number.String(), 10); ok {
			return count, nil
		}
		return nil, errors.New("invalid integer counter")
	}
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, errors.New("counter must be finite")
	}
	count, _ := new(big.Float).SetFloat64(value).Int(nil)
	return count, nil
}
