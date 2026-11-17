// Package rules serves profile rule composition, DNS choices and public cached
// providers. Shared syntax, fetching, matching and routing remain with their owners.
package rules

import (
	"encoding/json"
	"fmt"
	"strings"
)

type record map[string]json.RawMessage

func (r record) text(key string) string { value, _ := textValue(r[key]); return value }
func textValue(raw json.RawMessage) (string, bool) {
	var value *string
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return "", false
	}
	return *value, true
}
func stringJSON(value string) json.RawMessage { raw, _ := json.Marshal(value); return raw }
func boolJSON(value bool) json.RawMessage {
	if value {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}
func (r record) clone() record {
	out := record{}
	for key, value := range r {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}
func (r record) object(key string) record {
	var out record
	if json.Unmarshal(r[key], &out) != nil || out == nil {
		return record{}
	}
	return out
}
func (r record) list(key string) ([]record, error) {
	out := []record{}
	if len(r[key]) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(r[key], &out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", key, err)
	}
	if out == nil {
		out = []record{}
	}
	return out, nil
}
func (r record) set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	r[key] = raw
	return nil
}
func defaultText(r record, key, fallback string) string {
	if _, ok := r[key]; !ok {
		return fallback
	}
	return r.text(key)
}
func exactTrue(raw json.RawMessage) bool { return strings.TrimSpace(string(raw)) == "true" }
func notFalse(raw json.RawMessage) bool  { return strings.TrimSpace(string(raw)) != "false" }
func find(items []record, id string) int {
	for i, item := range items {
		if item.text("id") == id {
			return i
		}
	}
	return -1
}

type mutationResult struct {
	Success        bool     `json:"success"`
	Data           record   `json:"data,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
	PinnedWithheld *bool    `json:"pinned_withheld,omitempty"`
}
type fault struct {
	status  int
	message string
}

func (e *fault) Error() string     { return e.message }
func missing(message string) error { return &fault{404, message} }
