package notifications_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// The task rows come from packages not ported yet; these tests use a
// subscription row built like scheduled_refresh's (_subscription_rows), the
// real WebDAV backup row, and the Agents stored in the repository.

var running = false

func subscriptionRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := repository.GetShared(ctx)
	if err != nil {
		return nil, err
	}
	var rows []*jsonx.Object
	for _, value := range shared.GetList("subscriptions") {
		sub := value.(*jsonx.Object)
		key := "subscription:" + sub.GetString("id")
		entry, _ := statuses.Value(key).(*jsonx.Object)
		enabled := sub.Value("enabled") != false && jsonx.Truthy(sub.Value("url"))
		fields := []any{"resource_id", sub.Value("id"), "enabled", enabled, "interval_seconds", sub.Value("interval"),
			"notifications_enabled", enabled, "running", running, "link", "/subscriptions"}
		fields = append(fields, tasks.Spread(tasks.History(entry))...)
		rows = append(rows, tasks.Row(key, "subscription", sub.GetString("name"), fields...))
	}
	return rows, nil
}

func none(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }

func storedAgents(ctx context.Context) ([]*jsonx.Object, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	system, err := repository.GetSystem(ctx)
	if err != nil {
		return nil, err
	}
	var agents []*jsonx.Object
	for _, item := range system.GetList("agents") {
		agents = append(agents, item.(*jsonx.Object))
	}
	return agents, nil
}

func summarize(report, plan *jsonx.Object) *jsonx.Object {
	pending := 0
	for _, stage := range plan.GetList("stages") {
		pending += len(stage.(*jsonx.Object).GetList("ops"))
	}
	state := "synced"
	if pending > 0 {
		state = "pending"
	}
	return jsonx.Obj("state", state, "pending", pending, "items", []any{}, "message", "")
}

type fixture struct {
	t      *testing.T
	srv    *httpxtest.Server
	status *taskstatus.Store
}

func setup(t *testing.T) *fixture {
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents,
		tasks.SlotDomains, tasks.SlotMetrics} {
		tasks.SetRows(slot, none)
	}
	tasks.SetRows(tasks.SlotSubscriptions, subscriptionRows)
	notifications.ResetSources()
	notifications.SetSources(notifications.Sources{ListTasks: tasks.ListTasks, Agents: storedAgents, Summarize: summarize})
	t.Cleanup(notifications.ResetSources)
	running = false
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apinotifications.RegisterAlways)})
	_, err := srv.Repo.UpdateSharedTransaction(srv.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscriptions", []any{jsonx.Obj("id", "feed", "name", "常用订阅", "url", "https://test.invalid/?token=private",
			"enabled", true, "interval", 3600)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t, srv, notifications.TaskStatus(srv.Context(), srv.DataDir)}
}

func (f *fixture) items() map[string]*jsonx.Object {
	f.t.Helper()
	res := f.srv.Get("/api/notifications").WantStatus(200)
	if res.Header.Get("Cache-Control") != "no-store" {
		f.t.Fatal(res.Header)
	}
	out := map[string]*jsonx.Object{}
	for _, item := range res.Object().GetList("items") {
		o := item.(*jsonx.Object)
		out[o.GetString("id")] = o
	}
	return out
}

func (f *fixture) saved() *jsonx.Object {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.srv.DataDir, "notifications.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	value, _ := jsonx.Parse(data)
	return value.(*jsonx.Object).GetObject("items")
}

func (f *fixture) record(key string, result taskstatus.Result) {
	f.t.Helper()
	if _, err := f.status.Record(key, result); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) fail() {
	retained := true
	f.record("subscription:feed", taskstatus.Result{Error: "连接上游超时", CacheRetained: &retained})
}

func (f *fixture) read(versions ...string) {
	o := jsonx.NewObject()
	for i := 0; i+1 < len(versions); i += 2 {
		o.Set(versions[i], versions[i+1])
	}
	f.srv.Post("/api/notifications/read", httpxtest.JSON(jsonx.Obj("versions", o))).WantStatus(200)
}

const key = "task:subscription:feed"

