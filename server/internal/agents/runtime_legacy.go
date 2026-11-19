package agents

// Compatibility codecs for the remaining monitor/upgrades and old test API.
// Runtime ownership, merge and deletion rules live only in runtime_state.go.
import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
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

// Entries returns independent compatibility snapshots for diagnostics.
func (s *RuntimeState) Entries() map[string]RuntimeEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]RuntimeEntry, len(s.entries))
	for id, entry := range s.entries {
		fields, err := entry.Fields.object()
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
	projection, err := runtimeProjection(fields)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.entries[id] = heartbeatEntry{entry.HeartbeatAt, entry.LastHeartbeat, projection}
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

func (fields runtimeFields) object() (*jsonx.Object, error) {
	result := jsonx.NewObject()
	for i, key := range RuntimeFields {
		if fields[i] != nil {
			value, err := jsonx.Parse(fields[i])
			if err != nil {
				return nil, err
			}
			result.Set(key, value)
		}
	}
	return result, nil
}

func decodeHeartbeatObject(raw []byte) (*jsonx.Object, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	result, _ := value.(*jsonx.Object)
	return result, nil
}

// ImportLegacyDatabase adapts the old persistence callback to the native importer.
func ImportLegacyDatabase(ctx context.Context, repository *repo.Repository, state *RuntimeState,
	persist func(context.Context, map[string]*jsonx.Object) (map[string]bool, error)) error {
	return importLegacyDatabase(ctx, repository, state, func(tx context.Context, changes map[string]model.Fields) (map[string]bool, error) {
		legacy := make(map[string]*jsonx.Object, len(changes))
		for id, fields := range changes {
			raw, err := json.Marshal(fields)
			if err != nil {
				return nil, err
			}
			var record jsonx.Object
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, err
			}
			legacy[id] = &record
		}
		return persist(tx, legacy)
	})
}
