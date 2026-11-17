package tasks_test

import (
	"context"
	"slices"
	"testing"
	"time"

	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func none(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }

func sourceRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	return []*jsonx.Object{tasks.Row("subscription:s", "subscription", "Feed", "resource_id", "s")}, nil
}

func setup(t *testing.T) *httpxtest.Server {
	for _, slot := range []string{tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		t.Cleanup(tasks.SetRows(slot, none))
	}
	t.Cleanup(tasks.SetRows(tasks.SlotSubscriptions, sourceRows))
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apitasks.Register)})
	_, err := srv.Repo.UpdateSharedTransaction(srv.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://airport.test/sub", "interval", 86400)})
		shared.Set("rule_library", []any{jsonx.Obj("id", "rule", "name", "Streaming", "source_type", "url",
			"url", "https://rules.test/list.txt", "update_interval", 86400)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestTasksListIsNoStoreAndIncludesTheBackupRow(t *testing.T) {
	srv := setup(t)
	res := srv.Get("/api/tasks").WantStatus(200)
	if res.Header.Get("Cache-Control") != "no-store" || len(res.Object().GetList("tasks")) != 1 {
		t.Fatal(res.Text())
	}
	_, err := srv.Repo.UpdateSystemTransaction(srv.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("backup", jsonx.Obj("webdav_url", "https://dav.test", "webdav_username", "u", "webdav_password", "p", "auto_backup", true))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	taskstatus.New(srv.DataDir).Record(tasks.BackupTaskKey, taskstatus.Result{Error: "HTTP 401", Trigger: "manual"})
	rows := srv.Get("/api/tasks").Object().GetList("tasks")
	backup := rows[1].(*jsonx.Object)
	want := jsonx.Obj("key", "webdav_backup", "category", "webdav_backup", "name", "WebDAV 自动备份", "enabled", true,
		"notifications_enabled", true, "schedule_text", "配置变更后自动备份", "can_run", true, "run_hint", "",
		"running", false, "link", "/system-settings", "last_status", "failed", "last_error", "HTTP 401",
		"last_trigger", "manual", "run_count", 1, "failure_count", 1, "consecutive_failures", 1)
	for key, value := range want.All() {
		if !jsonx.Equal(backup.Value(key), value) {
			t.Errorf("%s = %v, want %v", key, jsonx.Repr(backup.Value(key)), jsonx.Repr(value))
		}
	}
}

// test_scheduled_tasks.py::test_interval_edit_validates_and_rule_source_edits_keep_it (route part)
func TestIntervalEditValidates(t *testing.T) {
	srv := setup(t)
	srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", "subscription:s", "interval_seconds", 30))).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数"))
	srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", "subscription:s"))).WantStatus(400)
	srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", "router_rule_sync:p:r", "interval_seconds", 60))).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "这个任务的间隔不能在这里修改"))
	srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", "rule_source:missing", "interval_seconds", 3600))).WantStatus(404).
		WantJSON(jsonx.Obj("success", false, "message", "任务对应的来源已不存在，请刷新后重试"))
	res := srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", "rule_source:rule", "interval_seconds", 3600))).WantStatus(200)
	if res.Object().Value("success") != true || res.Object().Value("task") != nil {
		t.Fatal(res.Text()) // the fake slots have no rule_source row
	}
	shared, _ := srv.Repo.GetShared(srv.Context())
	if shared.GetList("rule_library")[0].(*jsonx.Object).GetInt("update_interval") != 3600 {
		t.Fatal("not saved")
	}
	res = srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", "subscription:s", "interval_seconds", 0))).WantStatus(200)
	if res.Object().GetObject("task").GetString("key") != "subscription:s" {
		t.Fatal(res.Text())
	}
}

func TestRunDispatchAndBackgroundBusy(t *testing.T) {
	srv := setup(t)
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.NewObject())).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "请指定任务"))
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "other:x"))).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "这个任务不能在这里立即运行"))
	release := make(chan struct{})
	t.Cleanup(tasks.SetRunner("webdav_backup", func(ctx context.Context, key, resource string) error {
		return tasks.StartBackground(ctx, key, func(context.Context) error { <-release; return nil })
	}))
	t.Cleanup(tasks.SetRunner("subscription", func(ctx context.Context, key, resource string) error {
		return &tasks.NotFound{Message: "订阅不存在"}
	}))
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "webdav_backup"))).WantStatus(202).
		WantJSON(jsonx.Obj("success", true, "message", "已开始运行，结果会显示在列表中"))
	if !tasks.IsBackground("webdav_backup") {
		t.Fatal("running")
	}
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "webdav_backup"))).WantStatus(409).
		WantJSON(jsonx.Obj("success", false, "message", "这个任务正在运行，请稍后查看结果"))
	close(release)
	for i := 0; i < 100 && tasks.IsBackground("webdav_backup"); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "subscription:s"))).WantStatus(404)
}

func TestRoutesStayUnregisteredUntilEverySlotIsProvided(t *testing.T) {
	t.Cleanup(tasks.SetRows(tasks.SlotMetrics, nil))
	app := httpx.New()
	apitasks.Register(app.Router)
	if !app.Router.HasEndpoint("tasks.get_tasks") {
		t.Fatal("missing route when slot not ready")
	}
	if missing := tasks.Missing(); !slices.Contains(missing, tasks.SlotMetrics) {
		t.Fatal(missing)
	}
}

func TestISO(t *testing.T) {
	value := time.Date(2026, 10, 24, 1, 0, 0, 500, time.UTC)
	if got := tasks.ISO(&value); got != "2026-10-24T01:00:00Z" {
		t.Fatal(got)
	}
	if tasks.ISO(nil) != nil {
		t.Fatal("nil")
	}
}
