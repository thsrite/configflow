package agents

// Compatibility codecs for the remaining monitor/upgrades and old test API.
// Runtime ownership, merge and deletion rules live only in runtime_state.go.
import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// RuntimeEntry is the old runtime diagnostic envelope.
type RuntimeEntry struct {
	HeartbeatAt   float64
	LastHeartbeat string
	Fields        *jsonx.Object
}

func agentModel(raw *jsonx.Object) (*model.Agent, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result model.Agent
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func agentObject(record *model.Agent) (*jsonx.Object, error) {
	data, err := model.Marshal(record)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	result, _ := value.(*jsonx.Object)
	return result, nil
}

// Overlay adapts legacy records to the single typed runtime owner.
func (s *RuntimeState) Overlay(records []*jsonx.Object) []*jsonx.Object {
	for _, raw := range records {
		record, err := agentModel(raw)
		if err != nil {
			continue
		}
		overlaid := s.overlayModels([]model.Agent{*record})
		result, err := agentObject(&overlaid[0])
		if err == nil {
			raw.Update(result)
		}
	}
	return records
}

// RecordHeartbeat adapts a legacy heartbeat to the typed runtime owner.
func (s *RuntimeState) RecordHeartbeat(agent, payload *jsonx.Object, mark int64) bool {
	record, err := agentModel(agent)
	if err != nil {
		return false
	}
	fields, err := agentModel(payload)
	if err != nil {
		return false
	}
	ok, err := s.recordModel(context.Background(), record, fields, mark)
	return err == nil && ok
}

// Seed imports a legacy heartbeat projection through the typed owner.
func (s *RuntimeState) Seed(id string, at float64, heartbeat string, fields *jsonx.Object) {
	record, err := agentModel(fields)
	if err == nil {
		_ = s.seedModel(id, at, heartbeat, record)
	}
}

// Entries returns independent compatibility snapshots for diagnostics.
func (s *RuntimeState) Entries() map[string]RuntimeEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]RuntimeEntry, len(s.entries))
	for id, entry := range s.entries {
		fields, err := agentObject(&entry.Fields)
		if err == nil {
			result[id] = RuntimeEntry{entry.HeartbeatAt, entry.LastHeartbeat, fields}
		}
	}
	return result
}

// SetEntry replaces a diagnostic entry, retaining the typed ownership boundary.
func (s *RuntimeState) SetEntry(id string, entry RuntimeEntry) {
	fields, err := agentModel(entry.Fields)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.entries[id] = heartbeatEntry{entry.HeartbeatAt, entry.LastHeartbeat, *fields}
	s.mu.Unlock()
}

func (m *Manager) addReportedMetrics(ctx context.Context, id string, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hooks := currentHooks()
	if hooks.AddMetricsJSON != nil {
		return hooks.AddMetricsJSON(ctx, m, id, raw)
	}
	if add := hooks.AddMetrics; add != nil {
		value, err := jsonx.Parse(raw)
		if err != nil {
			return err
		}
		return add(m, id, value)
	}
	return nil
}
