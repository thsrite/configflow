package testjson

import (
	"encoding/json"
	"testing"
)

// Decode converts a test fixture to T through encoding/json. It is test-only
// and must never be used by production code.
func Decode[T any](t testing.TB, value any) T {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
