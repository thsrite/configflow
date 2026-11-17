package tasks

import (
	"context"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/weburl"
)

func findRow(ctx context.Context, key, slot string) (*jsonx.Object, error) {
	rows, err := tasks.RowsFor(ctx, slot)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.GetString("key") == key {
			return row, nil
		}
	}
	return nil, nil
}

func updateInterval(ctx context.Context, body intervalRequest) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	category, resource, _ := strings.Cut(body.Key, ":")
	if body.Key == tasks.MetricsTaskKey {
		category = body.Key
	}
	slot, known := tasks.SlotFor(category)
	if known && !tasks.HasRows(slot) {
		return nil, &tasks.Unavailable{Slots: []string{slot}}
	}
	if body.Key != tasks.MetricsTaskKey && category != tasks.SlotSubscriptions && category != tasks.SlotRuleSources {
		return nil, &tasks.Invalid{Message: msgIntervalUnsupported}
	}
	if body.Key == tasks.MetricsTaskKey {
		setter := tasks.IntervalSetter(body.Key)
		if setter == nil {
			return nil, &tasks.Unavailable{Slots: []string{tasks.SlotMetrics}}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := setter(ctx, body.intervalValue()); err != nil {
			return nil, err
		}
	} else {
		interval, err := refresh.ValidateRefreshInterval(body.intervalValue(), msgIntervalLabel)
		if err != nil {
			return nil, &tasks.Invalid{Message: err.Error()}
		}
		field, listName := "update_interval", "rule_library"
		if category == tasks.SlotSubscriptions {
			field, listName = "interval", "subscriptions"
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, err = repo.UpdateSharedConfigTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, value := range shared.GetList(listName) {
				item, ok := value.(*jsonx.Object)
				if ok && item.GetString("id") == resource {
					item.Set(field, interval)
					return nil, nil
				}
			}
			return nil, &tasks.NotFound{Message: msgSourceMissing}
		})
		if err != nil {
			return nil, err
		}
	}
	row, err := findRow(ctx, body.Key, slot)
	if err != nil {
		// The commit succeeded. An unavailable presentation row must not make
		// the response claim the interval save failed.
		logx.Get("backend.routes.tasks").Warning(fmt.Sprintf(msgRowReadFailed, weburl.SafeErrorDetails(err)))
		return nil, nil
	}
	return row, nil
}
