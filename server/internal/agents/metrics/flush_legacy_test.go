package metrics

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/taskstatus"
	"context"
	"encoding/json"
	"time"
)

// ValidateInterval transcodes a former dynamic interval value.
func ValidateInterval(value any) (int64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	n, ok := integerJSON(raw)
	if !ok || !n.IsInt64() || !allowedInterval(n.Int64()) {
		return 0, errInterval
	}
	return n.Int64(), nil
}

// IntervalSeconds adapts the native settings reader.
func IntervalSeconds(system *jsonx.Object) int64 {
	raw, err := json.Marshal(system)
	if err != nil {
		return DefaultInterval
	}
	var s model.System
	if json.Unmarshal(raw, &s) != nil {
		return DefaultInterval
	}
	return FlushInterval(&s)
}

// SetInterval adapts the native interval writer.
func SetInterval(ctx context.Context, value any) error {
	seconds, err := ValidateInterval(value)
	if err != nil {
		return err
	}
	return SetFlushInterval(ctx, seconds)
}

// NextRunAt adapts native receipt scheduling.
func NextRunAt(entry *jsonx.Object, interval int64) *time.Time {
	raw, _ := json.Marshal(entry)
	var r taskstatus.Receipt
	_ = json.Unmarshal(raw, &r)
	return NextFlushAt(&r, interval)
}

// TrafficFlush is the compatibility traffic task hook.
type TrafficFlush func(context.Context, *agents.Manager, []string, *jsonx.Object) (int64, error)

// SetTrafficFlush adapts former dynamic hooks to the native task engine.
func SetTrafficFlush(fn TrafficFlush) {
	if fn == nil {
		SetNativeTrafficFlush(nil)
		return
	}
	SetNativeTrafficFlush(func(ctx context.Context, m *agents.Manager, ids []string, s *model.System) (int64, error) {
		out, err := legacyObject(s)
		if err != nil {
			return 0, err
		}
		return fn(ctx, m, ids, out)
	})
}

// Run adapts native save counters.
func Run(ctx context.Context, trigger string, idle bool) (*jsonx.Object, error) {
	out, err := FlushNow(ctx, trigger, idle)
	if err != nil {
		return nil, err
	}
	return legacyObject(out)
}

// FlushOnShutdown adapts native shutdown counters.
func FlushOnShutdown(ctx context.Context, idle bool) *jsonx.Object {
	out := SaveOnShutdown(ctx, idle)
	if out == nil {
		return nil
	}
	r, _ := legacyObject(out)
	return r
}

// LoadHistory adapts native startup counters.
func LoadHistory(ctx context.Context, m *agents.Manager) *jsonx.Object {
	out := LoadSavedHistory(ctx, m)
	if out == nil {
		return nil
	}
	return out.Object()
}

// Rows adapts typed task rows for remaining dynamic callers.
func Rows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	raw, err := json.Marshal(statuses)
	if err != nil {
		return nil, err
	}
	var snapshot taskstatus.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	rows, err := BuildRows(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	out := make([]*jsonx.Object, 0, len(rows))
	for _, row := range rows {
		object, err := legacyObject(row)
		if err != nil {
			return nil, err
		}
		out = append(out, object)
	}
	return out, nil
}
