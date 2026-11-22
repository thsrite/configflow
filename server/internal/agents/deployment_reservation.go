package agents

import (
	"context"
	"fmt"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func copyDeployment(state *model.Deployment) (*model.Deployment, error) {
	result, err := model.Clone(state)
	if err != nil {
		return nil, fmt.Errorf("copy deployment: %w", err)
	}
	return result, nil
}

func deploymentFailure(message string, status int) (*model.Deployment, error) {
	result := &model.Deployment{Success: new(false), Message: new(message)}
	if status != 0 {
		result.HTTPStatus = new(model.Int(status))
	}
	return result, nil
}

func haDeploymentConflict(a *model.Agent, id string) (*model.Deployment, error) {
	if !HAActiveModel(a) {
		return nil, nil
	}
	job := a.OxiDNSHAPublication
	if child, ok := job.Children.Get(a.ID); ok && child == id {
		return nil, nil
	}
	result, err := deploymentFailure(msgHAConflict, 409)
	if err != nil {
		return nil, err
	}
	result.HAPublication, err = HASummaryModel(job)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func followOwnsDeployment(a *model.Agent, id string) bool {
	if a.DNSFollowState == nil {
		return true
	}
	if a.DNSFollowState.Status != "preparing" && a.DNSFollowState.Status != "publishing" {
		return true
	}
	child := a.DNSFollowState.TargetDeploymentID
	return child != nil && *child == id
}

func followUsesSource(a *model.Agent, id string) bool {
	if a.DNSFollowState == nil || a.DNSFollowState.Status != "preparing" && a.DNSFollowState.Status != "publishing" {
		return false
	}
	if a.DNSFollow == nil {
		return false
	}
	return a.DNSFollow.SourceAgentID != "" && a.DNSFollow.SourceAgentID == id
}

// ReserveDeployment persists a profile-bound preparing task and owns the shared
// preparation lease. Validation precedes lease admission; a commit failure or
// shutdown releases only this reservation's physical lease.
func (m *Manager) ReserveDeployment(ctx context.Context, agentID, id string) (*DeploymentReply, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !deploymentIDPattern.MatchString(id) {
		return nil, &ValueError{Message: msgInvalidDeploymentID}
	}
	key := [2]string{agentID, id}
	var lease *preparationLease
	keep := false
	defer func() {
		if !keep && lease != nil {
			m.releasePreparation(key, lease)
		}
	}()
	var result *model.Deployment
	existing := false
	startedLabel := ""
	started := false
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		fail := func(message string, status int) error {
			var err error
			result, err = deploymentFailure(message, status)
			return err
		}
		a := registryAgent(*records, agentID)
		if a == nil {
			return fail(msgAgentNotFound, 404)
		}
		conflict, err := haDeploymentConflict(a, id)
		if err != nil {
			return err
		}
		if conflict != nil {
			result = conflict
			return nil
		}
		if !followOwnsDeployment(a, id) {
			return fail(msgDeploymentDNSFollowTarget, 409)
		}
		for i := range *records {
			if followUsesSource(&(*records)[i], agentID) {
				return fail(msgDeploymentDNSFollowSource, 409)
			}
		}
		if upgradeBusyModel(a) {
			return fail(msgDeploymentUpgradeBusy, 409)
		}
		if task, present := a.Deployments.Get(id); present {
			result, err = copyDeployment(&task)
			existing = true
			return err
		}
		for _, task := range a.Deployments.All() {
			if !deploymentTerminalModel(task.Status) {
				if err := fail(msgDeploymentUnfinished, 409); err != nil {
					return err
				}
				result.DeploymentID, result.Status = task.DeploymentID, task.Status
				return nil
			}
		}
		profile, err := m.agentProfileID(tx, a)
		if err != nil {
			return err
		}
		task := model.Deployment{
			Success: new(true), DeploymentID: id, Status: "preparing", ProfileID: profile, ServiceType: agentService(a),
			CreatedAt: nowISO(), Events: AppendOperationEvent(nil, "preparing", "", "", nil),
		}
		_, release, acquired := preparationLocks.TryLock(tx, [3]string{m.repository.DataDir(), agentID, id})
		if !acquired {
			return fail(msgDeploymentPreparationBusy, 409)
		}
		lease = &preparationLease{release: release}
		m.deploymentMu.Lock()
		m.preparations[key] = lease
		m.deploymentMu.Unlock()
		for key, previous := range a.Deployments.All() {
			if previous.PreparedContent != nil {
				prepared := *previous.PreparedContent
				prepared.Files = nil
				previous.PreparedContent = &prepared
				a.Deployments.Set(key, previous)
			}
		}
		a.Deployments.Set(id, task)
		a.LatestDeployment, err = copyDeployment(&task)
		if err != nil {
			return err
		}
		keys := a.Deployments.Keys()
		for i := 0; i < len(keys)-30; i++ {
			a.Deployments.Delete(keys[i])
		}
		result, err = copyDeployment(&task)
		if err != nil {
			return err
		}
		name := a.Name
		if name == "" {
			name = a.ID
		}
		startedLabel = registryDiagnostic(a, name, 120)
		started = true
		return tx.Err()
	})
	if err != nil {
		return nil, err
	}
	if started && lease != nil {
		m.deploymentMu.Lock()
		if m.preparations[key] == lease {
			lease.stop = context.AfterFunc(ctx, func() { m.releasePreparation(key, lease) })
		}
		m.deploymentMu.Unlock()
		keep = true
		logger.Infof(msgDeploymentStarted, startedLabel, prefix(id, 12))
	}
	reply, err := journalReply(result)
	if err != nil {
		return nil, err
	}
	if existing {
		if err := reply.Set("existing", true); err != nil {
			return nil, err
		}
	}
	return reply, nil
}
