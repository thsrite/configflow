package refresh

import (
	"configflow/internal/model"
	"configflow/internal/taskstatus"
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// ErrScheduleTime rejects a due time outside the supported calendar range.
var ErrScheduleTime = errors.New("refresh due time is outside years 1..9999")

// ErrFailureCount rejects a task receipt with an invalid retry counter.
var ErrFailureCount = errors.New("invalid refresh failure counter")
var stampOffset = regexp.MustCompile(`^[+-][0-9]{2}(:?[0-9]{2}(:?[0-9]{2}([.,][0-9]+)?)?)?$`)

func parseStamp(text string) (time.Time, bool) {
	instant, ok := taskstatus.ParseTimestamp(text)
	if !ok {
		return time.Time{}, false
	}
	text = strings.ReplaceAll(text, "Z", "+00:00")
	if i := strings.LastIndexAny(text, "+-"); i >= 7 && stampOffset.MatchString(text[i:]) {
		if local, ok := taskstatus.FromISOFormat(text[:i]); ok {
			return instant.In(time.FixedZone("", int(local.Sub(instant)/time.Second))), true
		}
	}
	return instant, true
}

// NextDue uses a typed receipt, retained cache baseline and bounded retry
// backoff. It never writes a receipt or advances a cache timestamp.
func NextDue(entry *taskstatus.Receipt, interval model.Int, baseline *string, now *time.Time) (*time.Time, error) {
	if interval == 0 {
		return nil, nil
	}
	if entry == nil {
		entry = &taskstatus.Receipt{}
	}
	lastText := textOr(entry.LastRunAt, "")
	if raw, present, err := entry.Raw.MemberJSON("last_run_at", entry.LastRunAt); err != nil || (present && (len(raw) == 0 || raw[0] != '"')) {
		lastText = ""
	}
	last, ok := parseStamp(lastText)
	if !ok {
		last, ok = parseStamp(textOr(baseline, ""))
	}
	if !ok {
		if now != nil {
			return now, nil
		}
		current := refreshEnv().Clock.Now().UTC().Truncate(time.Microsecond)
		return &current, nil
	}
	delay := int64(interval)
	if textOr(entry.LastStatus, "") == "failed" {
		failures, err := taskstatus.Count(entry.ConsecutiveFailures)
		if err != nil {
			return nil, errors.Join(ErrFailureCount, err)
		}
		exponent := int64(0)
		if failures.Cmp(big.NewInt(11)) >= 0 {
			exponent = 10
		} else if failures.Sign() > 0 {
			exponent = failures.Int64() - 1
		}
		delay = min(delay, int64(RetrySeconds)<<exponent)
	}
	seconds := new(big.Int).Add(big.NewInt(last.Unix()), big.NewInt(delay))
	if !seconds.IsInt64() {
		return nil, ErrScheduleTime
	}
	due := time.Unix(seconds.Int64(), int64(last.Nanosecond())).In(last.Location())
	if due.Year() < 1 || due.Year() > 9999 {
		return nil, ErrScheduleTime
	}
	return &due, nil
}
func fresh(timestamp string, interval int64) bool {
	updated, ok := taskstatus.ParseTimestamp(timestamp)
	if !ok {
		return false
	}
	now := refreshEnv().Clock.Now()
	micros := (now.Unix()-updated.Unix())*1000000 + int64(now.Nanosecond()/1000) - int64(updated.Nanosecond()/1000)
	return micros >= 0 && micros < interval*1000000
}
func textOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
func checkOwner(ctx context.Context) error { return ctx.Err() }
