package rules

import (
	"encoding/json"
	"strings"
)

// IdentifierList formats the identifier list embedded in Chinese reorder errors.
func IdentifierList(ids []string) string {
	quoted := make([]string, len(ids))
	escape := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	for i, id := range ids {
		quoted[i] = "'" + escape.Replace(id) + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// InvalidValueText renders rejected JSON input in a validation message.
func InvalidValueText(raw json.RawMessage) string {
	var text string
	if len(raw) == 0 {
		return "null"
	}
	if raw[0] == '"' && json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(raw))
}

// EnabledField reads a stored enable flag. Legacy empty JSON values disable it;
// absent members retain the caller's default.
func EnabledField(raw json.RawMessage, fallback bool) bool {
	if len(raw) == 0 {
		return fallback
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return fallback
	}
	switch text {
	case "null", "false", `""`, "[]", "{}":
		return false
	case "true":
		return true
	}
	switch text[0] {
	case '"':
		var value string
		return json.Unmarshal(raw, &value) != nil || value != ""
	case '[':
		var values []json.RawMessage
		return json.Unmarshal(raw, &values) != nil || len(values) > 0
	case '{':
		var values map[string]json.RawMessage
		return json.Unmarshal(raw, &values) != nil || len(values) > 0
	default:
		var n json.Number
		if json.Unmarshal(raw, &n) == nil {
			value, err := n.Float64()
			if err == nil {
				return value != 0
			}
		}
	}
	return true
}
