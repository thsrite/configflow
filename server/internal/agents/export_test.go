package agents

import (
	"configflow/internal/model"
	"context"
)

func (s *RuntimeState) recordModel(ctx context.Context, agent, payload *model.Agent, mark int64) (bool, error) {
	fields, err := runtimeProjection(agent)
	if err != nil {
		return false, err
	}
	reported, err := reportedHeartbeat(payload)
	if err != nil {
		return false, err
	}
	return s.recordFields(ctx, heartbeatSource{agent.ID, agent.Name, agent.LastHeartbeat, fields}, reported, mark)
}
