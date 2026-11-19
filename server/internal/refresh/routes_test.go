package refresh_test

// The /api/tasks halves of backend/test_scheduled_tasks.py
// (test_run_now_refreshes_in_background_and_rejects_unknown_sources,
// test_interval_edit_validates_and_rule_source_edits_keep_it without the
// rule-library PUT, which is C33) on the real subscription / rule-source
// rows; the slots of other modules are empty fakes.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

func none(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }

// server mutates process-wide task slots; its callers must stay sequential.
func server(t *testing.T) *httpxtest.Server {
	t.Helper()
	refresh.Wire()
	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		t.Cleanup(tasks.SetRows(slot, none))
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apitasks.RegisterAlways)})
	_, err := srv.Repo.UpdateSharedTransaction(srv.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://airport.test/sub?token=private",
			"enabled", true, "interval", jsonx.Int(86400))})
		shared.Set("rule_library", []any{jsonx.Obj("id", "rule", "name", "Streaming", "source_type", "url",
			"url", "https://rules.test/list.txt?key=private", "behavior", "domain", "format", "text", "enabled", true)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestReviewTaskFixtureRestoresMissingSlots(t *testing.T) {
	refresh.Wire()
	before := tasks.Missing()
	t.Run("fixture", func(t *testing.T) { server(t) })
	if after := tasks.Missing(); !reflect.DeepEqual(before, after) {
		t.Fatal("fake task providers leaked", before, after)
	}
}

func rows(t *testing.T, srv *httpxtest.Server) map[string]*jsonx.Object {
	res := srv.Get("/api/tasks").WantStatus(200)
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(res.Header)
	}
	out := map[string]*jsonx.Object{}
	for _, row := range res.Object().GetList("tasks") {
		out[row.(*jsonx.Object).GetString("key")] = row.(*jsonx.Object)
	}
	return out
}

// test_scheduled_tasks.py::test_run_now_refreshes_in_background_and_rejects_unknown_sources
func TestRunNowRefreshesInBackgroundAndRejectsUnknownSources(t *testing.T) {
	srv := server(t)
	data, err := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(yamlx.Map(
		yamlx.Str("name"), yamlx.Str("Remote"), yamlx.Str("type"), yamlx.Str("ss"),
		yamlx.Str("server"), yamlx.Str("example.test"), yamlx.Str("port"), yamlx.Int(443),
		yamlx.Str("cipher"), yamlx.Str("aes-128-gcm"), yamlx.Str("password"), yamlx.Str("p")))))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	env := refresh.DefaultRefreshEnv()
	env.RandomHex = func() string { return "abcdef01" }
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		return &refresh.SourceResult{Text: text, Source: "direct_yaml"}, nil
	}
	t.Cleanup(refresh.SetRefreshEnv(env))
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "subscription:s"))).WantStatus(202).
		WantJSON(jsonx.Obj("success", true, "message", "已开始运行，结果会显示在列表中"))
	deadline := time.Now().Add(10 * time.Second)
	for refresh.IsRunning("subscription:s") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	row := rows(t, srv)["subscription:s"]
	if row.Value("last_status") != "success" || row.Value("last_trigger") != "manual" || row.GetInt("run_count") != 1 || row.Value("running") != false {
		t.Fatal(jsonx.Repr(row))
	}
	cache, err := substore.LoadSubscriptionCache(srv.Context(), "s")
	if err != nil || cache.GetInt("count") != 1 {
		t.Fatal(cache, err)
	}
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "subscription:gone"))).WantStatus(404).
		WantJSON(jsonx.Obj("success", false, "message", "任务对应的来源已不存在，请刷新后重试"))
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", "agent_rule_sync:a"))).WantStatus(400)
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.NewObject())).WantStatus(400)
}