// test_read_removes_body_persists_dedupe_and_a_new_failure_notifies_again
func TestReadRemovesBodyPersistsDedupeAndANewFailureNotifiesAgain(t *testing.T) {
	f := setup(t)
	f.fail()
	if f.saved().GetObject(key).GetString("state") != "pending" {
		t.Fatal("pending after the failure, without a browser")
	}
	first := f.items()[key]
	if first.GetString("severity") != "warning" || first.GetString("impact") != "仍使用上一次有效缓存" {
		t.Fatal(jsonx.Repr(first))
	}
	if !jsonx.Equal(first.Value("target"), notifications.Target("/subscriptions", "检查订阅设置", "subscription", "feed", "section", "settings")) {
		t.Fatal(jsonx.Repr(first.Value("target")))
	}
	f.read(key, first.GetString("version"))
	if _, ok := f.items()[key]; ok {
		t.Fatal("read")
	}
	saved := f.saved().GetObject(key)
	if saved.Len() != 3 || len(saved.GetString("fingerprint")) != 64 || saved.GetString("state") != "dismissed" ||
		saved.Value("source") != "task" {
		t.Fatal(jsonx.Repr(saved))
	}
	if _, ok := f.items()[key]; ok {
		t.Fatal("acknowledgement persisted")
	}
	f.fail()
	third := f.items()[key]
	if third.GetBool("read") || third.GetInt("failure_count") != 2 || third.GetString("version") == first.GetString("version") {
		t.Fatal(jsonx.Repr(third))
	}
	f.read(key, first.GetString("version"))
	if _, ok := f.items()[key]; !ok {
		t.Fatal("stale read")
	}
	f.read(key, third.GetString("version"))
	f.record("subscription:feed", taskstatus.Result{OK: true})
	if f.saved().Has(key) {
		t.Fatal("recovered")
	}
}

// test_recovery_and_recurrence_without_browser_and_stale_read_is_ignored
func TestRecoveryAndRecurrenceWithoutBrowserAndStaleReadIsIgnored(t *testing.T) {
	f := setup(t)
	f.fail()
	original := f.items()[key]
	f.record("subscription:feed", taskstatus.Result{OK: true})
	if f.saved().Has(key) {
		t.Fatal("recovered without a browser")
	}
	f.fail()
	reopened := f.items()[key]
	if reopened.GetString("state") != "pending" || reopened.GetBool("read") || reopened.GetString("version") == original.GetString("version") {
		t.Fatal(jsonx.Repr(reopened))
	}
	f.read(key, original.GetString("version"))
	if f.items()[key].GetBool("read") {
		t.Fatal("stale read")
	}
}

