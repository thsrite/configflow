package agents

// This file adapts legacy Agent records at the HTTP boundary; comparison,
// paging and scheduling use the typed deploy/contentsync interfaces.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	core "configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/auth"
	"configflow/internal/deploy"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/deploy/pushdiff"
	"configflow/internal/httpx"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

type contentSnapshot struct {
	source *deploy.Source
	err    error
}

func loadContentSource(ctx context.Context, m *core.Manager, record *core.Record, cache map[string]contentSnapshot) (*deploy.Source, error) {
	profile := record.GetString("profile_id")
	if profile == "" {
		var err error
		profile, err = m.Repository().DefaultProfileID(ctx)
		if err != nil {
			return nil, err
		}
	}
	if saved, ok := cache[profile]; ok {
		return saved.source, saved.err
	}
	config, err := m.Repository().GetCompatConfig(ctx, profile, nil)
	var source *deploy.Source
	if err == nil {
		source = deploy.NewSource(config, nil)
	}
	if cache != nil {
		cache[profile] = contentSnapshot{source, err}
	}
	return source, err
}
func contentOrigin(record *core.Record) string {
	return record.GetObject("applied_content").GetString("base_url")
}
func publicWithContent(ctx context.Context, m *core.Manager, record *core.Record, cache map[string]contentSnapshot, history bool) (*core.Record, error) {
	secrets := redact.CollectCredentials(record, nil)
	view := record
	if !history {
		view = record.Clone()
		for _, field := range repo.AgentHistoryFields {
			view.Set(field, repo.AgentHistoryNotRead)
		}
	}
	public := publicAgentWithCredentials(view, secrets)
	source, err := loadContentSource(ctx, m, record, cache)
	if err != nil {
		var repositoryError *repo.Error
		if !errors.As(err, &repositoryError) {
			return nil, err
		}
		public.Set("content_sync", contentsync.Result{Status: "unknown", Reason: "profile_unavailable"})
		return public, nil
	}
	public.Set("content_sync", contentChecks.checker(m).Status(ctx, deploy.NewTarget(record), source, contentOrigin(record)))
	return public, nil
}
func listContentAgents(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	m, err := core.Get(ctx)
	if err != nil {
		return nil, err
	}
	summary := c.ArgDefault("summary", "") == "1"
	var secrets []redact.Secrets
	var inspect func(*core.Record)
	if summary {
		inspect = func(raw *core.Record) { secrets = append(secrets, redact.CollectCredentials(raw, nil)) }
	}
	records, err := m.GetAllAgentsInspect(ctx, !summary, inspect)
	if err != nil {
		return nil, err
	}
	result := make([]*core.Record, 0, len(records))
	if summary {
		for i, record := range records {
			result = append(result, publicAgentWithCredentials(record, secrets[i]))
		}
		return httpx.JSON(result), nil
	}
	history := c.ArgDefault("history", "") != "0"
	snapshots := map[string]contentSnapshot{}
	for _, record := range records {
		public, err := publicWithContent(ctx, m, record, snapshots, history)
		if err != nil {
			return nil, err
		}
		result = append(result, public)
	}
	artifacts, err := upgrades.ArtifactsModel(ctx)
	if err != nil {
		return nil, err
	}
	ready := artifacts.Len() > 0
	for _, record := range result {
		version, present := record.Get("version")
		if !present {
			version = "0.0.0"
		}
		text, valid := version.(string)
		if !valid {
			return nil, fmt.Errorf("invalid Agent version")
		}
		record.Set("has_update", core.HasUpdate(text))
		record.Set("upgrade_available", ready && record.GetString("deployment_method") == "shell")
	}
	return httpx.JSON(result), nil
}
func getContentAgent(c *httpx.Ctx, m *core.Manager) (*httpx.Response, error) {
	record, err := m.GetAgentByID(c.Context(), c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return httpx.Message(404, false, "Agent not found"), nil
	}
	public, err := publicWithContent(c.Context(), m, record, map[string]contentSnapshot{}, true)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(public), nil
}

func registerContentRoutes(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/push-diff", "agents.agent_push_diff", auth.Require(agentPushDiff))
	r.GET("/api/agents/<agent_id>/push-diff/entries", "agents.agent_push_diff_entries", auth.Require(agentPushDiffEntries))
}
func agentPushDiff(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	m, err := core.Get(ctx)
	if err != nil {
		return nil, err
	}
	record, err := m.GetAgentByID(ctx, c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return httpx.Message(404, false, messageContentAgentMissing), nil
	}
	source, err := loadContentSource(ctx, m, record, nil)
	var answer contentsync.Answer
	if err != nil {
		var repositoryError *repo.Error
		if !errors.As(err, &repositoryError) {
			return nil, err
		}
		answer = contentsync.Answer{Result: contentsync.Result{Status: "unknown", Reason: "profile_unavailable"}}
	} else {
		answer = contentChecks.checker(m).PushDiff(ctx, deploy.NewTarget(record), source, contentOrigin(record))
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	service := record.GetString("service_type")
	if _, ok := record.Get("service_type"); !ok {
		service = "mihomo"
	}
	body["service_type"], _ = json.Marshal(service)
	if deploy.HasAppliedContent(deploy.NewTarget(record)) {
		baseline := struct {
			PushedAt any `json:"pushed_at"`
			Revision any `json:"config_revision"`
		}{record.Value("pushed_at"), record.Value("config_revision")}
		body["baseline"], err = json.Marshal(baseline)
		if err != nil {
			return nil, err
		}
	}
	find, err := redact.StoredSecretFinder(ctx)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(pushdiff.WithoutStored(body, find)).NoStore(), nil
}
func agentPushDiffEntries(c *httpx.Ctx) (*httpx.Response, error) {
	q, err := contentPageQuery(c.ArgDefault("version", ""), c.ArgDefault("file", ""), c.ArgDefault("change", ""), c.ArgDefault("offset", ""), c.ArgDefault("limit", ""), c.ArgDefault("q", ""))
	if err != nil {
		return httpx.Message(400, false, messageInvalidContentPage), nil
	}
	ctx := c.Context()
	m, err := core.Get(ctx)
	if err != nil {
		return nil, err
	}
	record, err := m.GetAgentByID(ctx, c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return httpx.Message(404, false, messageContentAgentMissing), nil
	}
	source, err := loadContentSource(ctx, m, record, nil)
	if err != nil {
		var repositoryError *repo.Error
		if !errors.As(err, &repositoryError) {
			return nil, err
		}
		return httpx.JSON(contentsync.EntriesResult{Status: "stale", Reason: "changed"}).NoStore(), nil
	}
	q.Find, err = redact.StoredSecretFinder(ctx)
	if err != nil {
		return nil, err
	}
	page, err := contentChecks.checker(m).Entries(ctx, deploy.NewTarget(record), source, contentOrigin(record), q)
	if errors.Is(err, contentsync.ErrNoEntries) {
		return httpx.Message(404, false, messageContentEntriesMissing), nil
	}
	if err != nil {
		return nil, err
	}
	return httpx.JSON(pushdiff.WithoutStored(page, q.Find)).NoStore(), nil
}
