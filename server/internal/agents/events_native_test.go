package agents

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/redact"
)

func TestTypedEventsBindEachOriginalToken(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{`{"at":"2026-01-01T00:00:00Z","status":"ready","message":7}`, `[{"at":"2026-01-01T00:00:00.000000Z","status":"ready"}]`},
		{`{"at":"2026-01-01T00:00:00Z","status":7}`, `[]`},
		{`{"at":null,"status":"ready"}`, `[]`},
		{`{"at":"2026-01-01T00:00:00Z","status":"ready","message":"token=synthetic-secret"}`, `[{"at":"2026-01-01T00:00:00.000000Z","status":"ready","message":"token=[REDACTED]"}]`},
		{`{"at":"2026-01-01T00:00:00Z","status":"ready","message":"fetch https://example.com/config?token=synthetic-secret"}`, `[{"at":"2026-01-01T00:00:00.000000Z","status":"ready","message":"fetch [下载地址已隐藏]"}]`},
	} {
		var event model.OperationEvent
		if err := json.Unmarshal([]byte(tt.raw), &event); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(event)
		got, err := json.Marshal(CleanOperationEvents([]model.OperationEvent{event}, nil, nil))
		if err != nil || string(got) != tt.want {
			t.Fatalf("input=%s got=%s want=%s err=%v", tt.raw, got, tt.want, err)
		}
		after, _ := json.Marshal(event)
		if !bytes.Equal(before, after) {
			t.Fatal("source event mutated")
		}
	}
}

func TestLegacyEventWindowCountsInvalidEntries(t *testing.T) {
	var event Record
	if err := json.Unmarshal([]byte(`{"at":"2026-01-01T00:00:00Z","status":"ready"}`), &event); err != nil {
		t.Fatal(err)
	}
	items := []any{&event}
	for range EventLimit {
		items = append(items, nil)
	}
	if got := CleanEvents(items, nil, nil); len(got) != 0 {
		t.Fatal("event outside last 80 inputs survived")
	}
	// Unknown non-finite fields cannot discard an otherwise valid event.
	event.Set("future", jsonx.Float(math.Inf(1)))
	if got := CleanEvents([]any{&event}, nil, nil); len(got) != 1 {
		t.Fatal("unknown field discarded valid event")
	}
}

func FuzzTypedOperationEvents(f *testing.F) {
	f.Add([]byte(`[{"at":"2026-01-01T00:00:00Z","status":"ready","message":"fetch https://example.com/config?token=synthetic-secret"}]`))
	f.Add([]byte(`[null,7,{"at":7,"status":"ready","message":false}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var items []json.RawMessage
		if json.Unmarshal(data, &items) != nil {
			return
		}
		events := make([]model.OperationEvent, len(items))
		for i, raw := range items {
			_ = json.Unmarshal(raw, &events[i])
		}
		before, err := json.Marshal(events)
		if err != nil {
			return
		}
		clean := CleanOperationEvents(events, redact.Secrets{"synthetic-secret"}, nil)
		merged := MergeOperationEvents(clean, events, redact.Secrets{"synthetic-secret"}, nil)
		if len(clean) > EventLimit || len(merged) > EventLimit {
			t.Fatal("event bound exceeded")
		}
		for i, event := range merged {
			if !statusPattern.MatchString(event.Status) {
				t.Fatal("invalid status escaped")
			}
			if event.Message != nil && len(*event.Message) > MessageLimit {
				t.Fatal("message bound exceeded")
			}
			if i > 0 && event.At < merged[i-1].At {
				t.Fatal("merged events out of order")
			}
		}
		after, err := json.Marshal(events)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("events mutated", err)
		}
	})
}

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
