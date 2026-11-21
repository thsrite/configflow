// Package notifications projects actionable task, Agent and router incidents.
// It reads saved receipts and supplied sources, never contacts a device.
package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/agents/diag"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

var logger = logx.Get("backend.notifications")

// Inputs supplies typed snapshots from owners not yet migrated here.
type Inputs struct {
	Rows         func(context.Context) ([]*tasks.Task, error)
	MissingTasks func() []string
	Agents       func(context.Context) ([]model.Agent, error)
	Summarize    func(*RouterReport, json.RawMessage) (*RouterSummary, error)
}

var (
	sourcesMu sync.Mutex
	inputs    Inputs
)

// SetInputs installs nonnil sources and returns an isolated-test restoration.
func SetInputs(s Inputs) func() {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	previous := inputs
	if s.Rows != nil {
		inputs.Rows = s.Rows
	}
	if s.MissingTasks != nil {
		inputs.MissingTasks = s.MissingTasks
	}
	if s.Agents != nil {
		inputs.Agents = s.Agents
	}
	if s.Summarize != nil {
		inputs.Summarize = s.Summarize
	}
	return func() { sourcesMu.Lock(); inputs = previous; sourcesMu.Unlock() }
}

// ResetInputs clears source wiring.
func ResetInputs()          { sourcesMu.Lock(); inputs = Inputs{}; sourcesMu.Unlock() }
func currentInputs() Inputs { sourcesMu.Lock(); defer sourcesMu.Unlock(); return inputs }

// Complete reports whether all source kinds are wired.
func Complete() bool { s := currentInputs(); return s.Rows != nil && s.Agents != nil }

// MissingSources lists required unwired sources.
func MissingSources() []string {
	s := currentInputs()
	var missing []string
	if s.Rows == nil {
		missing = append(missing, "task_rows")
	}
	if s.Agents == nil {
		missing = append(missing, "agents")
	}
	return missing
}

// ErrSourceUnavailable identifies a source read failure (HTTP 503).
var ErrSourceUnavailable = errors.New("notification source unavailable")

// ErrSourcesMissing identifies incomplete source wiring.
var ErrSourcesMissing = errors.New("notification sources are not wired")

// Safely logs an incident projection failure without failing its producer.
func Safely(fn func() error) {
	defer func() {
		if recover() != nil {
			logger.Error("Could not update notification receipts")
		}
	}()
	if err := fn(); err != nil {
		logger.Exception("Could not update notification receipts", err)
	}
}

// MessageLimit is the shared operation-event byte budget.
const MessageLimit = diag.MessageLimit

var taskLabels = map[string]string{"subscription": msgSubscription, "rule_source": msgRuleSource, "router_rule_sync": msgRouterSync, "agent_rule_sync": msgAgentSync, "dns_follow": msgDNSFollow, "webdav_backup": msgBackup, "domain_discovery": msgDomains, "metrics_history": msgMetrics}

func labelOr(category string) string {
	if label, ok := taskLabels[category]; ok {
		return label
	}
	return msgBackground
}
func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}
func eventText(text string, secrets ...string) string { return diag.EventText(text, secrets...) }

