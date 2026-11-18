package tasks

// TODO(N8-9): remove codecs and hook adapters as task owners migrate.
import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/taskstatus"
)

// RowsFunc is the compatibility signature of old task owners.
type RowsFunc func(context.Context, *jsonx.Object) ([]*jsonx.Object, error)

// IntervalFunc is the compatibility signature of old interval owners.
type IntervalFunc func(context.Context, any) error

func legacyObject(value any) (*jsonx.Object, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result jsonx.Object
	err = json.Unmarshal(data, &result)
	return &result, err
}
func legacyRows(values []*Task, err error) ([]*jsonx.Object, error) {
	if err != nil {
		return nil, err
	}
	result := make([]*jsonx.Object, 0, len(values))
	for _, value := range values {
		row, err := legacyObject(value)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, nil
}

// SetRows adapts an old producer at registration, retaining restoration semantics.
func SetRows(slot string, f RowsFunc) func() {
	if f == nil {
		return SetRowBuilder(slot, nil)
	}
	return SetRowBuilder(slot, func(ctx context.Context, statuses taskstatus.Snapshot) ([]*Task, error) {
		input, err := legacyObject(statuses)
		if err != nil {
			return nil, err
		}
		rows, err := f(ctx, input)
		if err != nil {
			return nil, err
		}
		result := make([]*Task, 0, len(rows))
		for _, row := range rows {
			data, err := json.Marshal(row)
			if err != nil {
				return nil, err
			}
			var item Task
			if err = json.Unmarshal(data, &item); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidHistory, err)
			}
			result = append(result, &item)
		}
		return result, nil
	})
}

// SetIntervalSetter adapts an old owner at registration.
func SetIntervalSetter(key string, f IntervalFunc) func() {
	if f == nil {
		return SetIntervalEditor(key, nil)
	}
	return SetIntervalEditor(key, func(ctx context.Context, value *int64) error {
		if value == nil {
			return f(ctx, nil)
		}
		return f(ctx, jsonx.Int(*value))
	})
}

// IntervalSetter returns a compatibility codec for old command callers.
func IntervalSetter(key string) IntervalFunc {
	editor := IntervalEditorFor(key)
	if editor == nil {
		return nil
	}
	return func(ctx context.Context, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var number *int64
		if json.Unmarshal(data, &number) != nil {
			number = nil
		}
		return editor(ctx, number)
	}
}

// ISO adapts an optional timestamp.
func ISO(t *time.Time) any {
	value := Timestamp(t)
	if value == nil {
		return nil
	}
	return *value
}

// History adapts a saved legacy receipt to the one native projection.
func History(entry *jsonx.Object) (*jsonx.Object, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var value taskstatus.Receipt
	if string(data) != "null" {
		if err = json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
	}
	history, err := ProjectHistory(&value)
	if err != nil {
		return nil, err
	}
	return legacyObject(history)
}

// Row packs old owner fields, then applies native row defaults in the decoder.
func Row(key, category, name string, fields ...any) *jsonx.Object {
	wire := jsonx.Obj("key", key, "category", category, "name", name)
	for i := 0; i+1 < len(fields); i += 2 {
		if object, ok := fields[i].(*jsonx.Object); ok && fields[i+1] == nil {
			wire.Update(object)
			continue
		}
		key, _ := fields[i].(string)
		wire.Set(key, fields[i+1])
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil
	}
	var value Task
	if json.Unmarshal(data, &value) != nil {
		return wire
	}
	result, _ := legacyObject(&value)
	return result
}

// Spread is legacy owner field packing.
func Spread(o *jsonx.Object) []any { return []any{o, nil} }

// ListTasks adapts the complete native list.
func ListTasks(ctx context.Context) ([]*jsonx.Object, error) { return legacyRows(List(ctx)) }

// AvailableTasks adapts registered native owners.
func AvailableTasks(ctx context.Context) ([]*jsonx.Object, error) { return legacyRows(Available(ctx)) }

// RowsFor adapts a selected native owner.
func RowsFor(ctx context.Context, slot string) ([]*jsonx.Object, error) {
	return legacyRows(ForSlot(ctx, slot))
}
