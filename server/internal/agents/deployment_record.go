package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// DeviceResponseError rejects an invalid upstream device reply. The HTTP
// boundary reports INVALID_RESPONSE with a Chinese reason and status 502.
type DeviceResponseError struct{ Code, Message string }

// Error returns the bounded public reason for the upstream refusal.
func (e *DeviceResponseError) Error() string { return e.Message }

// ErrInvalidDeploymentStatus rejects nonscalar remote journal status values.
var ErrInvalidDeploymentStatus = &DeviceResponseError{Code: "INVALID_RESPONSE", Message: msgDeploymentStatusShape}

var deploymentUpdateFields = []string{
	"status", "message", "error", "error_details", "failed_stage", "rollback_error", "rollback_error_details", "config_version",
	"config_revision", "success", "sha256", "submitted", "previous_running", "health", "http_status", "manual_rollback", "source_sha256",
}

// DeploymentUpdate is a presence-sensitive remote journal patch. Server-owned
// identity, prepared contents and validation receipts cannot enter this DTO.
type DeploymentUpdate struct {
	record  model.Deployment
	present map[string]bool
}

// DecodeDeploymentUpdate selects exact remote protocol keys through the shared
// model codec. Original status tokens remain available for structural checks.
func DecodeDeploymentUpdate(data json.RawMessage) (DeploymentUpdate, error) {
	var members map[string]json.RawMessage
	result := DeploymentUpdate{present: make(map[string]bool)}
	if err := json.Unmarshal(data, &members); err != nil {
		return result, err
	}
	for _, key := range append(append([]string(nil), deploymentUpdateFields...), "updated_at", "dns_checks", "dns_checked_at") {
		if raw, present := members[key]; present {
			if err := result.record.SetJSON(key, raw); err != nil {
				return DeploymentUpdate{}, err
			}
			result.present[key] = true
		}
	}
	return result, nil
}

func deploymentFlagTrue(state *model.Deployment, name string) bool {
	raw, present, err := state.MemberJSON(name)
	var flag *bool
	return err == nil && present && json.Unmarshal(raw, &flag) == nil && flag != nil && *flag
}

func inactiveDeploymentFlag(state *model.Deployment, name string) bool {
	raw, present, err := state.MemberJSON(name)
	if err != nil {
		return false
	}
	if storedAbsent(raw, present, nil) {
		return true
	}
	var flag *bool
	return json.Unmarshal(raw, &flag) == nil && flag != nil && !*flag
}

func noDeploymentHTTPFailure(state *model.Deployment) bool {
	raw, present, err := state.MemberJSON("http_status")
	if err != nil {
		return false
	}
	if storedAbsent(raw, present, nil) {
		return true
	}
	var number *float64
	var code model.Int
	return json.Unmarshal(raw, &number) == nil && number != nil && json.Unmarshal(raw, &code) == nil && code == 0
}

func receiptTerminalModel(status string) bool {
	return deploymentTerminalModel(status) || status == "rollback_failed"
}

func agentDiagnosticSecrets(a *model.Agent) redact.Secrets {
	if token, valid := storedText(a.MemberJSON("token")); valid {
		return redact.NewSecrets([]string{token})
	}
	return nil
}

func originalDeploymentText(state *model.Deployment, key string) string {
	text, _ := storedText(state.MemberJSON(key))
	return text
}

func nonscalarStatus(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && (raw[0] == '[' || raw[0] == '{')
}

