package rulesync

import (
	"configflow/internal/model"
	"encoding/json"
)

func fixtureReceiptAgent(v any) *model.Agent {
	raw, _ := json.Marshal(v)
	var out model.Agent
	_ = json.Unmarshal(raw, &out)
	return &out
}
