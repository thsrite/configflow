package refresh

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/weburl"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func notFalse(value *bool) bool { return value == nil || *value }

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
	type subView struct {
		id       string
		name     string
		url      string
		enabled  bool
		interval model.Int
	}
	var subs []subView
	if err := r.View(ctx, func(document *model.Document) error {
		subs = make([]subView, 0, len(document.Shared.Subscriptions))
		for _, sub := range document.Shared.Subscriptions {
			subs = append(subs, subView{
				id:       sub.ID,
				name:     sub.Name,
				url:      sub.URL,
				enabled:  notFalse(sub.Enabled),
				interval: SourceInterval(sub),
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rows := make([]*tasks.Task, 0, len(subs))
	for _, sub := range subs {
		key := SourceTaskKey(sub.id)
		entry := statuses.Entry(key)
		interval := sub.interval
		hasURL := substore.SourceURLConfigured(sub.url)
		enabled := sub.enabled && hasURL
		var due *time.Time
		if enabled {
			baseline, err := sourceCacheAtID(ctx, sub.id, sub.url)
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
		name := sub.name
		if name == "" {
			name = sub.id
		}
		hint, message := "", ""
		if sub.url == "" {
			hint = msgNoURL
		} else if !hasURL {
			hint = msgInvalidSourceURL
		}
		if !sub.enabled {
			message = msgSubscriptionDisabled
		}
		seconds, _ := json.Marshal(interval)
		link := "/subscriptions"
		rows = append(rows, &tasks.Task{Key: key, Category: "subscription", Name: name, ResourceID: sub.id, Enabled: enabled && interval != 0, IntervalSeconds: seconds, Editable: hasURL, CanRun: hasURL, RunHint: hint, Running: IsRunning(key), NextRunAt: tasks.Timestamp(due), StateMessage: message, Link: &link, NotificationsEnabled: &enabled, HistoryData: history})
	}
	return rows, nil
}

// RuleTasks projects native task rows for URL-backed library sources.
func RuleTasks(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	type ruleView struct {
		id       string
		name     string
		url      string
		enabled  bool
		interval model.Int
		library  model.RuleConfig
	}
	var list []ruleView
	if err := r.View(ctx, func(document *model.Document) error {
		list = make([]ruleView, 0, len(document.Shared.RuleLibrary))
		for _, rule := range document.Shared.RuleLibrary {
			url := textOr(rule.URL, "")
			if !urlRule(rule) || url == "" {
				continue
			}
			list = append(list, ruleView{
				id:       rule.ID,
				name:     rule.Name,
				url:      url,
				enabled:  notFalse(rule.Enabled),
				interval: RemoteInterval(rule),
				library:  rules.LibraryRule(rule),
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rows := make([]*tasks.Task, 0, len(list))
	for _, rule := range list {
		key := RuleTaskKey(rule.id)
		entry := statuses.Entry(key)
		interval := rule.interval
		enabled := rule.enabled
		var due *time.Time
		var baseline *string
		var baselineChecked bool
		if enabled {
			baseline, err = ruleCacheAtLibrary(ctx, rule.library)
			if err != nil {
				return nil, err
			}
			baselineChecked = true
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
			if baselineChecked {
				history.CacheUpdatedAt = baseline
			} else {
				history.CacheUpdatedAt, err = ruleCacheAtLibrary(ctx, rule.library)
				if err != nil {
					return nil, err
				}
			}
		}
		name := rule.name
		if name == "" {
			name = rule.id
		}
		message := ""
		if !enabled {
			message = msgRuleDisabled
		}
		seconds, _ := json.Marshal(interval)
		link := "/rule-library"
		rows = append(rows, &tasks.Task{Key: key, Category: "rule_source", Name: name, ResourceID: rule.id, Detail: sourceHost(rule.url), Enabled: enabled && interval != 0, IntervalSeconds: seconds, Editable: true, CanRun: true, Running: IsRunning(key), NextRunAt: tasks.Timestamp(due), StateMessage: message, Link: &link, NotificationsEnabled: &enabled, HistoryData: history})
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
