package refresh_test

// The /api/tasks halves of backend/test_scheduled_tasks.py
// (test_run_now_refreshes_in_background_and_rejects_unknown_sources,
// test_interval_edit_validates_and_rule_source_edits_keep_it without the
// rule-library PUT, which is C33) on the real subscription / rule-source
// rows; the slots of other modules are empty fakes.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func none(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }

func wantJSON(t *testing.T, res *httpxtest.Result, want any) {
	t.Helper()
	if diff := testjson.Diff(json.RawMessage(res.Body), want); diff != "" {
		t.Fatalf("body %s\n%s", res.Body, diff)
	}
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func server(t *testing.T) (*httpxtest.Server, *refresh.Service) {
	t.Helper()
	srv := httpxtest.New(t, httpxtest.Options{})
	reg := tasks.NewRegistry(srv.Repo, lifecycle.Current())
	apitasks.NewHandlerWithRepo(reg, srv.Repo).Register(srv.App.Router)
	rf := refresh.New(refresh.Options{Repo: srv.Repo})
	rf.Register(lifecycle.Current(), reg)
	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		reg.SetRowBuilder(slot, none)
	}
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, shared *model.Shared) error {
		shared.Subscriptions = []model.Subscription{testjson.Decode[model.Subscription](t, map[string]any{
			"id": "s", "name": "Feed", "url": "https://airport.test/sub?token=private",
			"enabled": true, "interval": 86400,
		})}
		shared.RuleLibrary = []model.RuleLibraryItem{testjson.Decode[model.RuleLibraryItem](t, map[string]any{
			"id": "rule", "name": "Streaming", "source_type": "url",
			"url": "https://rules.test/list.txt?key=private", "behavior": "domain", "format": "text", "enabled": true,
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, rf
}

func TestReviewTaskFixtureRestoresMissingSlots(t *testing.T) {
	reg := tasks.NewRegistry(nil, nil)
	refresh.New(refresh.Options{}).Register(lifecycle.Current(), reg)
	before := reg.Missing()
	t.Run("fixture", func(t *testing.T) { _, _ = server(t) })
	if after := reg.Missing(); !reflect.DeepEqual(before, after) {
		t.Fatal("fake task providers leaked", before, after)
	}
}

func rows(t *testing.T, srv *httpxtest.Server) map[string]map[string]any {
	res := srv.Get("/api/tasks").WantStatus(200)
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(res.Header)
	}
	body := decodeObject(t, res.Body)
	out := map[string]map[string]any{}
	for _, item := range body["tasks"].([]any) {
		row := item.(map[string]any)
		out[row["key"].(string)] = row
	}
	return out
}

// test_scheduled_tasks.py::test_run_now_refreshes_in_background_and_rejects_unknown_sources
func TestRunNowRefreshesInBackgroundAndRejectsUnknownSources(t *testing.T) {
	srv, rf := server(t)
	data, err := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(yamlx.Map(
		yamlx.Str("name"), yamlx.Str("Remote"), yamlx.Str("type"), yamlx.Str("ss"),
		yamlx.Str("server"), yamlx.Str("example.test"), yamlx.Str("port"), yamlx.Int(443),
		yamlx.Str("cipher"), yamlx.Str("aes-128-gcm"), yamlx.Str("password"), yamlx.Str("p")))))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	env := refresh.DefaultRefreshEnvFor(srv.Repo, nil)
	env.RandomHex = func() string { return "abcdef01" }
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		return &refresh.SourceResult{Text: text, Source: "direct_yaml"}, nil
	}
	t.Cleanup(rf.SetEnv(env))
	wantJSON(t, srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "subscription:s"})).WantStatus(202),
		map[string]any{"success": true, "message": "已开始运行，结果会显示在列表中"})
	deadline := time.Now().Add(10 * time.Second)
	for refresh.IsRunning("subscription:s") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	row := rows(t, srv)["subscription:s"]
	if row["last_status"] != "success" || row["last_trigger"] != "manual" || !testjson.Equal(row["run_count"], 1) || row["running"] != false {
		t.Fatalf("unexpected row: %v", row)
	}
	cache, err := substore.LoadCache(srv.Context(), srv.Repo, "s")
	if err != nil || cache == nil || cache.Count == nil || int64(*cache.Count) != 1 {
		t.Fatal(cache, err)
	}
	wantJSON(t, srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "subscription:gone"})).WantStatus(404),
		map[string]any{"success": false, "message": "任务对应的来源已不存在，请刷新后重试"})
	srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": "agent_rule_sync:a"})).WantStatus(400)
	srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{})).WantStatus(400)
}

