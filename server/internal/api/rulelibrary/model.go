// Package rulelibrary owns shared rule-source editing and connectivity checks.
// Parsing, fetching, native DNS reconstruction and storage stay with their owners.
package rulelibrary

import (
	"encoding/json"
	"errors"

	"configflow/internal/model"
)

type record = model.Fields

func text(r record, key string) string {
	raw, _ := r.Get(key)
	var out string
	_ = json.Unmarshal(raw, &out)
	return out
}
func field(r record, key string) json.RawMessage {
	raw, ok := r.Get(key)
	if !ok {
		return json.RawMessage("null")
	}
	return raw
}
func put[T any](r *record, key string, value T) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.Set(key, raw)
}
func rows(r record, key string) ([]record, error) {
	out := []record{}
	raw, ok := r.Get(key)
	if !ok {
		return out, nil
	}
	err := json.Unmarshal(raw, &out)
	if out == nil {
		out = []record{}
	}
	return out, err
}
func child(r record, key string) record {
	var out record
	_ = json.Unmarshal(field(r, key), &out)
	return out
}
func find(items []record, id string) int {
	for i, item := range items {
		if text(item, "id") == id {
			return i
		}
	}
	return -1
}
func stringField(r record, key string) (string, bool) {
	raw, ok := r.Get(key)
	if !ok || string(raw) == "null" {
		return "", false
	}
	var out string
	err := json.Unmarshal(raw, &out)
	return out, err == nil
}
func stringsField(r record, key string) ([]string, error) {
	raw, ok := r.Get(key)
	if !ok {
		return []string{}, nil
	}
	var out []string
	if string(raw) == "null" {
		return nil, errors.New("expected string array")
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	_ = json.Unmarshal(raw, &entries)
	for _, entry := range entries {
		if string(entry) == "null" {
			return nil, errors.New("expected string array")
		}
	}
	return out, nil
}
func sourceType(r record) string {
	if !r.Has("source_type") {
		return "url"
	}
	return text(r, "source_type")
}

type fault struct {
	status  int
	message string
}

func (e *fault) Error() string              { return e.message }
func fail(status int, message string) error { return &fault{status, message} }

type success struct {
	Success bool `json:"success"`
}
type itemResult struct {
	Success bool   `json:"success"`
	Data    record `json:"data"`
}
