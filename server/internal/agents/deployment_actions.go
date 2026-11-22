package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
)

// deploymentEdit changes a few known journal members in a chain.
type deploymentEdit struct {
	state *model.Deployment
	err   error
}

func editDeployment(state *model.Deployment) *deploymentEdit { return &deploymentEdit{state: state} }
func (e *deploymentEdit) text(key, value string) *deploymentEdit {
	switch key {
	case "status":
		e.state.Status = value
	case "message":
		e.state.Message = new(value)
	case "error":
		e.state.Error = new(value)
	case "error_details":
		e.state.ErrorDetails = new(value)
	case "rollback_error":
		e.state.RollbackError = new(value)
	case "failed_stage":
		e.state.FailedStage = new(value)
	case "updated_at":
		e.state.UpdatedAt = new(value)
	case "rollback_message":
		e.state.RollbackMessage = new(value)
	case "rollback_error_details":
		e.state.RollbackErrorDetails = new(value)
	default:
		e.fail(key)
	}
	return e
}
func (e *deploymentEdit) flag(key string, value bool) *deploymentEdit {
	switch key {
	case "success":
		e.state.Success = new(value)
	case "pending":
		e.state.Pending = new(value)
	case "manual_rollback":
		e.state.ManualRollback = new(value)
	case "submitted":
		e.state.Submitted = new(value)
	case "rollback_rejected":
		e.state.RollbackRejected = new(value)
	default:
		e.fail(key)
	}
	return e
}
func (e *deploymentEdit) code(value int) *deploymentEdit {
	e.state.HTTPStatus = new(model.Int(value))
	return e
}
func (e *deploymentEdit) remove(keys ...string) *deploymentEdit {
	for _, key := range keys {
		switch key {
		case "manual_rollback":
			e.state.ManualRollback = nil
		case "rollback_rejected":
			e.state.RollbackRejected = nil
		case "pending":
			e.state.Pending = nil
		case "rollback_message":
			e.state.RollbackMessage = nil
		case "message", "error", "error_details", "failed_stage", "rollback_error", "rollback_error_details", "http_status":
			clearDeploymentMember(e.state, key)
		default:
			e.fail(key)
		}
	}
	return e
}
func (e *deploymentEdit) fail(key string) {
	if e.err == nil {
		e.err = fmt.Errorf("edit of an unexpected deployment member %q", key)
	}
}
func (e *deploymentEdit) result() (*model.Deployment, error) { return e.state, e.err }

func deploymentOf(a *model.Agent, id string) *model.Deployment {
	if a == nil {
		return nil
	}
	task, present := a.Deployments.Get(id)
	if !present {
		return nil
	}
	return &task
}
func deploymentHasHTTPFailure(d *model.Deployment) bool {
	return d != nil && d.HTTPStatus != nil && *d.HTTPStatus != 0
}
func deploymentHTTPIn(d *model.Deployment, codes ...int) bool {
	if d == nil || d.HTTPStatus == nil {
		return false
	}
	for _, code := range codes {
		if int64(*d.HTTPStatus) == int64(code) {
			return true
		}
	}
	return false
}

// agentService is the Agent's service type; a missing one is mihomo.
func agentService(a *model.Agent) string {
	if a.ServiceType == "" {
		return "mihomo"
	}
	return a.ServiceType
}
func deploymentServiceMatches(a *model.Agent, d *model.Deployment, optional bool) bool {
	if a == nil {
		return false
	}
	if optional && d.ServiceType == "" {
		return true
	}
	return agentService(a) == d.ServiceType
}
func deploymentTaskIDMatches(d *model.Deployment, id string) bool {
	return d != nil && d.DeploymentID != "" && d.DeploymentID == id
}
func (m *Manager) deploymentProfileMatches(ctx context.Context, a *model.Agent, d *model.Deployment) (bool, error) {
	profile, err := m.agentProfileID(ctx, a)
	if err != nil {
		return false, err
	}
	return d.ProfileID != "" && d.ProfileID == profile, nil
}
func saveDeploymentTask(a *model.Agent, id string, state *model.Deployment) error {
	a.Deployments.Set(id, *state)
	if deploymentTaskIDMatches(a.LatestDeployment, id) && deploymentTaskIDMatches(state, id) {
		latest, err := copyDeployment(state)
		if err != nil {
			return err
		}
		a.LatestDeployment = latest
	}
	return nil
}
func pendingDeployment(state *model.Deployment, message string) (*model.Deployment, error) {
	return editDeployment(state).text("message", message).flag("pending", true).result()
}

