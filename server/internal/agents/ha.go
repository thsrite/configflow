package agents

import (
	"configflow/internal/jsonx"
	"context"
	"errors"
)

// ErrPairIncomplete means saved HA members no longer form a reciprocal pair.
var ErrPairIncomplete = errors.New(msgHAPairIncomplete)

// HAEnabled reads the registry's strict boolean HA setting.
func HAEnabled(agent *Record) bool { return haEnabled(agent) }

// HAPair reads a reciprocal HA pair from one lightweight registry snapshot.
// A disabled member has no pair; incomplete identity proofs fail closed.
func (m *Manager) HAPair(ctx context.Context, agentID string) ([]*Record, error) {
	records, err := m.GetAllAgents(ctx, false)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Record, len(records))
	for _, r := range records {
		byID[r.GetString("id")] = r
	}
	agent := byID[agentID]
	if agent == nil || !HAEnabled(agent) {
		return nil, nil
	}
	ha := agent.GetObject("oxidns_ha")
	peer := byID[ha.GetString("peer_id")]
	other := peer.GetObject("oxidns_ha")
	if peer == nil || !HAEnabled(peer) || ha.GetString("group_id") == "" || ha.GetString("group_id") != other.GetString("group_id") || other.GetString("peer_id") != agentID || !jsonx.Equal(peer.Value("profile_id"), agent.Value("profile_id")) {
		return nil, ErrPairIncomplete
	}
	return []*Record{agent, peer}, nil
}

// haField is oxidns_ha_publish.FIELD.
const haField = "oxidns_ha_publication"

var haTerminal = pyStatuses{"succeeded": true, "failed": true, "rollback_failed": true}

// HAField is oxidns_ha_publish.FIELD (exported for internal/agents/monitor).
const HAField = haField

// haActive is oxidns_ha_publish.active(agent).
func haActive(agent *jsonx.Object) bool {
	job, _ := agent.Value(haField).(*jsonx.Object)
	return jsonx.Truthy(job) && !haTerminal.has(job.Value("status"))
}

// haConflict is oxidns_ha_publish.conflict(agent, deployment_id); nil when
// the Agent is free. deploymentID nil is Python's None.
func haConflict(agent *jsonx.Object, deploymentID any) *jsonx.Object {
	job, _ := agent.Value(haField).(*jsonx.Object)
	children, _ := job.Value("children").(*jsonx.Object)
	if haActive(agent) && !jsonx.Equal(children.Value(jsonx.Str(agent.Value("id"))), deploymentID) {
		return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "message", msgHAConflict,
			"ha_publication", HASummary(job))
	}
	return nil
}

// HASummary is oxidns_ha_publish.summary(job).
func HASummary(job *jsonx.Object) *jsonx.Object {
	result := jsonx.NewObject()
	for _, key := range []string{"id", "status", "phase", "message", "owner_agent_id", "owner_deployment_id",
		"active_agent_id", "members", "updated_at", "warnings", "routers"} {
		if value, ok := job.Get(key); ok {
			result.Set(key, jsonx.DeepCopy(value))
		}
	}
	return result
}

// HAAggregate is oxidns_ha_publish._aggregate(record, public).
func HAAggregate(record, public *jsonx.Object) *jsonx.Object {
	result := record.Clone()
	status := public.Value("status")
	result.Set("ha_publication", public)
	result.Set("status", status)
	result.Set("message", public.Value("message"))
	result.Set("success", status != "failed" && status != "rollback_failed")
	result.Delete("http_status")
	result.Delete("pending")
	return result
}

// haChild is job.get('children', {}).get(agent_id) of the Agent's journal.
func haChild(agent *jsonx.Object, agentID string) any {
	job, _ := agent.Value(haField).(*jsonx.Object)
	children, _ := job.Value("children").(*jsonx.Object)
	return children.Value(agentID)
}

// defaultServiceOperation is service_operation without the pair lock file:
// The production HA owner registers the pair/member leased version.
func defaultServiceOperation(ctx context.Context, m *Manager, agentID string, operation func() (*jsonx.Object, error), uninstall bool) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if rejection := haConflict(agent, nil); rejection != nil {
		return rejection, nil
	}
	if uninstall && haEnabled(agent) {
		return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "message", msgHAUninstall), nil
	}
	return operation()
}

// defaultHADeployment is oxidns_ha_publish.deployment for a task outside a
// pair publication.
func defaultHADeployment(ctx context.Context, m *Manager, agentID, deploymentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if jsonx.Equal(haChild(agent, agentID), deploymentID) {
		job := agent.GetObject(HAField)
		// Python advance is a no-op for these durable pair snapshots. Reading
		// them does not require A18's scheduler; active/recovery work still does.
		switch job.GetString("status") {
		case "succeeded", "failed", "ready":
			record := taskOf(agent, deploymentID)
			if !jsonx.Truthy(record) {
				record = jsonx.Obj("deployment_id", deploymentID)
			}
			return HAAggregate(record, HASummary(job)), nil
		default:
			return nil, ErrHAOwnerMissing
		}
	}
	record, err := m.getDeployment(ctx, agentID, deploymentID)
	if err != nil {
		return nil, err
	}
	if public, ok := record.Value("ha_publication").(*jsonx.Object); ok && jsonx.Truthy(public) {
		return HAAggregate(record, public), nil
	}
	return record, nil
}

// defaultHAActivate is oxidns_ha_publish.activate for a task outside a pair
// publication.
func defaultHAActivate(ctx context.Context, m *Manager, agentID, deploymentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if jsonx.Equal(haChild(agent, agentID), deploymentID) {
		return nil, ErrHAOwnerMissing
	}
	if rejection := haConflict(agent, nil); rejection != nil {
		return rejection, nil
	}
	return m.activateDeployment(ctx, agentID, deploymentID)
}

// HAActive reports an unfinished authoritative HA publication journal. A past
// task's public summary alone is not an active pair publication.
func HAActive(agent *Record) bool { return haActive(agent) }

// ErrHAOwnerMissing refuses active HA operations on a manager without its owner hooks.
var ErrHAOwnerMissing = errors.New("agents: DNS HA owner is not wired")
