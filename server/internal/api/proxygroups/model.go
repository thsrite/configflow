// Package proxygroups manages profile policy groups. Membership, health,
// listener editing and DNS routing remain owned by their shared packages.
package proxygroups

import (
	"encoding/json"
	"errors"

	"configflow/internal/model"
)

type object = model.Fields

func text(o object, key string) string {
	raw, _ := o.Get(key)
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
func list(o object, key string) ([]object, error) {
	raw, ok := o.Get(key)
	if !ok {
		return []object{}, nil
	}
	var rows []object
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []object{}
	}
	return rows, nil
}
func value[T any](o object, key string) (T, error) {
	var result T
	raw, ok := o.Get(key)
	if !ok {
		return result, errors.New("missing request field")
	}
	err := json.Unmarshal(raw, &result)
	return result, err
}
func set[T any](o *object, key string, v T) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return o.Set(key, raw)
}
func find(rows []object, id string) int {
	for i, row := range rows {
		if text(row, "id") == id {
			return i
		}
	}
	return -1
}
func objectOf(o object, key string) object {
	raw, _ := o.Get(key)
	var result object
	_ = json.Unmarshal(raw, &result)
	return result
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
	Success         bool     `json:"success"`
	Data            *object  `json:"data,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	ListenerChanges []string `json:"listener_changes,omitempty"`
	Message         string   `json:"message,omitempty"`
}
