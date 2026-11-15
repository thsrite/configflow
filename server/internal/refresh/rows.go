package refresh

// The subscription and rule-source rows of backend/routes/tasks.py
// (_subscription_rows, _rule_rows), their "run now" (run_task →
// scheduled_refresh.run_now) and the scheduler loop (start_loop).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/tasks"
)

// isNotFalse is `value is not False`.
func isNotFalse(value any) bool {
	b, ok := value.(bool)
	return !ok || b
}

// historyOf is _history(entry) for `statuses.get(key) or {}`.
func historyOf(entry any) (*jsonx.Object, error) {
	object, ok := entry.(*jsonx.Object)
	if !ok {
		return nil, attributeGet(entry)
	}
	return tasks.History(object), nil
}

// host is routes/tasks._host(url): the host name, "" when there is none or
// the address does not parse.
func host(url any) string {
	text, _ := orDefault(url, "").(string)
	parts, err := rulefetch.SplitURL(text, "")
	if err != nil {
		return ""
	}
	return parts.Hostname()
}

func sharedOf(ctx context.Context) (*jsonx.Object, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return r.GetShared(ctx)
}

func requiredValue(item *jsonx.Object, key string) (any, error) {
	value, present := item.Get(key)
	if !present {
		return nil, pyErr("KeyError", "%s", jsonx.Repr(key))
	}
	return value, nil
}

// SubscriptionRows is routes/tasks._subscription_rows(shared, statuses).
func SubscriptionRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	shared, err := sharedOf(ctx)
	if err != nil {
		return nil, err
	}
	items, err := objectsOf(shared.GetOr("subscriptions", []any{}))
	if err != nil {
		return nil, err
	}
	rows := []*jsonx.Object{}
	for _, sub := range items {
		id, err := requiredValue(sub, "id")
		if err != nil {
			return nil, err
		}
		key := SubscriptionKey(id)
		entry := entryOr(statuses, key)
		interval := SubscriptionInterval(sub)
		hasURL := jsonx.Truthy(sub.Value("url"))
		enabled := isNotFalse(sub.GetOr("enabled", true)) && hasURL
		var due *time.Time
		if enabled {
			baseline, err := SubscriptionCacheTime(ctx, sub)
			if err != nil {
				return nil, err
			}
			if due, err = NextRunAt(entry, interval, baseline, nil); err != nil {
				return nil, err
			}
		}
		history, err := historyOf(entry)
		if err != nil {
			return nil, err
		}
		hint, message := "", ""
		if !hasURL {
			hint = "没有订阅地址"
		}
		if !isNotFalse(sub.GetOr("enabled", true)) {
			message = "订阅已停用"
		}
		fields := []any{"name", orDefault(sub.Value("name"), id), "resource_id", id,
			"enabled", enabled && jsonx.Truthy(interval), "interval_seconds", interval, "editable", hasURL,
			"notifications_enabled", enabled, "can_run", hasURL, "run_hint", hint,
			"running", IsRunning(key), "next_run_at", tasks.ISO(due), "state_message", message,
			"link", "/subscriptions"}
		rows = append(rows, tasks.Row(key, "subscription", "", append(fields, tasks.Spread(history)...)...))
	}
	return rows, nil
}

// RuleRows is routes/tasks._rule_rows(shared, statuses).
func RuleRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	shared, err := sharedOf(ctx)
	if err != nil {
		return nil, err
	}
	items, err := objectsOf(shared.GetOr("rule_library", []any{}))
	if err != nil {
		return nil, err
	}
	rows := []*jsonx.Object{}
	for _, rule := range items {
		if !isURLSource(rule) || !jsonx.Truthy(rule.Value("url")) {
			continue
		}
		id, err := requiredValue(rule, "id")
		if err != nil {
			return nil, err
		}
		key := RuleSourceKey(id)
		entry := entryOr(statuses, key)
		interval := RuleInterval(rule)
		enabled := isNotFalse(rule.GetOr("enabled", true))
		var due *time.Time
		if enabled {
			baseline, err := RuleCacheTime(ctx, rule)
			if err != nil {
				return nil, err
			}
			if due, err = NextRunAt(entry, interval, baseline, nil); err != nil {
				return nil, err
			}
		}
		history, err := historyOf(entry)
		if err != nil {
			return nil, err
		}
		if !jsonx.Truthy(history.Value("cache_updated_at")) {
			updated, err := RuleCacheTime(ctx, rule)
			if err != nil {
				return nil, err
			}
			history.Set("cache_updated_at", updated)
		}
		message := ""
		if !enabled {
			message = "规则集已停用"
		}
		fields := []any{"name", orDefault(rule.Value("name"), id), "resource_id", id,
			"detail", host(rule.Value("url")), "enabled", enabled && jsonx.Truthy(interval),
			"interval_seconds", interval, "notifications_enabled", enabled, "editable", true, "can_run", true,
			"running", IsRunning(key), "next_run_at", tasks.ISO(due), "state_message", message,
			"link", "/rule-library"}
		rows = append(rows, tasks.Row(key, "rule_source", "", append(fields, tasks.Spread(history)...)...))
	}
	return rows, nil
}

// runTask is the subscription / rule_source branch of routes/tasks.run_task.
func runTask(ctx context.Context, key, resource string) error {
	category, _, _ := strings.Cut(key, ":")
	_, err := RunNow(ctx, category, resource)
	var busy *TaskBusy
	var lookup *LookupError
	var value *PyError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &busy):
		return &tasks.Busy{Message: busy.Message}
	case errors.As(err, &lookup):
		return &tasks.NotFound{Message: strings.Trim(lookup.Message, "'")}
	case errors.As(err, &value) && value.Class == "ValueError":
		return &tasks.Invalid{Message: value.Message}
	case repo.IsValueError(err):
		return &tasks.Invalid{Message: err.Error()}
	}
	return err
}

// loopSleep waits d or until ctx ends (false); tests replace it.
var loopSleep = func(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// round is one iteration of start_loop's loop.
func round(ctx context.Context) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	defer func() {
		if p := recover(); p != nil {
			logger.Errorf("Scheduled refresh round failed\n%v", p)
		}
	}()
	if _, err := RunRound(scope, nil); err != nil {
		logger.Exception("Scheduled refresh round failed", err)
	}
}

// Loop is start_loop()'s thread: one scheduler across processes, guarded
// by an exclusive non-blocking flock on DATA_DIR/.task-scheduler.lock
// (held until ctx ends); the first round after FirstDelaySeconds, then one
// every TickSeconds.
func Loop(ctx context.Context) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		logger.Exception("Scheduled refresh could not start", err)
		return
	}
	handle, err := os.OpenFile(filepath.Join(r.DataDir(), ".task-scheduler.lock"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		logger.Exception("Scheduled refresh could not start", err)
		return
	}
	defer handle.Close()
	if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return
	}
	if !loopSleep(ctx, FirstDelaySeconds*time.Second) {
		return
	}
	for {
		round(ctx)
		if !loopSleep(ctx, TickSeconds*time.Second) {
			return
		}
	}
}

var startOnce sync.Once

// Wire is C15's entry in internal/server wirePackages: the subscription
// and rule-source rows of the task list with their "run now", and the
// scheduler loop (started once).
func Wire() {
	tasks.SetRows(tasks.SlotSubscriptions, SubscriptionRows)
	tasks.SetRows(tasks.SlotRuleSources, RuleRows)
	tasks.SetRunner("subscription", runTask)
	tasks.SetRunner("rule_source", runTask)
	startOnce.Do(func() { lifecycle.Loop("scheduled-refresh", Loop) })
}
