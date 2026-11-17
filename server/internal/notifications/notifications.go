// Package notifications is the port of backend/notifications.py and
// backend/utils/notification_store.py: actionable notifications projected
// from receipts that already exist (task results, Agent deployments, router
// checks and releases, refused registrations, address changes, notices). It
// never contacts a device.
//
// Producers call the Record… functions; the task results reach it through
// RefreshTasks, wired as the OnRecord hook of taskstatus stores (use
// TaskStatus(dataDir) to get such a store). The notification list needs the
// task rows, the Agents and the routers of every profile: packages provide
// them with SetSources (#20 Agents, #32 routers, the task rows come from
// internal/tasks).
package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

var logger = logx.Get("backend.notifications")

// Sources are the data snapshot needs from packages not ported here.
type Sources struct {
	// ListTasks is routes.tasks.list_tasks.
	ListTasks func(ctx context.Context) ([]*jsonx.Object, error)
	// MissingTasks prevents incomplete observations from retiring old receipts.
	// Production wiring supplies tasks.Missing; isolated source fixtures may omit it.
	MissingTasks func() []string
	// Agents is get_agent_manager().get_all_agents().
	Agents func(ctx context.Context) ([]*jsonx.Object, error)
	// Summarize is routers.pending.summarize(report, plan).
	Summarize func(report, plan *jsonx.Object) *jsonx.Object
}

var (
	sourcesMu sync.Mutex
	sources   Sources
)

// SetSources installs non-nil fields and returns an isolated-test restoration.
func SetSources(s Sources) func() {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	previous := sources
	if s.ListTasks != nil {
		sources.ListTasks = s.ListTasks
	}
	if s.MissingTasks != nil {
		sources.MissingTasks = s.MissingTasks
	}
	if s.Agents != nil {
		sources.Agents = s.Agents
	}
	if s.Summarize != nil {
		sources.Summarize = s.Summarize
	}
	return func() { sourcesMu.Lock(); sources = previous; sourcesMu.Unlock() }
}

// ResetSources clears them (tests).
func ResetSources() {
	sourcesMu.Lock()
	sources = Sources{}
	sourcesMu.Unlock()
}

func currentSources() Sources {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	return sources
}

// Complete reports whether every source the notification list needs is set.
func Complete() bool {
	s := currentSources()
	return s.ListTasks != nil && s.Agents != nil
}

// MissingSources names the unwired sources required for a complete projection.
func MissingSources() []string {
	s := currentSources()
	var missing []string
	if s.ListTasks == nil {
		missing = append(missing, "task_rows")
	}
	if s.Agents == nil {
		missing = append(missing, "agents")
	}
	return missing
}

// ErrSourceUnavailable may be wrapped by a source to report a read failure
// that Python raised as OSError (the list answers 503).
var ErrSourceUnavailable = errors.New("notification source unavailable")

// ErrSourcesMissing is returned by Snapshot before the sources are wired.
var ErrSourcesMissing = errors.New("notification sources are not wired")

// Safely is notifications.safely: a notification failure is logged, never
// propagated into the operation that produced it.
func Safely(fn func() error) {
	defer func() {
		if p := recover(); p != nil {
			logger.Error("Could not update notification receipts")
		}
	}()
	if err := fn(); err != nil {
		logger.Exception("Could not update notification receipts", err)
	}
}

// Target is target(path, label, **query): None values dropped, others
// str()ed; query keys keep their order.
func Target(path, label string, query ...any) *jsonx.Object {
	q := jsonx.NewObject()
	for i := 0; i+1 < len(query); i += 2 {
		key, _ := query[i].(string)
		if query[i+1] == nil {
			continue
		}
		q.Set(key, jsonx.Str(query[i+1]))
	}
	return jsonx.Obj("path", path, "query", q, "label", label)
}

