package routers

import (
	"configflow/internal/routers/spec"
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
func recordNotice(c *httpx.Ctx, cfg json.RawMessage, r *spec.Router, report *core.Report, plan *core.Plan) {
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
		row.ProfileID, row.Host, row.Status, row.ServiceType = a.ProfileID, a.Host, a.Status, a.ServiceType
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
// scope it can display. The bounded shared redactor owns credential detection;
// the repository remembers each profile's values per generation. The OxiDNS
// Agent projection a profile view adds holds a subset of the members of the
// stored Agent records, whose credentials AgentRecordCredentials adds anyway.
func allProfileSecrets(ctx context.Context, db *repo.Repository) (redact.Secrets, error) {
	seeds, err := redact.ExternalPayloadSecrets(ctx, nil)
	if err != nil {
		return nil, err
	}
	var profiles []string
	err = db.View(ctx, func(document *model.Document) error {
		for i := range document.Profiles {
			profiles = append(profiles, document.Profiles[i].ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if seeds, err = redact.ConfigCredentials(ctx, db, profile, seeds); err != nil {
			return nil, err
		}
	}
	return redact.AgentRecordCredentials(ctx, seeds)
}
