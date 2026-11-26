package agents

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/redact"
)

func TestEventWindowCountsInvalidEntries(t *testing.T) {
	var event model.OperationEvent
	if err := json.Unmarshal([]byte(`{"at":"2026-01-01T00:00:00Z","status":"ready","future":1e400}`), &event); err != nil {
		t.Fatal(err)
	}
	items := []model.OperationEvent{event}
	for range EventLimit {
		items = append(items, model.OperationEvent{})
	}
	if got := CleanOperationEvents(items, nil, nil); len(got) != 0 {
		t.Fatal("event outside last 80 inputs survived")
	}
	// Unknown fields cannot discard an otherwise valid event.
	if got := CleanOperationEvents([]model.OperationEvent{event}, nil, nil); len(got) != 1 {
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
