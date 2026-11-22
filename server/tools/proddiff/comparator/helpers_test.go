package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

const jsonType = "application/json"

// repoRoot is the checkout root (the comparator runs from there).
func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing source location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}

var realKeys = sync.OnceValues(func() (*keySet, error) {
	_, file, _, _ := runtime.Caller(0)
	return loadKeys(filepath.Clean(filepath.Join(filepath.Dir(file), "../../../..")))
})

// testComparator uses the real public key set of this tree (read-only).
func testComparator(t testing.TB) *comparator {
	t.Helper()
	keys, err := realKeys()
	if err != nil {
		t.Fatal(err)
	}
	return &comparator{keys: keys}
}

// jsonBody encodes a Go value (maps are emitted with sorted keys).
func jsonBody(t testing.TB, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rep(body []byte, contentType string, status int, headers map[string]string) reply {
	return httpReply(status, contentType, body, headers)
}

func jrep(t testing.TB, v any) reply {
	t.Helper()
	if b, ok := v.([]byte); ok {
		return rep(b, jsonType, 200, nil)
	}
	if s, ok := v.(string); ok {
		return rep([]byte(s), jsonType, 200, nil)
	}
	return rep(jsonBody(t, v), jsonType, 200, nil)
}

func diffsOf(t testing.TB, c *comparator, left, right reply, route string, rules []rule) []diff {
	t.Helper()
	found, err := c.responseDiffs(left, right, route, rules, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func contains(found []diff, want diff) bool {
	for _, d := range found {
		if d == want {
			return true
		}
	}
	return false
}

func assertDiffs(t testing.TB, got, want []diff) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("differences = %v, want %v", got, want)
	}
}

func mustRule(t testing.TB, fields map[string]any) rule {
	t.Helper()
	m := newMap()
	for _, k := range []string{"route", "path", "reason", "pattern", "tolerance", "bracket", "unordered", "timestamp_format", "mode"} {
		if v, ok := fields[k]; ok {
			switch x := v.(type) {
			case int:
				m.Set(k, int64(x))
			default:
				m.Set(k, x)
			}
		}
	}
	if !m.Has("reason") {
		m.Set("reason", "test")
	}
	route, _ := fields["route"].(string)
	r, _, err := validRule(m, map[string]bool{route: true}, "go")
	if err != nil {
		t.Fatalf("rule %v: %v", fields, err)
	}
	return r
}

func mkdirAll(t testing.TB, file string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t testing.TB, file, text string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
