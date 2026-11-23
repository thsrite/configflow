package webdav

import (
	"bytes"
	"configflow/internal/jsondecode"
	"encoding/json"
	"reflect"

	"configflow/internal/repo/staterows"
)

type commitDocument struct {
	Profiles json.RawMessage `json:"profiles"`
	Shared   json.RawMessage `json:"shared"`
	System   struct {
		Config json.RawMessage   `json:"system_config"`
		Backup json.RawMessage   `json:"backup"`
		Agents []json.RawMessage `json:"agents"`
	} `json:"system"`
}

// The Agent state members (model.AgentStateMembers, removed with
// staterows.Split) and updated_at are not configuration a backup has to
// follow.
const agentUpdatedAt = "updated_at"

func equalJSON(a, b json.RawMessage) bool {
	var left, right any
	decode := func(data []byte, value *any) error {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		return decoder.Decode(value)
	}
	if decode(a, &left) != nil || decode(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
func configuredAgents(agents []json.RawMessage) (map[string]map[string]json.RawMessage, error) {
	result := map[string]map[string]json.RawMessage{}
	if agents == nil {
		return result, nil
	}
	row, err := json.Marshal(agents)
	if err != nil {
		return nil, err
	}
	config, _ := staterows.Split(row)
	agents = nil
	if err := json.Unmarshal(config, &agents); err != nil {
		return nil, err
	}
	for _, raw := range agents {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		var id string
		if err := json.Unmarshal(fields["id"], &id); err != nil {
			return nil, err
		}
		delete(fields, agentUpdatedAt)
		result[id] = fields
	}
	return result, nil
}
func configurationChanged(previous, current []byte) (bool, error) {
	var before, after commitDocument
	if err := jsondecode.Unmarshal(previous, &before); err != nil {
		return false, err
	}
	if err := jsondecode.Unmarshal(current, &after); err != nil {
		return false, err
	}
	if !equalJSON(before.Profiles, after.Profiles) || !equalJSON(before.Shared, after.Shared) || !equalJSON(before.System.Config, after.System.Config) || !equalJSON(before.System.Backup, after.System.Backup) {
		return true, nil
	}
	old, err := configuredAgents(before.System.Agents)
	if err != nil {
		return false, err
	}
	next, err := configuredAgents(after.System.Agents)
	if err != nil {
		return false, err
	}
	a, err := json.Marshal(old)
	if err != nil {
		return false, err
	}
	b, err := json.Marshal(next)
	return !equalJSON(a, b), err
}
