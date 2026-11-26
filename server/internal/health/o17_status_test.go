package health

import (
	"bytes"
	"encoding/json"
	"testing"
)

// o17PythonNoneStatus maps the region evidence text for a probe without an
// HTTP status back to the recorded Python text ("HTTP None") so the recorded
// references still compare; since O17 users see "无 HTTP 响应".
func o17PythonNoneStatus(t *testing.T, value any) any {
	t.Helper()
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil || !bytes.Contains(raw, []byte("无 HTTP 响应")) {
		return value
	}
	var mapped any
	decoder := json.NewDecoder(bytes.NewReader(bytes.ReplaceAll(raw, []byte("无 HTTP 响应"), []byte("HTTP None"))))
	decoder.UseNumber()
	if err := decoder.Decode(&mapped); err != nil {
		t.Fatal(err)
	}
	return mapped
}