func orValue(values ...any) any {
	for _, v := range values {
		if jsonx.Truthy(v) {
			return v
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values[len(values)-1]
}

var taskLabels = map[string]string{"subscription": "订阅更新", "rule_source": "规则集更新", "router_rule_sync": "路由规则同步",
	"agent_rule_sync": "规则同步", "dns_follow": "DNS 联动", "webdav_backup": "WebDAV 备份",
	"domain_discovery": "域名探测", "metrics_history": "监控历史保存"}

// TaskObservation is task_observation(task, agents, profiles).
func TaskObservation(task *jsonx.Object, agents map[string]*jsonx.Object, profiles map[string]any) *jsonx.Object {
	category := task.GetString("category")
	resource := task.Value("resource_id")
	resourceID := jsonx.Str(resource)
	profile := task.Value("profile_id")
	agent := jsonx.NewObject()
	if category == "agent_rule_sync" || category == "dns_follow" {
		if a, ok := agents[resourceID]; ok {
			agent = a
		}
	}
	profile = orValue(profile, agent.Value("profile_id"))
	link := Target("/system-settings", "查看对应任务", "tab", "tasks", "task", task.Value("key"))
	switch category {
	case "subscription":
		link = Target("/subscriptions", "检查订阅设置", "subscription", resource, "section", "settings")
	case "rule_source":
		link = Target("/rule-library", "检查规则集设置", "rule", resource, "section", "settings")
	case "router_rule_sync":
		link = Target("/routers", "查看同步设置", "router", resource, "profile", profile, "section", "rule-sync")
	case "agent_rule_sync", "dns_follow":
		section := "dns-follow"
		if category == "agent_rule_sync" {
			section = "rule-sync"
		}
		link = Target("/agents", "查看同步设置", "agent", resource, "profile", profile, "section", section)
	case "webdav_backup":
		link = Target("/system-settings", "检查 WebDAV 设置", "section", "backup")
	}
	notificationsEnabled, ok := task.Get("notifications_enabled")
	if !ok {
		notificationsEnabled = task.Value("enabled")
	}
	enabled := jsonx.Truthy(notificationsEnabled) && agent.Value("enabled") != false
	status := task.Value("last_status")
	manual := category == "router_rule_sync" && jsonx.Truthy(task.Value("state_message")) && !jsonx.Truthy(task.Value("running"))
	problem := status == "failed" || (status == "skipped" && jsonx.Truthy(task.Value("last_error"))) || manual
	var state string
	switch {
	case !enabled:
		state = "inactive"
	case jsonx.Truthy(task.Value("running")):
		state = "unknown"
	case problem:
		state = "pending"
	case status == "success":
		state = "resolved"
	default:
		state = "unknown"
	}
	message := orValue(task.Value("last_error"), task.Value("state_message"), "任务未完成，请查看任务详情。")
	cache := task.Value("cache_retained")
	cacheTime := task.Value("cache_updated_at")
	impact := ""
	if jsonx.Truthy(cache) {
		impact = "仍使用上一次有效缓存"
		if jsonx.Truthy(cacheTime) {
			impact += "（" + jsonx.Str(cacheTime) + "）"
		}
	} else if cache == false {
		impact = "没有可用缓存"
	}
	name := jsonx.Str(task.Value("name"))
	title := name + " · " + labelOr(category)
	if category == "webdav_backup" || category == "domain_discovery" || category == "metrics_history" {
		title = name
	}
	if category == "webdav_backup" && task.Value("last_trigger") == "manual" {
		title = "WebDAV 手动备份"
	}
	statusLabel := "执行失败"
	if manual || status == "skipped" {
		statusLabel = "需要手动处理"
	}
	severity := "error"
	if manual || status == "skipped" || jsonx.Truthy(cache) {
		severity = "warning"
	}
	scope := "system"
	if jsonx.Truthy(profile) {
		scope = "profile"
	} else if category == "subscription" || category == "rule_source" {
		scope = "shared"
	}
	profileName := profile
	if key, ok := profile.(string); ok {
		if name, found := profiles[key]; found {
			profileName = name
		}
	}
	runCount, present := task.Get("run_count")
	if !present {
		runCount = 0
	}
	return jsonx.Obj("state", state, "source", "task", "title", title,
		"message", EventMessage(message), "impact", impact,
		"status_label", statusLabel, "severity", severity,
		"profile_id", profile, "scope", scope, "profile_name", profileName, "target", link,
		"checked_at", task.Value("last_run_at"),
		"failure_count", orValue(task.Value("consecutive_failures"), 0),
		"evidence", jsonx.Str(runCount)+":"+jsonx.Str(orValue(task.Value("last_run_at"), ""))+":"+jsonx.Str(status))
}

func labelOr(category string) string {
	if label, ok := taskLabels[category]; ok {
		return label
	}
	return "后台任务"
}

var deploymentLabels = map[string]string{"failed": "发布失败", "rolled_back": "发布失败，已回滚", "rollback_failed": "回滚失败",
	"ready": "等待激活", "recovery_pending": "等待恢复检查", "unknown": "结果待确认"}

// AgentObservation is agent_observation(agent, repository).
func AgentObservation(ctx context.Context, agent *jsonx.Object, repository *repo.Repository) (*jsonx.Object, error) {
	record, _ := agent.Value("latest_deployment").(*jsonx.Object)
	if !jsonx.Truthy(record) {
		record = jsonx.NewObject()
	}
	status := record.Value("status")
	defaultID := func() (string, error) { return repository.DefaultProfileID(ctx) }
	profile := orValue(record.Value("profile_id"), agent.Value("profile_id"))
	if !jsonx.Truthy(profile) {
		id, err := defaultID()
		if err != nil {
			return nil, err
		}
		profile = id
	}
	statusText, _ := status.(string)
	label, labelled := deploymentLabels[statusText]
	state := "unknown"
	if labelled {
		state = "pending"
	}
	if status == "succeeded" || (status == "rolled_back" && jsonx.Truthy(record.Value("manual_rollback"))) {
		state = "resolved"
	}
	agentProfile := agent.Value("profile_id")
	if !jsonx.Truthy(agentProfile) {
		id, err := defaultID()
		if err != nil {
			return nil, err
		}
		agentProfile = id
	}
	if agent.Value("enabled") == false || !jsonx.Equal(profile, agentProfile) {
		state = "inactive"
	}
	name := orValue(agent.Value("name"), agent.Value("id"))
	service := record.Value("service_type")
	if !jsonx.Truthy(service) {
		service = agent.GetOr("service_type", "")
	}
	failed := status == "failed" || status == "rolled_back" || status == "rollback_failed"
	severity, linkLabel := "warning", "查看发布状态"
	if failed {
		severity, linkLabel = "error", "查看失败详情"
	}
	impact := ""
	if status == "rolled_back" {
		impact = "已恢复发布前的配置"
	}
	message := orValue(record.Value("rollback_error"), record.Value("error"), record.Value("message"))
	if !jsonx.Truthy(message) {
		message = label
	}
	return jsonx.Obj("state", state, "source", "deployment", "profile_id", profile, "scope", "profile",
		"title", jsonx.Str(name)+" · "+jsonx.Str(service)+" 配置发布",
		"message", EventMessage(message, agent.Value("token")),
		"impact", impact, "status_label", label, "severity", severity,
		"checked_at", orValue(record.Value("updated_at"), record.Value("created_at")),
		"evidence", jsonx.Str(record.Value("deployment_id"))+jsonx.Str(status),
		"target", Target("/agents", linkLabel, "agent", agent.Value("id"), "profile", profile,
			"section", "deployment", "deployment", record.Value("deployment_id"))), nil
}

// RecordAgent is record_agent(agent, repository).
func RecordAgent(ctx context.Context, agent *jsonx.Object, repository *repo.Repository) error {
	ctx = lifecycle.Detach(ctx)
	if !jsonx.Truthy(agent) {
		return nil
	}
	observation, err := AgentObservation(ctx, agent, repository)
	if err != nil {
		return err
	}
	return TransactionContext(ctx, repository.DataDir(), func(items *jsonx.Object) error {
		Observe(items, "deployment:"+jsonx.Str(agent.Value("id")), observation)
		return nil
	})
}

// Constants of the registration notices.
const (
	RegistrationSource       = "agent-registration"
	RegistrationLimit        = 20
	RegistrationFreshSeconds = 300
	AgentAddressSource       = "agent-address"
	NoticeSource             = "notice"
)

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// RecordRegistrationRejection is record_registration_rejection.
func RecordRegistrationRejection(ctx context.Context, repository *repo.Repository, agentData *jsonx.Object, reason string) error {
	if !jsonx.Truthy(agentData) {
		agentData = jsonx.NewObject()
	}
	name := truncateRunes(EventMessage(jsonx.Str(orValue(agentData.Value("name"), "未命名"))), 64)
	host := truncateRunes(EventMessage(jsonx.Str(orValue(agentData.Value("host"), ""))), 64)
	service := truncateRunes(jsonx.Str(orValue(agentData.Value("service_type"), "mihomo")), 16)
	sum := sha256.Sum256([]byte(name + "|" + host + "|" + service))
	key := "registration:" + hex.EncodeToString(sum[:])[:24]
	return TransactionContext(ctx, repository.DataDir(), func(items *jsonx.Object) error {
		if _, exists := items.Get(key); !exists {
			count := 0
			for _, item := range items.Values() {
				if o, ok := item.(*jsonx.Object); ok && o.Value("source") == RegistrationSource {
					count++
				}
			}
			if count >= RegistrationLimit {
				return nil
			}
		}
		where := host
		if where == "" {
			where = "地址未知"
		}
		Observe(items, key, jsonx.Obj(
			"state", "pending", "source", RegistrationSource, "scope", "system", "severity", "error",
			"status_label", "注册被拒绝", "title", name+"（"+where+"）· Agent 注册被拒绝",
			"message", EventMessage(reason), "impact", "设备上的 Agent 会一直重试，处理后自动注册",
			"checked_at", Now(), "evidence", jsonx.Int(Clock().Unix()/60).String(),
			"target", Target("/agents", "查看 Agent 列表")))
		return nil
	})
}

// RecordAgentAddress is record_agent_address.
func RecordAgentAddress(ctx context.Context, repository *repo.Repository, agent *jsonx.Object, host, label, message string) error {
	name := truncateRunes(EventMessage(jsonx.Str(orValue(agent.Value("name"), agent.Value("id")))), 64)
	id := jsonx.Str(agent.Value("id"))
	return TransactionContext(ctx, repository.DataDir(), func(items *jsonx.Object) error {
		Observe(items, "agent-address:"+id, jsonx.Obj(
			"state", "pending", "source", AgentAddressSource, "scope", "system", "severity", "warning",
			"status_label", label, "title", name+" · 地址已从 "+jsonx.Str(agent.Value("host"))+" 变为 "+host,
			"message", truncateRunes(message, MessageLimit), "impact", "面板仍按旧地址联系这台设备",
			"checked_at", Now(), "evidence", jsonx.Int(Clock().Unix()/120).String(),
			"target", Target("/agents", "修改地址", "agent", agent.Value("id"), "section", "address")))
		return nil
	})
}

// ResolveAgentAddress is resolve_agent_address.
func ResolveAgentAddress(ctx context.Context, repository *repo.Repository, agentID string) error {
	return TransactionContext(ctx, repository.DataDir(), func(items *jsonx.Object) error {
		Observe(items, "agent-address:"+agentID, jsonx.Obj("state", "resolved"))
		return nil
	})
}

// RecordNotice is record_notice: something ConfigFlow changed on its own,
// shown until it is read.
func RecordNotice(ctx context.Context, repository *repo.Repository, key, title, message string, link *jsonx.Object) error {
	return TransactionContext(ctx, repository.DataDir(), func(items *jsonx.Object) error {
		Observe(items, key, jsonx.Obj(
			"state", "pending", "source", NoticeSource, "scope", "system", "severity", "warning",
			"status_label", "已自动调整", "title", title, "message", truncateRunes(message, MessageLimit), "impact", "",
			"checked_at", Now(), "evidence", key, "target", link))
		return nil
	})
}

func registrationFresh(item *jsonx.Object) bool {
	seen, ok := taskstatus.FromISOFormat(jsonx.Str(orValue(item.Value("last_seen_at"), "")))
	if !ok {
		return false
	}
	return Clock().Sub(seen) < RegistrationFreshSeconds*time.Second
}

// RouterSignature is router_signature(config, router): a digest only.
func RouterSignature(config, router *jsonx.Object) string {
	data, _ := jsonx.Encode([]any{config, router}, jsonx.Options{SortKeys: true, EnsureASCII: true})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type routerCheck struct {
	key, title string
	data       *jsonx.Object
	reason     string
	section    string
}

func objectOr(v any) *jsonx.Object {
	o, _ := v.(*jsonx.Object)
	if o == nil {
		return jsonx.NewObject()
	}
	return o
}

// RecordRouter is record_router(profile_id, config, router, report, plan,
// checked_at=...). plan may be nil; checkedAt "" means now.
func RecordRouter(ctx context.Context, profileID string, config, router, report, plan *jsonx.Object, checkedAt string) error {
	ctx = lifecycle.Detach(ctx)
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	routerID := jsonx.Str(router.Value("id"))
	prefix := "router:" + profileID + ":" + routerID + ":"
	stamp := checkedAt
	if stamp == "" {
		stamp = Now()
	}
	common := jsonx.Obj("source", "router-check", "scope", "profile", "profile_id", profileID,
		"checked_at", stamp, "evidence", stamp, "signature", RouterSignature(config, router))
	routerName := jsonx.Str(orValue(router.Value("name"), router.Value("id")))
	password := router.Value("password")
	var checks []routerCheck
	ipv4 := objectOr(objectOr(report.Value("routes")).Value("forward_hint"))
	if !jsonx.Truthy(report.Value("routes")) {
		ipv4 = jsonx.NewObject()
	}
	reason := ""
	if jsonx.Truthy(ipv4.Value("fix_command")) {
		reason = jsonx.Str(ipv4.Value("warning"))
		if ipv4.Value("warning") == nil {
			reason = ""
		}
	}
	checks = append(checks, routerCheck{"firewall", "IPv4 同口转发", ipv4, reason, "ipv4-forward-hint"})
	for _, section := range [][3]string{{"ipv6_fakeip", "IPv6 Fake-IP 分流路由", "ipv6-fakeip"},
		{"ipv6_routes", "IPv6 规则集路由", "ipv6-routes"}, {"ipv6_dns", "IPv6 DNS 下发", "ipv6-dns"},
		{"ipv6_pd_guard", "IPv6 前缀防护", "ipv6-pd-guard"}} {
		data := objectOr(report.Value(section[0]))
		list, _ := orValue(data.Value("blockers"), data.Value("skip")).([]any)
		var parts []string
		for _, item := range list {
			parts = append(parts, jsonx.Str(item))
		}
		reason := strings.Join(parts, "；")
		if jsonx.Truthy(data.Value("fix_command")) {
			reason = jsonx.Str(orValue(data.Value("warning"), reason, data.Value("message"), "需要在路由器终端手动处理。"))
		}
		checks = append(checks, routerCheck{section[0], section[1], data, reason, section[2]})
	}
	var summary *jsonx.Object
	if plan != nil {
		summarize := currentSources().Summarize
		if summarize == nil {
			return errors.New("routers.pending.summarize is not wired")
		}
		summary = summarize(report, plan)
	}
	return TransactionContext(ctx, repository.DataDir(), func(items *jsonx.Object) error {
		_, hasDesired := report.Get("desired")
		_, hasRoutes := report.Get("routes")
		for _, check := range checks {
			var state string
			status := check.data.Value("status")
			switch {
			case check.reason != "":
				state = "pending"
			case check.data.Len() == 0 || status == "unknown" || status == "error":
				state = "unknown"
			case check.data.Value("enabled") == false:
				state = "inactive"
			default:
				state = "resolved"
			}
			if check.data.Len() == 0 && hasDesired && hasRoutes {
				state = "inactive"
			}
			observation := common.Clone()
			observation.Update(jsonx.Obj("state", state, "severity", "warning",
				"title", routerName+" · "+check.title, "status_label", "需要手动处理",
				"message", EventMessage(check.reason, password),
				"target", Target("/routers", "前往对应设置", "router", router.Value("id"), "profile", profileID, "section", check.section)))
			Observe(items, prefix+check.key, observation)
		}
		switch {
		case summary != nil:
			state := summary.Value("state")
			mapped := "unknown"
			switch state {
			case "synced":
				mapped = "resolved"
			case "pending", "blocked":
				mapped = "pending"
			}
			label, severity, linkLabel := "需要手动处理", "warning", "查看阻断原因"
			if state == "pending" {
				label, severity, linkLabel = "待发布", "neutral", "查看并发布"
			}
			message := summary.Value("message")
			if !jsonx.Truthy(message) {
				message = "有 " + jsonx.Str(summary.Value("pending")) + " 项配置差异待发布。"
			}
			observation := common.Clone()
			observation.Update(jsonx.Obj("state", mapped, "title", routerName+" · 上游路由发布",
				"status_label", label, "severity", severity, "message", EventMessage(message, password),
				"target", Target("/routers", linkLabel, "router", router.Value("id"), "profile", profileID,
					"section", "report", "preview", "1")))
			Observe(items, prefix+"changes", observation)
		case report.Value("in_sync") == true:
			Observe(items, prefix+"changes", jsonx.Obj("state", "resolved"))
		case report.Value("in_sync") == false:
			observation := common.Clone()
			observation.Update(jsonx.Obj("state", "pending", "severity", "neutral",
				"title", routerName+" · 上游路由发布", "status_label", "待确认差异",
				"message", "检查发现配置差异，请预览发布确认可应用的改动。",
				"target", Target("/routers", "查看并发布", "router", router.Value("id"), "profile", profileID,
					"section", "report", "preview", "1")))
			Observe(items, prefix+"changes", observation)
		}
		return nil
	})
}

type observation struct {
	key   string
	value *jsonx.Object
}

type collectedObservations struct {
	profiles     map[string]any
	routers      map[string]string
	keys         map[string]bool
	observations []observation
}

func collect(ctx context.Context, repository *repo.Repository, completedTask string, s Sources) (*collectedObservations, error) {
	profileList, err := repository.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	profiles := map[string]any{}
	var profileOrder []string
	for _, item := range profileList {
		o, _ := item.(*jsonx.Object)
		id := o.GetString("id")
		profiles[id] = o.Value("name")
		profileOrder = append(profileOrder, id)
	}
	agentList, err := s.Agents(ctx)
	if err != nil {
		return nil, err
	}
	agents := map[string]*jsonx.Object{}
	var agentOrder []string
	for _, agent := range agentList {
		id := jsonx.Str(agent.Value("id"))
		if _, seen := agents[id]; !seen {
			agentOrder = append(agentOrder, id)
		}
		agents[id] = agent
	}
	keys := map[string]bool{}
	var observations []observation
	tasks, err := s.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if completedTask != "" && task.Value("key") == completedTask {
			task = task.Clone()
			task.Set("running", false)
		}
		key := "task:" + jsonx.Str(task.Value("key"))
		keys[key] = true
		observations = append(observations, observation{key, TaskObservation(task, agents, profiles)})
	}
	for _, id := range agentOrder {
		key := "deployment:" + id
		keys[key] = true
		value, err := AgentObservation(ctx, agents[id], repository)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation{key, value})
	}
	routers := map[string]string{}
	for _, profileID := range profileOrder {
		config, err := repository.GetCompatConfig(ctx, profileID, nil)
		if err != nil {
			return nil, err
		}
		for _, router := range RoutersOf(config) {
			prefix := "router:" + profileID + ":" + jsonx.Str(router.Value("id")) + ":"
			routers[prefix] = RouterSignature(config, router)
			release := objectOr(router.Value("last_release"))
			key := prefix + "release"
			keys[key] = true
			state := "unknown"
			switch release.Value("ok") {
			case false:
				state = "pending"
			case true:
				state = "resolved"
			}
			message := release.Value("message")
			if !jsonx.Truthy(message) {
				message = "阶段「" + jsonx.Str(orValue(release.Value("failed_stage"), "未知")) + "」失败，请重新检查差异。"
			}
			observations = append(observations, observation{key, jsonx.Obj("state", state, "source", "router-release",
				"scope", "profile", "profile_id", profileID, "severity", "error", "status_label", "发布失败",
				"title", jsonx.Str(orValue(router.Value("name"), router.Value("id")))+" · 路由器发布",
				"message", EventMessage(message, router.Value("password")),
				"checked_at", release.Value("at"), "evidence", release.Value("at"),
				"target", Target("/routers", "查看发布结果", "router", router.Value("id"), "profile", profileID, "section", "report"))})
		}
	}
	return &collectedObservations{profiles: profiles, routers: routers, keys: keys, observations: observations}, nil
}

func (c *collectedObservations) apply(items *jsonx.Object, retainUnseen bool) {
	for _, observation := range c.observations {
		Observe(items, observation.key, observation.value)
	}
	profiles, routers, keys := c.profiles, c.routers, c.keys
	for _, key := range append([]string{}, items.Keys()...) {
		item, _ := items.Value(key).(*jsonx.Object)
		if item == nil {
			continue
		}
		if item.Value("source") == "router-check" {
			prefix := key[:strings.LastIndex(key, ":")] + ":"
			if signature, ok := routers[prefix]; ok {
				keys[key] = true
				if item.Value("state") == "pending" {
					item.Set("stale", item.Value("signature") != signature)
				}
			}
		}
		source := item.Value("source")
		if (source == RegistrationSource || source == AgentAddressSource) && registrationFresh(item) {
			keys[key] = true
		}
		if source == NoticeSource {
			keys[key] = true
		}
		if !keys[key] && !retainUnseen {
			Observe(items, key, jsonx.Obj("state", "inactive"))
		}
		if profileID, ok := item.Value("profile_id").(string); ok {
			if name, found := profiles[profileID]; found {
				item.Set("profile_name", name)
			}
		}
	}
}

// Snapshot is snapshot(completed_task): refresh every receipt and return the
// pending notifications, newest first within each severity.
func Snapshot(ctx context.Context, completedTask string) ([]any, error) {
	ctx = lifecycle.Detach(ctx)
	return snapshot(ctx, completedTask, true, false)
}

// Preview projects the same notifications without migration or persistence.
// HEAD uses this path so reading it cannot acknowledge or create receipts.
func Preview(ctx context.Context) ([]any, error) {
	return snapshot(ctx, "", false, false)
}

func snapshot(ctx context.Context, completedTask string, persist, partial bool) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := currentSources()
	if s.ListTasks == nil || s.Agents == nil {
		return nil, ErrSourcesMissing
	}
	retainUnseen := false
	if s.MissingTasks != nil {
		if missing := s.MissingTasks(); len(missing) > 0 {
			if !partial {
				return nil, &tasks.Unavailable{Slots: missing}
			}
			retainUnseen = true
		}
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	var ticket uint64
	if persist {
		ticket = observationSequence.Add(1)
	}
	collected, err := collect(ctx, repository, completedTask, s)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []*jsonx.Object
	err = transaction(ctx, repository.DataDir(), ticket, persist, func(items *jsonx.Object, apply bool) error {
		if apply {
			collected.apply(items, retainUnseen)
		}
		for _, value := range items.Values() {
			item, _ := value.(*jsonx.Object)
			if item == nil || item.Value("state") != "pending" {
				continue
			}
			copy := jsonx.NewObject()
			for key, v := range item.All() {
				if key != "signature" && key != "evidence" {
					copy.Set(key, jsonx.DeepCopy(v))
				}
			}
			result = append(result, copy)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	priority := map[any]int{"error": 0, "warning": 1, "neutral": 2}
	rank := func(item *jsonx.Object) int {
		if p, ok := priority[item.Value("severity")]; ok {
			return p
		}
		return 2
	}
	sort.SliceStable(result, func(i, j int) bool {
		return jsonx.Str(orValue(result[i].Value("last_seen_at"), "")) > jsonx.Str(orValue(result[j].Value("last_seen_at"), ""))
	})
	sort.SliceStable(result, func(i, j int) bool { return rank(result[i]) < rank(result[j]) })
	out := make([]any, len(result))
	for i, item := range result {
		out[i] = item
	}
	return out, nil
}

// RefreshTasks is refresh_tasks(completed_task): called after a task
// result was recorded, also without an open browser.
func RefreshTasks(ctx context.Context, completedTask string) error {
	if !Complete() {
		return nil
	}
	_, err := snapshot(lifecycle.Detach(ctx), completedTask, true, true)
	return err
}

// TaskStatus returns a task status store for dataDir whose records refresh
// the notifications (Python: task_status.record calls
// safely(notifications.refresh_tasks, key)).
func TaskStatus(ctx context.Context, dataDir string) *taskstatus.Store {
	store := taskstatus.New(dataDir)
	store.OnRecord = func(key string) {
		Safely(func() error { return RefreshTasks(ctx, key) })
	}
	return store
}
