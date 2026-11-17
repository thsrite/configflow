package agents

// The deployment bookkeeping of AgentManager: persistent task ownership,
// result recording, status polling, activation, manual rollback and
// recovery. Preparing and uploading bundles (_publish_deployment,
// _remember_deployment_content) belongs to #24/#26 and reaches the registry
// through the PublishDeployment hook.

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
)

var deploymentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}\z`)

const busyMessage = "another service operation is in progress"

type preparationLease struct {
	release func()
	stop    func() bool
}

var preparationLocks = lockx.NewKeyed[[3]string](lockx.LevelJob, "deployment-preparation")

// FinishDeploymentPreparation releases the preparation lease after its owner
// has finished. Multiple Manager objects observe one shared physical key.
func (m *Manager) FinishDeploymentPreparation(agentID, deploymentID string) {
	m.releasePreparation([2]string{agentID, deploymentID}, nil)
}

func (m *Manager) releasePreparation(key [2]string, owned *preparationLease) {
	m.deploymentMu.Lock()
	lease := m.preparations[key]
	if lease != nil && (owned == nil || owned == lease) {
		delete(m.preparations, key)
	} else {
		lease = nil
	}
	m.deploymentMu.Unlock()
	if lease != nil {
		if lease.stop != nil {
			lease.stop()
		}
		lease.release()
	} else if owned != nil {
		owned.release() // Idempotent; never release a newer owner's lease.
	}
}

func (m *Manager) defaultPreparationRunning(agentID, deploymentID string) (bool, error) {
	return preparationLocks.Held([3]string{m.repository.DataDir(), agentID, deploymentID}), nil
}

// BeginDeployment is begin_deployment(agent_id, deployment_id): persist a
// profile-bound task before preparing or sending its artifacts.
func (m *Manager) BeginDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if !deploymentIDPattern.MatchString(deploymentID) {
		return nil, &ValueError{Message: "Invalid deployment ID"}
	}
	var started *jsonx.Object
	key := [2]string{agentID, deploymentID}
	var lease *preparationLease
	keep := false
	defer func() {
		if !keep && lease != nil {
			m.releasePreparation(key, lease)
		}
	}()
	value, err := m.UpdateAgents(ctx, "", func(ctx context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, agentID)
		if agent == nil {
			return jsonx.Obj("success", false, "message", "Agent not found", "http_status", jsonx.Int(404)), nil
		}
		if conflict := haConflict(agent, deploymentID); conflict != nil {
			return conflict, nil
		}
		follow, _ := agent.Value("dns_follow_state").(*jsonx.Object)
		if status := follow.Value("status"); (status == "preparing" || status == "publishing") && !jsonx.Equal(follow.Value("target_deployment_id"), deploymentID) {
			return jsonx.Obj("success", false, "message", "DNS 联动发布正在执行，请等待结果后再发布 OxiDNS", "http_status", jsonx.Int(409)), nil
		}
		for _, item := range *agents {
			other, _ := item.(*jsonx.Object)
			source, _ := other.Value("dns_follow").(*jsonx.Object)
			state, _ := other.Value("dns_follow_state").(*jsonx.Object)
			if jsonx.Equal(source.Value("source_agent_id"), agentID) && (state.Value("status") == "preparing" || state.Value("status") == "publishing") {
				return jsonx.Obj("success", false, "message", "绑定的 OxiDNS 正在同步，请等待结果后再发布 Mihomo", "http_status", jsonx.Int(409)), nil
			}
		}
		if upgradeBusy(agent) {
			return jsonx.Obj("success", false, "message", "Agent update is in progress", "http_status", jsonx.Int(409)), nil
		}
		deployments, ok := agent.Value("deployments").(*jsonx.Object)
		if !ok || deployments == nil {
			deployments = jsonx.NewObject()
			agent.Set("deployments", deployments)
		}
		if existing, ok := deployments.Value(deploymentID).(*jsonx.Object); ok {
			result := existing.Clone()
			result.Set("existing", true)
			return result, nil
		}
		for _, item := range deployments.Values() {
			task, _ := item.(*jsonx.Object)
			if !deploymentTerminal.has(task.Value("status")) {
				return jsonx.Obj("success", false, "message", "Agent already has an unfinished deployment",
					"deployment_id", task.Value("deployment_id"), "status", task.Value("status"), "http_status", jsonx.Int(409)), nil
			}
		}
		profile, err := m.profileOf(ctx, agent)
		if err != nil {
			return nil, err
		}
		record := jsonx.Obj("success", true, "deployment_id", deploymentID, "status", "preparing", "profile_id", profile,
			"service_type", agent.GetOr("service_type", "mihomo"), "created_at", nowISO())
		record.Set("events", AppendEvent([]any{}, "preparing", nil, "", nil))
		_, release, acquired := preparationLocks.TryLock(ctx, [3]string{m.repository.DataDir(), agentID, deploymentID})
		if !acquired {
			return jsonx.Obj("success", false, "message", "Deployment preparation already running", "http_status", jsonx.Int(409)), nil
		}
		lease = &preparationLease{release: release}
		m.deploymentMu.Lock()
		m.preparations[key] = lease
		m.deploymentMu.Unlock()

		// Earlier tasks can no longer become the latest one, the only task whose
		// per-file digests are read. Keep their fingerprint; drop the digests.
		for _, item := range deployments.Values() {
			previous, _ := item.(*jsonx.Object)
			if prepared, ok := previous.Value("prepared_content").(*jsonx.Object); ok && prepared != nil {
				prepared.Delete("files")
			}
		}
		deployments.Set(deploymentID, record)
		agent.Set("latest_deployment", record.Clone())
		started = agent
		// Bound history, retaining failed records as well as the active task.
		keys := deployments.Keys()
		for i := 0; i < len(keys)-30; i++ {
			deployments.Delete(keys[i])
		}
		return record.Clone(), nil
	})
	if err != nil {
		return nil, err
	}
	result := value.(*jsonx.Object)
	if jsonx.Truthy(result.Value("success")) && !jsonx.Truthy(result.Value("existing")) {
		if lease != nil {
			m.deploymentMu.Lock()
			if m.preparations[key] == lease {
				lease.stop = context.AfterFunc(ctx, func() { m.releasePreparation(key, lease) })
			}
			m.deploymentMu.Unlock()
			keep = true
		}
		if started != nil {
			logger.Infof("开始向 Agent %s 发布配置（任务 %s）", agentLabel(started), prefix(deploymentID, 12))
		}
	}
	return result, nil
}

func prefix(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		runes = runes[:n]
	}
	return string(runes)
}

// RecordOptions are the keyword flags of record_deployment.
type RecordOptions struct {
	OnlyUnsubmitted   bool
	UploadReceipt     bool
	ActivationReceipt bool
	// SkipUnchanged: a status poll that would only move updated_at returns
	// the stored task without rewriting the configuration document.
	SkipUnchanged bool
}

var receiptTerminal = pyStatuses{"succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}

type finishedDeployment struct {
	label string
	state *jsonx.Object
}

// recordUpdater is the record() closure of record_deployment.
func (m *Manager) recordUpdater(agentID, deploymentID string, updates *jsonx.Object, options RecordOptions, finished *[]finishedDeployment) func(ctx context.Context, agents *[]any) (any, error) {
	return func(ctx context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, agentID)
		if agent == nil {
			return jsonx.Obj("success", false, "message", "Agent not found"), nil
		}
		deployments, _ := agent.Value("deployments").(*jsonx.Object)
		state, _ := deployments.Value(deploymentID).(*jsonx.Object)
		if state == nil {
			return jsonx.Obj("success", false, "message", "Deployment not found", "http_status", jsonx.Int(404)), nil
		}
		status := state.Value("status")
		if options.OnlyUnsubmitted && jsonx.Truthy(state.Value("submitted")) {
			return state.Clone(), nil
		}
		if options.UploadReceipt && !receiptTerminal.has(updates.Value("status")) &&
			status != "preparing" && status != "uploading" && status != "receiving" {
			// A POST acceptance snapshot can arrive after a GET has already
			// observed validation/startup. It is not a fresh worker status.
			return state.Clone(), nil
		}
		if options.ActivationReceipt && !receiptTerminal.has(updates.Value("status")) && status != "ready" && status != "unknown" {
			return state.Clone(), nil
		}
		if jsonx.Truthy(state.Value("manual_rollback")) && !jsonx.Truthy(updates.Value("manual_rollback")) {
			// An overlapping publication poll cannot undo a reserved rollback.
			return state.Clone(), nil
		}
		if deploymentTerminal.has(status) && !jsonx.Equal(updates.GetOr("status", status), status) {
			// Never regress a confirmed transaction.
			return state.Clone(), nil
		}
		reportedAt, hasReported := "", false
		if _, isString := updates.Value("updated_at").(string); isString {
			reportedAt, hasReported = EventTime(updates.Value("updated_at"))
		}
		if hasReported {
			if previous, ok := state.Value("agent_updated_at").(string); ok && previous != "" && reportedAt < previous {
				return state.Clone(), nil
			}
		}
		before := status
		if newStatus := updates.Value("status"); jsonx.Truthy(newStatus) && newStatus != "unknown" && !jsonx.Truthy(updates.Value("http_status")) {
			if status == "unknown" {
				// Transport diagnostics describe an unconfirmed outcome.
				for _, field := range []string{"error_details", "rollback_error_details"} {
					if !updates.Has(field) {
						state.Delete(field)
					}
				}
			}
			state.Delete("http_status")
			if !updates.Has("message") {
				state.Delete("message")
			}
		}
		for _, pair := range [][2]string{{"error", "error_details"}, {"rollback_error", "rollback_error_details"}} {
			if value, ok := updates.Get(pair[0]); ok && !jsonx.Equal(value, state.Value(pair[0])) && !updates.Has(pair[1]) {
				state.Delete(pair[1])
			}
		}
		// Never let an upstream response replace task ownership or identity.
		for _, key := range []string{"status", "message", "error", "error_details", "failed_stage", "rollback_error", "rollback_error_details", "config_version",
			"config_revision", "success", "sha256", "submitted", "previous_running", "health",
			"http_status", "manual_rollback", "source_sha256"} {
			value, ok := updates.Get(key)
			if !ok {
				continue
			}
			switch key {
			case "message", "error", "error_details", "rollback_error", "rollback_error_details":
				state.Set(key, FailureText(value, tokenSecrets(agent), 0))
			default:
				state.Set(key, value)
			}
		}
		state.Update(DNSReportFields(updates, tokenSecrets(agent)))
		if s := updates.Value("status"); s == "ready" || s == "succeeded" {
			for _, field := range []string{"error", "error_details", "failed_stage", "rollback_error", "rollback_error_details"} {
				if !updates.Has(field) {
					state.Delete(field)
				}
			}
		}
		if updates.Value("status") == "rolled_back" && !jsonx.Truthy(updates.Value("rollback_error")) {
			state.Delete("rollback_error")
			state.Delete("rollback_error_details")
		}
		state.Set("updated_at", nowISO())
		if hasReported {
			state.Set("agent_updated_at", reportedAt)
		}
		message := state.Value("message")
		if !jsonx.Truthy(message) {
			message = state.Value("error")
		}
		state.Set("events", AppendEvent(state.Value("events"), jsonx.Str(state.Value("status")), message, "", tokenSecrets(agent)))
		latest, _ := agent.Value("latest_deployment").(*jsonx.Object)
		isLatest := jsonx.Equal(latest.Value("deployment_id"), deploymentID)
		bound := false
		if state.Value("status") == "succeeded" && isLatest {
			// default_profile_id() is read only when Python's `and` gets there.
			profile, err := m.profileOf(ctx, agent)
			if err != nil {
				return nil, err
			}
			bound = jsonx.Equal(state.Value("profile_id"), profile)
		}
		if bound && jsonx.Equal(state.GetOr("service_type", agent.Value("service_type")), agent.Value("service_type")) {
			agent.Set("config_version", state.GetOr("config_version", agent.GetOr("config_version", "0")))
			// Revisions are diagnostic; only server-prepared complete contents
			// may establish a baseline after confirmed activation.
			agent.Set("pushed_at", utcNowZ())
			if revision := state.Value("config_revision"); revision != nil {
				agent.Set("config_revision", revision)
			}
			if prepared := state.Value("prepared_content"); jsonx.Truthy(prepared) {
				applied, _ := jsonx.DeepCopy(prepared).(*jsonx.Object)
				if applied == nil {
					applied = jsonx.NewObject()
				}
				applied.Set("config_version", agent.Value("config_version"))
				applied.Set("deployment_id", deploymentID)
				agent.Set("applied_content", applied)
			} else {
				agent.Delete("applied_content")
			}
		}
		if jsonx.Truthy(state.Value("manual_rollback")) && state.Value("status") == "rolled_back" && isLatest {
			agent.Set("config_version", truthyOr(state.Value("config_version"), "0"))
			// The restored pre-publication files may predate ConfigFlow.
			for _, field := range []string{"applied_content", "config_revision", "pushed_at"} {
				agent.Delete(field)
			}
		}
		if isLatest {
			agent.Set("latest_deployment", state.Clone())
		}
		if newStatus, _ := state.Value("status").(string); !jsonx.Equal(state.Value("status"), before) {
			// `status in _DEPLOYMENT_RESULTS` raises for a list or dict an Agent
			// answered: the transaction is abandoned and nothing is stored.
			if err := unhashable(state.Value("status")); err != nil {
				return nil, err
			}
			if _, known := deploymentResults[newStatus]; known {
				*finished = append(*finished, finishedDeployment{agentLabel(agent), state.Clone()})
			}
		}
		return state.Clone(), nil
	}
}

// RecordDeployment is record_deployment(agent_id, deployment_id, updates,
// ...): only a confirmed successful activation changes the displayed version.
func (m *Manager) RecordDeployment(ctx context.Context, agentID, deploymentID string, updates *jsonx.Object, options RecordOptions) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var finished []finishedDeployment
	record := m.recordUpdater(agentID, deploymentID, updates, options, &finished)
	if options.SkipUnchanged {
		unchanged := m.unchangedDeployment(ctx, record, agentID, deploymentID)
		finished = nil
		if unchanged != nil {
			if unchanged.Value("service_type") == "oxidns" {
				if err := m.publicationComplete(ctx, agentID, deploymentID, unchanged.Value("status")); err != nil {
					return nil, err
				}
			}
			return unchanged, nil
		}
	}
	value, err := m.UpdateAgents(ctx, "", record)
	if err != nil {
		return nil, err
	}
	result := value.(*jsonx.Object)
	current, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	notifications.Safely(func() error { return notifications.RecordAgent(ctx, current, m.repository) })
	if len(finished) > 0 {
		last := finished[len(finished)-1]
		status := last.state.GetString("status")
		outcome := deploymentResults[status]
		detail := truthyOr(last.state.Value("error"), truthyOr(last.state.Value("rollback_error"), truthyOr(last.state.Value("message"), "")))
		suffix := ""
		if jsonx.Truthy(detail) && status != "succeeded" {
			suffix = "：" + logx.LogText(detail, 300)
		}
		logger.Log(outcome.level, "Agent "+last.label+" "+outcome.text+"（任务 "+prefix(deploymentID, 12)+"）"+suffix)
	}
	if result.Value("service_type") == "oxidns" {
		if err := m.publicationComplete(ctx, agentID, deploymentID, result.Value("status")); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (m *Manager) publicationComplete(ctx context.Context, agentID, deploymentID string, status any) error {
	if hook := currentHooks().PublicationComplete; hook != nil {
		return hook(ctx, m, agentID, deploymentID, status)
	}
	return nil
}

// unchangedDeployment is _unchanged_deployment: the stored task when the
// record would change nothing but its updated_at (decided on a read snapshot).
func (m *Manager) unchangedDeployment(ctx context.Context, record func(ctx context.Context, agents *[]any) (any, error), agentID, deploymentID string) *jsonx.Object {
	system, err := m.repository.GetSystem(ctx)
	if err != nil {
		return nil
	}
	agents := system.GetList("agents")
	agent, _ := findAgent(agents, agentID)
	deployments, _ := agent.Value("deployments").(*jsonx.Object)
	if _, ok := deployments.Value(deploymentID).(*jsonx.Object); !ok {
		return nil
	}
	stored := agent.DeepCopy()
	if _, err := record(ctx, &agents); err != nil {
		return nil // Let the real transaction report it.
	}
	storedDeployments := stored.GetObject("deployments")
	newDeployments, _ := agent.Value("deployments").(*jsonx.Object)
	pairs := [][2]any{{storedDeployments.Value(deploymentID), newDeployments.Value(deploymentID)},
		{stored.Value("latest_deployment"), agent.Value("latest_deployment")}}
	for _, pair := range pairs {
		old, okOld := pair[0].(*jsonx.Object)
		updated, okNew := pair[1].(*jsonx.Object)
		if okOld && okNew && old != nil && updated != nil {
			if value, ok := old.Get("updated_at"); ok {
				updated.Set("updated_at", value)
			} else {
				updated.Delete("updated_at")
			}
		}
	}
	if jsonx.Equal(agent, stored) {
		return storedDeployments.GetObject(deploymentID).Clone()
	}
	return nil
}

// DeploymentRequest is _deployment_request(agent, method, suffix, ...): a
// call to /api/<suffix> whose answer is always an object (sanitized, with
// http_status for HTTP errors). Only the exact busy rejection of a
// deployment POST is retried, for up to two seconds. Transport failures are
// *TransportError.
func (m *Manager) DeploymentRequest(ctx context.Context, agent *jsonx.Object, method, suffix string, header http.Header, body []byte, timeout *Timeout) (*jsonx.Object, error) {
	if agent == nil {
		// The Agent was deleted between the reservation and the request:
		// agent['host'] on None, not a RequestException.
		return nil, errors.New("'NoneType' object is not subscriptable")
	}
	t := Pair(5, 30)
	if timeout != nil {
		t = *timeout
	}
	retryBusy := method == "POST" && (suffix == "deployments" || (strings.HasPrefix(suffix, "deployments/") &&
		(strings.HasSuffix(suffix, "/activate") || strings.HasSuffix(suffix, "/rollback") || strings.HasSuffix(suffix, "/recover"))))
	deadline := m.monotonic() + 2
	for attempt := 0; ; attempt++ {
		// Only this exact lock rejection proves no mutation was accepted.
		response, err := m.Call(ctx, agent, method, "/api/"+suffix, header, body, t)
		if err != nil {
			return nil, err
		}
		value, jsonErr := response.JSON()
		var result *jsonx.Object
		if jsonErr != nil {
			result = jsonx.Obj("success", false, "message", "Agent returned HTTP "+strconv.Itoa(response.StatusCode))
		} else if object, ok := value.(*jsonx.Object); ok {
			result = object
		} else {
			result = jsonx.Obj("success", false, "message", "Invalid Agent response")
		}
		if response.StatusCode >= 400 {
			result.Set("success", false)
			result.Set("http_status", jsonx.Int(response.StatusCode))
		}
		busy := response.StatusCode == 409 && result.Value("message") == busyMessage
		for _, field := range []string{"message", "error", "error_details", "rollback_error", "rollback_error_details", "rollback_message", "failed_stage"} {
			if value, ok := result.Get(field); ok {
				result.Set(field, FailureText(value, tokenSecrets(agent), 0))
			}
		}
		report := DNSReportFields(result, tokenSecrets(agent))
		result.Delete("dns_checks")
		result.Delete("dns_checked_at")
		result.Update(report)
		remaining := deadline - m.monotonic()
		if !retryBusy || !busy || attempt == 20 || remaining <= 0 {
			return result, nil
		}
		m.sleep(time.Duration(min(0.1, remaining) * float64(time.Second)))
	}
}

func notFoundDeployment() *jsonx.Object {
	return jsonx.Obj("success", false, "message", "Deployment not found", "http_status", jsonx.Int(404))
}

func agentNotFound404() *jsonx.Object {
	return jsonx.Obj("success", false, "message", "Agent not found", "http_status", jsonx.Int(404))
}

func merged(state *jsonx.Object, pairs ...any) *jsonx.Object {
	result := state.Clone()
	for i := 0; i+1 < len(pairs); i += 2 {
		result.Set(pairs[i].(string), pairs[i+1])
	}
	return result
}

func taskOf(agent *jsonx.Object, deploymentID string) *jsonx.Object {
	deployments, _ := agent.Value("deployments").(*jsonx.Object)
	state, _ := deployments.Value(deploymentID).(*jsonx.Object)
	return state
}

// GetDeployment is get_deployment (oxidns_ha_publish.deployment, #29).
func (m *Manager) GetDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if hook := currentHooks().HADeployment; hook != nil {
		return hook(ctx, m, agentID, deploymentID)
	}
	return defaultHADeployment(ctx, m, agentID, deploymentID)
}

// GetDeploymentDirect is _get_deployment (for #29).
func (m *Manager) GetDeploymentDirect(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	return m.getDeployment(ctx, agentID, deploymentID)
}

func (m *Manager) getDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return agentNotFound404(), nil
	}
	state := taskOf(agent, deploymentID)
	if !jsonx.Truthy(state) {
		return notFoundDeployment(), nil
	}
	profile, err := m.profileOf(ctx, agent)
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(state.Value("profile_id"), profile) {
		return jsonx.Obj("success", false, "message", "Deployment belongs to a different profile", "http_status", jsonx.Int(409)), nil
	}
	if deploymentTerminal.has(state.Value("status")) {
		return state, nil
	}
	if !jsonx.Equal(state.Value("service_type"), agent.GetOr("service_type", "mihomo")) {
		return merged(state, "success", false, "http_status", jsonx.Int(409), "message", "Deployment belongs to a different service type"), nil
	}
	if !jsonx.Truthy(state.Value("submitted")) {
		if running, err := m.preparationRunning(agentID, deploymentID); err != nil || running {
			return stateOrError(state, err)
		}
		// The POST may finish preparation between our first read and the lock
		// check. Re-read before classifying an interrupted task.
		agent, err = m.GetAgentByID(ctx, agentID, true)
		if err != nil {
			return nil, err
		}
		if agent == nil {
			// Deleted meanwhile: agent.get('deployments', {}) on None.
			return nil, errors.New("'NoneType' object has no attribute 'get'")
		}
		if again := taskOf(agent, deploymentID); again != nil {
			state = again
		}
		if deploymentTerminal.has(state.Value("status")) {
			return state, nil
		}
		if !jsonx.Truthy(state.Value("submitted")) {
			if running, err := m.preparationRunning(agentID, deploymentID); err != nil || running {
				return stateOrError(state, err)
			}
			return m.RecordDeployment(ctx, agentID, deploymentID, jsonx.Obj("success", false, "status", "failed",
				"message", "Preparation interrupted before upload; current service unchanged"), RecordOptions{OnlyUnsubmitted: true})
		}
	}
	uploadInFlight, err := m.preparationRunning(agentID, deploymentID)
	if err != nil {
		return nil, err
	}
	pending := func() *jsonx.Object {
		return merged(state, "message", "Deployment status temporarily unavailable", "pending", true)
	}
	result, err := m.DeploymentRequest(ctx, agent, "GET", "deployments/"+deploymentID, nil, nil, nil)
	if IsTransportError(err) {
		return pending(), nil
	} else if err != nil {
		return nil, err
	}
	if jsonx.Truthy(state.Value("manual_rollback")) && result.Value("status") == "succeeded" && !jsonx.Truthy(result.Value("manual_rollback")) {
		if state.Value("status") != "unknown" {
			return merged(state, "message", "Rollback submission awaiting Agent confirmation", "pending", true), nil
		}
		// A lost POST response is ambiguous. Reissuing the same guarded,
		// idempotent rollback is safe; a publication status alone is not.
		result, err = m.DeploymentRequest(ctx, agent, "POST", "deployments/"+deploymentID+"/rollback", nil, nil, nil)
		if IsTransportError(err) {
			return pending(), nil
		} else if err != nil {
			return nil, err
		}
		busy := httpStatusIs(result, 409) && result.Value("message") == busyMessage
		if httpStatusIn(result, 400, 404, 405, 409) && !busy {
			return m.cancelManualRollback(ctx, agentID, deploymentID, result)
		}
	}
	if httpStatusIs(result, 404) {
		if jsonx.Truthy(state.Value("manual_rollback")) {
			// A missing journal cannot prove whether the rollback ran.
			return merged(state, "message", "Rollback journal unavailable; restore Agent records before retrying", "pending", true), nil
		}
		running := uploadInFlight
		if !running {
			if running, err = m.preparationRunning(agentID, deploymentID); err != nil {
				return nil, err
			}
		}
		if running {
			return merged(state, "message", "设备正在接收发布，等待确认", "pending", true), nil
		}
		return m.RecordDeployment(ctx, agentID, deploymentID, jsonx.Obj("success", false, "status", "failed",
			"message", "Agent has no record of the uploaded deployment"), RecordOptions{})
	}
	if _, isString := result.Value("status").(string); jsonx.Truthy(result.Value("http_status")) || !isString {
		return pending(), nil
	}
	return m.RecordDeployment(ctx, agentID, deploymentID, result, RecordOptions{SkipUnchanged: true})
}

func stateOrError(state *jsonx.Object, err error) (*jsonx.Object, error) {
	if err != nil {
		return nil, err
	}
	return state, nil
}

func httpStatusIs(result *jsonx.Object, status int) bool {
	return jsonx.Equal(result.Value("http_status"), jsonx.Int(status))
}

func httpStatusIn(result *jsonx.Object, statuses ...int) bool {
	for _, status := range statuses {
		if httpStatusIs(result, status) {
			return true
		}
	}
	return false
}

// ActivateDeployment is activate_deployment (oxidns_ha_publish.activate, #29).
func (m *Manager) ActivateDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if hook := currentHooks().HAActivate; hook != nil {
		return hook(ctx, m, agentID, deploymentID)
	}
	return defaultHAActivate(ctx, m, agentID, deploymentID)
}

// ActivateDeploymentDirect is _activate_deployment (for #29).
func (m *Manager) ActivateDeploymentDirect(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	return m.activateDeployment(ctx, agentID, deploymentID)
}

func (m *Manager) activateDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	state, err := m.getDeployment(ctx, agentID, deploymentID)
	if err != nil {
		return nil, err
	}
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if jsonx.Truthy(state.Value("http_status")) {
		return state, nil
	}
	if agent == nil {
		return agentNotFound404(), nil
	}
	profile, err := m.profileOf(ctx, agent)
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(state.Value("profile_id"), profile) || !jsonx.Equal(state.Value("service_type"), agent.GetOr("service_type", "mihomo")) {
		return merged(state, "success", false, "http_status", jsonx.Int(409),
			"message", "Deployment profile or service type changed; activation refused"), nil
	}
	if agent.Value("service_type") == "oxidns" {
		if state.Value("status") == "succeeded" {
			return state, nil
		}
		if state.Value("status") == "ready" {
			if jsonx.Truthy(state.Value("pending")) {
				return merged(state, "success", false, "http_status", jsonx.Int(409), "message", "尚未确认 Agent 候选状态，请稍后重试激活"), nil
			}
			matches := currentHooks().ReceiptMatches
			if matches == nil || !matches(agent, state) {
				// Leave the running service and remote candidate untouched. End
				// this server task so a new checked push is possible.
				value, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
					current, _ := findAgent(*agents, agentID)
					task := taskOf(current, deploymentID)
					if task == nil {
						return nil, errors.New("KeyError: '" + deploymentID + "'")
					}
					// A simultaneous status poll may have observed progress.
					if task.Value("status") != "ready" || !jsonx.Equal(task.Value("sha256"), state.Value("sha256")) {
						return merged(task, "success", false, "http_status", jsonx.Int(409), "message", "候选状态已变化，请刷新后重试"), nil
					}
					task.Set("success", false)
					task.Set("status", "failed")
					task.Set("failed_stage", "verifying")
					task.Set("http_status", jsonx.Int(409))
					task.Set("message", MessageUncheckedCandidate)
					task.Set("updated_at", nowISO())
					task.Set("events", AppendEvent(task.Value("events"), "failed", task.Value("message"), "", tokenSecrets(current)))
					latest, _ := current.Value("latest_deployment").(*jsonx.Object)
					if jsonx.Equal(latest.Value("deployment_id"), deploymentID) {
						current.Set("latest_deployment", task.Clone())
					}
					return task.Clone(), nil
				})
				if err != nil {
					return nil, err
				}
				return value.(*jsonx.Object), nil
			}
		}
	}
	if state.Value("status") != "ready" {
		return merged(state, "success", false, "message", "Deployment must be ready before activation", "http_status", jsonx.Int(409)), nil
	}
	if agent.Value("service_type") == "oxidns" {
		if hook := currentHooks().PublicationCheckpoint; hook != nil {
			prepared, _ := state.Value("prepared_content").(*jsonx.Object)
			if err := hook(ctx, m, agentID, deploymentID, prepared.Value("deployed_config_sha256")); err != nil {
				return nil, err
			}
		}
	}
	result, err := m.DeploymentRequest(ctx, agent, "POST", "deployments/"+deploymentID+"/activate", nil, nil, nil)
	if IsTransportError(err) {
		return m.RecordDeployment(ctx, agentID, deploymentID, jsonx.Obj("success", true, "status", "unknown",
			"message", "Activation sent; awaiting Agent status"), RecordOptions{ActivationReceipt: true})
	} else if err != nil {
		return nil, err
	}
	if jsonx.Truthy(result.Value("http_status")) {
		return result, nil
	}
	return m.RecordDeployment(ctx, agentID, deploymentID, result, RecordOptions{ActivationReceipt: true})
}

// RollbackDeployment is rollback_deployment: the rollback is reserved before
// any network I/O so a new push cannot race it.
func (m *Manager) RollbackDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if haEnabled(agent) || haBusy(agent) || jsonx.Truthy(taskOf(agent, deploymentID).Value("ha_publication")) {
		text := "主备发布不能单独回滚一台；请将目标配置重新发布到主备"
		return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "rollback_rejected", true, "rollback_message", text, "message", text), nil
	}
	value, err := m.UpdateAgents(ctx, "", func(ctx context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, agentID)
		if agent == nil {
			return agentNotFound404(), nil
		}
		state := taskOf(agent, deploymentID)
		if !jsonx.Truthy(state) {
			return notFoundDeployment(), nil
		}
		if haEnabled(agent) || haBusy(agent) {
			text := "请将目标配置重新发布到主备，不能单独回滚成员"
			return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "rollback_rejected", true, "rollback_message", text, "message", text), nil
		}
		if upgradeBusy(agent) {
			return jsonx.Obj("success", false, "message", "Agent update is in progress", "http_status", jsonx.Int(409)), nil
		}
		if dnsFollowBusy(*agents, agentID) {
			return jsonx.Obj("success", false, "message", "DNS 联动发布尚未确认结束，请等待结果后再回滚", "http_status", jsonx.Int(409)), nil
		}
		profile, err := m.profileOf(ctx, agent)
		if err != nil {
			return nil, err
		}
		latest, _ := agent.Value("latest_deployment").(*jsonx.Object)
		if !jsonx.Equal(state.Value("profile_id"), profile) ||
			!jsonx.Equal(state.GetOr("service_type", agent.Value("service_type")), agent.Value("service_type")) ||
			!jsonx.Equal(latest.Value("deployment_id"), deploymentID) {
			return jsonx.Obj("success", false, "message", "Only the latest deployment of this profile can be rolled back", "http_status", jsonx.Int(409)), nil
		}
		if jsonx.Truthy(state.Value("manual_rollback")) {
			return merged(state, "_already_reserved", true), nil
		}
		if state.Value("status") != "succeeded" {
			return jsonx.Obj("success", false, "message", "Only a successful deployment can be rolled back", "http_status", jsonx.Int(409)), nil
		}
		state.Set("status", "rolling_back")
		state.Set("manual_rollback", true)
		state.Set("success", true)
		state.Set("submitted", true)
		state.Set("events", AppendEvent(state.Value("events"), "rolling_back", "正在向 Agent 提交回滚请求", "", nil))
		for _, field := range []string{"http_status", "rollback_rejected", "rollback_message", "rollback_error", "rollback_error_details"} {
			state.Delete(field)
		}
		agent.Set("latest_deployment", state.Clone())
		return state.Clone(), nil
	})
	if err != nil {
		return nil, err
	}
	state := value.(*jsonx.Object)
	if jsonx.Truthy(state.Value("http_status")) {
		return state, nil
	}
	if reserved, _ := state.Pop("_already_reserved"); jsonx.Truthy(reserved) {
		return state, nil
	}
	agent, err = m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	unknown := func() (*jsonx.Object, error) {
		return m.RecordDeployment(ctx, agentID, deploymentID, jsonx.Obj("success", true, "status", "unknown", "manual_rollback", true,
			"message", "Rollback outcome unknown; awaiting Agent status"), RecordOptions{})
	}
	result, err := m.DeploymentRequest(ctx, agent, "POST", "deployments/"+deploymentID+"/rollback", nil, nil, nil)
	if IsTransportError(err) {
		return unknown()
	} else if err != nil {
		return nil, err
	}
	if httpStatusIn(result, 400, 404, 405, 409) {
		// Explicit rejection proves the Agent did not accept this action.
		if _, err := m.cancelManualRollback(ctx, agentID, deploymentID, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if jsonx.Truthy(result.Value("http_status")) {
		return unknown()
	}
	return m.RecordDeployment(ctx, agentID, deploymentID, result, RecordOptions{})
}

// RecoverDeployment is recover_deployment: ask the Agent to recover a
// deployment whose rollback failed (stop the core, restore the verified
// backups, start it again).
func (m *Manager) RecoverDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	previous := jsonx.NewObject()
	value, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		previous = jsonx.NewObject()
		agent, _ := findAgent(*agents, agentID)
		if agent == nil {
			return agentNotFound404(), nil
		}
		state := taskOf(agent, deploymentID)
		if !jsonx.Truthy(state) {
			return notFoundDeployment(), nil
		}
		if upgradeBusy(agent) {
			return jsonx.Obj("success", false, "message", "Agent update is in progress", "http_status", jsonx.Int(409)), nil
		}
		if state.Value("status") != "rollback_failed" {
			return jsonx.Obj("success", false, "message", "只有回滚失败的发布需要重新恢复", "http_status", jsonx.Int(409)), nil
		}
		for _, field := range []string{"rollback_error", "rollback_error_details"} {
			if value, ok := state.Get(field); ok {
				previous.Set(field, value)
			}
		}
		state.Set("status", "rolling_back")
		state.Set("success", false)
		state.Set("submitted", true)
		state.Set("events", AppendEvent(state.Value("events"), "rolling_back", "正在请求 Agent 重新恢复原配置", "", nil))
		for _, field := range []string{"http_status", "message", "rollback_error", "rollback_error_details"} {
			state.Delete(field)
		}
		latest, _ := agent.Value("latest_deployment").(*jsonx.Object)
		if jsonx.Equal(latest.Value("deployment_id"), deploymentID) {
			agent.Set("latest_deployment", state.Clone())
		}
		return state.Clone(), nil
	})
	if err != nil {
		return nil, err
	}
	state := value.(*jsonx.Object)
	if jsonx.Truthy(state.Value("http_status")) {
		return state, nil
	}
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	unknown := func() (*jsonx.Object, error) {
		return m.RecordDeployment(ctx, agentID, deploymentID, jsonx.Obj("success", false, "status", "unknown",
			"message", "Recovery outcome unknown; awaiting Agent status"), RecordOptions{})
	}
	result, err := m.DeploymentRequest(ctx, agent, "POST", "deployments/"+deploymentID+"/recover", nil, nil, nil)
	if IsTransportError(err) {
		return unknown()
	} else if err != nil {
		return nil, err
	}
	if httpStatusIn(result, 404, 405) {
		// Agents before 1.8.57-go have no recovery endpoint.
		service := agent.Value("service_type")
		if !jsonx.Truthy(service) {
			service = "oxidns"
		}
		config := "/opt/configflow-agent/config-" + jsonx.Str(service) + ".json"
		message := "此 Agent 版本不支持在 ConfigFlow 中重新恢复。请升级 Agent 到 1.8.57-go 或更新版本，" +
			"或在设备上依次执行：停止 configflow-agent 服务，运行 " +
			"/usr/local/bin/configflow-agent -config " + config + " -recover-and-exit，再启动 configflow-agent 服务。"
		return m.restoreRollbackFailed(ctx, agentID, deploymentID, message, result, previous)
	}
	if httpStatusIn(result, 400, 409) {
		message := jsonx.Str(truthyOr(result.Value("message"), "Agent 拒绝了恢复请求"))
		return m.restoreRollbackFailed(ctx, agentID, deploymentID, message, result, previous)
	}
	if jsonx.Truthy(result.Value("http_status")) {
		return unknown()
	}
	return m.RecordDeployment(ctx, agentID, deploymentID, jsonx.Obj("success", false, "status", "rolling_back"), RecordOptions{})
}

// restoreRollbackFailed is _restore_rollback_failed: a rejected recovery
// request changed nothing on the Agent.
func (m *Manager) restoreRollbackFailed(ctx context.Context, agentID, deploymentID, message string, rejection, previous *jsonx.Object) (*jsonx.Object, error) {
	value, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		current, _ := findAgent(*agents, agentID)
		task := taskOf(current, deploymentID)
		if task == nil {
			return nil, errors.New("KeyError: '" + deploymentID + "'")
		}
		if status := task.Value("status"); status == "rolling_back" || status == "unknown" {
			task.Update(previous)
			task.Set("status", "rollback_failed")
			task.Set("success", false)
			task.Set("message", FailureText(message, tokenSecrets(current), 0))
			task.Set("updated_at", nowISO())
			task.Set("events", AppendEvent(task.Value("events"), "rollback_failed", "恢复请求未执行："+message, "", tokenSecrets(current)))
			latest, _ := current.Value("latest_deployment").(*jsonx.Object)
			if jsonx.Equal(latest.Value("deployment_id"), deploymentID) {
				current.Set("latest_deployment", task.Clone())
			}
		}
		return merged(task, "http_status", rejection.GetOr("http_status", jsonx.Int(409))), nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}

// cancelManualRollback is _cancel_manual_rollback: a definite preflight
// rejection preserves the successful publication.
func (m *Manager) cancelManualRollback(ctx context.Context, agentID, deploymentID string, rejection *jsonx.Object) (*jsonx.Object, error) {
	value, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		current, _ := findAgent(*agents, agentID)
		task := taskOf(current, deploymentID)
		if task == nil {
			return nil, errors.New("KeyError: '" + deploymentID + "'")
		}
		if status := task.Value("status"); (status == "rolling_back" || status == "unknown") && jsonx.Truthy(task.Value("manual_rollback")) {
			task.Set("status", "succeeded")
			task.Set("success", true)
			task.Set("rollback_rejected", true)
			task.Set("rollback_message", FailureText(rejection.GetOr("message", "Rollback was rejected by Agent"), tokenSecrets(current), 0))
			task.Set("updated_at", nowISO())
			task.Set("events", AppendEvent(task.Value("events"), "succeeded", "回滚请求被拒绝，回滚未执行，原配置继续运行", "", tokenSecrets(current)))
			details := truthyOr(rejection.Value("rollback_error_details"), rejection.Value("error_details"))
			task.Delete("rollback_error")
			if jsonx.Truthy(details) {
				task.Set("rollback_error_details", FailureText(details, tokenSecrets(current), 0))
			} else {
				task.Delete("rollback_error_details")
			}
			for _, field := range []string{"manual_rollback", "http_status", "message"} {
				task.Delete(field)
			}
			latest, _ := current.Value("latest_deployment").(*jsonx.Object)
			if jsonx.Equal(latest.Value("deployment_id"), deploymentID) {
				current.Set("latest_deployment", task.Clone())
			}
		}
		return task.Clone(), nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}