type deploymentOutcome struct {
	Success        bool   `json:"success"`
	Status         string `json:"status"`
	Message        string `json:"message,omitempty"`
	ManualRollback *bool  `json:"manual_rollback,omitempty"`
}

func (m *Manager) recordDeploymentOutcome(ctx context.Context, agentID, id string, outcome deploymentOutcome, options RecordOptions) (*model.Deployment, error) {
	raw, err := json.Marshal(outcome)
	if err != nil {
		return nil, err
	}
	patch, err := DecodeDeploymentUpdate(raw)
	if err != nil {
		return nil, err
	}
	return m.RecordDeploymentModel(ctx, agentID, id, patch, options)
}

// ReadDeploymentModel reads and reconciles one profile-bound remote journal.
// Ambiguous upload/rollback outcomes remain pending until the Agent confirms them.
func (m *Manager) ReadDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return deploymentFailure(msgAgentNotFound, 404)
	}
	state := deploymentOf(a, id)
	if state == nil {
		return deploymentFailure(msgDeploymentNotFound, 404)
	}
	matches, err := m.deploymentProfileMatches(ctx, a, state)
	if err != nil {
		return nil, err
	}
	if !matches {
		return deploymentFailure(msgDeploymentOtherProfile, 409)
	}
	if !deploymentTaskIDMatches(state, id) {
		return deploymentFailure(msgDeploymentIdentity, 409)
	}
	if deploymentTerminalModel(state.Status) {
		return state, nil
	}
	if !deploymentServiceMatches(a, state, false) {
		return editDeployment(state).flag("success", false).code(409).text("message", msgDeploymentOtherService).result()
	}
	if preparationUnsubmitted(state) {
		running, err := m.preparationRunning(agentID, id)
		if err != nil || running {
			return state, err
		}
		a, err = m.ReadAgent(ctx, agentID, true)
		if err != nil {
			return nil, err
		}
		if a == nil {
			return deploymentFailure(msgAgentNotFound, 404)
		}
		if again := deploymentOf(a, id); again != nil {
			state = again
		}
		if deploymentTerminalModel(state.Status) {
			return state, nil
		}
		if preparationUnsubmitted(state) {
			running, err = m.preparationRunning(agentID, id)
			if err != nil || running {
				return state, err
			}
			return m.recordDeploymentOutcome(ctx, agentID, id, deploymentOutcome{Success: false, Status: "failed", Message: msgDeploymentPreparingLost}, RecordOptions{OnlyUnsubmitted: true})
		}
	}
	uploadInFlight, err := m.preparationRunning(agentID, id)
	if err != nil {
		return nil, err
	}
	pending := func() (*model.Deployment, error) { return pendingDeployment(state, msgDeploymentPending) }
	result, err := m.DeploymentRequestModel(ctx, a, http.MethodGet, "deployments/"+id, nil, nil, nil)
	if IsTransportError(err) {
		return pending()
	}
	if err != nil {
		return nil, err
	}
	if flagSet(state.ManualRollback) && result.Status == "succeeded" && !flagSet(result.ManualRollback) {
		if state.Status != "unknown" && rollbackKeys.Held([3]string{m.repository.DataDir(), agentID, id}) {
			return pendingDeployment(state, msgRollbackAwaiting)
		}
		result, err = m.DeploymentRequestModel(ctx, a, http.MethodPost, "deployments/"+id+"/rollback", nil, nil, nil)
		if IsTransportError(err) {
			return pending()
		}
		if err != nil {
			return nil, err
		}
		busy := deploymentHTTPIn(&result.Deployment, 409) && textOf(result.Message) == busyMessage
		if deploymentHTTPIn(&result.Deployment, 400, 404, 405, 409) && !busy {
			return m.cancelManualRollbackModel(ctx, agentID, id, result)
		}
	}
	if deploymentHTTPIn(&result.Deployment, 404) {
		if flagSet(state.ManualRollback) {
			return pendingDeployment(state, msgRollbackJournalMissing)
		}
		if !uploadInFlight {
			uploadInFlight, err = m.preparationRunning(agentID, id)
			if err != nil {
				return nil, err
			}
		}
		if uploadInFlight {
			return pendingDeployment(state, msgDeploymentReceiving)
		}
		return m.recordDeploymentOutcome(ctx, agentID, id, deploymentOutcome{Success: false, Status: "failed", Message: msgDeploymentRemoteMissing}, RecordOptions{})
	}
	if deploymentHasHTTPFailure(&result.Deployment) {
		return pending()
	}
	if raw, _ := result.MemberJSON("status"); !jsonText(raw) {
		if nonscalarStatus(raw) {
			return nil, ErrInvalidDeploymentStatus
		}
		return pending()
	}
	return m.recordDeploymentReply(ctx, agentID, id, result, RecordOptions{SkipUnchanged: true})
}

