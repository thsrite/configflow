package refresh

import (
	"configflow/internal/jsonx"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"context"
)

func snapshot(value *jsonx.Object) taskstatus.Snapshot {
	var result taskstatus.Snapshot
	_ = decodeLegacy(value, &result)
	return result
}
func rowObjects(rows []*tasks.Task, err error) ([]*jsonx.Object, error) {
	if err != nil {
		return nil, err
	}
	result := make([]*jsonx.Object, len(rows))
	for i, row := range rows {
		result[i] = legacyObject(row)
	}
	return result, nil
}

// SubscriptionRows adapts task rows for unported tests/callers.
func SubscriptionRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	return rowObjects(SubscriptionTasks(ctx, snapshot(statuses)))
}

// RuleRows adapts task rows for unported tests/callers.
func RuleRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	return rowObjects(RuleTasks(ctx, snapshot(statuses)))
}
func historyOf(value any) (*jsonx.Object, error) {
	var receipt taskstatus.Receipt
	_ = decodeLegacy(value, &receipt)
	result, err := tasks.ProjectHistory(&receipt)
	if err != nil {
		return nil, err
	}
	return legacyObject(result), nil
}
func host(value any) string { text, _ := value.(string); return sourceHost(text) }
