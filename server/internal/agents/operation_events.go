package agents

// Port of backend/agents/operation_events.py: small, sanitized timelines
// for long operations, separate from recovery state.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
)

// Limits of operation_events.py.
const (
	EventLimit   = 80
	MessageLimit = diag.MessageLimit
)

var statusPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}\z`)

// EventMessage is event_message(value, secrets): download addresses hidden,
// then FailureText with the 1024-byte limit.
func EventMessage(value any, secrets []any) string { return diag.EventMessage(value, secrets) }

// EventTimeNow is event_time(): the current UTC time with microseconds and "Z".
func EventTimeNow() string { return formatEventTime(clock()) }

// EventTime is event_time(value) for a value: an aware ISO time normalized to
// UTC with microseconds and "Z"; ok=false for anything else (non-strings,
// naive times, more than 64 characters, out of range).
func EventTime(value any) (string, bool) {
	text, ok := value.(string)
	if !ok || utf8.RuneCountInString(text) > 64 {
		return "", false
	}
	moment, naive, ok := fromISO(strings.ReplaceAll(text, "Z", "+00:00"))
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

// CleanEvents is clean_events(value, secrets, statuses): the last 80
// well-formed events, sanitized, adjacent duplicates dropped. A nil statuses
// set accepts every status.
func CleanEvents(value any, secrets []any, statuses map[string]bool) []any {
	list, ok := value.([]any)
	result := []any{}
	if !ok {
		return result
	}
	if len(list) > EventLimit {
		list = list[len(list)-EventLimit:]
	}
	for _, item := range list {
		entry, ok := item.(*jsonx.Object)
		if !ok || entry == nil {
			continue
		}
		status, statusOK := entry.Value("status").(string)
		at, atOK := EventTime(entry.Value("at"))
		if !statusOK || !statusPattern.MatchString(status) || !atOK || (statuses != nil && !statuses[status]) {
			continue
		}
		event := jsonx.Obj("at", at, "status", status)
		if message := EventMessage(entry.Value("message"), secrets); message != "" {
			event.Set("message", message)
		}
		if len(result) == 0 || !sameContent(result[len(result)-1].(*jsonx.Object), event) {
			result = append(result, event)
		}
	}
	return result
}

func eventContent(event *jsonx.Object) (string, string) {
	message, _ := event.GetOr("message", "").(string)
	return event.GetString("status"), message
}

func sameContent(a, b *jsonx.Object) bool {
	as, am := eventContent(a)
	bs, bm := eventContent(b)
	return as == bs && am == bm
}

// AppendEvent is append_event(events, status, message, at=..., secrets=...).
// message nil is None; at "" is now.
func AppendEvent(events any, status string, message any, at string, secrets []any) []any {
	result := CleanEvents(events, secrets, nil)
	if at == "" {
		at = EventTimeNow()
	}
	event := CleanEvents([]any{jsonx.Obj("at", at, "status", status, "message", message)}, secrets, nil)
	if len(event) > 0 && (len(result) == 0 || !sameContent(result[len(result)-1].(*jsonx.Object), event[0].(*jsonx.Object))) {
		result = append(result, event[0])
	}
	if len(result) > EventLimit {
		result = result[len(result)-EventLimit:]
	}
	return result
}

// MergeEvents is merge_events(events, incoming, secrets=..., statuses=...):
// merges an Agent journal without replaying already-observed stages.
func MergeEvents(events, incoming any, secrets []any, statuses map[string]bool) []any {
	previous := CleanEvents(events, secrets, statuses)
	received := CleanEvents(incoming, secrets, statuses)
	if len(received) == 0 {
		return previous
	}
	reported := map[string]bool{}
	for _, item := range received {
		reported[item.(*jsonx.Object).GetString("status")] = true
	}
	kept := []any{}
	for _, item := range previous {
		event := item.(*jsonx.Object)
		if jsonx.Truthy(event.Value("message")) || !reported[event.GetString("status")] {
			kept = append(kept, event)
		}
	}
	type keyed struct {
		key   [3]string
		event *jsonx.Object
	}
	var order []keyed
	index := map[[3]string]int{}
	for _, item := range append(kept, received...) {
		event := item.(*jsonx.Object)
		status, message := eventContent(event)
		key := [3]string{event.GetString("at"), status, message}
		if i, ok := index[key]; ok {
			order[i].event = event
			continue
		}
		index[key] = len(order)
		order = append(order, keyed{key, event})
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].key[0] < order[j].key[0] })
	ordered := make([]any, len(order))
	for i, item := range order {
		ordered[i] = item.event
	}
	return CleanEvents(ordered, secrets, statuses)
}
