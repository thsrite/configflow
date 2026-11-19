package metrics

import (
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
)

// PyTypeName is type(value).__name__ of a JSON value.
func PyTypeName(value any) string { return pyTypeName(value) }

func pyTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case jsonx.Number:
		if v.IsInt() {
			return "int"
		}
		return "float"
	case []any:
		return "list"
	case *jsonx.Object:
		if v == nil {
			return "NoneType"
		}
		return "dict"
	}
	return fmt.Sprintf("%T", value)
}

// Object is the dict Python returns.
func (r LoadResult) Object() *jsonx.Object {
	return jsonx.Obj("loaded", r.Loaded, "migrated", r.Migrated, "removed", r.Removed)
}

// AddMetrics is add_metrics(agent_id, system_metrics): append one point to
// memory (the flush task writes it to the file). The error is the
// AttributeError Python raised when system_metrics is not a dict.
func (h *History) AddMetrics(agentID string, systemMetrics any) (bool, error) {
	metrics, ok := systemMetrics.(*jsonx.Object)
	if !ok || metrics == nil {
		return false, &AttributeError{fmt.Sprintf("'%s' object has no attribute 'get'", pyTypeName(systemMetrics))}
	}
	data, err := jsonresp.Encode(metrics)
	if err != nil {
		historyLogger.Warningf("Agent %s 的监控数据无法保存：%s", agentID, err)
		return false, nil
	}
	return h.AddSample(context.Background(), agentID, data)
}

// GetMetrics transcodes native samples for remaining dynamic callers.
func (h *History) GetMetrics(id string, hours *big.Int) ([]any, error) {
	samples, err := h.Samples(id, hours)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(samples))
	for _, raw := range samples {
		var value *jsonx.Object
		if err = json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// GetLatestMetrics transcodes the latest native sample.
func (h *History) GetLatestMetrics(id string) any {
	samples, _ := h.GetMetrics(id, nil)
	if len(samples) == 0 {
		return nil
	}
	return samples[len(samples)-1]
}

// Object is the dict Python returns.
func (r FlushResult) Object() *jsonx.Object {
	return jsonx.Obj("agents", r.Agents, "appended", r.Appended, "compacted", r.Compacted)
}