// ActivateCandidateModel activates a ready candidate only after binding and
// server validation evidence have been checked against the original identities.
func (m *Manager) ActivateCandidateModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.ReadDeploymentModel(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if deploymentHasHTTPFailure(state) {
		return state, nil
	}
	if a == nil {
		return deploymentFailure(msgAgentNotFound, 404)
	}
	matches, err := m.deploymentProfileMatches(ctx, a, state)
	if err != nil {
		return nil, err
	}
	if !matches || !deploymentServiceMatches(a, state, false) || !deploymentTaskIDMatches(state, id) {
		return editDeployment(state).flag("success", false).code(409).text("message", msgDeploymentOwnerChanged).result()
	}
	service := agentService(a)
	status := state.Status
	if service == "oxidns" {
		if status == "succeeded" {
			return state, nil
		}
		if status == "ready" {
			if flagSet(state.Pending) {
				return editDeployment(state).flag("success", false).code(409).text("message", msgCandidatePending).result()
			}
			checked, err := m.candidateReceiptMatches(a, state)
			if err != nil {
				return nil, err
			}
			if !checked {
				return m.failUncheckedCandidate(ctx, agentID, id, state)
			}
		}
	}
	if status != "ready" {
		return editDeployment(state).flag("success", false).text("message", msgDeploymentNotReady).code(409).result()
	}
	if service == "oxidns" {
		if err := m.checkpointCandidate(ctx, agentID, id, state.PreparedContent); err != nil {
			return nil, err
		}
	}
	result, err := m.DeploymentRequestModel(ctx, a, http.MethodPost, "deployments/"+id+"/activate", nil, nil, nil)
	if IsTransportError(err) {
		return m.recordDeploymentOutcome(ctx, agentID, id, deploymentOutcome{Success: true, Status: "unknown", Message: msgActivationAwaiting}, RecordOptions{ActivationReceipt: true})
	}
	if err != nil {
		return nil, err
	}
	if deploymentHasHTTPFailure(&result.Deployment) {
		return &result.Deployment, nil
	}
	return m.recordDeploymentReply(ctx, agentID, id, result, RecordOptions{ActivationReceipt: true})
}

func (m *Manager) failUncheckedCandidate(ctx context.Context, agentID, id string, observed *model.Deployment) (*model.Deployment, error) {
	var result *model.Deployment
	err := m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, agentID)
		task := deploymentOf(a, id)
		if task == nil {
			var err error
			result, err = deploymentFailure(msgDeploymentNotFound, 404)
			return err
		}
		if task.Status != "ready" || !sameText(task.SHA256, observed.SHA256) {
			var err error
			result, err = editDeployment(task).flag("success", false).code(409).text("message", msgCandidateChanged).result()
			return err
		}
		edit := editDeployment(task).flag("success", false).text("status", "failed").text("failed_stage", "verifying").code(409).text("message", MessageUncheckedCandidate).text("updated_at", nowISO())
		if edit.err != nil {
			return edit.err
		}
		task.Events = AppendOperationEvent(task.Events, "failed", MessageUncheckedCandidate, "", agentDiagnosticSecrets(a))
		if err := saveDeploymentTask(a, id, task); err != nil {
			return err
		}
		var err error
		result, err = copyDeployment(task)
		return err
	})
	return result, err
}

