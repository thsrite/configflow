// Package upgrades owns durable, version-confirmed Agent updates and address following.
// Acceptance is never success: worker completion also needs identity, version and migration proof.
package upgrades

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
)

var logger = logx.Get("backend.agents.upgrades")

// Terminal lists all finished upgrade stages, including failed rollback.
var Terminal = map[string]bool{"succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}
var terminalNotRollbackFailed = map[string]bool{"succeeded": true, "failed": true, "rolled_back": true}

// Stages lists the stages reported by an independent update worker.
var Stages = map[string]bool{"queued": true, "downloading": true, "verifying": true, "backing_up": true, "migrating": true, "replacing": true, "restarting": true, "checking": true, "rolling_back": true, "succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}
var stagesOrUnknown = func() map[string]bool {
	out := map[string]bool{"unknown": true}
	for k := range Stages {
		out[k] = true
	}
	return out
}()
var now = agents.NowTimestamp
var artifactsFunc = ArtifactsModel
var updateIDSource = func() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate update id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:]), nil
}

type upgradePatch struct {
	Status             string
	Error              *string
	Message            *string
	Events             []model.OperationEvent
	AgentUpdatedAt     *string
	RecoveryReportedAt *float64
}

func readState(a *model.Agent) (*model.AgentUpgrade, error) {
	if !a.LatestUpgrade.HasMembers() {
		return nil, nil
	}
	state := *a.LatestUpgrade
	return &state, nil
}
func stateID(state *model.AgentUpgrade) (string, error) {
	id, ok := state.UpdateID, true
	if !ok || id == "" {
		return "", &CorruptStateError{Field: "latest_upgrade.update_id"}
	}
	return id, nil
}
func stateProtocol(state *model.AgentUpgrade) (model.Int, error) {
	if state.Protocol == 0 {
		return 0, &CorruptStateError{Field: "latest_upgrade.protocol"}
	}
	return state.Protocol, nil
}

