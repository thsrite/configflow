package rules

import (
	"encoding/json"
	"strings"

	"configflow/internal/model"
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

// EnabledField reads a stored enable flag using the shared JSON switch rule.
func EnabledField(raw json.RawMessage, fallback bool) bool {
	return model.Switch(raw, fallback)
}
