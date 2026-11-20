package notifications_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/agents/diag"
	apinotifications "configflow/internal/api/notifications"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

// The task rows come from packages not ported yet; these tests use a
// subscription row built like scheduled_refresh's (_subscription_rows), the
// real WebDAV backup row, and the Agents stored in the repository.

var running = false

func subscriptionRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := repository.Shared(ctx)
	if err != nil {
		return nil, err
	}
	var rows []*tasks.Task
	for _, sub := range shared.Subscriptions {
		key := "subscription:" + sub.ID
		enabled := model.SwitchEnabled(&sub, "enabled", sub.Enabled, true) && strings.TrimSpace(sub.URL) != ""
		history, err := tasks.ProjectHistory(statuses.Entry(key))
		if err != nil {
			return nil, err
		}
		intervalRaw, _ := json.Marshal(sub.Interval)
		link := "/subscriptions"
		rows = append(rows, &tasks.Task{
			Key:                  key,
			Category:             "subscription",
			Name:                 sub.Name,
			ResourceID:           sub.ID,
			Enabled:              enabled,
			IntervalSeconds:      intervalRaw,
			NotificationsEnabled: new(enabled),
			Running:              running,
			Link:                 &link,
			HistoryData:          history,
		})
	}
	return rows, nil
}

func none(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }

func storedAgents(ctx context.Context) ([]model.Agent, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	system, err := repository.System(ctx)
	if err != nil {
		return nil, err
	}
	return system.Agents, nil
}

func summarize(_ *notifications.RouterReport, plan json.RawMessage) (*notifications.RouterSummary, error) {
	var parsed struct {
		Stages []struct {
			Ops []json.RawMessage `json:"ops"`
		} `json:"stages"`
	}
	_ = json.Unmarshal(plan, &parsed)
	pending := 0
	for _, stage := range parsed.Stages {
		pending += len(stage.Ops)
	}
	state := "synced"
	if pending > 0 {
		state = "pending"
	}
	return &notifications.RouterSummary{State: state, Pending: model.Int(pending)}, nil
}

type fixture struct {
	t      *testing.T
	srv    *httpxtest.Server
	status *taskstatus.Store
}

func setup(t *testing.T) *fixture {
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents,
		tasks.SlotDomains, tasks.SlotMetrics} {
		t.Cleanup(tasks.SetRowBuilder(slot, none))
	}
	t.Cleanup(tasks.SetRowBuilder(tasks.SlotSubscriptions, subscriptionRows))
	notifications.ResetInputs()
	notifications.SetInputs(notifications.Inputs{Rows: tasks.List, Agents: storedAgents, Summarize: summarize})
	t.Cleanup(notifications.ResetInputs)
	running = false
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apinotifications.RegisterAlways)})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("subscriptions", json.RawMessage(`[{"id":"feed","name":"常用订阅","url":"https://test.invalid/?token=private","enabled":true,"interval":3600}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t, srv, notifications.TaskStatus(srv.Context(), srv.DataDir)}
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolVal(p *bool) bool {
	return p != nil && *p
}

func (f *fixture) items() map[string]*notifications.Item {
	f.t.Helper()
	res := f.srv.Get("/api/notifications").WantStatus(200)
	if res.Header.Get("Cache-Control") != "no-store" {
		f.t.Fatal(res.Header)
	}
	var payload struct {
		Items []*notifications.Item `json:"items"`
	}
	if err := json.Unmarshal(res.Body, &payload); err != nil {
		f.t.Fatal(err)
	}
	out := map[string]*notifications.Item{}
	for _, item := range payload.Items {
		out[item.ID] = item
	}
	return out
}

func (f *fixture) saved() map[string]map[string]any {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.srv.DataDir, "notifications.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	var doc struct {
		Items map[string]map[string]any `json:"items"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		f.t.Fatal(err)
	}
	if doc.Items == nil {
		return map[string]map[string]any{}
	}
	return doc.Items
}

