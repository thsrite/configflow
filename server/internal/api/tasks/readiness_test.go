package tasks_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/tasks"
)

func TestReadyCommandsDoNotWaitForUnportedOwners(t *testing.T) {
	s := setup(t)
	t.Cleanup(tasks.SetRows(tasks.SlotRouters, nil))
	t.Cleanup(tasks.SetRows(tasks.SlotAgents, nil))
	t.Cleanup(tasks.SetRunner(tasks.SlotRouters, nil))
	called := 0
	t.Cleanup(tasks.SetRunner(tasks.SlotSubscriptions, func(context.Context, string, string) error { called++; return nil }))
	response := s.Get("/api/tasks").WantStatus(503)
	if !reflect.DeepEqual(response.Object().GetList("missing_slots"), []any{tasks.SlotRouters, tasks.SlotAgents}) {
		t.Fatal(response.Text())
	}
	s.Post("/api/tasks/run", httpxtest.JSON(map[string]string{"key": "subscription:s"})).WantStatus(202)
	if called != 1 {
		t.Fatal("ready runner did not execute")
	}
	s.Post("/api/tasks/run", httpxtest.JSON(map[string]string{"key": "router_rule_sync:default:r"})).WantStatus(503)
	s.Post("/api/tasks/run", httpxtest.JSON(map[string]string{"key": "unknown:s"})).WantStatus(400)
	s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "router_rule_sync:default:r", "interval_seconds": 60})).WantStatus(503)
	s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "unknown:s", "interval_seconds": 60})).WantStatus(400)
	s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 60})).WantStatus(200)
	shared, err := s.Repo.GetShared(s.Context())
	if err != nil || shared.GetList("subscriptions")[0].(*jsonx.Object).GetInt("interval") != 60 {
		t.Fatal("ready interval was not saved", err)
	}
}

func TestIntervalPresentationFailureCannotUndoSuccessfulResponse(t *testing.T) {
	s := setup(t)
	calls := 0
	t.Cleanup(tasks.SetRows(tasks.SlotSubscriptions, func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) {
		calls++
		return nil, errors.New("synthetic presentation failure")
	}))
	response := s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 600})).WantStatus(200)
	if response.Object().Value("task") != nil {
		t.Fatal(response.Text())
	}
	if calls != 1 {
		t.Fatal("presentation read before commit or retried", calls)
	}
	shared, err := s.Repo.GetShared(s.Context())
	if err != nil || shared.GetList("subscriptions")[0].(*jsonx.Object).GetInt("interval") != 600 {
		t.Fatal("save did not complete", err)
	}
}

func TestBadPresentationCannotOverrideIntervalValidation(t *testing.T) {
	s := setup(t)
	t.Cleanup(tasks.SetRows(tasks.SlotSubscriptions, func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) {
		t.Error("invalid interval read presentation")
		return nil, errors.New("synthetic presentation failure")
	}))
	s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 30})).WantStatus(400)
	shared, err := s.Repo.GetShared(s.Context())
	if err != nil || shared.GetList("subscriptions")[0].(*jsonx.Object).GetInt("interval") != 86400 {
		t.Fatal("invalid interval committed", err)
	}
}

func TestRunBodyBindingIsSilentAndExact(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apitasks.Register)})
	for _, body := range []string{"{", "[]", "null", "true", `{"key":2}`, `{"Key":"subscription:s"}`, `{"key":"subscription:s","future":NaN}`} {
		s.Post("/api/tasks/run", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
}
