package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// TextLines is the two stored forms of a MosDNS matches editor: multiline text
// or a list of expressions. Text != nil selects text; otherwise Lines selects
// the list form. A decoded null has the zero value; its containing Raw retains
// the original null until changed. No string conversion is done while loading.
type TextLines struct {
	Text  *string
	Lines []string
}

// UnmarshalJSON accepts either text or a string list without changing its form.
func (m *TextLines) UnmarshalJSON(data []byte) error {
	text := bytes.TrimSpace(data)
	var result TextLines
	if bytes.Equal(text, []byte("null")) {
		*m = result
		return nil
	}
	if len(text) == 0 {
		return fmt.Errorf("decode matches: %w", ErrType)
	}
	switch text[0] {
	case '"':
		var value string
		if err := json.Unmarshal(text, &value); err != nil {
			return fmt.Errorf("decode matches text: %w", err)
		}
		result.Text = &value
	case '[':
		if err := json.Unmarshal(text, &result.Lines); err != nil {
			return fmt.Errorf("decode matches lines: %w", err)
		}
	default:
		return fmt.Errorf("decode matches: %w", ErrType)
	}
	*m = result
	return nil
}

// MarshalJSON writes the selected form; a nil list is an empty JSON array.
func (m TextLines) MarshalJSON() ([]byte, error) {
	if m.Text != nil {
		return Marshal(*m.Text)
	}
	return Marshal(m.Lines)
}
