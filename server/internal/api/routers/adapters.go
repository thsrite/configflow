package routers

import (
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	core "configflow/internal/routers"
	"context"
	"encoding/json"
)

// recordNotice is the C07 legacy boundary. Notification failures do not change
// a successfully read report; cancelled shutdown never starts a new commit.
func recordNotice(c *httpx.Ctx, cfg json.RawMessage, r *core.Router, report *core.Report, plan *core.Plan) {
	ctx := lifecycle.Detach(c.Context())
	if ctx.Err() != nil {
		return
	}
	object := func(value any) *jsonx.Object {
		if value == nil {
			return nil
		}
		raw, err := json.Marshal(value)
		if err != nil || string(raw) == "null" {
			return nil
		}
		var out jsonx.Object
		if json.Unmarshal(raw, &out) != nil {
			return nil
		}
		return &out
	}
	notifications.Safely(func() error {
		return notifications.RecordRouter(ctx, c.ProfileID, object(cfg), object(r), object(report), object(plan), "")
	})
}
func workspacePeers(c *httpx.Ctx, db *repo.Repository) ([]core.WorkspaceAgent, string, error) {
	m, err := manager(c)
	if err != nil {
		return nil, "", err
	}
	defaultProfile, err := db.DefaultProfileID(c.Context())
	if err != nil {
		return nil, "", err
	}
	raw, err := m.GetAllAgents(c.Context(), true)
	if err != nil {
		return nil, "", err
	}
	out := []core.WorkspaceAgent{}
	for _, a := range raw {
		profile := a.GetString("profile_id")
		if profile == "" {
			profile = defaultProfile
		}
		name := a.GetString("name")
		if name == "" {
			name = a.GetString("id")
		}
		out = append(out, core.WorkspaceAgent{ID: a.GetString("id"), Name: name, Host: a.GetString("host"), Status: a.GetString("status"), ServiceType: a.GetString("service_type"), ProfileID: profile})
	}
	return out, defaultProfile, nil
}
func profileName(ctx context.Context, db *repo.Repository, id string) (string, error) {
	p, err := db.GetProfile(ctx, id)
	if err != nil {
		return "", err
	}
	name := p.GetString("name")
	if name == "" {
		name = id
	}
	return name, nil
}
func publishSnapshot(ctx context.Context, db *repo.Repository, id string) (json.RawMessage, int64, error) {
	cfg, revision, err := db.PublishConfig(ctx, id, false)
	if err != nil {
		return nil, 0, err
	}
	raw, err := json.Marshal(cfg)
	return raw, revision, err
}