func (f *fixture) setSubscriptions(list []any) {
	_, err := f.srv.Repo.UpdateSharedTransaction(f.srv.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscriptions", list)
		return nil, nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// test_removed_and_disabled_sources_clear_notifications_and_dedupe_markers
func TestRemovedAndDisabledSourcesClearNotificationsAndDedupeMarkers(t *testing.T) {
	f := setup(t)
	f.fail()
	sub := jsonx.Obj("id", "feed", "name", "常用订阅", "url", "https://test.invalid/?token=private", "enabled", false, "interval", 3600)
	f.setSubscriptions([]any{sub})
	if _, ok := f.items()[key]; ok {
		t.Fatal("disabled")
	}
	sub.Set("enabled", true)
	f.setSubscriptions([]any{sub})
	row := f.items()[key]
	f.read(key, row.GetString("version"))
	f.setSubscriptions([]any{})
	if _, ok := f.items()[key]; ok || f.saved().Has(key) {
		t.Fatal("removed source keeps its marker")
	}
}

// test_task_running_or_source_read_failure_never_clears_pending
func TestTaskRunningOrSourceReadFailureNeverClearsPending(t *testing.T) {
	f := setup(t)
	f.fail()
	running = true
	if f.items()[key].GetString("state") != "pending" {
		t.Fatal("running is not recovery")
	}
	notifications.SetSources(notifications.Sources{ListTasks: func(context.Context) ([]*jsonx.Object, error) {
		return nil, notifications.ErrSourceUnavailable
	}})
	f.srv.Get("/api/notifications").WantStatus(503)
}

// test_completed_receipt_is_persisted_before_worker_releases_running_flag
func TestCompletedReceiptIsPersistedBeforeWorkerReleasesRunningFlag(t *testing.T) {
	f := setup(t)
	running = true
	f.fail()
	if f.saved().GetObject(key).GetString("state") != "pending" {
		t.Fatal("pending")
	}
	f.record("subscription:feed", taskstatus.Result{OK: true})
	if f.saved().Has(key) {
		t.Fatal("the completed receipt counts even while the running flag is set")
	}
}

// test_manual_backup_failure_is_actionable_even_without_automatic_backup
func TestManualBackupFailureIsActionableEvenWithoutAutomaticBackup(t *testing.T) {
	f := setup(t)
	_, err := f.srv.Repo.UpdateSystemTransaction(f.srv.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("backup", jsonx.Obj("auto_backup", false, "webdav_url", "https://backup.invalid",
			"webdav_username", "admin", "webdav_password", "private"))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.record("webdav_backup", taskstatus.Result{Error: "HTTP 401", Trigger: "manual"})
	row := f.items()["task:webdav_backup"]
	if row.GetString("state") != "pending" || row.GetString("title") != "WebDAV 手动备份" ||
		!jsonx.Equal(row.GetObject("target").Value("query"), jsonx.Obj("section", "backup")) {
		t.Fatal(jsonx.Repr(row))
	}
	f.record("webdav_backup", taskstatus.Result{OK: true, Trigger: "manual"})
	if _, ok := f.items()["task:webdav_backup"]; ok {
		t.Fatal("recovered")
	}
}

// test_corrupt_store_is_not_replaced_by_empty_success
func TestCorruptStoreIsNotReplacedByEmptySuccess(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.srv.DataDir, "notifications.json")
	os.WriteFile(path, []byte("broken"), 0o644)
	f.srv.Get("/api/notifications").WantStatus(503)
	if data, _ := os.ReadFile(path); string(data) != "broken" {
		t.Fatal(string(data))
	}
}

// test_read_payload_validation
func TestReadPayloadValidation(t *testing.T) {
	f := setup(t)
	for _, body := range []string{"null", "{}", `{"versions": []}`, `{"versions": {"a": 3}}`} {
		f.srv.Post("/api/notifications/read", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
}

// test_existing_history_and_read_bodies_are_removed_on_upgrade
func TestExistingHistoryAndReadBodiesAreRemovedOnUpgrade(t *testing.T) {
	f := setup(t)
	f.fail()
	path := filepath.Join(f.srv.DataDir, "notifications.json")
	data, _ := os.ReadFile(path)
	value, _ := jsonx.Parse(data)
	items := value.(*jsonx.Object).GetObject("items")
	recovered := items.GetObject(key).Clone()
	recovered.Set("state", "resolved")
	recovered.Set("resolved_at", notifications.Now())
	items.Set("recently-recovered", recovered)
	items.GetObject(key).Set("read", true)
	encoded, _ := jsonx.Default(value)
	os.WriteFile(path, encoded, 0o644)
	if len(f.items()) != 0 {
		t.Fatal("nothing pending")
	}
	saved := f.saved()
	if saved.Has("recently-recovered") || saved.GetObject(key).Len() != 3 {
		t.Fatal(jsonx.Repr(saved))
	}
}

// test_old_unread_problem_is_retained_and_stale_read_cannot_clear_a_new_receipt
func TestOldUnreadProblemIsRetainedAndStaleReadCannotClearANewReceipt(t *testing.T) {
	f := setup(t)
	f.fail()
	first := f.items()[key]
	err := notifications.Transaction(f.srv.DataDir, func(items *jsonx.Object) error {
		items.GetObject(key).Set("first_seen_at", "2000-01-01T00:00:00+00:00")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.items()[key]; !ok {
		t.Fatal("old problems stay")
	}
	f.fail()
	if f.items()[key].GetString("version") == first.GetString("version") {
		t.Fatal("new version")
	}
	f.read(key, first.GetString("version"))
	if _, ok := f.items()[key]; !ok {
		t.Fatal("stale read")
	}
}

func (f *fixture) setAgents(agents ...any) {
	_, err := f.srv.Repo.UpdateSystemTransaction(f.srv.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", agents)
		return nil, nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// test_latest_deployment_status_uses_exact_record_and_keeps_failures_while_retrying
func TestLatestDeploymentStatusUsesExactRecordAndKeepsFailuresWhileRetrying(t *testing.T) {
	f := setup(t)
	agent := jsonx.Obj("id", "a", "name", "网关", "service_type", "mihomo", "profile_id", "default", "enabled", true,
		"token", "very-private-token", "latest_deployment", jsonx.Obj("deployment_id", "d1", "profile_id", "default",
			"status", "rollback_failed", "rollback_error", "恢复失败 very-private-token https://device.test/?secret=hidden"))
	f.setAgents(agent)
	if err := notifications.RecordAgent(f.srv.Context(), agent, f.srv.Repo); err != nil {
		t.Fatal(err)
	}
	row := f.items()["deployment:a"]
	if row.GetString("severity") != "error" ||
		!jsonx.Equal(row.GetObject("target").Value("query"), jsonx.Obj("agent", "a", "profile", "default", "section", "deployment", "deployment", "d1")) {
		t.Fatal(jsonx.Repr(row))
	}
	if text := jsonx.Repr(row); strings.Contains(text, "very-private-token") || strings.Contains(text, "secret=hidden") {
		t.Fatal(text)
	}
	agent.Set("latest_deployment", jsonx.Obj("deployment_id", "d2", "profile_id", "default", "status", "preparing"))
	f.setAgents(agent)
	if f.items()["deployment:a"].GetString("state") != "pending" {
		t.Fatal("unknown is not recovery")
	}
	agent.GetObject("latest_deployment").Set("status", "succeeded")
	f.setAgents(agent)
	if err := notifications.RecordAgent(f.srv.Context(), agent, f.srv.Repo); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.items()["deployment:a"]; ok {
		t.Fatal("succeeded")
	}
}

func (f *fixture) seedRouter(profile string) *jsonx.Object {
	router := jsonx.Obj("id", "r", "name", "主路由", "type", "routeros", "endpoint", "http://router.test",
		"username", "user", "password", "private", "ip_rule_sets", []any{})
	_, _, err := f.srv.Repo.UpdateProfileTransaction(f.srv.Context(), profile, func(_ context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		config.Set("routers", jsonx.Obj("items", []any{router}))
		return nil, nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return router
}

func (f *fixture) config(profile string) *jsonx.Object {
	config, err := repo.GetConfig(f.srv.Context(), profile)
	if err != nil {
		f.t.Fatal(err)
	}
	return config
}

func forwardReport() *jsonx.Object {
	return jsonx.Obj("in_sync", false, "routes", jsonx.Obj("forward_hint", jsonx.Obj("status", "drop", "warning", "同口转发被阻止", "fix_command", "command")))
}

func routePlan() *jsonx.Object {
	return jsonx.Obj("stages", []any{jsonx.Obj("key", "routes", "title", "代理路由", "ops", []any{jsonx.Obj("kind", "route_add")})})
}

// test_router_notices_keep_profile_section_freshness_and_unknown_is_not_recovery
func TestRouterNoticesKeepProfileSectionFreshnessAndUnknownIsNotRecovery(t *testing.T) {
	f := setup(t)
	router := f.seedRouter("default")
	report := forwardReport()
	ctx := f.srv.Context()
	if err := notifications.RecordRouter(ctx, "default", f.config("default"), router, report, routePlan(), ""); err != nil {
		t.Fatal(err)
	}
	k := "router:default:r:firewall"
	row := f.items()[k]
	if !jsonx.Equal(row.GetObject("target").Value("query"), jsonx.Obj("router", "r", "profile", "default", "section", "ipv4-forward-hint")) ||
		!jsonx.Truthy(row.Value("checked_at")) || row.GetBool("stale") {
		t.Fatal(jsonx.Repr(row))
	}
	if f.items()["router:default:r:changes"].GetString("severity") != "neutral" {
		t.Fatal("changes")
	}
	report.GetObject("routes").Set("forward_hint", jsonx.Obj("status", "unknown"))
	notifications.RecordRouter(ctx, "default", f.config("default"), router, report, nil, "")
	if f.items()[k].GetString("state") != "pending" {
		t.Fatal("unknown")
	}
	notifications.RecordRouter(ctx, "default", f.config("default"), router, jsonx.NewObject(), nil, "")
	if f.items()[k].GetString("state") != "pending" {
		t.Fatal("empty")
	}
	_, _, err := f.srv.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		config.Set("custom_notification_test", "changed")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !f.items()[k].GetBool("stale") {
		t.Fatal("stale after a configuration change")
	}
	report.GetObject("routes").Set("forward_hint", jsonx.Obj("status", "ok"))
	report.Set("in_sync", true)
	notifications.RecordRouter(ctx, "default", f.config("default"), router, report, nil, "")
	if _, ok := f.items()[k]; ok {
		t.Fatal("resolved")
	}
}

// test_same_router_id_in_different_profiles_does_not_merge
func TestSameRouterIDInDifferentProfilesDoesNotMerge(t *testing.T) {
	f := setup(t)
	ctx := f.srv.Context()
	if _, err := f.srv.Repo.CreateProfile(ctx, jsonx.Obj("id", "other", "name", "其他"), ""); err != nil {
		t.Fatal(err)
	}
	var router *jsonx.Object
	for _, profile := range []string{"default", "other"} {
		router = f.seedRouter(profile)
		notifications.RecordRouter(ctx, profile, f.config(profile), router,
			jsonx.Obj("ipv6_dns", jsonx.Obj("status", "blocked", "blockers", []any{"需要指定 LAN"})), nil, "")
	}
	rows := f.items()
	if rows["router:default:r:ipv6_dns"].GetString("profile_id") != "default" || rows["router:other:r:ipv6_dns"].GetString("profile_id") != "other" {
		t.Fatal("profiles")
	}
	notifications.RecordRouter(ctx, "other", f.config("other"), router, jsonx.Obj("desired", jsonx.NewObject(), "routes", jsonx.NewObject()), nil, "")
	rows = f.items()
	if _, ok := rows["router:other:r:ipv6_dns"]; ok || rows["router:default:r:ipv6_dns"].GetString("state") != "pending" {
		t.Fatal("independent")
	}
}

// test_router_read_is_not_replayed_by_repeated_checks_and_marker_clears_on_removal
func TestRouterReadIsNotReplayedByRepeatedChecksAndMarkerClearsOnRemoval(t *testing.T) {
	f := setup(t)
	ctx := f.srv.Context()
	router := f.seedRouter("default")
	config := f.config("default")
	report := forwardReport()
	notifications.RecordRouter(ctx, "default", config, router, report, routePlan(), "")
	rows := f.items()
	if len(rows) != 2 {
		t.Fatal(len(rows))
	}
	readAll := func(rows map[string]*jsonx.Object) {
		var versions []string
		for k, row := range rows {
			versions = append(versions, k, row.GetString("version"))
		}
		f.read(versions...)
	}
	readAll(rows)
	notifications.RecordRouter(ctx, "default", config, router, report, nil, "")
	notifications.RecordRouter(ctx, "default", config, router, report, routePlan(), "")
	if len(f.items()) != 0 {
		t.Fatal("replayed")
	}
	report.GetObject("routes").Set("forward_hint", jsonx.Obj("status", "unknown"))
	notifications.RecordRouter(ctx, "default", config, router, report, nil, "")
	if len(f.items()) != 0 {
		t.Fatal("unknown")
	}
	report.GetObject("routes").Set("forward_hint", jsonx.Obj("status", "ok"))
	report.Set("in_sync", true)
	notifications.RecordRouter(ctx, "default", config, router, report, nil, "")
	if f.saved().Len() != 0 {
		t.Fatal(jsonx.Repr(f.saved()))
	}
	report = forwardReport()
	notifications.RecordRouter(ctx, "default", config, router, report, routePlan(), "")
	rows = f.items()
	if len(rows) != 2 {
		t.Fatal(len(rows))
	}
	readAll(rows)
	// This notification fixture owns no publisher; explicitly permit catalog cleanup.
	previous := repo.SetHooks(repo.Hooks{JobLock: func(context.Context, string, string, *jsonx.Object) (func(), bool) { return func() {}, true }})
	defer repo.SetHooks(previous)
	_, _, err := f.srv.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, config *jsonx.Object) (*jsonx.Object, error) {
		config.Set("routers", jsonx.Obj("items", []any{}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.items()) != 0 || f.saved().Len() != 0 {
		t.Fatal(jsonx.Repr(f.saved()))
	}
}

// test_notifications_require_authentication
func TestNotificationsRequireAuthentication(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apinotifications.RegisterAlways)})
	srv.Get("/api/notifications").WantStatus(401)
	srv.Post("/api/notifications/read", httpxtest.JSON(jsonx.Obj("versions", jsonx.NewObject()))).WantStatus(401)
}

func TestEventMessageHidesAddressesAndSecrets(t *testing.T) {
	got := notifications.EventMessage("恢复失败 very-private-token https://device.test/?secret=hidden password: abc Bearer xyz", "very-private-token")
	if got != "恢复失败 [REDACTED] [下载地址已隐藏] password: [REDACTED] Bearer [REDACTED]" {
		t.Fatal(got)
	}
	text := "key: |\n  secret\n  more\nnext token=abc;x \"api_key\": \"q w\" -----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY----- tail"
	if got := notifications.EventMessage(text); got != "key: |\n  secret\n  more\nnext token=[REDACTED];x \"api_key\": [REDACTED] [PRIVATE KEY REDACTED] tail" {
		t.Fatalf("%q", got)
	}
	if got := notifications.EventMessage("password: |\n  hidden\n  more\nnext"); got != "password: [REDACTED]\nnext" {
		t.Fatalf("%q", got)
	}
	if notifications.EventMessage(5) != "" {
		t.Fatal("non-strings")
	}
}
