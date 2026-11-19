package monitor

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"encoding/json"
)

// AgentOrigin adapts the remaining gateway caller; W6 migrates it to Origin.
func AgentOrigin(raw *jsonx.Object) (string, error) {
	var a model.Agent
	if raw == nil {
		return "", unreachable()
	}
	// Only endpoint members are needed; histories can contain lazy sentinels.
	data, err := json.Marshal(jsonx.Obj("host", raw.Value("host"), "port", raw.Value("port")))
	if err != nil {
		return "", unreachable()
	}
	if err = json.Unmarshal(data, &a); err != nil {
		return "", unreachable()
	}
	return Origin(&a)
}