// TaskIncident projects one typed task result.
func TaskIncident(task *tasks.Task, agents map[string]model.Agent, profiles map[string]string) (*Item, error) {
	if task == nil {
		return nil, tasks.ErrInvalidHistory
	}
	history := task.HistoryData
	if history == nil {
		history = &tasks.HistoryData{RunCount: "0", FailureCount: "0"}
	}
	category, resource := task.Category, task.ResourceID
	agent := model.Agent{}
	if category == "agent_rule_sync" || category == "dns_follow" {
		agent = agents[resource]
	}
	profile := firstText(stringValue(task.ProfileID), agent.ProfileID)
	query := map[string]string{"tab": "tasks", "task": task.Key}
	link := Destination("/system-settings", msgTaskLink, query)
	switch category {
	case "subscription":
		link = Destination("/subscriptions", msgSubscriptionLink, map[string]string{"subscription": resource, "section": "settings"})
	case "rule_source":
		link = Destination("/rule-library", msgRuleLink, map[string]string{"rule": resource, "section": "settings"})
	case "router_rule_sync":
		query = map[string]string{"router": resource, "section": "rule-sync"}
		if profile != "" {
			query["profile"] = profile
		}
		link = Destination("/routers", msgSyncLink, query)
	case "agent_rule_sync", "dns_follow":
		section := "dns-follow"
		if category == "agent_rule_sync" {
			section = "rule-sync"
		}
		query = map[string]string{"agent": resource, "section": section}
		if profile != "" {
			query["profile"] = profile
		}
		link = Destination("/agents", msgSyncLink, query)
	case "webdav_backup":
		link = Destination("/system-settings", msgBackupLink, map[string]string{"section": "backup"})
	}
	enabled := task.Enabled
	if task.NotificationsEnabled != nil {
		enabled = *task.NotificationsEnabled
	}
	enabled = enabled && model.SwitchEnabled(&agent, "enabled", agent.Enabled, true)
	status := stringValue(history.LastStatus)
	manual := category == "router_rule_sync" && task.StateMessage != "" && !task.Running
	problem := status == "failed" || (status == "skipped" && stringValue(history.LastError) != "") || manual
	state := "unknown"
	switch {
	case !enabled:
		state = "inactive"
	case task.Running:
		state = "unknown"
	case problem:
		state = "pending"
	case status == "success":
		state = "resolved"
	}
	message := firstText(stringValue(history.LastError), task.StateMessage, msgTaskIncomplete)
	impact := ""
	if history.CacheRetained != nil {
		if *history.CacheRetained {
			impact = msgCacheRetained
			if stringValue(history.CacheUpdatedAt) != "" {
				impact += "（" + *history.CacheUpdatedAt + "）"
			}
		} else {
			impact = msgCacheUnavailable
		}
	}
	title := task.Name + " · " + labelOr(category)
	if category == "webdav_backup" || category == "domain_discovery" || category == "metrics_history" {
		title = task.Name
	}
	if category == "webdav_backup" && stringValue(history.LastTrigger) == "manual" {
		title = msgManualBackup
	}
	label, severity := msgFailed, "error"
	if manual || status == "skipped" {
		label = msgManualAction
	}
	if manual || status == "skipped" || (history.CacheRetained != nil && *history.CacheRetained) {
		severity = "warning"
	}
	scope := "system"
	if profile != "" {
		scope = "profile"
	} else if category == "subscription" || category == "rule_source" {
		scope = "shared"
	}
	run := history.RunCount
	if run == "" {
		run = "0"
	}
	failure := history.ConsecutiveFailures
	if len(failure) == 0 || string(failure) == "null" {
		failure = json.RawMessage("0")
	}
	item := &Item{State: state, Source: "task", Title: &title, Message: new(eventText(message)), Impact: &impact, StatusLabel: &label, Severity: &severity, Scope: &scope, Target: link,
		CheckedAt: nullableString(history.LastRunAt), FailureCount: failure, Evidence: jsonString(string(run) + ":" + stringValue(history.LastRunAt) + ":" + status)}
	var profileID, profileName *string
	if profile != "" {
		profileID = &profile
		name, ok := profiles[profile]
		if !ok {
			name = profile
		}
		profileName = &name
	}
	if err := item.profile(profileID, profileName); err != nil {
		return nil, err
	}
	return item, nil
}

var deploymentLabels = map[string]string{"failed": msgPublishFailed, "rolled_back": msgPublishRolledBack, "rollback_failed": msgRollbackFailed, "ready": msgActivationWaiting, "recovery_pending": msgRecoveryWaiting, "unknown": msgResultUnknown}

