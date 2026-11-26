package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"strconv"
)

// Truthy reports the truth value of one JSON value as Python's bool() of the
// decoded value: null, false, numeric zero (0, -0, 0.0, 0e5, and a number
// that rounds to 0.0 such as 1e-400), "", [] and {} are false, every other
// value is true. Empty input is false; input that is not one valid JSON
// value is ErrJSON. It is for protocol and request members that Python
// tested for truth (an Agent reply's flags, an optional display member),
// never for identity or authorization.
func Truthy(data json.RawMessage) (bool, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return false, nil
	}
	if err := validJSON(data); err != nil {
		return false, err
	}
	return truthy(data), nil
}

// Switch is the truth value of an optional JSON switch: absent (empty input)
// when it is missing, otherwise Truthy; a value that is not valid JSON is on.
func Switch(data json.RawMessage, absent bool) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return absent
	}
	if validJSON(data) != nil {
		return true
	}
	return truthy(data)
}

// truthy is Truthy of one valid, trimmed JSON value.
func truthy(data []byte) bool {
	switch data[0] {
	case 'f', 'n':
		return false
	case 't':
		return true
	case '"':
		return len(data) > 2
	case '[', '{':
		dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true), jsontext.AllowDuplicateNames(true))
		if _, err := dec.ReadToken(); err != nil {
			return true
		}
		kind := dec.PeekKind()
		return kind != ']' && kind != '}'
	default:
		// A float that underflows to 0.0 is false in Python; an overflow
		// (±Inf) is true. An integer literal is never rounded to zero.
		value, _ := strconv.ParseFloat(string(data), 64)
		return value != 0
	}
}

// Flag reads an optional stored switch: nil (absent) is fallback. A stored
// null or wrong type was already resolved when the record was decoded.
func Flag(current *bool, fallback bool) bool {
	if current == nil {
		return fallback
	}
	return *current
}
