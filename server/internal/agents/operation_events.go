package agents

// Port of backend/agents/operation_events.py: small, sanitized timelines
// for long operations, separate from recovery state.

import (
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"configflow/internal/agents/diag"
)

// Limits of operation_events.py.
const (
	EventLimit   = 80
	MessageLimit = diag.MessageLimit
)

var statusPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}\z`)

// EventTimeNow is event_time(): the current UTC time with microseconds and "Z".
func EventTimeNow() string { return formatEventTime(clock()) }

// EventTime is event_time(value) for a value: an aware ISO time normalized to
// UTC with microseconds and "Z"; ok=false for anything else (non-strings,
// naive times, more than 64 characters, out of range).
func EventTime(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return NormalizeEventTime(text)
}

// NormalizeEventTime normalizes a bounded, aware ISO timestamp to UTC.
func NormalizeEventTime(text string) (string, bool) {
	if utf8.RuneCountInString(text) > 64 {
		return "", false
	}
	moment, naive, ok := fromISO(text)
	if !ok || naive {
		return "", false
	}
	utc := moment.UTC()
	if utc.Year() < 1 || utc.Year() > 9999 {
		return "", false
	}
	return formatEventTime(utc), true
}

func formatEventTime(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d.%06dZ", t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1000)
}
