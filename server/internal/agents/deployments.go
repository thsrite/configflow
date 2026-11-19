package agents

// The deployment bookkeeping of AgentManager: persistent task ownership,
// result recording, status polling, activation, manual rollback and
// recovery. Preparing and uploading bundles (_publish_deployment,
// _remember_deployment_content) belongs to #24/#26 and reaches the registry
// through the PublishDeployment hook.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"configflow/internal/jsonx"
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
		return nil, ErrAgentEndpoint
	}
	record, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	result, err := m.DeploymentRequestModel(ctx, record, method, suffix, header, body, timeout)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}

// GetDeployment is get_deployment (oxidns_ha_publish.deployment, #29).
func (m *Manager) GetDeployment(ctx context.Context, agentID, id string) (*jsonx.Object, error) {
	result, err := m.GetDeploymentModel(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}

// GetDeploymentDirect is _get_deployment (for #29).
func (m *Manager) GetDeploymentDirect(ctx context.Context, agentID, id string) (*jsonx.Object, error) {
	result, err := m.ReadDeploymentModel(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}

// ActivateDeployment is activate_deployment (oxidns_ha_publish.activate, #29).
func (m *Manager) ActivateDeployment(ctx context.Context, agentID, id string) (*jsonx.Object, error) {
	result, err := m.ActivateDeploymentModel(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}

// ActivateDeploymentDirect is _activate_deployment (for #29).
func (m *Manager) ActivateDeploymentDirect(ctx context.Context, agentID, id string) (*jsonx.Object, error) {
	result, err := m.ActivateCandidateModel(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}

// RollbackDeployment is rollback_deployment: the rollback is reserved before
// any network I/O so a new push cannot race it.
func (m *Manager) RollbackDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	result, err := m.RollbackDeploymentModel(ctx, agentID, deploymentID)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}

// RecoverDeployment is recover_deployment: ask the Agent to recover a
// deployment whose rollback failed (stop the core, restore the verified
// backups, start it again).
func (m *Manager) RecoverDeployment(ctx context.Context, agentID, deploymentID string) (*jsonx.Object, error) {
	result, err := m.RecoverDeploymentModel(ctx, agentID, deploymentID)
	if err != nil {
		return nil, err
	}
	return deploymentObject(result)
}
