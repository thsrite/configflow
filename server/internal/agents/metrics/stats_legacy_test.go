package metrics

import (
	"configflow/internal/jsonx"
	"encoding/json"
	"math/big"
)

// GetMetricsSummary adapts native statistics for legacy callers.
func (h *History) GetMetricsSummary(id string, hours *big.Int) (*jsonx.Object, error) {
	out, err := h.Summarize(id, hours)
	if err != nil {
		return nil, err
	}
	return legacyObject(out)
}

// GetTrafficStats adapts native counters for legacy callers.
func (h *History) GetTrafficStats(id, period string) (*jsonx.Object, error) {
	out, err := h.TrafficStatistics(id, period)
	if err != nil {
		return nil, err
	}
	return legacyObject(out)
}

// GetTrafficTrend adapts native trend DTOs for legacy callers.
func (h *History) GetTrafficTrend(id string, hours, interval *big.Int) ([]any, error) {
	out, err := h.TrafficTrend(id, hours, interval)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.([]any), nil
}