// AgentIncident projects a typed deployment without contacting its Agent.
func AgentIncident(ctx context.Context, agent *model.Agent, repository *repo.Repository) (*Item, error) {
	record := agent.LatestDeployment
	if record == nil {
		record = &model.Deployment{}
	}
	profile := firstText(record.ProfileID, agent.ProfileID)
	if profile == "" {
		id, err := repository.DefaultProfileID(ctx)
		if err != nil {
			return nil, err
		}
		profile = id
	}
	agentProfile := agent.ProfileID
	if agentProfile == "" {
		id, err := repository.DefaultProfileID(ctx)
		if err != nil {
			return nil, err
		}
		agentProfile = id
	}
	label, known := deploymentLabels[record.Status]
	state := "unknown"
	if known {
		state = "pending"
	}
	if record.Status == "succeeded" || (record.Status == "rolled_back" && record.ManualRollback != nil && *record.ManualRollback) {
		state = "resolved"
	}
	if !model.SwitchEnabled(agent, "enabled", agent.Enabled, true) || profile != agentProfile {
		state = "inactive"
	}
	service := firstText(record.ServiceType, agent.ServiceType)
	title := firstText(agent.Name, agent.ID) + " · " + service + msgConfigPublication
	severity, linkLabel := "warning", msgViewPublication
	if record.Status == "failed" || record.Status == "rolled_back" || record.Status == "rollback_failed" {
		severity, linkLabel = "error", msgViewFailure
	}
	impact := ""
	if record.Status == "rolled_back" {
		impact = msgPreviousRestored
	}
	message := firstText(stringValue(record.RollbackError), stringValue(record.Error), stringValue(record.Message), label)
	checked := firstText(stringValue(record.UpdatedAt), record.CreatedAt)
	var checkedJSON json.RawMessage = json.RawMessage("null")
	if checked != "" {
		checkedJSON = jsonString(checked)
	}
	query := map[string]string{"agent": agent.ID, "profile": profile, "section": "deployment"}
	if record.DeploymentID != "" {
		query["deployment"] = record.DeploymentID
	}
	// An absent deployment id was Python None in evidence, but a real journal
	// always supplies it. Keep the unknown observation's empty identity native.
	return &Item{State: state, Source: "deployment", ProfileID: &profile, Scope: new("profile"), Title: &title, Message: new(eventText(message, agent.Token)), Impact: &impact, StatusLabel: &label, Severity: &severity,
		CheckedAt: checkedJSON, Evidence: jsonString(record.DeploymentID + record.Status), Target: Destination("/agents", linkLabel, query)}, nil
}

// SaveAgent records a deployment incident.
func SaveAgent(ctx context.Context, agent *model.Agent, repository *repo.Repository) error {
	ctx = lifecycle.Detach(ctx)
	if agent == nil {
		return nil
	}
	item, err := AgentIncident(ctx, agent, repository)
	if err != nil {
		return err
	}
	return UpdateItems(ctx, repository.DataDir(), func(items *Items) error { return ObserveItem(items, "deployment:"+agent.ID, item) })
}

// Incident source names and registration retention constants.
const (
	RegistrationSource       = "agent-registration"
	RegistrationLimit        = 20
	RegistrationFreshSeconds = 300
	AgentAddressSource       = "agent-address"
	NoticeSource             = "notice"
)

// RejectRegistration records a bounded incident for a rejected Agent.
func RejectRegistration(ctx context.Context, repository *repo.Repository, agent *model.Agent, reason string) error {
	if agent == nil {
		agent = &model.Agent{}
	}
	name := truncateRunes(eventText(firstText(agent.Name, msgUnnamed)), 64)
	host := truncateRunes(eventText(agent.Host), 64)
	service := truncateRunes(firstText(agent.ServiceType, "mihomo"), 16)
	sum := sha256.Sum256([]byte(name + "|" + host + "|" + service))
	key := "registration:" + hex.EncodeToString(sum[:])[:24]
	return UpdateItems(ctx, repository.DataDir(), func(items *Items) error {
		if _, exists := items.Get(key); !exists {
			count := 0
			for _, item := range items.All() {
				if item.Source == RegistrationSource {
					count++
				}
			}
			if count >= RegistrationLimit {
				return nil
			}
		}
		where := firstText(host, msgUnknownAddress)
		return ObserveItem(items, key, &Item{State: "pending", Source: RegistrationSource, Scope: new("system"), Severity: new("error"), StatusLabel: new(msgRegistrationRejected), Title: new(name + "（" + where + "）" + msgRegistrationTitle), Message: new(eventText(reason)), Impact: new(msgRegistrationImpact), CheckedAt: jsonString(Now()), Evidence: jsonString(strconv.FormatInt(Clock().Unix()/60, 10)), Target: Destination("/agents", msgAgentList, nil)})
	})
}

