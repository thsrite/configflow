package tasks

import (
	"context"
	"fmt"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/weburl"
)

func findRow(ctx context.Context, key, slot string) (*tasks.Task, error) {
	rows, err := tasks.ForSlot(ctx, slot)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Key == key {
			return row, nil
		}
	}
	return nil, nil
}

func updateInterval(ctx context.Context, body intervalRequest) (*tasks.Task, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	category, resource, _ := strings.Cut(body.Key, ":")
	if body.Key == tasks.MetricsTaskKey {
		category = body.Key
	}
	if category == tasks.MetricsTaskKey && body.Key != tasks.MetricsTaskKey {
		return nil, &tasks.Invalid{Message: msgIntervalUnsupported}
	}
	slot, known := tasks.SlotFor(category)
	if known && !tasks.HasRows(slot) {
		return nil, &tasks.Unavailable{Slots: []string{slot}}
	}
	if body.Key != tasks.MetricsTaskKey && category != tasks.SlotSubscriptions && category != tasks.SlotRuleSources {
		return nil, &tasks.Invalid{Message: msgIntervalUnsupported}
	}
	if body.Key == tasks.MetricsTaskKey {
		setter := tasks.IntervalEditorFor(body.Key)
		if setter == nil {
			return nil, &tasks.Unavailable{Slots: []string{tasks.SlotMetrics}}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := setter(ctx, body.interval()); err != nil {
			return nil, err
		}
	} else {
		interval, err := validateInterval(body.interval())
		if err != nil {
			return nil, &tasks.Invalid{Message: err.Error()}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		repository, err := repo.GetRepository(ctx)
		if err != nil {
			return nil, err
		}
		err = repository.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if category == tasks.SlotSubscriptions {
				for i := range shared.Subscriptions {
					item := &shared.Subscriptions[i]
					if item.ID == resource {
						item.Interval = new(model.Int(interval))
						return nil
					}
				}
			} else {
				for i := range shared.RuleLibrary {
					item := &shared.RuleLibrary[i]
					if item.ID == resource {
						item.UpdateInterval = new(model.Int(interval))
						return nil
					}
				}
			}
			return &tasks.NotFound{Message: msgSourceMissing}
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
