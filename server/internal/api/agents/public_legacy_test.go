package agents

// Test-only codec keeps the frozen Python-era projection fixtures unchanged.
import (
	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"encoding/json"
)

func publicAgent(record *agents.Record) *agents.Record {
	record = record.Clone()
	for key, value := range record.All() {
		if value == repo.AgentHistoryNotRead {
			record.Delete(key)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil
	}
	var value model.Agent
	if jsondecode.Unmarshal(raw, &value) != nil {
		return nil
	}
	public, err := publicAgentModel(&value)
	if err != nil {
		return nil
	}
	raw, err = json.Marshal(public)
	if err != nil {
		return nil
	}
	parsed, err := jsonx.Parse(raw)
	if err != nil {
		return nil
	}
	out, _ := parsed.(*jsonx.Object)
	return out
}
