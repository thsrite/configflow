package monitor

import (
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
)

func metricJSON(value any, target any) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(raw, target)
}
func metricObject(value any) *jsonx.Object {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return nil
	}
	return decoded.(*jsonx.Object)
}
func nativeObservations(value *jsonx.Object) *Observations {
	var out Observations
	_ = metricJSON(value, &out)
	return &out
}

// ParseMetrics converts native observations for pending legacy callers.
func ParseMetrics(text string) (*jsonx.Object, error) {
	o, err := ReadMetrics(text)
	if err != nil {
		return nil, err
	}
	return metricObject(o), nil
}

// MetricsCountersRestarted reports reset counters through the legacy codec.
func MetricsCountersRestarted(a, b *jsonx.Object) bool {
	return CountersRestarted(nativeObservations(a), nativeObservations(b))
}

// ApplyMetricsBaseline adapts existing dashboard callers.
func ApplyMetricsBaseline(a, b *jsonx.Object) (*jsonx.Object, bool, error) {
	var x, y Observations
	if err := metricJSON(a, &x); err != nil {
		return nil, false, err
	}
	if err := metricJSON(b, &y); err != nil {
		return nil, false, err
	}
	out, partial, err := ApplyBaseline(&x, &y)
	if err != nil {
		return nil, false, err
	}
	return metricObject(out), partial, nil
}

// ZeroBaseline adapts existing dashboard callers.
func ZeroBaseline(a *jsonx.Object) *jsonx.Object {
	return metricObject(ZeroObservations(nativeObservations(a)))
}

// AddObservations adapts existing dashboard callers.
func AddObservations(a, b *jsonx.Object) *jsonx.Object {
	return metricObject(CarryObservations(nativeObservations(a), nativeObservations(b)))
}

// SummarizeObservations adapts existing dashboard callers.
func SummarizeObservations(a *jsonx.Object) *jsonx.Object {
	return metricObject(Summarize(nativeObservations(a)))
}

// SummarizeMetrics adapts the legacy metrics entry point.
func SummarizeMetrics(text string) (*jsonx.Object, error) {
	o, err := ReadMetrics(text)
	if err != nil {
		return nil, err
	}
	return metricObject(Summarize(o)), nil
}

func identity(row *jsonx.Object, labels []string) string {
	parts := make([]any, len(labels))
	for i, key := range labels {
		parts[i] = row.Value(key)
	}
	raw, _ := jsonx.Compact(parts)
	return string(raw)
}
