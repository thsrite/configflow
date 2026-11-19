package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func copyDeployment(state *model.Deployment) (*model.Deployment, error) {
	raw, err := model.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("copy deployment: %w", err)
	}
	var result model.Deployment
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("copy deployment: %w", err)
	}
	return &result, nil
}

func deploymentFailure(message string, status int) (*model.Deployment, error) {
	result := &model.Deployment{Message: new(message)}
	if status != 0 {
		result.HTTPStatus = new(model.Int(status))
	}
	if err := result.SetJSON("success", []byte(`false`)); err != nil {
		return nil, err
	}
	return result, nil
}

func haDeploymentConflict(a *model.Agent, id string) (*model.Deployment, error) {
	if !HAActiveModel(a) {
		return nil, nil
	}
	job := a.OxiDNSHAPublication
	child, valid := storedText(job.Children.MemberJSON(a.ID))
	if valid && child == id {
		return nil, nil
	}
	result, err := deploymentFailure(msgHAConflict, 409)
	if err != nil {
		return nil, err
	}
	var summary model.HAPublicationSummary
	for _, key := range []string{"id", "status", "phase", "message", "owner_agent_id", "owner_deployment_id", "active_agent_id", "members", "updated_at", "warnings", "routers"} {
		raw, present, err := job.MemberJSON(key)
		if err != nil {
			return nil, err
		}
		if present {
			if err := summary.SetJSON(key, raw); err != nil {
				return nil, err
			}
		}
	}
	result.HAPublication = &summary
	return result, nil
}

func followOwnsDeployment(a *model.Agent, id string) bool {
	if a.DNSFollowState == nil {
		return true
	}
	if a.DNSFollowState.Status != "preparing" && a.DNSFollowState.Status != "publishing" {
		return true
	}
	// The reserved child ID is evidence, not a projected stored scalar.
	raw, present, err := a.MemberJSON("dns_follow_state")
	if err != nil || !present {
		return false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return false
	}
	child, valid := storedText(members["target_deployment_id"], members["target_deployment_id"] != nil, nil)
	return valid && child == id
}

func followUsesSource(a *model.Agent, id string) bool {
	if a.DNSFollowState == nil || a.DNSFollowState.Status != "preparing" && a.DNSFollowState.Status != "publishing" {
		return false
	}
	if a.DNSFollow == nil {
		return false
	}
	// Only this small binding is read, never the Agent's histories or config.
	raw, present, err := a.MemberJSON("dns_follow")
	if err != nil || !present {
		return false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return false
	}
	source, valid := storedText(members["source_agent_id"], members["source_agent_id"] != nil, nil)
	return valid && source == id
}

// ReserveDeployment persists a profile-bound preparing task and owns the shared
// preparation lease. Validation precedes lease admission; a commit failure or
// shutdown releases only this reservation's physical lease.
func (m *Manager) ReserveDeployment(ctx context.Context, agentID, id string) (*model.Deployment, error) {
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
		if existing, present := a.Deployments.Get(id); present && a.Deployments.EntryValid(id) {
			result, err = copyDeployment(&existing)
			if err != nil {
				return err
			}
			return result.Fields.Set("existing", []byte(`true`))
		}
		for _, task := range a.Deployments.All() {
			if !deploymentTerminalModel(task.Status) {
				if err := fail(msgDeploymentUnfinished, 409); err != nil {
					return err
				}
				for _, key := range []string{"deployment_id", "status"} {
					raw, present, err := task.MemberJSON(key)
					if err != nil {
						return err
					}
					if !present {
						raw = json.RawMessage(`null`)
					}
					if err := result.SetJSON(key, raw); err != nil {
						return err
					}
				}
				return nil
			}
		}
		profile, err := m.agentProfileID(tx, a)
		if err != nil {
			return err
		}
		service, serviceOK := storedText(a.MemberJSON("service_type"))
		if !serviceOK {
			if _, present, err := a.MemberJSON("service_type"); err != nil {
				return err
			} else if present {
				return &ValueError{Message: msgPreparationOwnershipChanged}
			}
			service = "mihomo"
		}
		wire := struct {
			Success     bool                   `json:"success"`
			ID          string                 `json:"deployment_id"`
			Status      string                 `json:"status"`
			ProfileID   string                 `json:"profile_id"`
			ServiceType string                 `json:"service_type"`
			CreatedAt   string                 `json:"created_at"`
			Events      []model.OperationEvent `json:"events"`
		}{true, id, "preparing", profile, service, nowISO(), AppendOperationEvent(nil, "preparing", "", "", nil)}
		raw, err := json.Marshal(wire)
		if err != nil {
			return err
		}
		var task model.Deployment
		if err := json.Unmarshal(raw, &task); err != nil {
			return err
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
				if err := prepared.Clear("files"); err != nil {
					return err
				}
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
	return result, nil
}