// applyDeploymentUpdate is shared by the durable transaction and the read-only
// no-op preview. It never emits logs, notifications, metrics or device requests.
func (m *Manager) applyDeploymentUpdate(ctx context.Context, a *model.Agent, id string, state *model.Deployment, patch DeploymentUpdate, options RecordOptions) (bool, error) {
	if options.OnlyUnsubmitted && !preparationUnsubmitted(state) {
		return false, nil
	}
	before, beforePresent, err := state.MemberJSON("status")
	if err != nil {
		return false, err
	}
	status := originalDeploymentText(state, "status")
	incoming := originalDeploymentText(&patch.record, "status")
	if options.UploadReceipt && !receiptTerminalModel(incoming) && status != "preparing" && status != "uploading" && status != "receiving" {
		return false, nil
	}
	if options.ActivationReceipt && !receiptTerminalModel(incoming) && status != "ready" && status != "unknown" {
		return false, nil
	}
	if !inactiveDeploymentFlag(state, "manual_rollback") && !deploymentFlagTrue(&patch.record, "manual_rollback") {
		return false, nil
	}
	if patch.present["status"] {
		raw, _, err := patch.record.MemberJSON("status")
		if err != nil {
			return false, err
		}
		if deploymentTerminalModel(status) && !sameStoredJSON(before, beforePresent, raw) {
			return false, nil
		}
		if nonscalarStatus(raw) {
			return false, ErrInvalidDeploymentStatus
		}
	}
	reportedAt, hasReported := NormalizeEventTime(originalDeploymentText(&patch.record, "updated_at"))
	if hasReported {
		previous := originalDeploymentText(state, "agent_updated_at")
		if previous != "" && reportedAt < previous {
			return false, nil
		}
	}
	if incoming != "" && incoming != "unknown" && noDeploymentHTTPFailure(&patch.record) {
		if status == "unknown" {
			for _, key := range []string{"error_details", "rollback_error_details"} {
				if !patch.present[key] {
					if err := state.Clear(key); err != nil {
						return false, err
					}
				}
			}
		}
		if err := state.Clear("http_status"); err != nil {
			return false, err
		}
		if !patch.present["message"] {
			if err := state.Clear("message"); err != nil {
				return false, err
			}
		}
	}
	for _, pair := range [][2]string{{"error", "error_details"}, {"rollback_error", "rollback_error_details"}} {
		if patch.present[pair[0]] && !patch.present[pair[1]] {
			raw, _, err := patch.record.MemberJSON(pair[0])
			if err != nil {
				return false, err
			}
			old, present, err := state.MemberJSON(pair[0])
			if err != nil {
				return false, err
			}
			if !sameStoredJSON(old, present, raw) {
				if err := state.Clear(pair[1]); err != nil {
					return false, err
				}
			}
		}
	}
	secrets := agentDiagnosticSecrets(a)
	for _, key := range deploymentUpdateFields {
		if !patch.present[key] {
			continue
		}
		raw, _, err := patch.record.MemberJSON(key)
		if err != nil {
			return false, err
		}
		switch key {
		case "message", "error", "error_details", "rollback_error", "rollback_error_details":
			text, _ := storedText(raw, true, nil)
			raw, err = json.Marshal(redact.DiagnosticText(text, secrets, 0))
			if err != nil {
				return false, err
			}
		}
		if err := state.SetJSON(key, raw); err != nil {
			return false, err
		}
	}
	report, err := SanitizeDNSReport(&patch.record, secrets)
	if err != nil {
		return false, err
	}
	if err := applyDNSReport(state, report); err != nil {
		return false, err
	}
	if incoming == "ready" || incoming == "succeeded" {
		for _, key := range []string{"error", "error_details", "failed_stage", "rollback_error", "rollback_error_details"} {
			if !patch.present[key] {
				if err := state.Clear(key); err != nil {
					return false, err
				}
			}
		}
	}
	if incoming == "rolled_back" && originalDeploymentText(&patch.record, "rollback_error") == "" {
		for _, key := range []string{"rollback_error", "rollback_error_details"} {
			if err := state.Clear(key); err != nil {
				return false, err
			}
		}
	}
	state.UpdatedAt = new(nowISO())
	if hasReported {
		state.AgentUpdatedAt = new(reportedAt)
	}
	message := originalDeploymentText(state, "message")
	if message == "" {
		message = originalDeploymentText(state, "error")
	}
	current := originalDeploymentText(state, "status")
	state.Events = AppendOperationEvent(state.Events, current, message, "", secrets)
	stateID, stateIDOK := storedText(state.MemberJSON("deployment_id"))
	latestID, latestIDOK := "", false
	if a.LatestDeployment != nil {
		latestID, latestIDOK = storedText(a.LatestDeployment.MemberJSON("deployment_id"))
	}
	isLatest := stateIDOK && latestIDOK && stateID == id && latestID == id
	if current == "succeeded" && isLatest {
		profile, profileErr := m.agentProfileID(ctx, a)
		if profileErr != nil && !errors.Is(profileErr, repo.ErrValidation) {
			return false, profileErr
		}
		owner, ownerOK := storedText(state.MemberJSON("profile_id"))
		service, serviceOK := storedText(a.MemberJSON("service_type"))
		stateService, stateServiceOK := storedText(state.MemberJSON("service_type"))
		if _, present, err := state.MemberJSON("service_type"); err != nil {
			return false, err
		} else if !present {
			stateService, stateServiceOK = service, serviceOK
		}
		if profileErr == nil && ownerOK && owner == profile && serviceOK && stateServiceOK && service == stateService {
			version, present, err := state.MemberJSON("config_version")
			if err != nil {
				return false, err
			}
			if !present {
				version, present, err = a.MemberJSON("config_version")
				if err != nil {
					return false, err
				}
			}
			if !present {
				version = json.RawMessage(`"0"`)
			}
			if err := a.SetJSON("config_version", version); err != nil {
				return false, err
			}
			a.PushedAt = new(utcNowZ())
			if revision, present, err := state.MemberJSON("config_revision"); err != nil {
				return false, err
			} else if present && !storedAbsent(revision, true, nil) {
				if err := a.SetJSON("config_revision", revision); err != nil {
					return false, err
				}
			}
			if state.PreparedContent != nil {
				raw, err := model.Marshal(state.PreparedContent)
				if err != nil {
					return false, err
				}
				var applied model.AppliedContent
				if err := json.Unmarshal(raw, &applied); err != nil {
					return false, err
				}
				if err := applied.SetJSON("config_version", version); err != nil {
					return false, err
				}
				applied.DeploymentID = id
				a.AppliedContent = &applied
			} else if err := a.Clear("applied_content"); err != nil {
				return false, err
			}
		}
	}
	if deploymentFlagTrue(state, "manual_rollback") && current == "rolled_back" && isLatest {
		version, valid := storedText(state.MemberJSON("config_version"))
		if !valid || version == "" {
			version = "0"
		}
		raw, err := json.Marshal(version)
		if err != nil {
			return false, err
		}
		if err := a.SetJSON("config_version", raw); err != nil {
			return false, err
		}
		for _, key := range []string{"applied_content", "config_revision", "pushed_at"} {
			if err := a.Clear(key); err != nil {
				return false, err
			}
		}
	}
	if isLatest {
		a.LatestDeployment, err = copyDeployment(state)
		if err != nil {
			return false, err
		}
	}
	after, present, err := state.MemberJSON("status")
	if err != nil {
		return false, err
	}
	_, known := deploymentResults[current]
	return known && !sameStoredJSON(before, beforePresent, after) && present, nil
}

