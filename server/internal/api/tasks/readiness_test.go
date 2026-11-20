package tasks_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func TestReadyCommandsDoNotWaitForUnportedOwners(t *testing.T) {
	s := setup(t)
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotRouters, nil))
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotAgents, nil))
	t.Cleanup(tasks.SetRunner(tasks.SlotRouters, nil))
	called := 0
	t.Cleanup(tasks.SetRunner(tasks.SlotSubscriptions, func(context.Context, string, string) error { called++; return nil }))
	response := s.Get("/api/tasks").WantStatus(503)
	if !testjson.Equal(decodeMap(t, response.Body)["missing_slots"], []any{tasks.SlotRouters, tasks.SlotAgents}) {
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
	shared, err := s.Repo.Shared(s.Context())
	if err != nil || len(shared.Subscriptions) == 0 || !testjson.Equal(shared.Subscriptions[0].Interval, 60) {
		t.Fatal("ready interval was not saved", err)
	}
}

func TestIntervalPresentationFailureCannotUndoSuccessfulResponse(t *testing.T) {
	s := setup(t)
	calls := 0
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotSubscriptions, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) {
		calls++
		return nil, errors.New("synthetic presentation failure")
	}))
	response := s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 600})).WantStatus(200)
	if decodeMap(t, response.Body)["task"] != nil {
		t.Fatal(response.Text())
	}
	if calls != 1 {
		t.Fatal("presentation read before commit or retried", calls)
	}
	shared, err := s.Repo.Shared(s.Context())
	if err != nil || len(shared.Subscriptions) == 0 || !testjson.Equal(shared.Subscriptions[0].Interval, 600) {
		t.Fatal("save did not complete", err)
	}
}

func TestBadPresentationCannotOverrideIntervalValidation(t *testing.T) {
	s := setup(t)
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotSubscriptions, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) {
		t.Error("invalid interval read presentation")
		return nil, errors.New("synthetic presentation failure")
	}))
	s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 30})).WantStatus(400)
	shared, err := s.Repo.Shared(s.Context())
	if err != nil || len(shared.Subscriptions) == 0 || !testjson.Equal(shared.Subscriptions[0].Interval, 86400) {
		t.Fatal("invalid interval committed", err)
	}
}

func TestRunBodyBindingIsSilentAndExact(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apitasks.Register)})
	for _, body := range []string{"{", "[]", "null", "true", `{"key":2}`, `{"Key":"subscription:s"}`, `{"key":"subscription:s","future":NaN}`} {
		s.Post("/api/tasks/run", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
}

func TestReviewUnknownExactKeyDoesNotBecomeUnavailable(t *testing.T) {
	s := setup(t)
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotMetrics, nil))
	key := tasks.MetricsTaskKey + ":unknown"
	s.Post("/api/tasks/run", httpxtest.JSON(map[string]string{"key": key})).WantStatus(400)
	assertJSON(t, s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": key, "interval_seconds": 600})).WantStatus(400),
		map[string]any{"success": false, "message": "这个任务的间隔不能在这里修改"})
	s.Post("/api/tasks/run", httpxtest.JSON(map[string]string{"key": tasks.MetricsTaskKey})).WantStatus(503)
	s.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": tasks.MetricsTaskKey, "interval_seconds": 600})).WantStatus(503)
}

func TestReviewRunFailuresUseCentralRedaction(t *testing.T) {
	s := setup(t)
	secret := "synthetic-private-task-token"
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleProxyToken = secret
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{&tasks.Busy{Message: secret}, 409},
		{&tasks.Conflict{Message: secret}, 409},
		{&tasks.NotFound{Message: secret}, 404},
		{&tasks.Invalid{Message: secret}, 409},
	} {
		t.Run(tc.err.Error()+"/"+reflect.TypeOf(tc.err).String(), func(t *testing.T) {
			t.Cleanup(tasks.SetRunner(tasks.SlotSubscriptions, func(context.Context, string, string) error { return tc.err }))
			assertJSON(t, s.Post("/api/tasks/run", httpxtest.JSON(map[string]string{"key": "subscription:s"})).WantStatus(tc.status),
				map[string]any{"success": false, "message": "[REDACTED]"})
		})
	}
}
