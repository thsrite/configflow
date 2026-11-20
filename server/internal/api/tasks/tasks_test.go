package tasks_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func none(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }

func sourceRows(_ context.Context, _ taskstatus.Snapshot) ([]*tasks.Task, error) {
	return []*tasks.Task{{
		Key:        "subscription:s",
		Category:   "subscription",
		Name:       "Feed",
		ResourceID: "s",
		Enabled:    true,
	}}, nil
}

func assertJSON(t *testing.T, res *httpxtest.Result, want any) *httpxtest.Result {
	t.Helper()
	if diff := testjson.Diff(want, json.RawMessage(res.Body)); diff != "" {
		t.Fatalf("response JSON diff: %s\nbody: %s", diff, res.Body)
	}
	return res
}

func decodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	return out
}

func setup(t *testing.T) *httpxtest.Server {
	for _, slot := range []string{tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		t.Cleanup(tasks.SetRowBuilder(slot, none))
	}
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotSubscriptions, sourceRows))
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apitasks.Register)})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, shared *model.Shared) error {
		subsRaw, err := json.Marshal([]any{map[string]any{"id": "s", "name": "Feed", "url": "https://airport.test/sub", "interval": 86400}})
		if err != nil {
			return err
		}
		if err := shared.SetJSON("subscriptions", subsRaw); err != nil {
			return err
		}
		rulesRaw, err := json.Marshal([]any{map[string]any{
			"id": "rule", "name": "Streaming", "source_type": "url",
			"url": "https://rules.test/list.txt", "update_interval": 86400,
		}})
		if err != nil {
			return err
		}
		return shared.SetJSON("rule_library", rulesRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestTasksListIsNoStoreAndIncludesTheBackupRow(t *testing.T) {
	srv := setup(t)
	res := srv.Get("/api/tasks").WantStatus(200)
	list, _ := decodeMap(t, res.Body)["tasks"].([]any)
	if res.Header.Get("Cache-Control") != "no-store" || len(list) != 1 {
		t.Fatal(res.Text())
	}
	err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
		raw, err := json.Marshal(map[string]any{
			"webdav_url": "https://dav.test", "webdav_username": "u", "webdav_password": "p", "auto_backup": true,
		})
		if err != nil {
			return err
		}
		return system.SetJSON("backup", raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	taskstatus.New(srv.DataDir).Record(tasks.BackupTaskKey, taskstatus.Result{Error: "HTTP 401", Trigger: "manual"})
	rows, _ := decodeMap(t, srv.Get("/api/tasks").Body)["tasks"].([]any)
	backup, _ := rows[1].(map[string]any)
	want := map[string]any{
		"key": "webdav_backup", "category": "webdav_backup", "name": "WebDAV 自动备份", "enabled": true,
		"notifications_enabled": true, "schedule_text": "配置变更后自动备份", "can_run": true, "run_hint": "",
		"running": false, "link": "/system-settings", "last_status": "failed", "last_error": "HTTP 401",
		"last_trigger": "manual", "run_count": 1, "failure_count": 1, "consecutive_failures": 1,
	}
	for key, value := range want {
		if !testjson.Equal(backup[key], value) {
			t.Errorf("%s = %#v, want %#v", key, backup[key], value)
		}
	}
}

// test_scheduled_tasks.py::test_interval_edit_validates_and_rule_source_edits_keep_it (route part)
func TestIntervalEditValidates(t *testing.T) {
	srv := setup(t)
	assertJSON(t, srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 30})).WantStatus(400),
		map[string]any{"success": false, "message": "更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数"})
	srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s"})).WantStatus(400)
	assertJSON(t, srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "router_rule_sync:p:r", "interval_seconds": 60})).WantStatus(400),
		map[string]any{"success": false, "message": "这个任务的间隔不能在这里修改"})
	assertJSON(t, srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "rule_source:missing", "interval_seconds": 3600})).WantStatus(404),
		map[string]any{"success": false, "message": "任务对应的来源已不存在，请刷新后重试"})
	res := srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "rule_source:rule", "interval_seconds": 3600})).WantStatus(200)
	body := decodeMap(t, res.Body)
	if body["success"] != true || body["task"] != nil {
		t.Fatal(res.Text()) // the fake slots have no rule_source row
	}
	shared, err := srv.Repo.Shared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.RuleLibrary) == 0 || !testjson.Equal(shared.RuleLibrary[0].UpdateInterval, 3600) {
		t.Fatal("not saved")
	}
	res = srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": "subscription:s", "interval_seconds": 0})).WantStatus(200)
	taskObj, _ := decodeMap(t, res.Body)["task"].(map[string]any)
	if taskObj["key"] != "subscription:s" {
		t.Fatal(res.Text())
	}
}

func TestRunDispatchAndBackgroundBusy(t *testing.T) {
	srv := setup(t)
	assertJSON(t, srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{})).WantStatus(400),
		map[string]any{"success": false, "message": "请指定任务"})
	assertJSON(t, srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "other:x"})).WantStatus(400),
		map[string]any{"success": false, "message": "这个任务不能在这里立即运行"})
	release := make(chan struct{})
	t.Cleanup(tasks.SetRunner("webdav_backup", func(ctx context.Context, key, resource string) error {
		return tasks.StartBackground(ctx, key, func(context.Context) error { <-release; return nil })
	}))
	t.Cleanup(tasks.SetRunner("subscription", func(ctx context.Context, key, resource string) error {
		return &tasks.NotFound{Message: "订阅不存在"}
	}))
	assertJSON(t, srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "webdav_backup"})).WantStatus(202),
		map[string]any{"success": true, "message": "已开始运行，结果会显示在列表中"})
	if !tasks.IsBackground("webdav_backup") {
		t.Fatal("running")
	}
	assertJSON(t, srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "webdav_backup"})).WantStatus(409),
		map[string]any{"success": false, "message": "这个任务正在运行，请稍后查看结果"})
	close(release)
	for i := 0; i < 100 && tasks.IsBackground("webdav_backup"); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "subscription:s"})).WantStatus(404)
}

func TestRoutesStayUnregisteredUntilEverySlotIsProvided(t *testing.T) {
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotMetrics, nil))
	app := httpx.New()
	apitasks.Register(app.Router)
	if !app.Router.HasEndpoint("tasks.get_tasks") {
		t.Fatal("missing route when slot not ready")
	}
	if missing := tasks.Missing(); !slices.Contains(missing, tasks.SlotMetrics) {
		t.Fatal(missing)
	}
}

func TestTimestamp(t *testing.T) {
	value := time.Date(2026, 10, 24, 1, 0, 0, 500, time.UTC)
	got := tasks.Timestamp(&value)
	if got == nil || *got != "2026-10-24T01:00:00Z" {
		t.Fatal(got)
	}
	if tasks.Timestamp(nil) != nil {
		t.Fatal("nil")
	}
}
