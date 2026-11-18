package check

import (
	"bytes"
	"encoding/json"
	"errors"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
)

// Text is a string field that accepts JSON string values and lossless JSON
// number literals as text, while rejecting booleans, arrays, and objects.
type Text string

// UnmarshalJSON decodes a JSON string, a lossless JSON number literal, or null.
func (t *Text) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*t = ""
		return nil
	}
	value, err := decodeNonNullText(trimmed)
	if err != nil {
		return err
	}
	*t = Text(value)
	return nil
}

// MarshalJSON encodes Text as a UTF-8 JSON string without HTML escaping.
func (t Text) MarshalJSON() ([]byte, error) {
	return jsonresp.Encode(string(t))
}

// NullableText represents a JSON string or null value, accepting lossless JSON
// number literals when non-null.
type NullableText struct {
	Value string
	Valid bool
}

// UnmarshalJSON decodes null as an invalid NullableText, and a JSON string or
// number literal as a valid text value.
func (t *NullableText) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*t = NullableText{}
		return nil
	}
	value, err := decodeNonNullText(trimmed)
	if err != nil {
		return err
	}
	*t = NullableText{Value: value, Valid: true}
	return nil
}

// MarshalJSON encodes an invalid NullableText as null and a valid one as a
// UTF-8 JSON string without HTML escaping.
func (t NullableText) MarshalJSON() ([]byte, error) {
	if !t.Valid {
		return []byte("null"), nil
	}
	return jsonresp.Encode(t.Value)
}

// StrictString is a string field that accepts only JSON string values (not
// null, numbers, booleans, arrays, or objects).
type StrictString string

// UnmarshalJSON decodes only a JSON string token.
func (s *StrictString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return errors.New(msgStrictString)
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	*s = StrictString(value)
	return nil
}

// MarshalJSON encodes StrictString as a UTF-8 JSON string without HTML escaping.
func (s StrictString) MarshalJSON() ([]byte, error) {
	return jsonresp.Encode(string(s))
}

func decodeNonNullText(trimmed []byte) (string, error) {
	if len(trimmed) == 0 {
		return "", errors.New(msgTextScalarRequired)
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	if trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9') {
		if !json.Valid(trimmed) {
			return "", errors.New(msgTextScalarRequired)
		}
		return string(trimmed), nil
	}
	return "", errors.New(msgTextScalarRequired)
}

func isJSONIntLiteral(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '-' {
		trimmed = trimmed[1:]
		if len(trimmed) == 0 {
			return false
		}
	}
	if trimmed[0] == '0' {
		return len(trimmed) == 1
	}
	if trimmed[0] < '1' || trimmed[0] > '9' {
		return false
	}
	for _, b := range trimmed[1:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

func compactRaw(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(bytes.TrimSpace(raw))
	}
	return buf.String()
}

type rawJSONObject struct {
	Fields model.Fields
	raw    model.Raw
}

func (o *rawJSONObject) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, o, &o.raw)
}

func (o rawJSONObject) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&o, &o.raw)
}
