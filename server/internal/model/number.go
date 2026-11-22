package model

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
)

// Number is a JSON number kept as written (Agent metric counters may exceed
// int64, rates may be fractional). The empty Number is no value.
type Number string

// UnmarshalJSON accepts one JSON number.
func (n *Number) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || jsontext.Value(data).Kind() != '0' || !jsontext.Value(data).IsValid() {
		return fmt.Errorf("%w: number expected", ErrType)
	}
	*n = Number(data)
	return nil
}

// MarshalJSON writes the number as stored; the empty Number is 0.
func (n Number) MarshalJSON() ([]byte, error) {
	if n == "" {
		return []byte("0"), nil
	}
	return []byte(n), nil
}