// SaveAgentAddress records an address-change warning.
func SaveAgentAddress(ctx context.Context, repository *repo.Repository, agent *model.Agent, host, label, message string) error {
	name := truncateRunes(eventText(firstText(agent.Name, agent.ID)), 64)
	return UpdateItems(ctx, repository.DataDir(), func(items *Items) error {
		return ObserveItem(items, "agent-address:"+agent.ID, &Item{State: "pending", Source: AgentAddressSource, Scope: new("system"), Severity: new("warning"), StatusLabel: &label, Title: new(fmt.Sprintf(msgAddressTitle, name, agent.Host, host)), Message: new(truncateRunes(message, MessageLimit)), Impact: new(msgAddressImpact), CheckedAt: jsonString(Now()), Evidence: jsonString(strconv.FormatInt(Clock().Unix()/120, 10)), Target: Destination("/agents", msgEditAddress, map[string]string{"agent": agent.ID, "section": "address"})})
	})
}

// ResolveAgentAddress retires an address incident.
func ResolveAgentAddress(ctx context.Context, repository *repo.Repository, agentID string) error {
	return UpdateItems(ctx, repository.DataDir(), func(items *Items) error {
		return ObserveItem(items, "agent-address:"+agentID, &Item{State: "resolved"})
	})
}

// SaveNotice records an automatic panel adjustment until acknowledged.
func SaveNotice(ctx context.Context, repository *repo.Repository, key, title, message string, link *Navigation) error {
	return UpdateItems(ctx, repository.DataDir(), func(items *Items) error {
		return ObserveItem(items, key, &Item{State: "pending", Source: NoticeSource, Scope: new("system"), Severity: new("warning"), StatusLabel: new(msgAutomaticallyAdjusted), Title: &title, Message: new(truncateRunes(message, MessageLimit)), Impact: new(""), CheckedAt: jsonString(Now()), Evidence: jsonString(key), Target: link})
	})
}
func registrationFresh(item *Item) bool {
	seen, ok := taskstatus.ParseTimestamp(stringValue(item.LastSeenAt))
	return ok && Clock().Sub(seen) < RegistrationFreshSeconds*time.Second
}