// test_scheduled_tasks.py::test_interval_edit_validates_and_rule_source_edits_keep_it (route part)
func TestIntervalEditValidatesOnTheRealRows(t *testing.T) {
	srv := server(t)
	env := refresh.DefaultRefreshEnv()
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		return nil, errors.New("no request")
	}
	t.Cleanup(refresh.SetRefreshEnv(env))
	put := func(body *jsonx.Object) *httpxtest.Result {
		return srv.Put("/api/tasks/interval", httpxtest.JSON(body))
	}
	put(jsonx.Obj("key", "rule_source:rule", "interval_seconds", jsonx.Int(30))).WantStatus(400)
	put(jsonx.Obj("key", "rule_source:rule", "interval_seconds", jsonx.Int(3600), "x", jsonx.Int(1))).WantStatus(400)
	put(jsonx.Obj("key", "router_rule_sync:p:r", "interval_seconds", jsonx.Int(3600))).WantStatus(400)
	task := put(jsonx.Obj("key", "rule_source:rule", "interval_seconds", jsonx.Int(0))).WantStatus(200).Object().GetObject("task")
	if task.GetInt("interval_seconds") != 0 || task.Value("enabled") != false || task.Value("next_run_at") != nil {
		t.Fatal(jsonx.Repr(task))
	}
	put(jsonx.Obj("key", "subscription:s", "interval_seconds", jsonx.Int(3600))).WantStatus(200)
	shared, _ := srv.Repo.GetShared(srv.Context())
	if shared.GetList("subscriptions")[0].(*jsonx.Object).GetInt("interval") != 3600 {
		t.Fatal(jsonx.Repr(shared))
	}
}

// Every row key and default, in Python's order, for a subscription and a
// rule source that never ran.
func TestRowShape(t *testing.T) {
	srv := server(t)
	subs, err := subscriptionRows(srv.Context(), jsonx.NewObject())
	if err != nil {
		t.Fatal(err)
	}
	rules, err := ruleRows(srv.Context(), jsonx.NewObject())
	if err != nil {
		t.Fatal(err)
	}
	sub, rule := subs[0], rules[0]
	wantSub := []string{"key", "category", "name", "detail", "resource_id", "profile_id", "profile_name", "enabled",
		"interval_seconds", "schedule_text", "editable", "can_run", "run_hint", "running", "next_run_at", "state_message",
		"link", "notifications_enabled", "last_run_at", "last_status", "last_error", "last_duration_ms", "last_trigger",
		"last_success_at", "run_count", "failure_count", "consecutive_failures", "cache_retained", "cache_updated_at"}
	wantRule := slices.Clone(wantSub)
	// _rule_rows passes notifications_enabled before editable/can_run, which
	// exist already: the same order.
	slices.Sort(wantSub)
	slices.Sort(wantRule)
	gotSub, gotRule := sub.Keys(), rule.Keys()
	slices.Sort(gotSub)
	slices.Sort(gotRule)
	if !slices.Equal(gotSub, wantSub) || !slices.Equal(gotRule, wantRule) {
		t.Fatal(sub.Keys(), rule.Keys())
	}
	want := jsonx.Obj("category", "subscription", "name", "Feed", "detail", "", "resource_id", "s", "enabled", true,
		"interval_seconds", jsonx.Int(86400), "editable", true, "can_run", true, "run_hint", "", "running", false,
		"state_message", "", "link", "/subscriptions", "notifications_enabled", true, "run_count", jsonx.Int(0),
		"failure_count", jsonx.Int(0), "cache_updated_at", nil)
	for key, value := range want.All() {
		if !jsonx.Equal(sub.Value(key), value) {
			t.Errorf("subscription %s = %s", key, jsonx.Repr(sub.Value(key)))
		}
	}
	if sub.Value("next_run_at") == nil || rule.Value("detail") != "rules.test" || rule.Value("link") != "/rule-library" {
		t.Fatal(jsonx.Repr(sub), jsonx.Repr(rule))
	}
}

func subscriptionRows(ctx context.Context, value *jsonx.Object) ([]*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var statuses taskstatus.Snapshot
	if err := json.Unmarshal(raw, &statuses); err != nil {
		return nil, err
	}
	rows, err := refresh.SubscriptionTasks(ctx, statuses)
	if err != nil {
		return nil, err
	}
	out := make([]*jsonx.Object, len(rows))
	for i, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		parsed, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		out[i] = parsed.(*jsonx.Object)
	}
	return out, nil
}

func ruleRows(ctx context.Context, value *jsonx.Object) ([]*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var statuses taskstatus.Snapshot
	if err := json.Unmarshal(raw, &statuses); err != nil {
		return nil, err
	}
	rows, err := refresh.RuleTasks(ctx, statuses)
	if err != nil {
		return nil, err
	}
	out := make([]*jsonx.Object, len(rows))
	for i, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		parsed, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		out[i] = parsed.(*jsonx.Object)
	}
	return out, nil
}