// RollbackDeploymentModel reserves a manual rollback durably before any I/O.
// A live submitting owner and a lost submission remain distinct during polls.
func (m *Manager) RollbackDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if HAEnabledModel(a) || HAActiveModel(a) || hasHAPublication(deploymentOf(a, id)) {
		return rollbackRejection(msgRollbackPair)
	}
	if _, release, owned := rollbackKeys.TryLock(ctx, [3]string{m.repository.DataDir(), agentID, id}); owned {
		defer release()
	}
	var state *model.Deployment
	reserved := false
	err = m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, agentID)
		fail := func(message string, code int) error {
			var err error
			state, err = deploymentFailure(message, code)
			return err
		}
		if a == nil {
			return fail(msgAgentNotFound, 404)
		}
		task := deploymentOf(a, id)
		if task == nil {
			return fail(msgDeploymentNotFound, 404)
		}
		if HAEnabledModel(a) || HAActiveModel(a) {
			var err error
			state, err = rollbackRejection(msgRollbackMember)
			return err
		}
		if upgradeBusyModel(a) {
			return fail(msgDeploymentUpgradeBusy, 409)
		}
		if dnsFollowBusyModel(*records, agentID) {
			return fail(msgRollbackFollow, 409)
		}
		matches, err := m.deploymentProfileMatches(tx, a, task)
		if err != nil {
			return err
		}
		if !matches || !deploymentServiceMatches(a, task, true) || !deploymentTaskIDMatches(a.LatestDeployment, id) || !deploymentTaskIDMatches(task, id) {
			return fail(msgRollbackLatest, 409)
		}
		if flagSet(task.ManualRollback) {
			reserved = true
			state, err = copyDeployment(task)
			return err
		}
		if task.Status != "succeeded" {
			return fail(msgRollbackSuccessful, 409)
		}
		edit := editDeployment(task).text("status", "rolling_back").flag("manual_rollback", true).flag("success", true).flag("submitted", true)
		task.Events = AppendOperationEvent(task.Events, "rolling_back", msgRollbackSubmitting, "", nil)
		edit.remove("http_status", "rollback_rejected", "rollback_message", "rollback_error", "rollback_error_details")
		if edit.err != nil {
			return edit.err
		}
		if err := saveDeploymentTask(a, id, task); err != nil {
			return err
		}
		state, err = copyDeployment(task)
		return err
	})
	if err != nil {
		return nil, err
	}
	if deploymentHasHTTPFailure(state) || reserved {
		return state, nil
	}
	a, err = m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	unknown := func() (*model.Deployment, error) {
		return m.recordDeploymentOutcome(ctx, agentID, id, deploymentOutcome{Success: true, Status: "unknown", Message: msgRollbackUnknown, ManualRollback: new(true)}, RecordOptions{})
	}
	result, err := m.DeploymentRequestModel(ctx, a, http.MethodPost, "deployments/"+id+"/rollback", nil, nil, nil)
	if IsTransportError(err) {
		return unknown()
	}
	if err != nil {
		return nil, err
	}
	if deploymentHTTPIn(&result.Deployment, 400, 404, 405, 409) {
		if _, err := m.cancelManualRollbackModel(ctx, agentID, id, result); err != nil {
			return nil, err
		}
		return &result.Deployment, nil
	}
	if deploymentHasHTTPFailure(&result.Deployment) {
		return unknown()
	}
	return m.recordDeploymentReply(ctx, agentID, id, result, RecordOptions{})
}
func hasHAPublication(task *model.Deployment) bool {
	if task == nil {
		return false
	}
	return task.HAPublication.HasMembers()
}
func rollbackRejection(message string) (*model.Deployment, error) {
	state, err := deploymentFailure(message, 409)
	if err != nil {
		return nil, err
	}
	return editDeployment(state).flag("rollback_rejected", true).text("rollback_message", message).result()
}

// RecoverDeploymentModel reserves recovery of a failed rollback and preserves
// prior diagnostics when the Agent explicitly rejects the request.
func (m *Manager) RecoverDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var state, previous *model.Deployment
	err := m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, agentID)
		fail := func(message string, code int) error {
			var err error
			state, err = deploymentFailure(message, code)
			return err
		}
		if a == nil {
			return fail(msgAgentNotFound, 404)
		}
		task := deploymentOf(a, id)
		if task == nil {
			return fail(msgDeploymentNotFound, 404)
		}
		if upgradeBusyModel(a) {
			return fail(msgDeploymentUpgradeBusy, 409)
		}
		if !deploymentTaskIDMatches(task, id) {
			return fail(msgDeploymentIdentity, 409)
		}
		if task.Status != "rollback_failed" {
			return fail(msgRecoveryRequired, 409)
		}
		var err error
		previous, err = copyDeployment(task)
		if err != nil {
			return err
		}
		edit := editDeployment(task).text("status", "rolling_back").flag("success", false).flag("submitted", true)
		task.Events = AppendOperationEvent(task.Events, "rolling_back", msgRecoverySubmitting, "", nil)
		edit.remove("http_status", "message", "rollback_error", "rollback_error_details")
		if edit.err != nil {
			return edit.err
		}
		if err := saveDeploymentTask(a, id, task); err != nil {
			return err
		}
		state, err = copyDeployment(task)
		return err
	})
	if err != nil {
		return nil, err
	}
	if deploymentHasHTTPFailure(state) {
		return state, nil
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	unknown := func() (*model.Deployment, error) {
		return m.recordDeploymentOutcome(ctx, agentID, id, deploymentOutcome{Success: false, Status: "unknown", Message: msgRecoveryUnknown}, RecordOptions{})
	}
	result, err := m.DeploymentRequestModel(ctx, a, http.MethodPost, "deployments/"+id+"/recover", nil, nil, nil)
	if IsTransportError(err) {
		return unknown()
	}
	if err != nil {
		return nil, err
	}
	if deploymentHTTPIn(&result.Deployment, 404, 405) {
		service := agentService(a)
		return m.restoreRollbackFailedModel(ctx, agentID, id, fmt.Sprintf(msgRecoveryUnsupported, service), &result.Deployment, previous)
	}
	if deploymentHTTPIn(&result.Deployment, 400, 409) {
		message := textOf(result.Message)
		if message == "" {
			message = msgRecoveryRejected
		}
		return m.restoreRollbackFailedModel(ctx, agentID, id, message, &result.Deployment, previous)
	}
	if deploymentHasHTTPFailure(&result.Deployment) {
		return unknown()
	}
	return m.recordDeploymentOutcome(ctx, agentID, id, deploymentOutcome{Success: false, Status: "rolling_back", Message: ""}, RecordOptions{})
}

