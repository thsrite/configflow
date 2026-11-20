package routers

import (
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/redact"
	"configflow/internal/repo"
	core "configflow/internal/routers"
)

// recordNotice is the C07 legacy boundary. Notification failures do not change
// a successfully read report; cancelled shutdown never starts a new commit.
func recordNotice(c *httpx.Ctx, cfg json.RawMessage, r *core.Router, report *core.Report, plan *core.Plan) {
	ctx := lifecycle.Detach(c.Context())
	if ctx.Err() != nil {
		return
	}
	notifications.Safely(func() error {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		var router model.Router
		if err = json.Unmarshal(raw, &router); err != nil {
			return err
		}
		raw, err = json.Marshal(report)
		if err != nil {
			return err
		}
		var nativeReport *notifications.RouterReport
		if err = json.Unmarshal(raw, &nativeReport); err != nil {
			return err
		}
		raw, err = json.Marshal(plan)
		if err != nil {
			return err
		}
		return notifications.SaveRouter(ctx, c.ProfileID, cfg, &router, nativeReport, raw, "")
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
	raw, err := m.ListAgents(c.Context(), true)
	if err != nil {
		return nil, "", err
	}
	out := []core.WorkspaceAgent{}
	for _, a := range raw {
		row := core.WorkspaceAgent{ID: a.ID}
		if !agents.AgentMatchesID(&a, row.ID) {
			row.ID = ""
		}
		row.Name, _ = agents.AgentName(&a)
		// Display/connection strings require original string tokens, not R5 numeric projections.
		value, _, err := a.MemberJSON("profile_id")
		if err != nil {
			return nil, "", err
		}
		_ = json.Unmarshal(value, &row.ProfileID)
		value, _, err = a.MemberJSON("host")
		if err != nil {
			return nil, "", err
		}
		_ = json.Unmarshal(value, &row.Host)
		value, _, err = a.MemberJSON("status")
		if err != nil {
			return nil, "", err
		}
		_ = json.Unmarshal(value, &row.Status)
		value, _, err = a.MemberJSON("service_type")
		if err != nil {
			return nil, "", err
		}
		_ = json.Unmarshal(value, &row.ServiceType)
		if row.ProfileID == "" {
			row.ProfileID = defaultProfile
		}
		if row.Name == "" {
			row.Name = row.ID
		}
		out = append(out, row)
	}
	return out, defaultProfile, nil
}
func profileName(ctx context.Context, db *repo.Repository, id string) (string, error) {
	p, err := db.ProfileMetadataRecord(ctx, id)
	if err != nil {
		return "", err
	}
	name := p.Name
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

// allProfileSecrets seeds the global reminder projection from the same profile
// scope it can display. The bounded shared redactor owns credential detection.
func allProfileSecrets(ctx context.Context, db *repo.Repository) (redact.Secrets, error) {
	seeds, err := redact.ExternalPayloadSecrets(ctx, nil)
	if err != nil {
		return nil, err
	}
	profiles, err := db.ListProfileRecords(ctx)
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		cfg, err := db.Config(ctx, profile.ID, repo.ResourceRoots{})
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		seeds = redact.CollectCredentials(json.RawMessage(data), seeds)
	}
	return seeds, nil
}
