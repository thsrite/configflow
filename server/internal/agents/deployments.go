package agents

// The deployment bookkeeping of AgentManager: persistent task ownership,
// result recording, status polling, activation, manual rollback and
// recovery. Preparing and uploading bundles (_publish_deployment,
// _remember_deployment_content) belongs to #24/#26 and reaches the registry
// through the PublishDeployment hook.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
)

var deploymentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}\z`)

const busyMessage = "another service operation is in progress"

// rollbackKeys marks manual rollback submissions owned by this process, from
// before the reservation is written until the Agent's answer is handled. A
// rolling_back reservation without a live owner (process exit, or shutdown
// cancelling the request before the Agent read it) is as ambiguous as an
// observed unknown outcome, so a poll reissues the idempotent rollback instead
// of waiting forever (A14 review M1).
var rollbackKeys = lockx.NewKeyed[[3]string](lockx.LevelJob, "agent-manual-rollback")

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
	result, err := m.ReserveDeployment(ctx, agentID, deploymentID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	return &record, nil
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

// RecordDeployment translates a legacy remote patch to the native journal owner.
func (m *Manager) RecordDeployment(ctx context.Context, agentID, deploymentID string, updates *jsonx.Object, options RecordOptions) (*jsonx.Object, error) {
	selected := jsonx.NewObject()
	for _, key := range append(append([]string(nil), deploymentUpdateFields...), "updated_at", "dns_checks", "dns_checked_at") {
		if value, present := updates.Get(key); present {
			selected.Set(key, value)
		}
	}
	raw, err := json.Marshal(selected)
	if err != nil {
		return nil, err
	}
	patch, err := DecodeDeploymentUpdate(raw)
	if err != nil {
		return nil, err
	}
	result, err := m.RecordDeploymentModel(ctx, agentID, deploymentID, patch, options)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *Manager) publicationComplete(ctx context.Context, agentID, deploymentID string, status any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if hook := currentHooks().PublicationComplete; hook != nil {
		return hook(ctx, m, agentID, deploymentID, status)
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		delay := time.Duration(min(0.1, remaining) * float64(time.Second))
		if m.sleep != nil {
			m.sleep(delay)
		} else {
			timer := lifecycle.ClockFrom(ctx).NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C():
			}
			timer.Stop()
		}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hook := currentHooks().HADeployment; hook != nil {
		return hook(ctx, m, agentID, deploymentID)
	}
	return defaultHADeployment(ctx, m, agentID, deploymentID)
}

// GetDeploymentDirect is _get_deployment (for #29).
func (m *Manager) GetDeploymentDirect(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		if state.Value("status") != "unknown" && rollbackKeys.Held([3]string{m.repository.DataDir(), agentID, deploymentID}) {
			return merged(state, "message", "Rollback submission awaiting Agent confirmation", "pending", true), nil
		}
		// A lost POST response, or a reservation whose submitting owner is gone,
		// is ambiguous. Reissuing the same guarded, idempotent rollback is safe;
		// a publication status alone is not.
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
	if jsonx.Truthy(result.Value("http_status")) {
		return pending(), nil
	}
	if _, isString := result.Value("status").(string); !isString {
		if value, present := result.Get("status"); present {
			raw, err := json.Marshal(value)
			if err != nil || nonscalarStatus(raw) {
				return nil, ErrInvalidDeploymentStatus
			}
		}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hook := currentHooks().HAActivate; hook != nil {
		return hook(ctx, m, agentID, deploymentID)
	}
	return defaultHAActivate(ctx, m, agentID, deploymentID)
}

// ActivateDeploymentDirect is _activate_deployment (for #29).
func (m *Manager) ActivateDeploymentDirect(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
			if err := ctx.Err(); err != nil {
				return nil, err
			}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if haEnabled(agent) || haBusy(agent) || jsonx.Truthy(taskOf(agent, deploymentID).Value("ha_publication")) {
		text := "主备发布不能单独回滚一台；请将目标配置重新发布到主备"
		return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "rollback_rejected", true, "rollback_message", text, "message", text), nil
	}
	// Another submission of this rollback in this process keeps its own mark;
	// this call then only reads the existing reservation.
	if _, release, owned := rollbackKeys.TryLock(ctx, [3]string{m.repository.DataDir(), agentID, deploymentID}); owned {
		defer release()
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