// RouterDigest uses native JSON for an ordered config/router pair (D2 policy B).
// Inputs are encoded configuration snapshots, not Python serialization artifacts.
func RouterDigest(config, router json.RawMessage) (string, error) {
	data, err := json.Marshal([2]json.RawMessage{config, router})
	if err != nil {
		return "", err
	}
	data, err = jsonresp.Canonical(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type routerCheck struct {
	key, title      string
	data            *RouterCheck
	reason, section string
}

// SaveRouter records a checked router's manual actions and publication summary.
func SaveRouter(ctx context.Context, profileID string, config json.RawMessage, router *model.Router, report *RouterReport, plan json.RawMessage, checkedAt string) error {
	ctx = lifecycle.Detach(ctx)
	if router == nil || report == nil {
		return ErrInvalidStore
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	encodedRouter, err := json.Marshal(router)
	if err != nil {
		return err
	}
	signature, err := RouterDigest(config, encodedRouter)
	if err != nil {
		return err
	}
	stamp := firstText(checkedAt, Now())
	prefix := "router:" + profileID + ":" + router.ID + ":"
	var ipv4 *RouterCheck
	if report.Routes != nil {
		ipv4 = report.Routes.ForwardHint
	}
	reason := ""
	if ipv4 != nil && ipv4.FixCommand != "" {
		reason = ipv4.Warning
	}
	checks := []routerCheck{{"firewall", msgIPv4Forward, ipv4, reason, "ipv4-forward-hint"}}
	sections := []routerCheck{{"ipv6_fakeip", msgIPv6FakeIP, report.IPv6FakeIP, "", "ipv6-fakeip"}, {"ipv6_routes", msgIPv6Routes, report.IPv6Routes, "", "ipv6-routes"}, {"ipv6_dns", msgIPv6DNS, report.IPv6DNS, "", "ipv6-dns"}, {"ipv6_pd_guard", msgIPv6PDGuard, report.IPv6PDGuard, "", "ipv6-pd-guard"}}
	for _, check := range sections {
		if data := check.data; data != nil {
			parts := data.Blockers
			if len(parts) == 0 {
				parts = data.Skip
			}
			check.reason = strings.Join(parts, "；")
			if data.FixCommand != "" {
				check.reason = firstText(data.Warning, check.reason, data.Message, msgRouterTerminal)
			}
		}
		checks = append(checks, check)
	}
	var summary *RouterSummary
	if len(plan) > 0 && string(plan) != "null" {
		summarize := currentInputs().Summarize
		if summarize == nil {
			return errors.New("routers.pending.summarize is not wired")
		}
		summary, err = summarize(report, plan)
		if err != nil {
			return err
		}
	}
	routerName := firstText(router.Name, router.ID)
	common := func() *Item {
		return &Item{Source: "router-check", Scope: new("profile"), ProfileID: &profileID, CheckedAt: jsonString(stamp), Evidence: jsonString(stamp), Signature: &signature}
	}
	destination := func(label, section string) *Navigation {
		return Destination("/routers", label, map[string]string{"router": router.ID, "profile": profileID, "section": section})
	}
	return UpdateItems(ctx, repository.DataDir(), func(items *Items) error {
		for _, check := range checks {
			state := "resolved"
			empty := check.data == nil
			if !empty {
				encoded, _ := json.Marshal(check.data)
				empty = string(encoded) == "{}"
			}
			switch {
			case check.reason != "":
				state = "pending"
			case empty || (check.data != nil && (check.data.Status == "unknown" || check.data.Status == "error")):
				state = "unknown"
			case check.data.Enabled != nil && !*check.data.Enabled:
				state = "inactive"
			}
			if empty && report.Desired != nil && (report.HasRoutes || report.Routes != nil) {
				state = "inactive"
			}
			item := common()
			item.State = state
			item.Severity = new("warning")
			item.Title = new(routerName + " · " + check.title)
			item.StatusLabel = new(msgManualAction)
			item.Message = new(eventText(check.reason, router.Password))
			item.Target = destination(msgCorrespondingSettings, check.section)
			if err := ObserveItem(items, prefix+check.key, item); err != nil {
				return err
			}
		}
		if summary != nil {
			state := "unknown"
			switch summary.State {
			case "synced":
				state = "resolved"
			case "pending", "blocked":
				state = "pending"
			}
			label, severity, linkLabel := msgManualAction, "warning", msgViewBlocker
			if summary.State == "pending" {
				label, severity, linkLabel = msgPendingPublish, "neutral", msgViewAndPublish
			}
			item := common()
			item.State = state
			item.Title = new(routerName + msgUpstreamPublication)
			item.StatusLabel = &label
			item.Severity = &severity
			item.Message = new(eventText(firstText(summary.Message, fmt.Sprintf(msgPendingChanges, summary.Pending)), router.Password))
			item.Target = destination(linkLabel, "report")
			item.Target.Query.Set("preview", "1")
			return ObserveItem(items, prefix+"changes", item)
		}
		if report.InSync != nil {
			if *report.InSync {
				return ObserveItem(items, prefix+"changes", &Item{State: "resolved"})
			}
			item := common()
			item.State = "pending"
			item.Severity = new("neutral")
			item.Title = new(routerName + msgUpstreamPublication)
			item.StatusLabel = new(msgPendingConfirmation)
			item.Message = new(msgCheckDifferences)
			item.Target = destination(msgViewAndPublish, "report")
			item.Target.Query.Set("preview", "1")
			return ObserveItem(items, prefix+"changes", item)
		}
		return nil
	})
}

type observation struct {
	key   string
	value *Item
}
type collectedObservations struct {
	profiles     map[string]string
	routers      map[string]string
	keys         map[string]bool
	observations []observation
}

type routerSignatureEntry struct {
	head       store.Head
	signatures map[string]string
}

var (
	routerSigMu    sync.Mutex
	routerSigCache = map[string]routerSignatureEntry{}
)

const routerSigCacheLimit = 16

type routerReleaseView struct {
	profileID   string
	id          string
	name        string
	password    string
	hasRelease  bool
	releaseOK   bool
	failedStage string
	message     string
	at          string
}

func routerSignatures(ctx context.Context, repository *repo.Repository, profileIDs []string) (map[string]string, error) {
	if len(profileIDs) == 0 {
		return map[string]string{}, nil
	}
	inTx := repository.InTransaction(ctx)
	var head store.Head
	cacheKey := repository.DataDir()
	if !inTx {
		var err error
		head, err = repository.Head(ctx)
		if err != nil {
			return nil, err
		}
		routerSigMu.Lock()
		if cached, ok := routerSigCache[cacheKey]; ok && cached.head == head {
			sigs := cached.signatures
			routerSigMu.Unlock()
			return sigs, nil
		}
		routerSigMu.Unlock()
	}
	routers := make(map[string]string, len(profileIDs))
	for _, profileID := range profileIDs {
		// Router checks digest the view router management reads, including the
		// OxiDNS Agent projection its policy entries depend on.
		config, err := repository.ConfigWithOxiDNSAgents(ctx, profileID, nil)
		if err != nil {
			return nil, err
		}
		encodedConfig, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		for _, router := range config.Profile.Routers.Items {
			prefix := "router:" + profileID + ":" + router.ID + ":"
			encoded, err := json.Marshal(router)
			if err != nil {
				return nil, err
			}
			signature, err := RouterDigest(encodedConfig, encoded)
			if err != nil {
				return nil, err
			}
			routers[prefix] = signature
		}
	}
	if !inTx {
		routerSigMu.Lock()
		if len(routerSigCache) >= routerSigCacheLimit {
			clear(routerSigCache)
		}
		routerSigCache[cacheKey] = routerSignatureEntry{head: head, signatures: routers}
		routerSigMu.Unlock()
	}
	return routers, nil
}

func collect(ctx context.Context, repository *repo.Repository, completedTask string, s Inputs) (*collectedObservations, error) {
	ctx = tasks.WithSnapshotCache(ctx)
	profiles := map[string]string{}
	var routerProfiles []string
	var routerReleases []routerReleaseView
	if err := repository.View(ctx, func(document *model.Document) error {
		for _, profile := range document.Profiles {
			pid := profile.Key()
			profiles[pid] = profile.Name
			if len(profile.Routers.Items) > 0 {
				routerProfiles = append(routerProfiles, pid)
			}
			for _, router := range profile.Routers.Items {
				view := routerReleaseView{
					profileID: pid,
					id:        router.ID,
					name:      router.Name,
					password:  router.Password,
				}
				if router.LastRelease != nil {
					view.hasRelease = true
					view.releaseOK = router.LastRelease.OK
					view.failedStage = stringValue(router.LastRelease.FailedStage)
					view.message = router.LastRelease.Message
					view.at = router.LastRelease.At
				}
				routerReleases = append(routerReleases, view)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	agentList, err := s.Agents(ctx)
	if err != nil {
		return nil, err
	}
	agents := map[string]model.Agent{}
	var agentOrder []string
	for _, agent := range agentList {
		if _, seen := agents[agent.ID]; !seen {
			agentOrder = append(agentOrder, agent.ID)
		}
		agents[agent.ID] = agent
	}
	keys := map[string]bool{}
	var observations []observation
	rows, err := s.Rows(ctx)
	if err != nil {
		return nil, err
	}
	for _, task := range rows {
		if completedTask != "" && task.Key == completedTask {
			copy := *task
			copy.Running = false
			task = &copy
		}
		key := "task:" + task.Key
		keys[key] = true
		item, err := TaskIncident(task, agents, profiles)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation{key, item})
	}
	for _, id := range agentOrder {
		key := "deployment:" + id
		keys[key] = true
		agent := agents[id]
		item, err := AgentIncident(ctx, &agent, repository)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation{key, item})
	}
	routers, err := routerSignatures(ctx, repository, routerProfiles)
	if err != nil {
		return nil, err
	}
	for _, router := range routerReleases {
		profileID := router.profileID
		prefix := "router:" + profileID + ":" + router.id + ":"
		key := prefix + "release"
		keys[key] = true
		state := "unknown"
		if router.hasRelease {
			if router.releaseOK {
				state = "resolved"
			} else {
				state = "pending"
			}
		}
		stage := firstText(router.failedStage, msgUnknown)
		message := firstText(router.message, fmt.Sprintf(msgReleaseFailed, stage))
		var at json.RawMessage = json.RawMessage("null")
		if router.at != "" {
			at = jsonString(router.at)
		}
		observations = append(observations, observation{key, &Item{State: state, Source: "router-release", Scope: new("profile"), ProfileID: &profileID, Severity: new("error"), StatusLabel: new(msgPublishFailed), Title: new(firstText(router.name, router.id) + msgRouterPublication), Message: new(eventText(message, router.password)), CheckedAt: at, Evidence: at, Target: Destination("/routers", msgViewPublishResult, map[string]string{"router": router.id, "profile": profileID, "section": "report"})}})
	}
	return &collectedObservations{profiles, routers, keys, observations}, nil
}
func (c *collectedObservations) apply(items *Items, retainUnseen bool) error {
	for _, observation := range c.observations {
		if err := ObserveItem(items, observation.key, observation.value); err != nil {
			return err
		}
	}
	for key, item := range items.All() {
		if item.Source == "router-check" {
			prefix := key[:strings.LastIndex(key, ":")] + ":"
			if signature, ok := c.routers[prefix]; ok {
				c.keys[key] = true
				if item.State == "pending" {
					item.Stale = new(stringValue(item.Signature) != signature)
				}
			}
		}
		if (item.Source == RegistrationSource || item.Source == AgentAddressSource) && registrationFresh(item) {
			c.keys[key] = true
		}
		if item.Source == NoticeSource {
			c.keys[key] = true
		}
		if !c.keys[key] && !retainUnseen {
			items.Delete(key)
		}
		if name, found := c.profiles[stringValue(item.ProfileID)]; found {
			item.ProfileName = &name
		}
	}
	return nil
}

// SnapshotItems refreshes receipts and returns pending incidents in display order.
func SnapshotItems(ctx context.Context, completedTask string) ([]*Item, error) {
	return snapshot(lifecycle.Detach(ctx), completedTask, true, false)
}

// PreviewItems returns the projection without creating or rewriting receipts.
func PreviewItems(ctx context.Context) ([]*Item, error) { return snapshot(ctx, "", false, false) }
func snapshot(ctx context.Context, completedTask string, persist, partial bool) ([]*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := currentInputs()
	if s.Rows == nil || s.Agents == nil {
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
	result := []*Item{}
	err = transaction(ctx, repository.DataDir(), ticket, persist, func(items *Items, apply bool) error {
		if apply {
			if err := collected.apply(items, retainUnseen); err != nil {
				return err
			}
		}
		// Map iteration must not affect stable ties in display order.
		for _, key := range items.Keys() {
			item, _ := items.Get(key)
			if item.State != "pending" {
				continue
			}
			copy := *item
			if err := model.ClearMember(&copy, &copy.Raw, "signature"); err != nil {
				return err
			}
			if err := model.ClearMember(&copy, &copy.Raw, "evidence"); err != nil {
				return err
			}
			result = append(result, &copy)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rank := func(item *Item) int {
		switch stringValue(item.Severity) {
		case "error":
			return 0
		case "warning":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return stringValue(result[i].LastSeenAt) > stringValue(result[j].LastSeenAt) })
	sort.SliceStable(result, func(i, j int) bool { return rank(result[i]) < rank(result[j]) })
	return result, nil
}

// RefreshTasks observes known sources without retiring unobserved old incidents.
func RefreshTasks(ctx context.Context, completedTask string) error {
	if !Complete() {
		return nil
	}
	_, err := snapshot(lifecycle.Detach(ctx), completedTask, true, true)
	return err
}

// TaskStatus wires status commits to notification refresh after the leaf unlock.
func TaskStatus(ctx context.Context, dataDir string) *taskstatus.Store {
	store := taskstatus.New(dataDir)
	store.OnRecord = func(key string) { Safely(func() error { return RefreshTasks(ctx, key) }) }
	return store
}
