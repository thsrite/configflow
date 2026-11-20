package difftest

import "testing"

func TestN811LibraryRecordRequestValidation(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, item := range []map[string]any{
		{"id": nil, "name": "Bad ID"},
		{"id": "bad-name", "name": false},
		{"id": "bad-policy", "name": "Policy", "policy": "DIRECT"},
		{"id": "bad-target", "name": "Target", "target": "DIRECT"},
		{"id": "record", "name": "Record", "description": map[string]any{"preserve": true}, "future": []any{nil, 42, "<raw>"}},
	} {
		item["source_type"] = "content"
		item["behavior"] = "domain"
		item["format"] = "text"
		item["content"] = "+.records.example\n"
		w.call("POST", "/api/rule-library", body(item), true)
	}
	w.call("GET", "/api/rule-library", "", true)
	for _, payload := range []map[string]any{
		{"content": "example.com", "behavior": "domain", "format": 123},
		{"content": "example.com", "behavior": "domain", "format": true},
		{"content": "example.com", "behavior": "domain", "format": nil},
		{"content": "example.com", "behavior": "domain", "format": ""},
		{"content": "example.com", "behavior": "domain"},
	} {
		w.call("POST", "/api/rule-library/check-content", body(payload), true)
	}
}
