package routers

import (
	"encoding/json"
	"testing"
)

func FuzzR15WorkspaceBoundary(f *testing.F) {
	for _, seed := range []string{`{}`, `[]`, `null`, `{"revision":"x","router":{"ip_rule_sets":[],"ros_policy_rule_sets":[],"proxy_gateway_agent_id":"gateway"},"base_url":""}`, `{"targets":[null]}`, `{"targets":["one"]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		raw := json.RawMessage(input)
		request, ok := workspaceRequest(raw)
		if ok {
			values, valid := fields(request.Router)
			if !valid || !exact(values, "ip_rule_sets", "ros_policy_rule_sets", "proxy_gateway_agent_id") {
				t.Fatal("accepted an unchecked router shape")
			}
		}
		values, _ := fields(raw)
		targets, valid := previewTargets(values["targets"])
		if valid {
			var expected []json.RawMessage
			if json.Unmarshal(values["targets"], &expected) != nil || len(targets) != len(expected) {
				t.Fatal("target parser changed array shape")
			}
			for _, row := range expected {
				if _, ok := stringValue(row); !ok {
					t.Fatal("non-string target accepted")
				}
			}
		}
	})
}
