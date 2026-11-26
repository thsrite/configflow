package configgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
)

// ErrBaseURL identifies a non-text URL override in a valid request object.
var ErrBaseURL = errors.New(msgBaseURLText)

// RequestBaseURL reads the exact base_url member of a generation request. The
// legacy empty body alternatives mean no override. The field itself permits
// only null or text, preventing invalid generated URLs from implicit coercion.
func RequestBaseURL(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		value, emptyErr := baseURLValue(raw)
		if emptyErr == nil && value == "" {
			return "", nil
		}
		return "", errors.New("generation request must be an object")
	}
	value := fields["base_url"]
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", ErrBaseURL
	}
	return text, nil
}
func baseURLValue(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("false")) {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	switch raw[0] {
	case '[':
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) == nil && len(values) == 0 {
			return "", nil
		}
	case '{':
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) == nil && len(values) == 0 {
			return "", nil
		}
	default:
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			value, err := strconv.ParseFloat(string(number), 64)
			if err == nil && value == 0 {
				return "", nil
			}
		}
	}
	return "", errors.New("base_url must be text")
}
