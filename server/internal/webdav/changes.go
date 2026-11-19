package webdav

import (
	"bytes"
	"configflow/internal/jsondecode"
	"encoding/json"
	"reflect"
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

var agentRuntimeFields = map[string]bool{
	"last_heartbeat": true, "status": true, "version": true, "service_status": true, "config_version": true,
	"system_metrics": true, "cpu": true, "memory": true, "disk": true, "network": true, "updated_at": true,
	"deployments": true, "latest_deployment": true, "latest_upgrade": true, "config_revision": true,
	"pushed_at": true, "applied_content": true,
}

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
	for _, raw := range agents {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		var id string
		if err := json.Unmarshal(fields["id"], &id); err != nil {
			return nil, err
		}
		for key := range agentRuntimeFields {
			delete(fields, key)
		}
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
