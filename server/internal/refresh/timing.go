package refresh

import (
	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"errors"
	"math/big"
	"time"
)

// ErrScheduleTime rejects a due time outside the supported calendar range.
var ErrScheduleTime = errors.New("refresh due time is outside years 1..9999")

// ErrFailureCount rejects a task receipt with an invalid retry counter.
var ErrFailureCount = errors.New("invalid refresh failure counter")

// parseStamp reads a receipt time keeping its offset (naive texts as UTC).
func parseStamp(text string) (time.Time, bool) { return taskstatus.ParseTimestamp(text) }

// NextDue uses a typed receipt, retained cache baseline and bounded retry
// backoff. It never writes a receipt or advances a cache timestamp.
func NextDue(entry *taskstatus.Receipt, interval model.Int, baseline *string, now *time.Time) (*time.Time, error) {
	if interval == 0 {
		return nil, nil
	}
	if entry == nil {
		entry = &taskstatus.Receipt{}
	}
	// last_run_at that is not text reads as empty (text:"strict").
	lastText := textOr(entry.LastRunAt, "")
	last, ok := parseStamp(lastText)
	if !ok {
		last, ok = parseStamp(textOr(baseline, ""))
	}
	if !ok {
		if now != nil {
			return now, nil
		}
		current := substore.SystemClock.Now().UTC().Truncate(time.Microsecond)
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
func (s *Service) nextDue(entry *taskstatus.Receipt, interval model.Int, baseline *string, now *time.Time) (*time.Time, error) {
	if now == nil {
		current := s.nowUTC()
		now = &current
	}
	return NextDue(entry, interval, baseline, now)
}
func fresh(clock substore.Clock, timestamp string, interval int64) bool {
	updated, ok := taskstatus.ParseTimestamp(timestamp)
	if !ok {
		return false
	}
	now := clock.Now()
	micros := (now.Unix()-updated.Unix())*1000000 + int64(now.Nanosecond()/1000) - int64(updated.Nanosecond()/1000)
	return micros >= 0 && micros < interval*1000000
}
func textOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
