package deploy

import (
	"configflow/internal/model"
	"encoding/json"
)

func fixtureAgent(v any) *model.Agent {
	raw, _ := json.Marshal(v)
	var out model.Agent
	_ = json.Unmarshal(raw, &out)
	return &out
}
func fixtureTask(v any) *model.Deployment {
	raw, _ := json.Marshal(v)
	var out model.Deployment
	_ = json.Unmarshal(raw, &out)
	return &out
}
