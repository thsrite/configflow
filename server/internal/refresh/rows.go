package refresh

import (
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/weburl"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func sourceHost(address string) string {
	parts, err := weburl.Parse(address)
	if err != nil {
		return ""
	}
	return weburl.Hostname(parts)
}

// SubscriptionTasks projects native task rows from the shared source snapshot.
func SubscriptionTasks(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]*tasks.Task, 0, len(shared.Subscriptions))
	for _, sub := range shared.Subscriptions {
		key := SourceTaskKey(sub.ID)
		entry := statuses.Entry(key)
		interval := SourceInterval(sub)
		hasURL := sub.URL != ""
		enabled := enabledResource(sub.Enabled) && hasURL
		var due *time.Time
		if enabled {
			baseline, err := SourceCacheAt(ctx, sub)
			if err != nil {
				return nil, err
			}
			due, err = NextDue(entry, interval, baseline, nil)
			if err != nil {
				return nil, err
			}
		}
		history, err := tasks.ProjectHistory(entry)
		if err != nil {
			return nil, err
		}
		name := sub.Name
		if name == "" {
			name = sub.ID
		}
		hint, message := "", ""
		if !hasURL {
			hint = msgNoURL
		}
		if !enabledResource(sub.Enabled) {
			message = msgSubscriptionDisabled
		}
		seconds, _ := json.Marshal(interval)
		link := "/subscriptions"
		rows = append(rows, &tasks.Task{Key: key, Category: "subscription", Name: name, ResourceID: sub.ID, Enabled: enabled && interval != 0, IntervalSeconds: seconds, Editable: hasURL, CanRun: hasURL, RunHint: hint, Running: IsRunning(key), NextRunAt: tasks.Timestamp(due), StateMessage: message, Link: &link, NotificationsEnabled: &enabled, HistoryData: history})
	}
	return rows, nil
}

// RuleTasks projects native task rows for URL-backed library sources.
func RuleTasks(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]*tasks.Task, 0, len(shared.RuleLibrary))
	for _, rule := range shared.RuleLibrary {
		if !urlRule(rule) || textOr(rule.URL, "") == "" {
			continue
		}
		key := RuleTaskKey(rule.ID)
		entry := statuses.Entry(key)
		interval := RemoteInterval(rule)
		enabled := enabledResource(rule.Enabled)
		var due *time.Time
		if enabled {
			baseline, err := RuleCacheAt(ctx, rule)
			if err != nil {
				return nil, err
			}
			due, err = NextDue(entry, interval, baseline, nil)
			if err != nil {
				return nil, err
			}
		}
		history, err := tasks.ProjectHistory(entry)
		if err != nil {
			return nil, err
		}
		if history.CacheUpdatedAt == nil || *history.CacheUpdatedAt == "" {
			history.CacheUpdatedAt, err = RuleCacheAt(ctx, rule)
			if err != nil {
				return nil, err
			}
		}
		name := rule.Name
		if name == "" {
			name = rule.ID
		}
		message := ""
		if !enabled {
			message = msgRuleDisabled
		}
		seconds, _ := json.Marshal(interval)
		link := "/rule-library"
		rows = append(rows, &tasks.Task{Key: key, Category: "rule_source", Name: name, ResourceID: rule.ID, Detail: sourceHost(textOr(rule.URL, "")), Enabled: enabled && interval != 0, IntervalSeconds: seconds, Editable: true, CanRun: true, Running: IsRunning(key), NextRunAt: tasks.Timestamp(due), StateMessage: message, Link: &link, NotificationsEnabled: &enabled, HistoryData: history})
	}
	return rows, nil
}
func runTask(ctx context.Context, key, id string) error {
	category, _, _ := strings.Cut(key, ":")
	_, err := RunResourceNow(ctx, category, id)
	var busy *TaskBusy
	var lookup *LookupError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &busy):
		return &tasks.Busy{Message: busy.Message}
	case errors.As(err, &lookup):
		return &tasks.NotFound{Message: lookup.Message}
	case errors.Is(err, repo.ErrValidation):
		return &tasks.Invalid{Message: err.Error()}
	}
	return err
}
func round(ctx context.Context) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	defer func() {
		if recover() != nil {
			logger.Errorf(msgRoundPanic)
		}
	}()
	if _, err := RunRound(scope, nil); err != nil && !errors.Is(err, context.Canceled) {
		logger.Errorf(msgRoundFailed, weburl.SafeErrorDetails(err))
	}
}
func periodic() lifecycle.Periodic {
	return lifecycle.Periodic{Name: "scheduled-refresh", Start: lifecycle.AtBoot, FirstDelay: FirstDelaySeconds * time.Second, Interval: func(context.Context) time.Duration { return TickSeconds * time.Second }, Round: func(ctx context.Context) error { round(ctx); return nil }}
}

// Wire registers native task builders/runners and the tracked refresh loop.
func Wire() {
	tasks.SetRowBuilder(tasks.SlotSubscriptions, SubscriptionTasks)
	tasks.SetRowBuilder(tasks.SlotRuleSources, RuleTasks)
	tasks.SetRunner("subscription", runTask)
	tasks.SetRunner("rule_source", runTask)
	lifecycle.RegisterPeriodic(periodic())
}
