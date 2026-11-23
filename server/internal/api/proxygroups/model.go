// Package proxygroups manages profile policy groups. Membership, health,
// listener editing and DNS routing remain owned by their shared packages.
package proxygroups

import (
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	shared "configflow/internal/rules"
)

// member is one request member; see jsondecode.Member.
type member[T any] = jsondecode.Member[T]

// groupFields is a policy group as sent: its members in order, as JSON. Group
// edits keep the request's members as sent until the profile record decodes
// them strictly: the kind-specific cleanup drops members whatever their type,
// and the repository's input messages (shared with document import) are
// defined on the JSON of the resulting group list.
type groupFields struct{ model.Fields }

func (f groupFields) text(key string) string {
	raw, _ := f.Get(key)
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
func (f *groupFields) set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return f.Set(key, raw)
}

// members are the group's candidate and filter members as sent.
func (f groupFields) members() (shared.GroupMembers, error) {
	data, err := f.MarshalJSON()
	if err != nil {
		return shared.GroupMembers{}, err
	}
	return shared.DecodeGroupMembers(data)
}

// enabled reads the group's enable switch.
func (f groupFields) enabled() bool {
	raw, ok := f.Get("enabled")
	if !ok {
		return true
	}
	return shared.EnabledField(raw, true)
}

func find(groups []model.ProxyGroup, id string) int {
	for i := range groups {
		if groups[i].ID == id {
			return i
		}
	}
	return -1
}

type fault struct {
	status  int
	message string
}

func (e *fault) Error() string      { return e.message }
func bad(message string) error      { return &fault{400, message} }
func conflict(message string) error { return &fault{409, message} }
func absent(message string) error   { return &fault{404, message} }

type result struct {
	Success         bool         `json:"success"`
	Data            *groupFields `json:"data,omitempty"`
	Warnings        []string     `json:"warnings,omitempty"`
	ListenerChanges []string     `json:"listener_changes,omitempty"`
	Message         string       `json:"message,omitempty"`
}