func (m *Manager) completeTypedPublication(ctx context.Context, agentID, id string, state *model.Deployment) error {
	if originalDeploymentText(state, "service_type") != "oxidns" {
		return nil
	}
	return m.publicationComplete(ctx, agentID, id, originalDeploymentText(state, "status"))
}

// RecordDeploymentModel applies only remote-observable fields in one typed
// transaction. A read-only no-op preview copies one journal and only the Agent
// metadata this update can change; it never decodes the configuration document.
func (m *Manager) RecordDeploymentModel(ctx context.Context, agentID, id string, patch DeploymentUpdate, options RecordOptions) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.SkipUnchanged {
		if unchanged := m.previewUnchangedDeployment(ctx, agentID, id, patch, options); unchanged != nil {
			if err := m.completeTypedPublication(ctx, agentID, id, unchanged); err != nil {
				return nil, err
			}
			return unchanged, nil
		}
	}
	var result *model.Deployment
	finished, label := false, ""
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, agentID)
		if a == nil {
			var err error
			result, err = deploymentFailure(msgAgentNotFound, 0)
			return err
		}
		state, present := a.Deployments.Get(id)
		if !present || !a.Deployments.EntryValid(id) {
			var err error
			result, err = deploymentFailure(msgDeploymentNotFound, 404)
			return err
		}
		var err error
		finished, err = m.applyDeploymentUpdate(tx, a, id, &state, patch, options)
		if err != nil {
			return err
		}
		a.Deployments.Set(id, state)
		result, err = copyDeployment(&state)
		if err != nil {
			return err
		}
		if finished {
			name := a.Name
			if name == "" {
				name = a.ID
			}
			label = registryDiagnostic(a, name, 120)
		}
		return tx.Err()
	})
	if err != nil {
		return nil, err
	}
	current, err := m.ReadAgent(ctx, agentID, false)
	if err != nil {
		return nil, err
	}
	notifications.Safely(func() error { return notifications.SaveAgent(ctx, current, m.repository) })
	if finished {
		status := originalDeploymentText(result, "status")
		outcome := deploymentResults[status]
		detail := ""
		for _, key := range []string{"error", "rollback_error", "message"} {
			if text := originalDeploymentText(result, key); text != "" {
				detail = redact.DiagnosticText(text, nil, 300)
				break
			}
		}
		suffix := ""
		if detail != "" && status != "succeeded" {
			suffix = "：" + logx.LogText(detail, 300)
		}
		logger.Log(outcome.level, fmt.Sprintf(msgDeploymentOutcome, label, outcome.text, prefix(id, 12), suffix))
	}
	if err := m.completeTypedPublication(ctx, agentID, id, result); err != nil {
		return nil, err
	}
	return result, nil
}

