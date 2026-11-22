package agents

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func FuzzDeploymentRouteBinding(f *testing.F) {
	f.Add([]byte(`{"agent":{"service_type":"mihomo"},"task":{"deployment_id":"device","service_type":"mihomo"}}`))
	f.Add([]byte(`{"agent":{"service_type":7,"Service_Type":"mihomo"},"task":{"deployment_id":7,"Deployment_ID":"device","service_type":"mihomo","http_status":"404"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var input struct{ Agent, Task json.RawMessage }
		if json.Unmarshal(data, &input) != nil {
			return
		}
		var a model.Agent
		var task model.Deployment
		if json.Unmarshal(input.Agent, &a) != nil || json.Unmarshal(input.Task, &task) != nil {
			return
		}
		var agentMembers, taskMembers map[string]json.RawMessage
		if json.Unmarshal(input.Agent, &agentMembers) != nil || json.Unmarshal(input.Task, &taskMembers) != nil {
			return
		}
		before, err := model.Marshal(task)
		if err != nil {
			return
		}
		text := func(raw json.RawMessage) (string, bool) {
			var value *string
			if json.Unmarshal(raw, &value) != nil || value == nil {
				return "", false
			}
			return *value, true
		}
		id, idOK := text(taskMembers["deployment_id"])
		if got := deploymentTaskIDMatches(&task, "device"); got != (idOK && id == "device") {
			t.Fatalf("task id=%s accepted=%v", input.Task, got)
		}
		// Stored text reads leniently (O18 T1b): a number is its literal, any
		// other type or "" is a missing member. A missing Agent service is
		// mihomo; a missing task service matches only when it is optional.
		stored := func(raw json.RawMessage) string {
			if value, ok := text(raw); ok {
				return value
			}
			if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && (trimmed[0] == '-' || trimmed[0] >= '0' && trimmed[0] <= '9') {
				return string(trimmed)
			}
			return ""
		}
		service := stored(agentMembers["service_type"])
		if service == "" {
			service = "mihomo"
		}
		stateService := stored(taskMembers["service_type"])
		for _, optional := range []bool{false, true} {
			expected := service == stateService || optional && stateService == ""
			if got := deploymentServiceMatches(&a, &task, optional); got != expected {
				t.Fatalf("agent=%s task=%s optional=%v accepted=%v", input.Agent, input.Task, optional, got)
			}
		}
		after, err := model.Marshal(task)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("ownership read changed journal", err)
		}
	})
}
