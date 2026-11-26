// Package agenttasks assembles the /api/tasks rows for the "agent" slot
// (rule sync and DNS follow) without creating an import cycle in internal/agents.
package agenttasks

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/redact"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// BuildRows returns a tasks.BuildRows function that snapshots Agents once per
// pass, loads redaction secrets once, and runs each AgentRowFunc in order.
func BuildRows(getManager func(context.Context) (*agents.Manager, error), builders ...agents.AgentRowFunc) tasks.BuildRows {
	list := append([]agents.AgentRowFunc(nil), builders...)
	return func(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
		manager, err := getManager(ctx)
		if err != nil {
			return nil, err
		}
		records, err := agents.SnapshotAgents(ctx, manager)
		if err != nil {
			return nil, err
		}
		agentSecrets, agentsErr := redact.AgentRecordCredentials(ctx, manager.Repository(), nil)
		sysSecrets, sysErr := agents.SystemSecrets(ctx, manager.Repository())
		rowCtx := agents.WithRowSecrets(ctx, agentSecrets, agentsErr, sysSecrets, sysErr)
		rows := []*tasks.Task{}
		for i := range records {
			agent := &records[i]
			for _, build := range list {
				if build == nil {
					continue
				}
				row, err := build(rowCtx, agent, statuses)
				if err != nil {
					return nil, err
				}
				if row != nil {
					rows = append(rows, row)
				}
			}
		}
		return rows, nil
	}
}
