package httpxtest

import (
	"encoding/json"
	"net/http"
	"strings"
)

// O9ExpectedHeaders applies only the approved O9-H1/L5 header deltas to the
// Python expectation for absent/foreign Origins. Values on the Go side still
// compare strictly; bodies, status codes and unrelated headers are untouched.
func O9ExpectedHeaders(python, goHeader http.Header) http.Header {
	expected := python.Clone()
	for _, name := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Headers", "Access-Control-Allow-Methods"} {
		expected.Del(name)
	}
	vary := []string{}
	for _, value := range expected.Values("Vary") {
		for _, token := range strings.Split(value, ",") {
			if name := strings.TrimSpace(token); !strings.EqualFold(name, "Origin") && name != "" {
				vary = append(vary, name)
			}
		}
	}
	expected.Del("Vary")
	if len(vary) > 0 {
		expected.Set("Vary", strings.Join(vary, ", "))
	}
	if expected.Get("X-Content-Type-Options") == "" && goHeader.Get("X-Content-Type-Options") == "nosniff" && strings.HasPrefix(goHeader.Get("Content-Type"), "text/") {
		expected.Set("X-Content-Type-Options", "nosniff")
	}
	return expected
}

// O9ExpectedProfileHeader removes only the profile echo on the exact unchanged
// 401 login/session-invalid JSON envelope (O9-L6). Other bodies/statuses retain
// their original expectation and remain visible to the caller's comparison.
func O9ExpectedProfileHeader(python string, pythonStatus, goStatus int, pythonBody, goBody []byte) string {
	if pythonStatus != 401 || goStatus != 401 {
		return python
	}
	decode := func(data []byte) (string, bool) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil || len(fields) != 2 || string(fields["success"]) != "false" {
			return "", false
		}
		var message string
		if json.Unmarshal(fields["message"], &message) != nil {
			return "", false
		}
		return message, message == "请先登录后再操作。" || message == "登录已失效，请重新登录。"
	}
	before, ok := decode(pythonBody)
	after, valid := decode(goBody)
	if ok && valid && before == after {
		return ""
	}
	return python
}
