package notifications_test

import (
	"context"
	"testing"

	"configflow/internal/agents"
	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/tasks"
)

const listEndpoint = "notifications.list_notifications"

func registered() bool {
	app := httpx.New()
	apinotifications.Register(app.Router)
	return app.Router.HasEndpoint(listEndpoint)
}

// Coordinator decision 4 (PROGRESS-agents.md): GET /api/notifications reads
// every task row, so it registers only when every notification source and
// every task-row slot is provided; agents.Wire installs the Agent source
// whatever the order of wiring. This test sets the task slots of the whole
// process (internal/tasks has no reset), so it must stay the only test of
// this package that depends on them.
func TestListRouteWaitsForEveryTaskSlotAndAgentsSourceIsUnconditional(t *testing.T) {
	notifications.ResetSources()
	agents.ResetHooks()
	t.Cleanup(func() { notifications.ResetSources(); agents.ResetHooks() })
	notifications.SetSources(notifications.Sources{ListTasks: tasks.ListTasks})
	if tasks.Complete() {
		t.Fatal("precondition: some task slots are still unported")
	}
	agents.Wire()
	if !notifications.Complete() {
		t.Fatal("agents.Wire must install the Agent source while task slots are missing")
	}
	if registered() || apinotifications.ListComplete() {
		t.Fatal("the list registered while task slots are missing")
	}
	if !func() bool {
		app := httpx.New()
		apinotifications.Register(app.Router)
		return app.Router.HasEndpoint("notifications.read_notifications")
	}() {
		t.Fatal("POST /read must not wait")
	}
	missing := tasks.Missing()
	for i, slot := range missing {
		tasks.SetRows(slot, func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil })
		if last := i == len(missing)-1; registered() != last {
			t.Fatalf("after providing %s (last %v) registered=%v", slot, last, registered())
		}
	}
	// Fully provided: the list answers with the Agents of the registry.
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apinotifications.Register)})
	if !srv.App.Router.HasEndpoint(listEndpoint) {
		t.Fatal("not registered in a test server")
	}
	body := srv.Get("/api/notifications").WantStatus(200).Object()
	if body.Value("success") != true {
		t.Fatalf("body %v", body)
	}
}
