package tasks_test

import (
	"configflow/internal/lifecycle"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	apilogs "configflow/internal/api/logs"
	apinotifications "configflow/internal/api/notifications"
	apistats "configflow/internal/api/stats"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/refresh"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testgolden"
)

const tasksJWTSecret = "c07-synthetic-jwt-key"

var tasksFixedTime = time.Date(2026, 11, 18, 12, 0, 0, 123456000, time.UTC)

func normalizeTasksGolden(v any, dataDir string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if k == "updated_at" || k == "modified" || k == "created" || k == "created_at" {
				if s, ok := item.(string); ok && s != "" {
					if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
						out[k] = "<time>"
						continue
					}
					if _, err := time.Parse("2006-01-02T15:04:05.999999", s); err == nil {
						out[k] = "<local-time>"
						continue
					}
				}
			}
			out[k] = normalizeTasksGolden(item, dataDir)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeTasksGolden(item, dataDir)
		}
		return out
	case string:
		return strings.ReplaceAll(x, dataDir, "<DATA>")
	}
	return v
}

func TestTasksRoutesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	s := httpxtest.New(t, httpxtest.Options{
		Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: tasksJWTSecret,
	})
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	t.Cleanup(func() { rt.Stop(5 * time.Second) })
	registry := tasks.NewRegistry(s.Repo, rt)
	empty := func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }
	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains} {
		registry.SetRowBuilder(slot, empty)
	}
	refreshService := refresh.New(refresh.Options{Repo: s.Repo})
	registry.SetRowBuilder(tasks.SlotSubscriptions, refreshService.SubscriptionTasks)
	registry.SetRowBuilder(tasks.SlotRuleSources, refreshService.RuleTasks)
	metrics.New(metrics.Options{Repo: s.Repo}).RegisterTasks(registry)
	for _, category := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources} {
		registry.SetRunner(category, func(context.Context, string, string) error { return nil })
	}
	notify := notifications.New(notifications.Options{
		Repo: s.Repo, Tasks: registry, Agents: func(context.Context) ([]model.Agent, error) { return nil, nil },
		Clock: func() time.Time { return tasksFixedTime },
	})
	previousClock := notifications.Clock
	notifications.Clock = func() time.Time { return tasksFixedTime }
	t.Cleanup(func() { notifications.Clock = previousClock })
	stats := apistats.NewHandler(s.Repo)
	stats.Now = func() time.Time { return tasksFixedTime }
	stats.Register(s.App.Router)
	apilogs.NewHandler().Register(s.App.Router)
	apitasks.NewHandlerWithRepo(registry, s.Repo).Register(s.App.Router)
	apinotifications.NewHandler(registry, notify, s.Repo).Register(s.App.Router)
	if err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, shared *model.Shared) error {
		if err := shared.SetJSON("subscriptions", json.RawMessage(`[{"id":"s","name":"Synthetic feed","url":"https://feed.example.com/sub","interval":3600,"enabled":true}]`)); err != nil {
			return err
		}
		return shared.SetJSON("rule_library", json.RawMessage(`[{"id":"r","name":"Synthetic rules","source_type":"url","url":"https://rules.example.com/list","update_interval":3600,"enabled":true}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "fixture-capability"
		system.SystemConfig.RuleProxyToken = "fixture-local-secret"
		return system.SetJSON("backup", json.RawMessage(`{"webdav_username":"fixture-user","webdav_password":"fixture-password","webdav_url":""}`))
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "task-status.json"),
		[]byte(`{"subscription:s":{"last_status":"success","last_run_at":"2026-11-18T08:00:00Z","run_count":"3","failure_count":"1"},"rule_source:r":{"last_status":"success","last_run_at":"2026-11-18T08:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "app.log"),
		[]byte("INFO: ready\nERROR: synthetic failure\nWARNING: last\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", s.DataDir)
	t.Setenv("LOG_FILE", filepath.Join(s.DataDir, "app.log"))

	token, err := auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, tasksJWTSecret)
	if err != nil {
		t.Fatal(err)
	}

	type cmd struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
	}
	authH := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	cmds := []cmd{
		{"tasks-list", "GET", "/api/tasks", "", authH},
		{"notifications-list", "GET", "/api/notifications", "", authH},
		{"stats-overview", "GET", "/api/stats/overview", "", authH},
		{"stats-missing-profile", "GET", "/api/stats/overview", "", map[string]string{"Authorization": "Bearer " + token, "X-ConfigFlow-Profile": "missing"}},
		{"logs-tail-search", "GET", "/api/logs/tail?lines=2&search=error", "", authH},
		{"logs-info", "GET", "/api/logs/info", "", authH},
		{"interval-sub-60", "PUT", "/api/tasks/interval", `{"key":"subscription:s","interval_seconds":60}`, authH},
		{"interval-rule-600", "PUT", "/api/tasks/interval", `{"key":"rule_source:r","interval_seconds":600}`, authH},
		{"interval-metrics-600", "PUT", "/api/tasks/interval", `{"key":"metrics_history_flush","interval_seconds":600}`, authH},
		{"interval-sub-too-low", "PUT", "/api/tasks/interval", `{"key":"subscription:s","interval_seconds":30}`, authH},
		{"interval-sub-missing", "PUT", "/api/tasks/interval", `{"key":"subscription:missing","interval_seconds":600}`, authH},
		{"run-sub", "POST", "/api/tasks/run", `{"key":"subscription:s"}`, authH},
		{"run-rule", "POST", "/api/tasks/run", `{"key":"rule_source:r"}`, authH},
		{"run-unknown", "POST", "/api/tasks/run", `{"key":"unknown:s"}`, authH},
		{"notifications-read-empty", "POST", "/api/notifications/read", `{"versions":{}}`, authH},
		{"logs-clear", "POST", "/api/logs/clear", "", authH},
		{"logs-tail-after-clear", "GET", "/api/logs/tail", "", authH},
	}
	badBodies := []string{"{", "[]", "null", "true", `"x"`, `{"key":2}`, `{"versions":{"x":null}}`, `{"ignored":NaN}`}
	for _, p := range []string{"/api/tasks/run", "/api/notifications/read"} {
		for i, body := range badBodies {
			cmds = append(cmds, cmd{fmt.Sprintf("bad-%s-%d", strings.TrimPrefix(p, "/api/"), i), "POST", p, body, authH})
		}
	}
	unauthRoutes := []struct{ method, path string }{
		{"GET", "/api/tasks"},
		{"GET", "/api/notifications"},
		{"GET", "/api/stats/overview"},
		{"GET", "/api/logs/tail"},
		{"GET", "/api/logs/info"},
		{"POST", "/api/logs/clear"},
		{"POST", "/api/tasks/run"},
		{"PUT", "/api/tasks/interval"},
		{"POST", "/api/notifications/read"},
	}
	for i, r := range unauthRoutes {
		cmds = append(cmds, cmd{fmt.Sprintf("unauth-%d", i), r.method, r.path, "", map[string]string{}})
	}

	for _, c := range cmds {
		t.Run(c.name, func(t *testing.T) {
			var opts []httpxtest.Option
			for k, v := range c.headers {
				opts = append(opts, httpxtest.Header(k, v))
			}
			if c.body != "" {
				opts = append(opts, httpxtest.Body([]byte(c.body), "application/json"))
			}
			resp := s.Do(c.method, c.path, opts...)
			var parsed any
			if err := json.Unmarshal(resp.Body, &parsed); err != nil {
				parsed = string(resp.Body)
			}
			key, _ := json.Marshal(map[string]string{"case": c.name})
			actual, _ := json.Marshal(normalizeTasksGolden(map[string]any{
				"status":       resp.Status,
				"content_type": resp.Header.Get("Content-Type"),
				"body":         parsed,
			}, s.DataDir))
			ref.Assert(t, key, actual)
		})
	}
}
