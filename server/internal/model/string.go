package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// String is text that also accepts lossless JSON number tokens. Null reads as
// absent text; booleans and containers are incompatible. DecodeRecord applies
// the same codec to ordinary string fields, pointers, lists and ordered maps.
type String string

// UnmarshalJSON reads text without rounding numeric literals through float64.
func (s *String) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	if len(data) > 0 && (data[0] == '-' || data[0] >= '0' && data[0] <= '9') {
		var number json.Number
		if err := json.Unmarshal(data, &number); err != nil {
			return fmt.Errorf("%w: numeric text: %w", ErrType, err)
		}
		*s = String(number.String())
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%w: text required: %w", ErrType, err)
	}
	*s = String(text)
	return nil
}

// TextValue is the text of an optional text member ("" when absent).
func TextValue(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}
