package agents_test

import (
	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"context"
)

func legacyUpgradeObject(value any) *jsonx.Object {
	raw, _ := model.Marshal(value)
	parsed, _ := jsonx.Parse(raw)
	out, _ := parsed.(*jsonx.Object)
	return out
}
func legacyUpgradeSetRequestForTest(fn func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error)) func() {
	return upgrades.SetRequestForTest(func(_ context.Context, a *model.Agent, method, path string, body []byte) (*agents.Response, error) {
		var payload *jsonx.Object
		if len(body) > 0 {
			v, _ := jsonx.Parse(body)
			payload, _ = v.(*jsonx.Object)
		}
		return fn(legacyUpgradeObject(a), method, path, payload)
	})
}
func legacyUpgradeSetReachesForTest(fn func(*jsonx.Object, string) bool) func() {
	return upgrades.SetReachesForTest(func(a *model.Agent, host string) bool { return fn(legacyUpgradeObject(a), host) })
}
func legacyUpgradeSetAskForTest(fn func(*jsonx.Object, string, string, *jsonx.Object) (int, *jsonx.Object)) func() {
	return upgrades.SetAskForTest(func(a *model.Agent, host, method string, body []byte) (int, model.Fields) {
		var payload *jsonx.Object
		if len(body) > 0 {
			v, _ := jsonx.Parse(body)
			payload, _ = v.(*jsonx.Object)
		}
		status, data := fn(legacyUpgradeObject(a), host, method, payload)
		raw, _ := jsonx.Compact(data)
		var fields model.Fields
		_ = jsondecode.Unmarshal(raw, &fields)
		return status, fields
	})
}
func legacyUpgradeAgentResponse(payload any, status int) *agents.Response {
	raw, _ := jsonx.Default(payload)
	return &agents.Response{StatusCode: status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: raw}
}
