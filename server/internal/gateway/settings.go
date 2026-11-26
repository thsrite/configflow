package gateway

import (
	"bytes"
	"encoding/json"
	"slices"
	"unicode/utf8"

	"configflow/internal/gateway/gwbase"
)

// Error is the error of a gateway setting that cannot safely be applied
// (Python's LinuxGatewayError). Its message is shown to the user as is.
// Router split errors (routersplit.RouterSplitError) also match it with
// errors.As.
type Error = gwbase.LinuxGatewayError

func gatewayError(message string) error { return &Error{Message: message} }

// Limits of a rule set selection.
const (
	maxRuleSetIDs     = 1024
	maxRuleSetIDRunes = 256
)

// Settings is a profile's mihomo.linux_gateway setting.
type Settings struct {
	Enabled bool `json:"enabled"`
	// RuleSetIDs are rule_configs IDs (rule sets) whose IPv4 networks the
	// gateway takes over, without duplicates; never nil.
	RuleSetIDs []string `json:"rule_set_ids"`
}

// NormalizeSettings validates a linux_gateway value given as JSON (empty or
// null: not set, i.e. disabled) and returns it with duplicate IDs removed.
// Unknown members are ignored. Errors are *Error.
func NormalizeSettings(raw json.RawMessage) (Settings, error) {
	settings := Settings{RuleSetIDs: []string{}}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return settings, nil
	}
	var members map[string]json.RawMessage
	if raw[0] != '{' || json.Unmarshal(raw, &members) != nil {
		return Settings{}, gatewayError(msgSettingsNotObject)
	}
	if value, ok := members["enabled"]; ok {
		if err := json.Unmarshal(value, &settings.Enabled); err != nil || !isJSONBool(value) {
			return Settings{}, gatewayError(msgEnabledNotBool)
		}
	}
	if value, ok := members["rule_set_ids"]; ok {
		ids, valid := ruleSetIDs(value)
		if !valid {
			return Settings{}, gatewayError(msgInvalidRuleSetIDs)
		}
		for _, id := range ids {
			if !slices.Contains(settings.RuleSetIDs, id) {
				settings.RuleSetIDs = append(settings.RuleSetIDs, id)
			}
		}
	}
	return settings, nil
}

func isJSONBool(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	return bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false"))
}

// ruleSetIDs reads a JSON list of rule set IDs: at most 1024 non-empty
// strings of at most 256 characters without control characters.
func ruleSetIDs(value json.RawMessage) ([]string, bool) {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil || items == nil || len(items) > maxRuleSetIDs {
		return nil, false
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		item = bytes.TrimSpace(item)
		var id string
		if len(item) == 0 || item[0] != '"' || json.Unmarshal(item, &id) != nil {
			return nil, false
		}
		if id == "" || utf8.RuneCountInString(id) > maxRuleSetIDRunes {
			return nil, false
		}
		for _, r := range id {
			if r < 32 || r == 127 {
				return nil, false
			}
		}
		ids = append(ids, id)
	}
	return ids, true
}
