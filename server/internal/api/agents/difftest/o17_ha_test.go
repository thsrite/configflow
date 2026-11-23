package difftest

import (
	"encoding/json"
	"testing"
)

// checkO17TextOnlyGuide reports a guide request the O17 ruling refuses on
// purpose: vip, ha_gateway and container_name must be JSON strings (Python
// read true/false/integers as IPv4 addresses and null/false/0/[]/{} as "not
// filled"). Go must answer 400 with the new message.
func checkO17TextOnlyGuide(t *testing.T, r request, g response) bool {
	t.Helper()
	var body map[string]any
	if json.Unmarshal([]byte(r.body), &body) != nil {
		return false
	}
	nonText := func(key string) bool {
		value, present := body[key]
		_, text := value.(string)
		return present && !text
	}
	var want string
	switch {
	case body["action"] == "verify" && nonText("vip"):
		want = "虚拟 IPv4 地址必须是文本"
	case body["action"] == "inspect" && nonText("container_name"):
		want = "原容器名只能包含字母、数字、点、下划线或短横线"
	case nonText("ha_gateway"):
		want = "高可用网关地址必须是文本"
	default:
		return false
	}
	var answer struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if g.status != 400 || json.Unmarshal(g.body, &answer) != nil || answer.Success || answer.Message != want {
		t.Errorf("O17 text-only guide %s: Go %d %s, want 400 %q", r.name, g.status, g.body, want)
	}
	return true
}
