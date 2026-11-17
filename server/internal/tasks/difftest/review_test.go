package difftest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/tasks"
)

func TestReviewC07FixtureRestoresRunner(t *testing.T) {
	want := errors.New("synthetic original runner")
	t.Cleanup(tasks.SetRunner(tasks.SlotSubscriptions, func(context.Context, string, string) error { return want }))
	t.Run("fixture", func(t *testing.T) { newWorld(t) })
	if err := tasks.Runner(tasks.SlotSubscriptions)(context.Background(), "subscription:s", "s"); !errors.Is(err, want) {
		t.Fatal("fixture leaked its production runner", err)
	}
}

func TestReviewC07LegacyCountersAndSilentBinding(t *testing.T) {
	w := newWorld(t)
	commands := w.commands([]command{
		{Method: "GET", Path: "/api/tasks", Write: map[string]json.RawMessage{"task-status.json": []byte(`{"subscription:s":{"run_count":"1_000","failure_count":" +01_002 ","last_run_at":"2026-11-18T08:00:00Z"},"rule_source:r":{"last_run_at":"2026-11-18T08:00:00Z"}}`)}},
		{Method: "GET", Path: "/api/notifications"},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"KEY":"subscription:s"}`},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":null}`},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":[],"key":"subscription:s"}`},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":"subscription:s","key":null}`},
		{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{"stale":"one"},"versions":{}}`},
		{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{"stale":"one"}} {}`},
	})
	want := python(t, w, commands)
	for i, c := range commands {
		t.Run(c.Method+" "+c.Path+"/"+c.Body, func(t *testing.T) { compare(t, w, c, send(t, w, c), want[i], true) })
	}
}
