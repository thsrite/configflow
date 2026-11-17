package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// ErrNumberForm identifies a Number that is not an integer literal.
var ErrNumberForm = errors.New("httpx: JSON value is not an integer literal")

// Number retains a bound JSON field's literal and form without coercion. Use it
// when validation distinguishes integer literals, fractions/exponents, booleans,
// strings, null or containers. Its zero value represents a missing field. It does
// not implement Python's int() coercions; string-valued integers remain strings.
type Number struct{ raw string }

// UnmarshalJSON retains any valid JSON form for the business validator.
func (n *Number) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) {
		return errors.New("httpx: invalid JSON field")
	}
	n.raw = string(bytes.TrimSpace(data))
	return nil
}

// MarshalJSON writes the original value; a missing field writes null.
func (n Number) MarshalJSON() ([]byte, error) {
	if n.raw == "" {
		return []byte("null"), nil
	}
	return []byte(n.raw), nil
}

// Raw returns the literal JSON text, or an empty string for a missing field.
func (n Number) Raw() string { return n.raw }

// IsSet reports whether the JSON member was present, including explicit null.
func (n Number) IsSet() bool { return n.raw != "" }

// IsNumber reports an integer, fraction or exponent literal.
func (n Number) IsNumber() bool {
	return len(n.raw) > 0 && (n.raw[0] == '-' || n.raw[0] >= '0' && n.raw[0] <= '9')
}

// IsIntegerLiteral reports a numeric literal without a fraction or exponent.
func (n Number) IsIntegerLiteral() bool { return n.IsNumber() && !strings.ContainsAny(n.raw, ".eE") }

// IsBool reports a JSON true or false literal.
func (n Number) IsBool() bool { return n.raw == "true" || n.raw == "false" }

// IsString reports a JSON string, without interpreting its contents.
func (n Number) IsString() bool { return len(n.raw) > 0 && n.raw[0] == '"' }

// IsNull reports explicit JSON null, independently of a missing field.
func (n Number) IsNull() bool { return n.raw == "null" }

// IsContainer reports an object or array value.
func (n Number) IsContainer() bool { return len(n.raw) > 0 && (n.raw[0] == '{' || n.raw[0] == '[') }

// Text returns the decoded string contents, or false for another JSON form.
func (n Number) Text() (string, bool) {
	if !n.IsString() {
		return "", false
	}
	var value string
	err := json.Unmarshal([]byte(n.raw), &value)
	return value, err == nil
}

// Int64 parses an integer literal with the native signed 64-bit range. Larger
// integers remain exact in Raw and return strconv.ErrRange through the error.
func (n Number) Int64() (int64, error) {
	if !n.IsIntegerLiteral() {
		return 0, ErrNumberForm
	}
	return strconv.ParseInt(n.raw, 10, 64)
}
