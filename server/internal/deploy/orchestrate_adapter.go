package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"context"
)

func publishSource(ctx context.Context, m *agents.Manager, profile string, guarded bool) (*Source, *Revision, error) {
	if guarded {
		cfg, rev, e := m.Repository().GetPublishSnapshot(ctx, profile, false)
		if e != nil {
			return nil, nil, e
		}
		return NewSource(cfg, &rev), &Revision{Value: rev}, nil
	}
	cfg, e := m.Repository().GetCompatConfig(ctx, profile, nil)
	if e != nil {
		return nil, nil, e
	}
	revision := &Revision{Null: true}
	if rev, e := m.Repository().ConfigRevision(ctx, profile); e == nil {
		revision = &Revision{Value: rev}
	}
	return NewSource(cfg, nil), revision, nil
}
func resultEvents(r *agents.Result) []any {
	events := r.GetList("events")
	if events == nil {
		return []any{}
	}
	return events
}
func publicBody(body map[string]any) *agents.Record {
	raw, _ := encodeJSON(body)
	v, e := jsonx.Parse(raw)
	if e != nil {
		return result(false, "", "Deployment preparation failed", 0)
	}
	return PublicDeployment(v.(*jsonx.Object))
}