func (m *Manager) restoreRollbackFailedModel(ctx context.Context, agentID, id, message string, rejection, previous *model.Deployment) (*model.Deployment, error) {
	var result *model.Deployment
	err := m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, agentID)
		task := deploymentOf(a, id)
		if task == nil {
			var err error
			result, err = deploymentFailure(msgDeploymentNotFound, 404)
			return err
		}
		status := task.Status
		if status == "rolling_back" || status == "unknown" {
			edit := editDeployment(task)
			if previous.RollbackError != nil {
				task.RollbackError = new(*previous.RollbackError)
			}
			if previous.RollbackErrorDetails != nil {
				task.RollbackErrorDetails = new(*previous.RollbackErrorDetails)
			}
			safe := redact.DiagnosticText(message, agentDiagnosticSecrets(a), 0)
			edit.text("status", "rollback_failed").flag("success", false).text("message", safe).text("updated_at", nowISO())
			if edit.err != nil {
				return edit.err
			}
			task.Events = AppendOperationEvent(task.Events, "rollback_failed", msgRecoveryNotRun+message, "", agentDiagnosticSecrets(a))
			if err := saveDeploymentTask(a, id, task); err != nil {
				return err
			}
		}
		code := 409
		if rejection.HTTPStatus != nil {
			code = int(*rejection.HTTPStatus)
		}
		var err error
		result, err = copyDeployment(task)
		if err != nil {
			return err
		}
		_, err = editDeployment(result).code(code).result()
		return err
	})
	return result, err
}
func (m *Manager) cancelManualRollbackModel(ctx context.Context, agentID, id string, rejection *DeploymentReply) (*model.Deployment, error) {
	var result *model.Deployment
	err := m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, agentID)
		task := deploymentOf(a, id)
		if task == nil {
			var err error
			result, err = deploymentFailure(msgDeploymentNotFound, 404)
			return err
		}
		status := task.Status
		if (status == "rolling_back" || status == "unknown") && flagSet(task.ManualRollback) {
			message := textOf(rejection.Message)
			if _, present := rejection.MemberJSON("message"); !present {
				message = msgRollbackRejected
			}
			edit := editDeployment(task).text("status", "succeeded").flag("success", true).flag("rollback_rejected", true).text("rollback_message", redact.DiagnosticText(message, agentDiagnosticSecrets(a), 0)).text("updated_at", nowISO())
			task.Events = AppendOperationEvent(task.Events, "succeeded", msgRollbackNotRun, "", agentDiagnosticSecrets(a))
			details := textOf(rejection.RollbackErrorDetails)
			if details == "" {
				details = textOf(rejection.ErrorDetails)
			}
			edit.remove("rollback_error")
			if details != "" {
				edit.text("rollback_error_details", redact.DiagnosticText(details, agentDiagnosticSecrets(a), 0))
			} else {
				edit.remove("rollback_error_details")
			}
			edit.remove("manual_rollback", "http_status", "message")
			if edit.err != nil {
				return edit.err
			}
			if err := saveDeploymentTask(a, id, task); err != nil {
				return err
			}
		}
		var err error
		result, err = copyDeployment(task)
		return err
	})
	return result, err
}

// jsonText reports a JSON string value.
func jsonText(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"'
}