func (f *fixture) record(key string, result taskstatus.Result) {
	f.t.Helper()
	if _, err := f.status.SaveResult(f.srv.Context(), key, result); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) fail() {
	retained := true
	f.record("subscription:feed", taskstatus.Result{Error: "连接上游超时", CacheRetained: &retained})
}

func (f *fixture) read(versions ...string) {
	m := map[string]string{}
	for i := 0; i+1 < len(versions); i += 2 {
		m[versions[i]] = versions[i+1]
	}
	f.srv.Post("/api/notifications/read", httpxtest.JSON(map[string]any{"versions": m})).WantStatus(200)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

const key = "task:subscription:feed"

// test_read_removes_body_persists_dedupe_and_a_new_failure_notifies_again
func TestReadRemovesBodyPersistsDedupeAndANewFailureNotifiesAgain(t *testing.T) {
	f := setup(t)
	f.fail()
	if f.saved()[key]["state"] != "pending" {
		t.Fatal("pending after the failure, without a browser")
	}
	first := f.items()[key]
	if strVal(first.Severity) != "warning" || strVal(first.Impact) != "仍使用上一次有效缓存" {
		t.Fatalf("%+v", first)
	}
	wantTarget := notifications.Destination("/subscriptions", "检查订阅设置", map[string]string{"subscription": "feed", "section": "settings"})
	if !testjson.Equal(wantTarget, first.Target) {
		t.Fatalf("target diff: %s", testjson.Diff(wantTarget, first.Target))
	}
	f.read(key, strVal(first.Version))
	if _, ok := f.items()[key]; ok {
		t.Fatal("read")
	}
	saved := f.saved()[key]
	fp, _ := saved["fingerprint"].(string)
	if len(saved) != 3 || len(fp) != 64 || saved["state"] != "dismissed" || saved["source"] != "task" {
		t.Fatalf("%+v", saved)
	}
	if _, ok := f.items()[key]; ok {
		t.Fatal("acknowledgement persisted")
	}
	f.fail()
	third := f.items()[key]
	if boolVal(third.Read) || string(third.FailureCount) != "2" || strVal(third.Version) == strVal(first.Version) {
		t.Fatalf("%+v", third)
	}
	f.read(key, strVal(first.Version))
	if _, ok := f.items()[key]; !ok {
		t.Fatal("stale read")
	}
	f.read(key, strVal(third.Version))
	f.record("subscription:feed", taskstatus.Result{OK: true})
	if _, ok := f.saved()[key]; ok {
		t.Fatal("recovered")
	}
}

// test_recovery_and_recurrence_without_browser_and_stale_read_is_ignored
func TestRecoveryAndRecurrenceWithoutBrowserAndStaleReadIsIgnored(t *testing.T) {
	f := setup(t)
	f.fail()
	original := f.items()[key]
	f.record("subscription:feed", taskstatus.Result{OK: true})
	if _, ok := f.saved()[key]; ok {
		t.Fatal("recovered without a browser")
	}
	f.fail()
	reopened := f.items()[key]
	if reopened.State != "pending" || boolVal(reopened.Read) || strVal(reopened.Version) == strVal(original.Version) {
		t.Fatalf("%+v", reopened)
	}
	f.read(key, strVal(original.Version))
	if boolVal(f.items()[key].Read) {
		t.Fatal("stale read")
	}
}

func (f *fixture) setSubscriptions(raw json.RawMessage) {
	err := f.srv.Repo.UpdateShared(f.srv.Context(), func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("subscriptions", raw)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// test_removed_and_disabled_sources_clear_notifications_and_dedupe_markers
func TestRemovedAndDisabledSourcesClearNotificationsAndDedupeMarkers(t *testing.T) {
	f := setup(t)
	f.fail()
	sub := map[string]any{"id": "feed", "name": "常用订阅", "url": "https://test.invalid/?token=private", "enabled": false, "interval": 3600}
	f.setSubscriptions(mustJSON([]any{sub}))
	if _, ok := f.items()[key]; ok {
		t.Fatal("disabled")
	}
	sub["enabled"] = true
	f.setSubscriptions(mustJSON([]any{sub}))
	row := f.items()[key]
	f.read(key, strVal(row.Version))
	f.setSubscriptions(json.RawMessage(`[]`))
	if _, ok := f.items()[key]; ok {
		t.Fatal("removed source keeps its marker")
	}
	if _, ok := f.saved()[key]; ok {
		t.Fatal("removed source keeps its marker")
	}
}

// test_task_running_or_source_read_failure_never_clears_pending
func TestTaskRunningOrSourceReadFailureNeverClearsPending(t *testing.T) {
	f := setup(t)
	f.fail()
	running = true
	if f.items()[key].State != "pending" {
		t.Fatal("running is not recovery")
	}
	notifications.SetInputs(notifications.Inputs{Rows: func(context.Context) ([]*tasks.Task, error) {
		return nil, notifications.ErrSourceUnavailable
	}})
	f.srv.Get("/api/notifications").WantStatus(503)
}

// test_completed_receipt_is_persisted_before_worker_releases_running_flag
func TestCompletedReceiptIsPersistedBeforeWorkerReleasesRunningFlag(t *testing.T) {
	f := setup(t)
	running = true
	f.fail()
	if f.saved()[key]["state"] != "pending" {
		t.Fatal("pending")
	}
	f.record("subscription:feed", taskstatus.Result{OK: true})
	if _, ok := f.saved()[key]; ok {
		t.Fatal("the completed receipt counts even while the running flag is set")
	}
}

// test_manual_backup_failure_is_actionable_even_without_automatic_backup
func TestManualBackupFailureIsActionableEvenWithoutAutomaticBackup(t *testing.T) {
	f := setup(t)
	err := f.srv.Repo.UpdateSystem(f.srv.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("backup", json.RawMessage(`{"auto_backup":false,"webdav_url":"https://backup.invalid","webdav_username":"admin","webdav_password":"private"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	f.record("webdav_backup", taskstatus.Result{Error: "HTTP 401", Trigger: "manual"})
	row := f.items()["task:webdav_backup"]
	if row.State != "pending" || strVal(row.Title) != "WebDAV 手动备份" ||
		!testjson.Equal(map[string]string{"section": "backup"}, row.Target.Query) {
		t.Fatalf("%+v", row)
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
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	items := doc["items"].(map[string]any)
	orig := items[key].(map[string]any)
	recovered := map[string]any{}
	for k, v := range orig {
		recovered[k] = v
	}
	recovered["state"] = "resolved"
	recovered["resolved_at"] = notifications.Now()
	items["recently-recovered"] = recovered
	orig["read"] = true
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, encoded, 0o644)
	if len(f.items()) != 0 {
		t.Fatal("nothing pending")
	}
	saved := f.saved()
	if _, has := saved["recently-recovered"]; has || len(saved[key]) != 3 {
		t.Fatalf("%+v", saved)
	}
}

// test_old_unread_problem_is_retained_and_stale_read_cannot_clear_a_new_receipt
func TestOldUnreadProblemIsRetainedAndStaleReadCannotClearANewReceipt(t *testing.T) {
	f := setup(t)
	f.fail()
	first := f.items()[key]
	err := notifications.UpdateItems(f.srv.Context(), f.srv.DataDir, func(items *notifications.Items) error {
		if item, ok := items.Get(key); ok {
			item.FirstSeenAt = new("2000-01-01T00:00:00+00:00")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.items()[key]; !ok {
		t.Fatal("old problems stay")
	}
	f.fail()
	if strVal(f.items()[key].Version) == strVal(first.Version) {
		t.Fatal("new version")
	}
	f.read(key, strVal(first.Version))
	if _, ok := f.items()[key]; !ok {
		t.Fatal("stale read")
	}
}

func (f *fixture) setAgents(agents ...model.Agent) {
	err := f.srv.Repo.UpdateSystem(f.srv.Context(), func(_ context.Context, system *model.System) error {
		system.Agents = agents
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// test_latest_deployment_status_uses_exact_record_and_keeps_failures_while_retrying
func TestLatestDeploymentStatusUsesExactRecordAndKeepsFailuresWhileRetrying(t *testing.T) {
	f := setup(t)
	agent := model.Agent{
		ID:          "a",
		Name:        "网关",
		ServiceType: "mihomo",
		ProfileID:   "default",
		Enabled:     new(true),
		Token:       "very-private-token",
		LatestDeployment: &model.Deployment{
			DeploymentID:  "d1",
			ProfileID:     "default",
			Status:        "rollback_failed",
			RollbackError: new("恢复失败 very-private-token https://device.test/?secret=hidden"),
		},
	}
	f.setAgents(agent)
	if err := notifications.SaveAgent(f.srv.Context(), &agent, f.srv.Repo); err != nil {
		t.Fatal(err)
	}
	row := f.items()["deployment:a"]
	wantQuery := map[string]string{"agent": "a", "profile": "default", "section": "deployment", "deployment": "d1"}
	if strVal(row.Severity) != "error" || !testjson.Equal(wantQuery, row.Target.Query) {
		t.Fatalf("%+v", row)
	}
	if text := string(mustJSON(row)); strings.Contains(text, "very-private-token") || strings.Contains(text, "secret=hidden") {
		t.Fatal(text)
	}
	agent.LatestDeployment = &model.Deployment{DeploymentID: "d2", ProfileID: "default", Status: "preparing"}
	f.setAgents(agent)
	if f.items()["deployment:a"].State != "pending" {
		t.Fatal("unknown is not recovery")
	}
	agent.LatestDeployment.Status = "succeeded"
	f.setAgents(agent)
	if err := notifications.SaveAgent(f.srv.Context(), &agent, f.srv.Repo); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.items()["deployment:a"]; ok {
		t.Fatal("succeeded")
	}
}

func (f *fixture) seedRouter(profile string) *model.Router {
	var router model.Router
	if err := json.Unmarshal([]byte(`{"id":"r","name":"主路由","type":"routeros","endpoint":"http://router.test","username":"user","password":"private","ip_rule_sets":[]}`), &router); err != nil {
		f.t.Fatal(err)
	}
	_, err := f.srv.Repo.UpdateProfile(f.srv.Context(), profile, func(_ context.Context, cfg *model.Profile) error {
		cfg.Routers.Items = []model.Router{router}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return &router
}

func (f *fixture) config(profile string) json.RawMessage {
	cfg, err := f.srv.Repo.Config(f.srv.Context(), profile, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return mustJSON(cfg)
}

func forwardReport() *notifications.RouterReport {
	return &notifications.RouterReport{
		InSync: new(false),
		Routes: &notifications.RouteHints{
			ForwardHint: &notifications.RouterCheck{
				Status:     "drop",
				Warning:    "同口转发被阻止",
				FixCommand: "command",
			},
		},
	}
}

func routePlan() json.RawMessage {
	return json.RawMessage(`{"stages":[{"key":"routes","title":"代理路由","ops":[{"kind":"route_add"}]}]}`)
}

// test_router_notices_keep_profile_section_freshness_and_unknown_is_not_recovery
func TestRouterNoticesKeepProfileSectionFreshnessAndUnknownIsNotRecovery(t *testing.T) {
	f := setup(t)
	router := f.seedRouter("default")
	report := forwardReport()
	ctx := f.srv.Context()
	if err := notifications.SaveRouter(ctx, "default", f.config("default"), router, report, routePlan(), ""); err != nil {
		t.Fatal(err)
	}
	k := "router:default:r:firewall"
	row := f.items()[k]
	wantQuery := map[string]string{"router": "r", "profile": "default", "section": "ipv4-forward-hint"}
	if !testjson.Equal(wantQuery, row.Target.Query) || len(row.CheckedAt) == 0 || string(row.CheckedAt) == "null" || string(row.CheckedAt) == `""` || boolVal(row.Stale) {
		t.Fatalf("%+v", row)
	}
	if strVal(f.items()["router:default:r:changes"].Severity) != "neutral" {
		t.Fatal("changes")
	}
	report.Routes.ForwardHint = &notifications.RouterCheck{Status: "unknown"}
	notifications.SaveRouter(ctx, "default", f.config("default"), router, report, nil, "")
	if f.items()[k].State != "pending" {
		t.Fatal("unknown")
	}
	notifications.SaveRouter(ctx, "default", f.config("default"), router, &notifications.RouterReport{}, nil, "")
	if f.items()[k].State != "pending" {
		t.Fatal("empty")
	}
	_, err := f.srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, cfg *model.Profile) error {
		cfg.Fields.Set("custom_notification_test", json.RawMessage(`"changed"`))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !boolVal(f.items()[k].Stale) {
		t.Fatal("stale after a configuration change")
	}
	report.Routes.ForwardHint = &notifications.RouterCheck{Status: "ok"}
	report.InSync = new(true)
	notifications.SaveRouter(ctx, "default", f.config("default"), router, report, nil, "")
	if _, ok := f.items()[k]; ok {
		t.Fatal("resolved")
	}
}

// test_same_router_id_in_different_profiles_does_not_merge
func TestSameRouterIDInDifferentProfilesDoesNotMerge(t *testing.T) {
	f := setup(t)
	ctx := f.srv.Context()
	if _, err := f.srv.Repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "其他"}, ""); err != nil {
		t.Fatal(err)
	}
	var router *model.Router
	for _, profile := range []string{"default", "other"} {
		router = f.seedRouter(profile)
		notifications.SaveRouter(ctx, profile, f.config(profile), router,
			&notifications.RouterReport{IPv6DNS: &notifications.RouterCheck{Status: "blocked", Blockers: []string{"需要指定 LAN"}}}, nil, "")
	}
	rows := f.items()
	if strVal(rows["router:default:r:ipv6_dns"].ProfileID) != "default" || strVal(rows["router:other:r:ipv6_dns"].ProfileID) != "other" {
		t.Fatal("profiles")
	}
	notifications.SaveRouter(ctx, "other", f.config("other"), router, &notifications.RouterReport{Desired: json.RawMessage(`{}`), Routes: &notifications.RouteHints{}, HasRoutes: true}, nil, "")
	rows = f.items()
	if _, ok := rows["router:other:r:ipv6_dns"]; ok || rows["router:default:r:ipv6_dns"].State != "pending" {
		t.Fatal("independent")
	}
}

// The routers owner confirms the empty release and writes its success receipt.
// C07 projects that persisted fact; planning/apply itself remains a routers test.
func TestConfirmedRouterReleaseRemovesPreviousFailure(t *testing.T) {
	f := setup(t)
	f.seedRouter("default")
	setRelease := func(ok bool) {
		f.t.Helper()
		_, err := f.srv.Repo.UpdateProfile(f.srv.Context(), "default", func(_ context.Context, cfg *model.Profile) error {
			cfg.Routers.Items[0].LastRelease = &model.RouterRelease{OK: ok, At: notifications.Now(), Message: "回读失败"}
			return nil
		})
		if err != nil {
			f.t.Fatal(err)
		}
	}
	key := "router:default:r:release"
	setRelease(false)
	if f.items()[key].State != "pending" {
		t.Fatal("old release failure missing")
	}
	setRelease(true)
	if _, exists := f.items()[key]; exists {
		t.Fatal("confirmed release retained old failure")
	}
	data, err := os.ReadFile(filepath.Join(f.srv.DataDir, "notifications.json"))
	if err != nil || strings.Contains(string(data), key) {
		t.Fatal("resolved release still persisted", err)
	}
}

// test_router_read_is_not_replayed_by_repeated_checks_and_marker_clears_on_removal
func TestRouterReadIsNotReplayedByRepeatedChecksAndMarkerClearsOnRemoval(t *testing.T) {
	f := setup(t)
	ctx := f.srv.Context()
	router := f.seedRouter("default")
	config := f.config("default")
	report := forwardReport()
	notifications.SaveRouter(ctx, "default", config, router, report, routePlan(), "")
	rows := f.items()
	if len(rows) != 2 {
		t.Fatal(len(rows))
	}
	readAll := func(rows map[string]*notifications.Item) {
		var versions []string
		for k, row := range rows {
			versions = append(versions, k, strVal(row.Version))
		}
		f.read(versions...)
	}
	readAll(rows)
	notifications.SaveRouter(ctx, "default", config, router, report, nil, "")
	notifications.SaveRouter(ctx, "default", config, router, report, routePlan(), "")
	if len(f.items()) != 0 {
		t.Fatal("replayed")
	}
	report.Routes.ForwardHint = &notifications.RouterCheck{Status: "unknown"}
	notifications.SaveRouter(ctx, "default", config, router, report, nil, "")
	if len(f.items()) != 0 {
		t.Fatal("unknown")
	}
	report.Routes.ForwardHint = &notifications.RouterCheck{Status: "ok"}
	report.InSync = new(true)
	notifications.SaveRouter(ctx, "default", config, router, report, nil, "")
	if len(f.saved()) != 0 {
		t.Fatalf("%+v", f.saved())
	}
	report = forwardReport()
	notifications.SaveRouter(ctx, "default", config, router, report, routePlan(), "")
	rows = f.items()
	if len(rows) != 2 {
		t.Fatal(len(rows))
	}
	readAll(rows)
	// This notification fixture owns no publisher; explicitly permit catalog cleanup.
	previous := repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{JobLockRecord: func(context.Context, string, string, model.Router) (func(), bool) { return func() {}, true }}})
	defer repo.SetHooks(previous)
	_, err := f.srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, cfg *model.Profile) error {
		cfg.Routers.Items = []model.Router{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.items()) != 0 || len(f.saved()) != 0 {
		t.Fatalf("%+v", f.saved())
	}
}

// test_notifications_require_authentication
func TestNotificationsRequireAuthentication(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apinotifications.RegisterAlways)})
	srv.Get("/api/notifications").WantStatus(401)
	srv.Post("/api/notifications/read", httpxtest.JSON(map[string]any{"versions": map[string]string{}})).WantStatus(401)
}

func TestEventMessageHidesAddressesAndSecrets(t *testing.T) {
	got := diag.EventMessage("恢复失败 very-private-token https://device.test/?secret=hidden password: abc Bearer xyz", []any{"very-private-token"})
	if got != "恢复失败 [REDACTED] [下载地址已隐藏] password: [REDACTED] Bearer [REDACTED]" {
		t.Fatal(got)
	}
	text := "key: |\n  secret\n  more\nnext token=abc;x \"api_key\": \"q w\" -----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY----- tail"
	if got := diag.EventMessage(text, nil); got != "key: |\n  secret\n  more\nnext token=[REDACTED];x \"api_key\": [REDACTED] [PRIVATE KEY REDACTED] tail" {
		t.Fatalf("%q", got)
	}
	if got := diag.EventMessage("password: |\n  hidden\n  more\nnext", nil); got != "password: [REDACTED]\nnext" {
		t.Fatalf("%q", got)
	}
	if diag.EventMessage(5, nil) != "" {
		t.Fatal("non-strings")
	}
}
