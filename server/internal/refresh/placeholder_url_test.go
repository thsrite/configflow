package refresh

import (
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

// The template subscription of a new data directory carries the placeholder
// address "订阅链接". It is treated as not configured: never due (so no
// failure retry every five minutes keeps an on-demand Sub-Store awake), a
// manual run is refused with the invalid-address message, and the task row
// cannot run.
func TestPlaceholderSubscriptionURLIsNotConfigured(t *testing.T) {
	f := setup(t, 86400)
	f.updateShared(func(shared *model.Shared) { shared.Subscriptions[0].URL = "订阅链接" })
	now := f.clock.now.Add(365 * 24 * time.Hour)
	jobs, err := f.svc.due(repo.WithScope(f.ctx), &now)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.Kind == "subscription" {
			t.Fatalf("placeholder subscription scheduled: %+v", job.Subscription)
		}
	}
	if _, err := f.svc.LookupResource(f.ctx, "subscription", "s"); err == nil || err.Error() != msgInvalidSourceURL {
		t.Fatalf("manual run: %v", err)
	}
	rows, err := f.svc.SubscriptionTasks(f.ctx, taskstatus.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CanRun || rows[0].Enabled || rows[0].RunHint != msgInvalidSourceURL {
		t.Fatalf("task row %+v", rows[0])
	}
	// An empty address keeps its own hint.
	f.updateShared(func(shared *model.Shared) { shared.Subscriptions[0].URL = "" })
	if rows, err = f.svc.SubscriptionTasks(f.ctx, taskstatus.Snapshot{}); err != nil || rows[0].RunHint != msgNoURL {
		t.Fatalf("empty address row %+v %v", rows, err)
	}
	if _, err := f.svc.LookupResource(f.ctx, "subscription", "s"); err == nil || err.Error() != msgNoSourceAddress {
		t.Fatalf("empty address manual run: %v", err)
	}
}
