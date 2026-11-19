package metrics

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
)

func legacyObject(value any) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out *jsonx.Object
	err = json.Unmarshal(raw, &out)
	return out, err
}

// CleanTrafficReport transcodes the former dynamic input to the native report decoder.
func CleanTrafficReport(report any, at *float64) *CleanedTraffic {
	raw, err := json.Marshal(report)
	if err != nil {
		return nil
	}
	return DecodeTrafficReport(raw, at)
}

// Query transcodes the native query response for remaining legacy consumers.
func (h *TrafficHistory) Query(id, rangeKey, by string, at *float64) (*jsonx.Object, error) {
	out, err := h.QueryTraffic(context.Background(), id, rangeKey, by, at)
	if err != nil {
		return nil, err
	}
	return legacyObject(out)
}

// RetentionDays transcodes global settings to the native retention reader.
func RetentionDays(system *jsonx.Object) (int64, error) {
	raw, err := json.Marshal(system.Value("system_config"))
	if err != nil {
		return 0, err
	}
	return retentionDaysJSON(raw)
}

// SetRetentionDays transcodes the former dynamic setting input.
func SetRetentionDays(ctx context.Context, m *agents.Manager, value any) (int64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	n, ok := integerJSON(raw)
	if !ok || !n.IsInt64() {
		return 0, RetentionValueError
	}
	return SetTrafficRetention(ctx, m, n.Int64())
}

// Object transcodes native flush counters for the legacy task facade.
func (r TrafficFlushResult) Object() *jsonx.Object {
	out, _ := legacyObject(struct {
		Rows      int64 `json:"rows"`
		Compacted int64 `json:"compacted"`
		Pruned    int64 `json:"pruned"`
	}{r.Rows, r.Compacted, r.Pruned})
	return out
}

// AgentSupported adapts the former dynamic version input.
func AgentSupported(value any, minimum string) bool {
	text, _ := value.(string)
	return SupportedTrafficVersion(text, minimum)
}

// AddReport delegates former context-free callers to the native write owner.
func (h *TrafficHistory) AddReport(agentID string, cleaned *CleanedTraffic) {
	_ = h.AddTraffic(context.Background(), agentID, cleaned)
}
