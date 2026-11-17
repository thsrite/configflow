//go:build configflow_difftest

package lifecycle_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
)

func TestDifftestEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("CONFIGFLOW_LIFECYCLE_EVENTS", path)
	reg := lifecycle.NewRegistry()
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "round", Round: func(context.Context) error { return nil }})
	r := setup(t, lifecycle.Options{Registry: reg, Mode: lifecycle.ModeManual})
	r.Start(context.Background())
	if err := r.RunRound(context.Background(), "round"); err != nil {
		t.Fatal(err)
	}
	if err := r.Spawn(context.Background(), "job", func(context.Context) {}); err != nil {
		t.Fatal(err)
	}
	idle(t, r)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("wrong event count: %s", data)
	}
	for i, want := range []string{"round-start", "round-end", "job-start", "job-end"} {
		var e map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &e); err != nil || e["event"] != want || e["t"] == nil || e["loop"] == nil {
			t.Fatalf("event %d: %s %v", i, lines[i], err)
		}
	}
}
