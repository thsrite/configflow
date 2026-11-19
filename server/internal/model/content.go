package model

import (
	"bytes"
	"encoding/json"
)

// JSONHasContent reports the native JSON equivalent of an omitempty field:
// null, false, an empty string/container or numeric zero has no display content.
// It is for optional legacy presentation/protocol members, never identity or
// authorization. Numbers follow the standard decoder's IEEE-754 interpretation.
func JSONHasContent(data json.RawMessage) (bool, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return false, nil
	}
	if err := validJSON(data); err != nil {
		return false, err
	}
	switch data[0] {
	case 'n':
		return false, nil
	case 'f':
		return false, nil
	case 't':
		return true, nil
	case '"':
		var text string
		err := json.Unmarshal(data, &text)
		return text != "", err
	case '[':
		var items []json.RawMessage
		err := json.Unmarshal(data, &items)
		return len(items) > 0, err
	case '{':
		var fields Fields
		err := json.Unmarshal(data, &fields)
		return fields.Len() > 0, err
	default:
		var number json.Number
		if err := json.Unmarshal(data, &number); err != nil {
			return false, err
		}
		value, _ := number.Float64()
		return value != 0, nil
	}
}