func deploymentProbe(a *model.Agent, id string, state *model.Deployment) (*model.Agent, error) {
	var result model.Agent
	for _, key := range []string{"id", "name", "token", "profile_id", "service_type", "config_version", "config_revision", "pushed_at", "applied_content", "latest_deployment"} {
		raw, present, err := a.MemberJSON(key)
		if err != nil {
			return nil, err
		}
		if present {
			if err := result.SetJSON(key, raw); err != nil {
				return nil, err
			}
		}
	}
	task, err := copyDeployment(state)
	if err != nil {
		return nil, err
	}
	result.Deployments.Set(id, *task)
	return &result, nil
}

func (m *Manager) previewUnchangedDeployment(ctx context.Context, agentID, id string, patch DeploymentUpdate, options RecordOptions) *model.Deployment {
	var probe *model.Agent
	err := m.repository.ViewAgents(ctx, func(records []model.Agent) error {
		a := registryAgent(records, agentID)
		if a == nil {
			return nil
		}
		state, present := a.Deployments.Get(id)
		if !present || !a.Deployments.EntryValid(id) {
			return nil
		}
		var err error
		probe, err = deploymentProbe(a, id, &state)
		return err
	})
	if err != nil || probe == nil {
		return nil
	}
	before, err := model.Marshal(probe)
	if err != nil {
		return nil
	}
	state, _ := probe.Deployments.Get(id)
	original, err := copyDeployment(&state)
	if err != nil {
		return nil
	}
	if _, err := m.applyDeploymentUpdate(ctx, probe, id, &state, patch, options); err != nil {
		return nil
	}
	if raw, present, err := original.MemberJSON("updated_at"); err != nil {
		return nil
	} else if present {
		if state.SetJSON("updated_at", raw) != nil {
			return nil
		}
	} else if state.Clear("updated_at") != nil {
		return nil
	}
	probe.Deployments.Set(id, state)
	var baseline model.Agent
	if json.Unmarshal(before, &baseline) != nil {
		return nil
	}
	if probe.LatestDeployment != nil && baseline.LatestDeployment != nil {
		raw, present, err := baseline.LatestDeployment.MemberJSON("updated_at")
		if err != nil {
			return nil
		}
		if present {
			if probe.LatestDeployment.SetJSON("updated_at", raw) != nil {
				return nil
			}
		} else if probe.LatestDeployment.Clear("updated_at") != nil {
			return nil
		}
	}
	after, err := model.Marshal(probe)
	if err == nil && bytes.Equal(before, after) {
		return original
	}
	return nil
}
