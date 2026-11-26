package metrics

import (
	"bytes"
	"configflow/internal/timefmt"
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
)

// AddSample appends one native JSON metrics sample. Raw component members keep
// unknown versions and original null/type evidence; only the four protocol
// components enter history, as before. It never decodes the configuration.
func (h *History) AddSample(ctx context.Context, id string, sample json.RawMessage) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	components, err := jsondecode.Select(sample, "cpu", "memory", "disk", "network")
	if err != nil || len(bytes.TrimSpace(sample)) == 0 || bytes.TrimSpace(sample)[0] != '{' {
		return false, fmt.Errorf("decode metrics sample: object required")
	}
	at := naiveNow()
	value := struct {
		Timestamp string          `json:"timestamp"`
		CPU       json.RawMessage `json:"cpu"`
		Memory    json.RawMessage `json:"memory"`
		Disk      json.RawMessage `json:"disk"`
		Network   json.RawMessage `json:"network"`
	}{Timestamp: timefmt.Naive(at)}
	get := func(name string) json.RawMessage {
		if raw, ok := components[name]; ok {
			return raw
		}
		return json.RawMessage(`{}`)
	}
	value.CPU, value.Memory, value.Disk, value.Network = get("cpu"), get("memory"), get("disk"), get("network")
	line, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("encode metrics sample: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	points := append(h.points[id], point{at, string(line)})
	if len(points) > h.maxPoints {
		points = points[len(points)-h.maxPoints:]
	}
	h.points[id] = points
	h.pending[id] = min(h.pending[id]+1, len(points))
	h.prune(id)
	return true, nil
}
