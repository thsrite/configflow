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
	"math/big"
	"strconv"

	"configflow/internal/jsonx"
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

func (e *IntervalError) Error() string       { return e.Message }
func (e *IntervalError) PythonClass() string { return "ValueError" }

// isPyInt is `type(value) is int`: a JSON integer (never a float or a bool).
// Go integers are accepted for callers that build values in code.
func isPyInt(value any) (*big.Int, bool) {
	switch v := value.(type) {
	case jsonx.Number:
		if v.IsInt() {
			return v.BigInt()
		}
	case int:
		return big.NewInt(int64(v)), true
	case int64:
		return big.NewInt(v), true
	}
	return nil, false
}

// ValidateRefreshInterval is validate_refresh_interval(value, label): an
// int that is 0 (automatic updates off) or 60 ~ MaxInterval seconds. An
// empty label is the default label.
func ValidateRefreshInterval(value any, label string) (int64, error) {
	if label == "" {
		label = DefaultIntervalLabel
	}
	if n, ok := isPyInt(value); ok && n.IsInt64() {
		v := n.Int64()
		if v == 0 || (60 <= v && v <= MaxInterval) {
			return v, nil
		}
	}
	return 0, &IntervalError{Message: label + "必须为 0（关闭自动更新）或 60 ~ " + strconv.Itoa(MaxInterval) + " 秒的整数"}
}

// intervalOf is `value if type(value) is int else default` for a value read
// with .get(key, default): the int as stored (any size), else the default.
func intervalOf(item *jsonx.Object, key string, fallback int64) jsonx.Number {
	value, present := item.Get(key)
	if !present {
		return jsonx.Int(fallback)
	}
	if n, ok := isPyInt(value); ok {
		return jsonx.BigInt(n)
	}
	return jsonx.Int(fallback)
}

// SubscriptionInterval is scheduled_refresh.subscription_interval(sub).
func SubscriptionInterval(sub *jsonx.Object) jsonx.Number {
	return intervalOf(sub, "interval", DefaultSourceInterval)
}

// RuleInterval is scheduled_refresh.rule_interval(rule).
func RuleInterval(rule *jsonx.Object) jsonx.Number {
	return intervalOf(rule, "update_interval", DefaultRuleInterval)
}
