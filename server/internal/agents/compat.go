package agents

// TODO(port): replace with package #29 DNS HA
// (backend/agents/oxidns_ha_publish.py: FIELD, TERMINAL, active, conflict,
// summary, _aggregate and the non-pair branches of service_operation,
// deployment and activate). These are the pure pieces the registry needs
// before #29 lands; #29 takes them over and registers the pair paths with
// RegisterHooks.

import (
	"context"

	"configflow/internal/jsonx"
)

// haField is oxidns_ha_publish.FIELD.
const haField = "oxidns_ha_publication"

var haTerminal = pyStatuses{"succeeded": true, "failed": true, "rollback_failed": true}

// HAField is oxidns_ha_publish.FIELD (exported for internal/agents/monitor).
const HAField = haField

// HAActive is oxidns_ha_publish.active(agent) (exported for
// internal/agents/monitor's rule update).
func HAActive(agent *jsonx.Object) bool { return haActive(agent) }

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
		return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "message", "DNS 主备正在同步，请等待两台发布完成",
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
// only #29 publishes pairs, and it registers the locked version.
func defaultServiceOperation(ctx context.Context, m *Manager, agentID string, operation func() (*jsonx.Object, error), uninstall bool) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if rejection := haConflict(agent, nil); rejection != nil {
		return rejection, nil
	}
	if uninstall && haEnabled(agent) {
		return jsonx.Obj("success", false, "http_status", jsonx.Int(409), "message", "请先停用 DNS 高可用，再卸载成员"), nil
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
		return nil, ErrNotPorted
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
		return nil, ErrNotPorted
	}
	if rejection := haConflict(agent, nil); rejection != nil {
		return rejection, nil
	}
	return m.activateDeployment(ctx, agentID, deploymentID)
}
