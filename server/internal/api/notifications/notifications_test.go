package notifications_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/notifications"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

const listEndpoint = "notifications.list_notifications"

func registered() bool {
	app := httpx.New()
	apinotifications.Register(app.Router)
	return app.Router.HasEndpoint(listEndpoint)
}

// C07 registers the list immediately and reports explicit readiness. Agent
// wiring supplies the real registry even before the task owners are ready.
func TestListRouteRegistersBeforeTaskSlotsAndAgentsSourceIsUnconditional(t *testing.T) {
	notifications.ResetInputs()
	agents.ResetHooks()
	t.Cleanup(func() { notifications.ResetInputs(); agents.ResetHooks() })
	notifications.SetInputs(notifications.Inputs{Rows: tasks.List})
	if tasks.Complete() {
		t.Fatal("precondition: some task slots are still unported")
	}
	agents.Wire()
	if !notifications.Complete() {
		t.Fatal("agents.Wire must install the Agent source while task slots are missing")
	}
	if !registered() || apinotifications.ListComplete() {
		t.Fatal("registration/readiness confused")
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
		t.Cleanup(tasks.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }))
		if last := i == len(missing)-1; apinotifications.ListComplete() != last {
			t.Fatalf("after providing %s (last %v) registered=%v", slot, last, registered())
		}
	}
	// Fully provided: the list answers with the Agents of the registry.
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apinotifications.Register)})
	if !srv.App.Router.HasEndpoint(listEndpoint) {
		t.Fatal("not registered in a test server")
	}
	res := srv.Get("/api/notifications").WantStatus(200)
	var body struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil || !body.Success {
		t.Fatalf("body %s (%v)", res.Body, err)
	}
}