func saveModel(ctx context.Context, m *agents.Manager, agentID, updateID string, patch upgradePatch, onlyQueued bool) (*model.AgentUpgrade, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var saved *model.AgentUpgrade
	var finishedName string
	var finished bool
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		if err := tx.Err(); err != nil {
			return err
		}
		a, err := findModel(*records, agentID)
		if err != nil {
			return err
		}
		state, err := readState(a)
		if err != nil {
			return err
		}
		if state == nil {
			return invalidInput("Upgrade ownership changed")
		}
		id, err := stateID(state)
		if err != nil {
			return err
		}
		if id != updateID {
			return invalidInput("Upgrade ownership changed")
		}
		before := state.Status
		saved = state
		if terminalNotRollbackFailed[before] || onlyQueued && before != "queued" {
			return tx.Err()
		}
		reportedAt := ""
		if patch.AgentUpdatedAt != nil {
			reportedAt, _ = agents.EventTime(*patch.AgentUpdatedAt)
		}
		if reportedAt != "" {
			if previous := state.AgentUpdatedAt; previous != nil && reportedAt < *previous {
				return tx.Err()
			}
		}
		secrets := agentSecrets(a)
		incoming := agents.CleanOperationEvents(patch.Events, secrets, stagesOrUnknown)
		events := agents.MergeOperationEvents(state.Events, incoming, secrets, stagesOrUnknown)
		if patch.Status != "" {
			state.Status = patch.Status
			if patch.Message == nil {
				state.Message = nil
			}
		}
		if patch.Error != nil {
			state.Error = new(redact.DiagnosticText(*patch.Error, secrets, 2048))
		}
		if patch.Message != nil {
			state.Message = new(redact.DiagnosticText(*patch.Message, secrets, 2048))
		}
		if patch.RecoveryReportedAt != nil {
			state.RecoveryReportedAt = patch.RecoveryReportedAt
		}
		if reportedAt != "" {
			state.AgentUpdatedAt = new(reportedAt)
		}
		if len(incoming) == 0 || incoming[len(incoming)-1].Status != state.Status || len(events) > 0 && events[len(events)-1].Status != state.Status {
			message := ""
			if state.Message != nil {
				message = *state.Message
			}
			if message == "" && state.Error != nil {
				message = *state.Error
			}
			events = agents.AppendOperationEvent(events, state.Status, message, "", secrets)
		}
		state.Events = events
		state.UpdatedAt = now()
		a.LatestUpgrade = state
		finished = before != state.Status && Terminal[state.Status]
		finishedName = agentLabel(a)
		return tx.Err()
	})
	if err != nil {
		return nil, err
	}
	if finished {
		level, text := logx.Warning, msgFailed
		switch saved.Status {
		case "succeeded":
			level, text = logx.Info, msgSucceeded
		case "rolled_back":
			text = msgRolledBack
		case "rollback_failed":
			level, text = logx.Error, msgRollbackFailed
		}
		detail := ""
		if saved.Error != nil {
			detail = *saved.Error
		}
		if detail == "" && saved.Message != nil {
			detail = *saved.Message
		}
		suffix := ""
		if detail != "" && saved.Status != "succeeded" {
			suffix = "（" + logx.LogText(detail, 300) + "）"
		}
		logger.Log(level, fmt.Sprintf(msgResultLog, logx.LogText(finishedName, 120), logx.LogText(displayVersion(saved.PreviousVersion), 40), logx.LogText(displayVersion(saved.TargetVersion), 40), text, suffix))
	}
	return cloneState(saved)
}
func cloneState(state *model.AgentUpgrade) (*model.AgentUpgrade, error) {
	if state == nil {
		return nil, nil
	}
	raw, err := model.Marshal(state)
	if err != nil {
		return nil, err
	}
	var copy model.AgentUpgrade
	if err := jsondecode.Unmarshal(raw, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

// StartModel reserves a durable update, then submits exactly that ID to the Agent.
func StartModel(ctx context.Context, m *agents.Manager, agentID string) (*model.AgentUpgrade, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, invalidInput("Agent not found")
	}
	method, _ := a.DeploymentMethod, true
	if method == "docker" {
		return nil, invalidInput(msgDockerUpgrade)
	}
	response, err := requestModel(ctx, m, a, "GET", "/api/upgrade-info", nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if agents.IsTransportError(err) || IsRefused(err) {
			return nil, invalidInput(msgCannotConnect)
		}
		return nil, err
	}
	var info Info
	hasInfo := false
	if response.StatusCode != 404 {
		if response.StatusCode >= 400 && response.StatusCode < 600 {
			return nil, invalidInput(msgCannotConnect)
		}
		data := responseData(response)
		if !json.Valid(data) {
			return nil, invalidInput(msgCannotConnect)
		}
		nonempty, err := model.Truthy(data)
		if err != nil {
			return nil, invalidInput(msgCannotConnect)
		}
		hasInfo = nonempty
		if hasInfo {
			fields, err := jsondecode.Select(data, "agent_id", "deployment_method", "architecture")
			if err != nil {
				return nil, agents.ErrInvalidAgentReply
			}
			info.AgentID, _ = originalString(fields["agent_id"], fields["agent_id"] != nil, nil)
			info.DeploymentMethod, _ = originalString(fields["deployment_method"], fields["deployment_method"] != nil, nil)
			info.Architecture, _ = originalString(fields["architecture"], fields["architecture"] != nil, nil)
		}
	}
	if hasInfo && (info.AgentID != agentID || info.DeploymentMethod != "shell") {
		return nil, invalidInput(msgIdentityMismatch)
	}
	if !hasInfo && method != "shell" {
		return nil, invalidInput(msgLegacyMethod)
	}
	available, err := artifactsFunc(ctx)
	if err != nil {
		return nil, err
	}
	if hasInfo {
		if _, ok := available.Get(info.Architecture); !ok {
			return nil, invalidInput(msgArtifactsNotReady)
		}
	} else {
		if available.Len() != len(Arches) {
			return nil, invalidInput(msgArtifactsNotReady)
		}
		for _, arch := range Arches {
			if _, ok := available.Get(arch); !ok {
				return nil, invalidInput(msgArtifactsNotReady)
			}
		}
	}
	target := agents.GetLatestVersion()
	previous, present := a.Version, true
	if !validVersion(target) || present && previous != "" && !validVersion(previous) {
		return nil, agents.ErrInvalidAgentReply
	}
	id, err := updateIDSource()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	protocol := model.Int(1)
	if hasInfo {
		protocol = 2
	}
	message := fmt.Sprintf(msgSubmitting, target)
	if hasInfo && info.Architecture != "" {
		message += "（" + info.Architecture + "）"
	}
	state := &model.AgentUpgrade{UpdateID: id, TargetVersion: target, PreviousVersion: previous, Status: "queued", AgentID: agentID, StartedAt: now(), UpdatedAt: now(), Protocol: protocol, Artifacts: available, Message: &message, Events: agents.AppendOperationEvent(nil, "queued", message, "", nil)}
	err = m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		current, err := findModel(*records, agentID)
		if err != nil {
			return err
		}
		active, err := activeHA(current)
		if err != nil {
			return err
		}
		if active {
			return invalidInput(msgHAActive)
		}
		prior, err := readState(current)
		if err != nil {
			return err
		}
		snapshot, err := readState(a)
		if err != nil {
			return err
		}
		priorID, snapshotID := "", ""
		if prior != nil {
			priorID, err = stateID(prior)
			if err != nil {
				return err
			}
		}
		if snapshot != nil {
			snapshotID, err = stateID(snapshot)
			if err != nil {
				return err
			}
		}
		if priorID != snapshotID {
			return invalidInput(msgUpgradeChanged)
		}
		if prior != nil && (!Terminal[prior.Status] || prior.Status == "rollback_failed") {
			return invalidInput(msgUpgradeBusy)
		}
		if deployment := current.LatestDeployment; deployment.HasMembers() {
			status := deployment.Status
			if status == "" {
				return &CorruptStateError{Field: "latest_deployment.status"}
			}
			if !terminalNotRollbackFailed[status] {
				return invalidInput(msgDeploymentBusy)
			}
		}
		if err := tx.Err(); err != nil {
			return err
		}
		current.LatestUpgrade = state
		return nil
	})
	if err != nil {
		return nil, err
	}
	logger.Info(fmt.Sprintf(msgStartLog, logx.LogText(agentLabel(a), 120), logx.LogText(displayVersion(previous), 40), logx.LogText(target, 40)))
	payload := struct {
		Version  string `json:"version"`
		UpdateID string `json:"update_id"`
		SHA256   string `json:"sha256,omitempty"`
		Size     *int64 `json:"size,omitempty"`
	}{Version: target, UpdateID: id}
	if hasInfo {
		artifact, _ := available.Get(info.Architecture)
		payload.SHA256 = artifact.SHA256
		payload.Size = new(int64(artifact.Size))
	}
	response, err = requestModel(ctx, m, a, "POST", "/api/update", payload)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if agents.IsTransportError(err) {
			return saveModel(ctx, m, agentID, id, upgradePatch{Status: "checking", Error: new(msgRequestUncertain)}, true)
		}
		return nil, err
	}
	if response.StatusCode != 200 && response.StatusCode != 202 {
		message := "Agent rejected upgrade"
		if fields, e := jsondecode.Select(responseData(response), "message"); e == nil {
			if raw, ok := fields["message"]; ok {
				message = rawText(raw)
			}
		}
		return saveModel(ctx, m, agentID, id, upgradePatch{Status: "failed", Error: &message}, false)
	}
	return saveModel(ctx, m, agentID, id, upgradePatch{Status: "queued", Message: new(fmt.Sprintf(msgDelivered, target))}, true)
}

