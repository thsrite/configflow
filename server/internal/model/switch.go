package model

import (
	"bytes"
	"encoding/json"
)

// SwitchMember reads a single effective stored member without encoding the
// enclosing record.
type SwitchMember interface {
	MemberJSON(name string) (json.RawMessage, bool, error)
}

// JSONSwitchEnabled evaluates a stored JSON boolean switch.
// Missing or whitespace-only bytes return defaultValue. Explicit false, null,
// numeric zero (0, -0, 0.0, 0e0, ...), empty string "", empty array [] and
// empty object {} return false. Every other JSON value returns true.
func JSONSwitchEnabled(raw json.RawMessage, defaultValue bool) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return defaultValue
	}
	switch raw[0] {
	case 'f', 'n':
		return false
	case 't':
		return true
	case '"':
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return text != ""
		}
		return true
	case '[', '{':
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if _, err := decoder.Token(); err != nil {
			return true
		}
		return decoder.More()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return !jsonNumberIsZero(raw)
	default:
		return true
	}
}

// SwitchEnabled reads a stored boolean switch on a typed model record without
// encoding unrelated members. A non-nil typed pointer takes the fast path;
// nil inspects the record's raw member JSON through JSONSwitchEnabled.
func SwitchEnabled(record SwitchMember, name string, current *bool, defaultValue bool) bool {
	if current != nil {
		return *current
	}
	if record == nil {
		return defaultValue
	}
	raw, present, err := record.MemberJSON(name)
	if err != nil || !present {
		return defaultValue
	}
	return JSONSwitchEnabled(raw, defaultValue)
}

func jsonNumberIsZero(raw []byte) bool {
	i := 0
	if i < len(raw) && raw[i] == '-' {
		i++
	}
	digits := 0
	for i < len(raw) {
		c := raw[i]
		if c == 'e' || c == 'E' {
			i++
			if i < len(raw) && (raw[i] == '+' || raw[i] == '-') {
				i++
			}
			expDigits := 0
			for i < len(raw) {
				if raw[i] < '0' || raw[i] > '9' {
					return false
				}
				expDigits++
				i++
			}
			return digits > 0 && expDigits > 0
		}
		if c == '.' {
			i++
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
		if c != '0' {
			return false
		}
		digits++
		i++
	}
	return digits > 0
}
