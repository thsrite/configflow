package agents

import (
	"sort"

	"configflow/internal/model"
	"configflow/internal/redact"
)

// CleanOperationEvents returns the last bounded, well-formed timeline entries.
// Original string tokens are required; R5 projections do not validate protocol
// timestamps or statuses. Adjacent duplicate status/message pairs collapse.
func CleanOperationEvents(events []model.OperationEvent, secrets redact.Secrets, statuses map[string]bool) []model.OperationEvent {
	result := make([]model.OperationEvent, 0, min(len(events), EventLimit))
	if len(events) > EventLimit {
		events = events[len(events)-EventLimit:]
	}
	for i := range events {
		source := &events[i]
		status, statusOK := storedText(source.MemberJSON("status"))
		timestamp, timeOK := storedText(source.MemberJSON("at"))
		at, validTime := NormalizeEventTime(timestamp)
		if !statusOK || !timeOK || !validTime || !statusPattern.MatchString(status) || statuses != nil && !statuses[status] {
			continue
		}
		event := model.OperationEvent{At: at, Status: status}
		if text, valid := storedText(source.MemberJSON("message")); valid {
			if message := redact.EventText(text, secrets); message != "" {
				event.Message = new(message)
			}
		}
		if len(result) == 0 || !sameOperationContent(&result[len(result)-1], &event) {
			result = append(result, event)
		}
	}
	return result
}

func sameOperationContent(a, b *model.OperationEvent) bool {
	left, right := "", ""
	if a.Message != nil {
		left = *a.Message
	}
	if b.Message != nil {
		right = *b.Message
	}
	return a.Status == b.Status && left == right
}

// AppendOperationEvent appends a sanitized stage unless it repeats the last
// status/message. A blank timestamp uses the current UTC clock.
func AppendOperationEvent(events []model.OperationEvent, status, message, at string, secrets redact.Secrets) []model.OperationEvent {
	result := CleanOperationEvents(events, secrets, nil)
	if at == "" {
		at = EventTimeNow()
	}
	incoming := CleanOperationEvents([]model.OperationEvent{{At: at, Status: status, Message: new(message)}}, secrets, nil)
	if len(incoming) > 0 && (len(result) == 0 || !sameOperationContent(&result[len(result)-1], &incoming[0])) {
		result = append(result, incoming[0])
	}
	if len(result) > EventLimit {
		result = result[len(result)-EventLimit:]
	}
	return result
}

// MergeOperationEvents merges a remote journal without replaying previously
// observed empty stage announcements. Timestamp sorting is stable.
func MergeOperationEvents(events, incoming []model.OperationEvent, secrets redact.Secrets, statuses map[string]bool) []model.OperationEvent {
	previous := CleanOperationEvents(events, secrets, statuses)
	received := CleanOperationEvents(incoming, secrets, statuses)
	if len(received) == 0 {
		return previous
	}
	reported := make(map[string]bool, len(received))
	for _, event := range received {
		reported[event.Status] = true
	}
	kept := make([]model.OperationEvent, 0, len(previous)+len(received))
	for _, event := range previous {
		if event.Message != nil && *event.Message != "" || !reported[event.Status] {
			kept = append(kept, event)
		}
	}
	kept = append(kept, received...)
	order := make([]model.OperationEvent, 0, len(kept))
	index := make(map[[3]string]int, len(kept))
	for _, event := range kept {
		message := ""
		if event.Message != nil {
			message = *event.Message
		}
		key := [3]string{event.At, event.Status, message}
		if i, found := index[key]; found {
			order[i] = event
			continue
		}
		index[key] = len(order)
		order = append(order, event)
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].At < order[j].At })
	return CleanOperationEvents(order, secrets, statuses)
}
