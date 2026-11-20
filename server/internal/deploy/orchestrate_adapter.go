package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/model"
	"context"
	"encoding/json"
)

func publishSource(ctx context.Context, m *agents.Manager, profile string, guarded bool) (*Source, *Revision, error) {
	if guarded {
		cfg, rev, e := m.Repository().PublishConfig(ctx, profile, false)
		if e != nil {
			return nil, nil, e
		}
		raw, e := model.Marshal(cfg)
		if e != nil {
			return nil, nil, e
		}
		source, e := SourceFromJSON(raw, &rev)
		return source, &Revision{Value: rev}, e
	}
	cfg, e := m.Repository().Config(ctx, profile, nil)
	if e != nil {
		return nil, nil, e
	}
	revision := &Revision{Null: true}
	if rev, e := m.Repository().ConfigRevision(ctx, profile); e == nil {
		revision = &Revision{Value: rev}
	}
	raw, e := model.Marshal(cfg)
	if e != nil {
		return nil, nil, e
	}
	source, e := SourceFromJSON(raw, nil)
	return source, revision, e
}
func resultEvents(r *model.Deployment) []model.OperationEvent {
	if r == nil || r.Events == nil {
		return []model.OperationEvent{}
	}
	return r.Events
}
func publicBody(body map[string]any) *model.Deployment {
	raw, _ := encodeJSON(body)
	var out model.Deployment
	if json.Unmarshal(raw, &out) != nil {
		return result(false, "", "Deployment preparation failed", 0)
	}
	return PublicDeployment(&out)
}
