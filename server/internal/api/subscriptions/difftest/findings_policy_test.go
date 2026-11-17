package difftest

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// approvedShapeError is Step B #1's exact intentional difference. Response
// effects still compare in call; no other status/message mismatch is accepted.
func approvedShapeError(path, input string, py answer, got *httpxtest.Result) bool {
	if got.Status != 400 {
		return false
	}
	var body struct {
		Success *bool  `json:"success"`
		Message string `json:"message"`
	}
	if json.Unmarshal(got.Body, &body) != nil || body.Success == nil || *body.Success {
		return false
	}
	if !(path == "/api/nodes/reorder" || path == "/api/subscriptions/reorder" || strings.HasPrefix(path, "/api/subscriptions/") && strings.HasSuffix(path, "/fetch")) {
		return false
	}
	var payload any
	if json.Unmarshal([]byte(input), &payload) != nil {
		return false
	}
	if _, object := payload.(map[string]any); !object && payload != nil {
		return py.Status == 500 && body.Message == "请求体必须是 JSON 对象"
	}
	if py.Status == 404 && strings.HasSuffix(path, "/reorder") && body.Message == "id 必须是互不重复的字符串" {
		fields, ok := py.Body.(map[string]any)
		if !ok {
			return false
		}
		message, _ := fields["message"].(string)
		return strings.Contains(message, "['id 必须是互不重复的字符串']")
	}
	return false
}

func TestApprovedShapePolicyRejectsOtherErrors(t *testing.T) {
	for _, result := range []*httpxtest.Result{{Status: 500, Body: []byte(`{"success":false,"message":"请求体必须是 JSON 对象"}`)}, {Status: 400, Body: []byte(`{"success":true,"message":"请求体必须是 JSON 对象"}`)}, {Status: 400, Body: []byte(`{"success":false,"message":"wrong"}`)}} {
		if approvedShapeError("/api/nodes/reorder", "[1]", answer{Status: 500}, result) {
			t.Fatal("unapproved mismatch accepted")
		}
	}
}
