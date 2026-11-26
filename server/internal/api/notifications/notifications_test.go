package notifications_test

import (
	"context"
	"encoding/json"
	"testing"

	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

const listEndpoint = "notifications.list_notifications"

func TestListRouteRegistersBeforeTaskSlotsAndAgentsSourceIsUnconditional(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{})
	reg := tasks.NewRegistry(srv.Repo, nil)
	svc := notifications.New(notifications.Options{Repo: srv.Repo, Tasks: reg, Rows: reg.List})
	h := apinotifications.NewHandler(reg, svc, srv.Repo)
	h.Register(srv.App.Router)
	if reg.Complete() {
		t.Fatal("precondition: some task slots are still unported")
	}
	svc.SetInputs(notifications.Inputs{Agents: func(context.Context) ([]model.Agent, error) { return nil, nil }})
	if !svc.Complete() {
		t.Fatal("installing the Agent source must complete notifications while task slots are missing")
	}
	if !srv.App.Router.HasEndpoint(listEndpoint) || (svc.Complete() && reg.Complete()) {
		t.Fatal("registration/readiness confused")
	}
	if !srv.App.Router.HasEndpoint("notifications.read_notifications") {
		t.Fatal("POST /read must not wait")
	}
	missing := reg.Missing()
	for i, slot := range missing {
		t.Cleanup(reg.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }))
		if last := i == len(missing)-1; (svc.Complete() && reg.Complete()) != last {
			t.Fatalf("after providing %s (last %v)", slot, last)
		}
	}
	res := srv.Get("/api/notifications").WantStatus(200)
	var body struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil || !body.Success {
		t.Fatalf("body %s (%v)", res.Body, err)
	}
}
