package agents

// The deployment bookkeeping of AgentManager: persistent task ownership,
// result recording, status polling, activation, manual rollback and
// recovery.

import (
	"context"
	"regexp"

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

func (m *Manager) publicationComplete(ctx context.Context, agentID, deploymentID string, status any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.publication != nil {
		return m.publication.PublicationComplete(ctx, m, agentID, deploymentID, status)
	}
	return nil
}
