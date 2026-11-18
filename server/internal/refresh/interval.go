// Package refresh is the port of backend/utils/subscription_refresh.py,
// backend/utils/subscription_health.py and backend/utils/scheduled_refresh.py:
// downloading subscriptions and remote rule sources into their validated
// caches, the subscription fetch history (健康条) and the server-side
// scheduler that refreshes every source when its interval is due, with the
// rows, "run now" and interval edits of 系统设置 → 定时任务
// (routes/tasks.py::_subscription_rows / _rule_rows).
//
// All network access goes through internal/substore (subscriptions,
// fetch_userinfo) and internal/rulefetch (rule sources); time comes from
// Env.Clock. Wire registers the scheduler loop and the task-list providers.
package refresh

import (
	"bytes"
	"configflow/internal/model"
	"encoding/json"
	"strconv"
)

// Intervals of subscription_refresh.py.
const (
	DefaultSourceInterval   = 86400
	DefaultProviderInterval = 3600
	MaxInterval             = 604800
)

// DefaultIntervalLabel is validate_refresh_interval's default label.
const DefaultIntervalLabel = "订阅源更新间隔"

// IntervalError is the ValueError of validate_refresh_interval.
type IntervalError struct{ Message string }

func (e *IntervalError) Error() string { return e.Message }

// CheckInterval validates a typed interval supplied by the source owner.
func CheckInterval(value model.Int, label string) (int64, error) {
	if label == "" {
		label = DefaultIntervalLabel
	}
	seconds := int64(value)
	if seconds == 0 || (seconds >= 60 && seconds <= MaxInterval) {
		return seconds, nil
	}
	return 0, &IntervalError{Message: label + msgIntervalRange + strconv.Itoa(MaxInterval) + msgIntervalUnit}
}

// ParseInterval validates the strict request form; quoted numbers/null are
// not interval edits even though stored model.Int can read legacy number text.
func ParseInterval(raw json.RawMessage, label string) (int64, error) {
	var seconds int64
	if len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, &seconds) == nil {
		return CheckInterval(model.Int(seconds), label)
	}
	if label == "" {
		label = DefaultIntervalLabel
	}
	return 0, &IntervalError{Message: label + msgIntervalRange + strconv.Itoa(MaxInterval) + msgIntervalUnit}
}

// SourceInterval returns the model value or the scheduling default.
func SourceInterval(source model.Subscription) model.Int {
	if source.Interval == nil {
		return DefaultSourceInterval
	}
	return *source.Interval
}

// RemoteInterval returns the model value or the scheduling default.
func RemoteInterval(source model.RuleLibraryItem) model.Int {
	if source.UpdateInterval == nil {
		return DefaultRuleInterval
	}
	return *source.UpdateInterval
}
