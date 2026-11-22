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
	// status is the reported status as received, for the shape check.
	status json.RawMessage
}

// DecodeDeploymentUpdate selects the remote protocol keys of a reply object.
// Values decode leniently (a value of the wrong type is its zero value).
func DecodeDeploymentUpdate(data json.RawMessage) (DeploymentUpdate, error) {
	result := DeploymentUpdate{present: make(map[string]bool)}
	var members model.OrderedMap[json.RawMessage]
	if err := requireJSONObject(data); err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &members); err != nil {
		return result, err
	}
	for _, key := range append(append([]string(nil), deploymentUpdateFields...), "updated_at", "dns_checks", "dns_checked_at") {
		raw, present := members.Get(key)
		if !present {
			continue
		}
		if _, _, err := model.DecodeStoredMember(&result.record, key, raw, key); err != nil {
			return DeploymentUpdate{}, err
		}
		result.present[key] = true
		if key == "status" {
			result.status = raw
		}
	}
	return result, nil
}

// requireJSONObject refuses JSON that is not an object.
func requireJSONObject(data []byte) error {
	if err := model.ValidJSON(data); err != nil {
		return err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return model.ErrType
	}
	return nil
}

func flagSet(flag *bool) bool { return flag != nil && *flag }

func receiptTerminalModel(status string) bool {
	return deploymentTerminalModel(status) || status == "rollback_failed"
}

func agentDiagnosticSecrets(a *model.Agent) redact.Secrets {
	if a != nil && a.Token != "" {
		return redact.NewSecrets([]string{a.Token})
	}
	return nil
}

func nonscalarStatus(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && (raw[0] == '[' || raw[0] == '{')
}

// sameText compares two optional text members; missing equals missing only.
func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// clearDeploymentMember drops one optional journal member.
func clearDeploymentMember(state *model.Deployment, key string) {
	switch key {
	case "message":
		state.Message = nil
	case "error":
		state.Error = nil
	case "error_details":
		state.ErrorDetails = nil
	case "failed_stage":
		state.FailedStage = nil
	case "rollback_error":
		state.RollbackError = nil
	case "rollback_error_details":
		state.RollbackErrorDetails = nil
	case "http_status":
		state.HTTPStatus = nil
	}
}

// copyReportedMember copies one reported protocol member into the journal.
func copyReportedMember(state, patch *model.Deployment, key string) {
	switch key {
	case "status":
		state.Status = patch.Status
	case "message":
		state.Message = patch.Message
	case "error":
		state.Error = patch.Error
	case "error_details":
		state.ErrorDetails = patch.ErrorDetails
	case "failed_stage":
		state.FailedStage = patch.FailedStage
	case "rollback_error":
		state.RollbackError = patch.RollbackError
	case "rollback_error_details":
		state.RollbackErrorDetails = patch.RollbackErrorDetails
	case "config_version":
		state.ConfigVersion = patch.ConfigVersion
	case "config_revision":
		state.ConfigRevision = patch.ConfigRevision
	case "success":
		state.Success = patch.Success
	case "sha256":
		state.SHA256 = patch.SHA256
	case "submitted":
		state.Submitted = patch.Submitted
	case "previous_running":
		state.PreviousRunning = patch.PreviousRunning
	case "health":
		state.Health = bytes.Clone(patch.Health)
	case "http_status":
		state.HTTPStatus = patch.HTTPStatus
	case "manual_rollback":
		state.ManualRollback = patch.ManualRollback
	case "source_sha256":
		state.SourceSHA256 = patch.SourceSHA256
	}
}

