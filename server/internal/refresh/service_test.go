package refresh

import (
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/tasks"
)

func TestServiceOnRulesRefreshedIsolation(t *testing.T) {
	first := New(Options{})
	second := New(Options{})

	var firstCount, secondCount int
	first.OnRulesRefreshed(func() { firstCount++ })
	second.OnRulesRefreshed(func() { secondCount++ })

	first.NotifyRulesRefreshed()
	if firstCount != 1 || secondCount != 0 {
		t.Fatalf("expected isolated rule refresh callbacks (1, 0), got (%d, %d)", firstCount, secondCount)
	}
	second.NotifyRulesRefreshed()
	if firstCount != 1 || secondCount != 1 {
		t.Fatalf("expected isolated rule refresh callbacks (1, 1), got (%d, %d)", firstCount, secondCount)
	}
}

func TestServiceRegisterInstallsTasksAndPeriodic(t *testing.T) {
	rt := lifecycle.NewRuntime(lifecycle.Options{})
	tk := tasks.NewRegistry(nil, rt)
	svc := New(Options{Runtime: rt, Tasks: tk})
	svc.Register(rt, tk)

	for _, category := range []string{"subscription", "rule_source"} {
		if tk.Runner(category) == nil {
			t.Fatalf("expected runner for %q to be registered", category)
		}
	}
	found := false
	for _, name := range rt.Names() {
		if name == "scheduled-refresh" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scheduled-refresh periodic loop to be registered, got %v", rt.Names())
	}
}
