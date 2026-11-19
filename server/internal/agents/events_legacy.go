package agents

import (
	"encoding/json"

	"configflow/internal/model"
	"configflow/internal/redact"
)

// legacyEvents is the old dynamic input codec. It accepts only original string
// fields, so unknown members (including non-finite legacy values) are ignored.
func legacyEvents(value any) []model.OperationEvent {
	items, _ := value.([]any)
	events := make([]model.OperationEvent, 0, len(items))
	for _, item := range items {
		record, ok := item.(*Record)
		if !ok || record == nil {
			events = append(events, model.OperationEvent{})
			continue
		}
		at, _ := record.Value("at").(string)
		status, _ := record.Value("status").(string)
		event := model.OperationEvent{At: at, Status: status}
		if text, ok := record.Value("message").(string); ok {
			event.Message = new(text)
		}
		events = append(events, event)
	}
	return events
}

func legacySecrets(values []any) redact.Secrets {
	var secrets []string
	for _, value := range values {
		if text, ok := value.(string); ok {
			secrets = append(secrets, text)
		}
	}
	return redact.NewSecrets(secrets)
}

func legacyEventResults(events []model.OperationEvent) []any {
	raw, err := json.Marshal(events)
	if err != nil {
		return []any{}
	}
	var records []*Record
	if json.Unmarshal(raw, &records) != nil {
		return []any{}
	}
	result := make([]any, len(records))
	for i, record := range records {
		result[i] = record
	}
	return result
}

// CleanEvents translates the old event-list boundary to the native timeline.
func CleanEvents(value any, secrets []any, statuses map[string]bool) []any {
	return legacyEventResults(CleanOperationEvents(legacyEvents(value), legacySecrets(secrets), statuses))
}

// AppendEvent translates one old event append to the native timeline.
func AppendEvent(events any, status string, message any, at string, secrets []any) []any {
	text, _ := message.(string)
	return legacyEventResults(AppendOperationEvent(legacyEvents(events), status, text, at, legacySecrets(secrets)))
}

// MergeEvents translates old journal lists to the native merge implementation.
func MergeEvents(events, incoming any, secrets []any, statuses map[string]bool) []any {
	return legacyEventResults(MergeOperationEvents(legacyEvents(events), legacyEvents(incoming), legacySecrets(secrets), statuses))
}

// EventMessage is the compatibility codec for a bounded timeline message.
func EventMessage(value any, secrets []any) string {
	text, _ := value.(string)
	return redact.EventText(text, legacySecrets(secrets))
}
