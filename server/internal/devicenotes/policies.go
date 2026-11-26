package devicenotes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"configflow/internal/clientrange"
)

// Policy is the client identity, display name and route mode stored by OxiDNS.
type Policy struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}

// ParsePolicies validates legacy policy fields, including repairing DEL in names.
func ParsePolicies(raw json.RawMessage) ([]Policy, error) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil || len(entries) > 512 {
		return nil, errors.New(PoliciesMessage)
	}
	rows := make([]Policy, 0, len(entries))
	seen := map[string]bool{}
	for _, raw := range entries {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil, errors.New(PolicyFieldsMessage)
		}
		for key := range fields {
			if key != "ip" && key != "name" && key != "mode" {
				return nil, errors.New(PolicyFieldsMessage)
			}
		}
		var ip *string
		if json.Unmarshal(fields["ip"], &ip) != nil || ip == nil {
			return nil, errors.New(AddressMessage)
		}
		canonical, ok := clientrange.Normalize(*ip)
		if !ok {
			return nil, errors.New(AddressMessage)
		}
		if seen[canonical] {
			return nil, fmt.Errorf(duplicatePolicyMessage, canonical)
		}
		seen[canonical] = true
		name := ""
		if value, exists := fields["name"]; exists {
			var text *string
			if json.Unmarshal(value, &text) != nil || text == nil {
				return nil, errors.New(NameMessage)
			}
			name = *text
		}
		name, err := normalizePolicyName(name)
		if err != nil {
			return nil, err
		}
		var mode string
		if json.Unmarshal(fields["mode"], &mode) != nil || mode != "smart" && mode != "direct" {
			return nil, errors.New(PolicyModeMessage)
		}
		rows = append(rows, Policy{canonical, name, mode})
	}
	return rows, nil
}

func normalizePolicies(rows []Policy) ([]Policy, error) {
	if len(rows) > 512 {
		return nil, errors.New(PoliciesMessage)
	}
	result := make([]Policy, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		ip, ok := clientrange.Normalize(row.IP)
		if !ok {
			return nil, errors.New(AddressMessage)
		}
		if seen[ip] {
			return nil, fmt.Errorf(duplicatePolicyMessage, ip)
		}
		seen[ip] = true
		name, err := normalizePolicyName(row.Name)
		if err != nil {
			return nil, err
		}
		if row.Mode != "smart" && row.Mode != "direct" {
			return nil, errors.New(PolicyModeMessage)
		}
		result = append(result, Policy{ip, name, row.Mode})
	}
	return result, nil
}

func normalizePolicyName(name string) (string, error) {
	if utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return "", errors.New(NameMessage)
	}
	return strings.TrimSpace(strings.ReplaceAll(name, "\x7f", "")), nil
}