// applyDeploymentUpdate is shared by the durable transaction and the read-only
// no-op preview. It never emits logs, notifications, metrics or device requests.
func (m *Manager) applyDeploymentUpdate(ctx context.Context, a *model.Agent, id string, state *model.Deployment, patch DeploymentUpdate, options RecordOptions) (bool, error) {
	if options.OnlyUnsubmitted && !preparationUnsubmitted(state) {
		return false, nil
	}
	before := state.Status
	status := state.Status
	incoming := patch.record.Status
	if options.UploadReceipt && !receiptTerminalModel(incoming) && status != "preparing" && status != "uploading" && status != "receiving" {
		return false, nil
	}
	if options.ActivationReceipt && !receiptTerminalModel(incoming) && status != "ready" && status != "unknown" {
		return false, nil
	}
	if flagSet(state.ManualRollback) && !flagSet(patch.record.ManualRollback) {
		return false, nil
	}
	if patch.present["status"] {
		if deploymentTerminalModel(status) && incoming != status {
			return false, nil
		}
		if nonscalarStatus(patch.status) {
			return false, ErrInvalidDeploymentStatus
		}
	}
	reportedAt, hasReported := NormalizeEventTime(textOf(patch.record.UpdatedAt))
	if hasReported {
		previous := textOf(state.AgentUpdatedAt)
		if previous != "" && reportedAt < previous {
			return false, nil
		}
	}
	noHTTPFailure := patch.record.HTTPStatus == nil || *patch.record.HTTPStatus == 0
	if incoming != "" && incoming != "unknown" && noHTTPFailure {
		if status == "unknown" {
			for _, key := range []string{"error_details", "rollback_error_details"} {
				if !patch.present[key] {
					clearDeploymentMember(state, key)
				}
			}
		}
		clearDeploymentMember(state, "http_status")
		if !patch.present["message"] {
			clearDeploymentMember(state, "message")
		}
	}
	if patch.present["error"] && !patch.present["error_details"] && !sameText(state.Error, patch.record.Error) {
		clearDeploymentMember(state, "error_details")
	}
	if patch.present["rollback_error"] && !patch.present["rollback_error_details"] && !sameText(state.RollbackError, patch.record.RollbackError) {
		clearDeploymentMember(state, "rollback_error_details")
	}
	secrets := agentDiagnosticSecrets(a)
	for _, key := range deploymentUpdateFields {
		if !patch.present[key] {
			continue
		}
		copyReportedMember(state, &patch.record, key)
	}
	// Reported diagnostics are stored redacted; a reported null is "".
	for _, text := range []struct {
		key   string
		field **string
	}{{"message", &state.Message}, {"error", &state.Error}, {"error_details", &state.ErrorDetails}, {"rollback_error", &state.RollbackError}, {"rollback_error_details", &state.RollbackErrorDetails}} {
		if patch.present[text.key] {
			*text.field = new(redact.DiagnosticText(textOf(*text.field), secrets, 0))
		}
	}
	report, err := SanitizeDNSReport(&patch.record, secrets)
	if err != nil {
		return false, err
	}
	applyDNSReport(state, report)
	if incoming == "ready" || incoming == "succeeded" {
		for _, key := range []string{"error", "error_details", "failed_stage", "rollback_error", "rollback_error_details"} {
			if !patch.present[key] {
				clearDeploymentMember(state, key)
			}
		}
	}
	if incoming == "rolled_back" && textOf(patch.record.RollbackError) == "" {
		clearDeploymentMember(state, "rollback_error")
		clearDeploymentMember(state, "rollback_error_details")
	}
	state.UpdatedAt = new(nowISO())
	if hasReported {
		state.AgentUpdatedAt = new(reportedAt)
	}
	message := textOf(state.Message)
	if message == "" {
		message = textOf(state.Error)
	}
	current := state.Status
	state.Events = AppendOperationEvent(state.Events, current, message, "", secrets)
	isLatest := state.DeploymentID == id && a.LatestDeployment != nil && a.LatestDeployment.DeploymentID == id
	if current == "succeeded" && isLatest {
		profile, profileErr := m.agentProfileID(ctx, a)
		if profileErr != nil && !errors.Is(profileErr, repo.ErrValidation) {
			return false, profileErr
		}
		service := a.ServiceType
		stateService := state.ServiceType
		if stateService == "" {
			stateService = service
		}
		if profileErr == nil && state.ProfileID != "" && state.ProfileID == profile && service == stateService {
			version := a.ConfigVersion
			if state.ConfigVersion != nil {
				version = *state.ConfigVersion
			} else if version == "" {
				version = "0"
			}
			a.ConfigVersion = version
			a.PushedAt = new(utcNowZ())
			if state.ConfigRevision != nil {
				a.ConfigRevision = new(*state.ConfigRevision)
			}
			if prepared := state.PreparedContent; prepared != nil {
				applied := &model.AppliedContent{
					Fingerprint: prepared.Fingerprint, ProfileID: prepared.ProfileID, ServiceType: prepared.ServiceType,
					ConfigPath: prepared.ConfigPath, BaseURL: prepared.BaseURL, DeployedConfigSHA256: prepared.DeployedConfigSHA256,
					SettingsDigest: prepared.SettingsDigest, ConfigVersion: version, DeploymentID: id,
				}
				if prepared.Files != nil {
					files := *prepared.Files
					applied.Files = &files
				}
				a.AppliedContent = applied
			} else {
				a.AppliedContent = nil
			}
		}
	}
	if flagSet(state.ManualRollback) && current == "rolled_back" && isLatest {
		version := textOf(state.ConfigVersion)
		if version == "" {
			version = "0"
		}
		a.ConfigVersion = version
		a.AppliedContent, a.ConfigRevision, a.PushedAt = nil, nil, nil
	}
	if isLatest {
		a.LatestDeployment, err = copyDeployment(state)
		if err != nil {
			return false, err
		}
	}
	_, known := deploymentResults[current]
	return known && before != current && current != "", nil
}

func (m *Manager) completeTypedPublication(ctx context.Context, agentID, id string, state *model.Deployment) error {
	if state.ServiceType != "oxidns" {
		return nil
	}
	return m.publicationComplete(ctx, agentID, id, state.Status)
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
		if !present {
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
		status := result.Status
		outcome := deploymentResults[status]
		detail := ""
		for _, text := range []string{textOf(result.Error), textOf(result.RollbackError), textOf(result.Message)} {
			if text != "" {
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
	result := model.Agent{
		ID: a.ID, Name: a.Name, Token: a.Token, ProfileID: a.ProfileID, ServiceType: a.ServiceType,
		ConfigVersion: a.ConfigVersion, ConfigRevision: a.ConfigRevision, PushedAt: a.PushedAt,
		AppliedContent: a.AppliedContent, LatestDeployment: a.LatestDeployment,
	}
	result, err := model.Clone(result)
	if err != nil {
		return nil, err
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
		if !present {
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
	state.UpdatedAt = original.UpdatedAt
	probe.Deployments.Set(id, state)
	var baseline model.Agent
	if json.Unmarshal(before, &baseline) != nil {
		return nil
	}
	if probe.LatestDeployment != nil && baseline.LatestDeployment != nil {
		probe.LatestDeployment.UpdatedAt = baseline.LatestDeployment.UpdatedAt
	}
	after, err := model.Marshal(probe)
	if err == nil && bytes.Equal(before, after) {
		return original
	}
	return nil
}