// test_scheduled_tasks.py::test_interval_edit_validates_and_rule_source_edits_keep_it (route part)
func TestIntervalEditValidatesOnTheRealRows(t *testing.T) {
	srv, rf := server(t)
	env := refresh.DefaultRefreshEnvFor(srv.Repo, nil)
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		return nil, errors.New("no request")
	}
	t.Cleanup(rf.SetEnv(env))
	put := func(body map[string]any) *httpxtest.Result {
		return srv.Put("/api/tasks/interval", httpxtest.JSON(body))
	}
	put(map[string]any{"key": "rule_source:rule", "interval_seconds": 30}).WantStatus(400)
	put(map[string]any{"key": "rule_source:rule", "interval_seconds": 3600, "x": 1}).WantStatus(400)
	put(map[string]any{"key": "router_rule_sync:p:r", "interval_seconds": 3600}).WantStatus(400)
	res := put(map[string]any{"key": "rule_source:rule", "interval_seconds": 0}).WantStatus(200)
	task := decodeObject(t, res.Body)["task"].(map[string]any)
	if !testjson.Equal(task["interval_seconds"], 0) || task["enabled"] != false || task["next_run_at"] != nil {
		t.Fatalf("unexpected task: %v", task)
	}
	put(map[string]any{"key": "subscription:s", "interval_seconds": 3600}).WantStatus(200)
	shared, err := srv.Repo.Shared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if shared.Subscriptions[0].Interval == nil || int64(*shared.Subscriptions[0].Interval) != 3600 {
		t.Fatalf("unexpected shared subscriptions: %+v", shared.Subscriptions)
	}
}

// Every row key and default, in Python's order, for a subscription and a
// rule source that never ran.
func TestRowShape(t *testing.T) {
	srv, rf := server(t)
	subs, err := subscriptionRows(t, srv.Context(), rf)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := ruleRows(t, srv.Context(), rf)
	if err != nil {
		t.Fatal(err)
	}
	sub, rule := subs[0], rules[0]
	wantSub := []string{"key", "category", "name", "detail", "resource_id", "profile_id", "profile_name", "enabled",
		"interval_seconds", "schedule_text", "editable", "can_run", "run_hint", "running", "next_run_at", "state_message",
		"link", "notifications_enabled", "last_run_at", "last_status", "last_error", "last_duration_ms", "last_trigger",
		"last_success_at", "run_count", "failure_count", "consecutive_failures", "cache_retained", "cache_updated_at"}
	wantRule := slices.Clone(wantSub)
	slices.Sort(wantSub)
	slices.Sort(wantRule)
	gotSub, gotRule := mapKeys(sub), mapKeys(rule)
	slices.Sort(gotSub)
	slices.Sort(gotRule)
	if !slices.Equal(gotSub, wantSub) || !slices.Equal(gotRule, wantRule) {
		t.Fatal(gotSub, gotRule)
	}
	want := map[string]any{
		"category": "subscription", "name": "Feed", "detail": "", "resource_id": "s", "enabled": true,
		"interval_seconds": 86400, "editable": true, "can_run": true, "run_hint": "", "running": false,
		"state_message": "", "link": "/subscriptions", "notifications_enabled": true, "run_count": 0,
		"failure_count": 0, "cache_updated_at": nil,
	}
	for key, value := range want {
		if !testjson.Equal(sub[key], value) {
			t.Errorf("subscription %s = %v", key, sub[key])
		}
	}
	if sub["next_run_at"] == nil || rule["detail"] != "rules.test" || rule["link"] != "/rule-library" {
		t.Fatalf("sub=%v rule=%v", sub, rule)
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func subscriptionRows(t *testing.T, ctx context.Context, rf *refresh.Service) ([]map[string]any, error) {
	rows, err := rf.SubscriptionTasks(ctx, taskstatus.Snapshot{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		out[i] = decodeObject(t, data)
	}
	return out, nil
}

func ruleRows(t *testing.T, ctx context.Context, rf *refresh.Service) ([]map[string]any, error) {
	rows, err := rf.RuleTasks(ctx, taskstatus.Snapshot{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		out[i] = decodeObject(t, data)
	}
	return out, nil
}
