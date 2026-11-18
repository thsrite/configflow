package mosdns

import (
	"configflow/internal/model"
	"encoding/json"
)

// RuleProxyCredentials contains literal capability strings from the captured
// settings. General R5 number-to-text compatibility is never an authority grant.
type RuleProxyCredentials struct {
	Current string
	Retired []string
}

// ProxyCredentials validates stored credential evidence before it is consumed.
// The existing stored-member guard is shared with public rule-proxy admission.
func ProxyCredentials(settings model.SystemConfig) RuleProxyCredentials {
	fields := storedMembers(settings)
	value := RuleProxyCredentials{Current: credentialMember(fields, "rule_proxy_token")}
	if data, ok := fields.Get("retired_rule_proxy_tokens"); ok {
		var entries []json.RawMessage
		if json.Unmarshal(data, &entries) == nil {
			for _, entry := range entries {
				var token string
				if json.Unmarshal(entry, &token) == nil && token != "" {
					value.Retired = append(value.Retired, token)
				}
			}
		}
	}
	return value
}