// PollModel reconciles worker state and independent executable proof without resubmitting.
func PollModel(ctx context.Context, m *agents.Manager, agentID string) (*model.AgentUpgrade, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, invalidInput("No upgrade recorded")
	}
	state, err := readState(a)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, invalidInput("No upgrade recorded")
	}
	if terminalNotRollbackFailed[state.Status] {
		return state, nil
	}
	id, err := stateID(state)
	if err != nil {
		return nil, err
	}
	previous, pOK := state.PreviousVersion, true
	target, tOK := state.TargetVersion, true
	if !pOK || !tOK || previous != "" && !validVersion(previous) || !validVersion(target) {
		return nil, &CorruptStateError{Field: "latest_upgrade.version"}
	}
	protocol, err := stateProtocol(state)
	if err != nil {
		return nil, err
	}
	recovery, present := 0.0, state.RecoveryReportedAt != nil
	if present {
		recovery = *state.RecoveryReportedAt
	}
	version, versionOK := a.Version, true
	afterRecovery := false
	if present && recovery != 0 && versionOK && version == previous {
		var e error
		afterRecovery, e = heartbeatAfterModel(a, recovery)
		if e != nil {
			return nil, e
		}
	}
	if afterRecovery {
		saved, e := saveModel(ctx, m, agentID, id, upgradePatch{Status: "rolled_back"}, false)
		if e == nil || !IsRefused(e) {
			return saved, e
		}
	}
	result, done, err := pollAgentModel(ctx, m, a, agentID, state, protocol)
	if done || err != nil {
		return result, err
	}
	started := state.StartedAt
	deadline := started + 600
	if now() > deadline {
		afterDeadline := false
		if versionOK && version == previous {
			var e error
			afterDeadline, e = heartbeatAfterModel(a, deadline)
			if e != nil {
				return nil, e
			}
		}
		if afterDeadline {
			return saveModel(ctx, m, agentID, id, upgradePatch{Status: "failed", Error: new(fmt.Sprintf(msgUpgradeIneffective, previous))}, false)
		}
		if state.Status != "unknown" {
			return saveModel(ctx, m, agentID, id, upgradePatch{Status: "unknown", Error: new(msgUpgradeUnknown)}, false)
		}
	}
	return state, nil
}
func pollAgentModel(ctx context.Context, m *agents.Manager, a *model.Agent, agentID string, state *model.AgentUpgrade, protocol model.Int) (*model.AgentUpgrade, bool, error) {
	caught := func(err error) (*model.AgentUpgrade, bool, error) {
		if ctx.Err() != nil {
			return nil, true, ctx.Err()
		}
		if agents.IsTransportError(err) || IsRefused(err) || errors.Is(err, agents.ErrInvalidAgentReply) {
			return nil, false, nil
		}
		return nil, true, err
	}
	var worker *upgradePatch
	var observation Worker
	if protocol == 2 {
		response, err := requestModel(ctx, m, a, "GET", "/api/update", nil)
		if err != nil {
			return caught(err)
		}
		if response.StatusCode == 200 {
			var err error
			observation, err = decodeWorker(responseData(response))
			if err != nil {
				return caught(err)
			}
			if observation.UpdateID == state.UpdateID && observation.AgentID == agentID && Stages[observation.Status] {
				worker = &upgradePatch{Status: observation.Status, Error: new(rawText(observation.Error)), Message: observation.Message, Events: observation.Events, AgentUpdatedAt: observation.UpdatedAt}
				if observation.Status != "succeeded" {
					saved, e := saveModel(ctx, m, agentID, state.UpdateID, *worker, false)
					if e != nil {
						return caught(e)
					}
					return saved, true, nil
				}
			}
		}
	}
	response, err := requestModel(ctx, m, a, "GET", "/api/upgrade-info", nil)
	if err != nil {
		return caught(err)
	}
	if response.StatusCode == 200 {
		info, err := decodeInfo(responseData(response))
		if err != nil {
			return caught(err)
		}
		if info.AgentID == agentID && validVersion(info.Version) && info.Version == state.TargetVersion && info.MigrationReady && (protocol == 1 || worker != nil && worker.Status == "succeeded") {
			patch := upgradePatch{}
			if worker != nil {
				patch = *worker
			}
			patch.Status = "succeeded"
			patch.Error = new("")
			saved, e := saveModel(ctx, m, agentID, state.UpdateID, patch, false)
			if e != nil {
				return caught(e)
			}
			return saved, true, nil
		}
	}
	if worker != nil && worker.Status == "succeeded" {
		worker.Status = "checking"
		worker.Message = new(msgConfirming)
		events := make([]model.OperationEvent, 0)
		for _, event := range agents.CleanOperationEvents(worker.Events, nil, Stages) {
			if !Terminal[event.Status] {
				events = append(events, event)
			}
		}
		worker.Events = events
		saved, e := saveModel(ctx, m, agentID, state.UpdateID, *worker, false)
		if e != nil {
			return caught(e)
		}
		return saved, true, nil
	}
	return nil, false, nil
}

// ReportLegacyJSON accepts only an exact-key protocol-1 report owned by the pending task.
func ReportLegacyJSON(ctx context.Context, m *agents.Manager, agentID string, data json.RawMessage) (*model.AgentUpgrade, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var report LegacyReport
	if decodeProtocol(data, &report) != nil {
		return nil, ErrInvalidReport
	}
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrInvalidReport
	}
	state, err := readState(a)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, ErrInvalidReport
	}
	protocol, err := stateProtocol(state)
	if err != nil {
		return nil, err
	}
	id, err := stateID(state)
	if err != nil {
		return nil, err
	}
	if protocol != 1 || id != report.UpdateID || report.Status != "rolling_back" {
		return nil, ErrInvalidReport
	}
	text := "Migration failed"
	if len(report.Error) > 0 {
		text = rawText(report.Error)
	}
	return saveModel(ctx, m, agentID, id, upgradePatch{Status: "rolling_back", RecoveryReportedAt: new(now()), Error: &text}, false)
}

// StoredState validates the original persisted journal without contacting the
// Agent. The protocol-1 bootstrap uses this to read its own pending plan.
func StoredState(a *model.Agent) (*model.AgentUpgrade, error) {
	if a == nil {
		return nil, nil
	}
	return readState(a)
}
