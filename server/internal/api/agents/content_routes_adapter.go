package agents

// This file captures typed Agent records at the HTTP boundary; comparison,
// paging and scheduling use the typed deploy/contentsync interfaces.
import (
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"

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

func loadContentSource(ctx context.Context, m *core.Manager, record *model.Agent, cache map[string]contentSnapshot) (*deploy.Source, error) {
	profile, err := m.ResolveAgentProfile(ctx, record)
	if err != nil {
		return nil, err
	}
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
	config, err := m.Repository().Config(ctx, profile, nil)
	var source *deploy.Source
	if err == nil {
		raw, marshalErr := model.Marshal(config)
		if marshalErr != nil {
			err = marshalErr
		} else {
			source, err = deploy.SourceFromJSON(raw, nil)
		}
	}
	if cache != nil {
		cache[profile] = contentSnapshot{source, err}
	}
	return source, err
}
func contentOrigin(record *model.Agent) string {
	if record.AppliedContent == nil {
		return ""
	}
	return record.AppliedContent.BaseURL
}
func publicWithContent(ctx context.Context, m *core.Manager, record *model.Agent, cache map[string]contentSnapshot, history bool) (map[string]json.RawMessage, error) {
	public, err := publicAgentModel(record)
	if err != nil {
		return nil, err
	}
	if !history {
		for _, field := range repo.AgentHistoryFields {
			delete(public, field)
		}
	}
	source, err := loadContentSource(ctx, m, record, cache)
	if err != nil {
		var repositoryError *repo.Error
		if !errors.As(err, &repositoryError) {
			return nil, err
		}
		public["content_sync"], _ = json.Marshal(contentsync.Result{Status: "unknown", Reason: "profile_unavailable"})
		return public, nil
	}
	// record is a borrowed registry record (or the caller's own); the checker
	// snapshots the target before any background comparison.
	target, err := deploy.BorrowedTarget(record)
	if err != nil {
		return nil, err
	}
	public["content_sync"], err = json.Marshal(contentChecks.checker(m).Status(ctx, target, source, contentOrigin(record)))
	return public, err
}
func listContentAgents(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	m, err := core.Get(ctx)
	if err != nil {
		return nil, err
	}
	summary := c.ArgDefault("summary", "") == "1"
	var secrets []redact.Secrets
	var inspect func(*model.Agent)
	var inspectErr error
	if summary {
		inspect = func(record *model.Agent) {
			raw, err := model.Marshal(record)
			if err != nil {
				inspectErr = err
			}
			secrets = append(secrets, redact.CollectCredentials(json.RawMessage(raw), nil))
		}
	}
	records, err := m.ReadAgents(ctx, !summary, inspect)
	if inspectErr != nil {
		return nil, inspectErr
	}
	if err != nil {
		return nil, err
	}
	result := make([]map[string]json.RawMessage, 0, len(records))
	for i := range records {
		if !core.FreshHeartbeat(records[i].LastHeartbeat, nil) {
			if err := records[i].SetJSON("status", json.RawMessage(`"offline"`)); err != nil {
				return nil, err
			}
		}
	}
	if summary {
		for i, record := range records {
			public, err := publicAgentWithCredentialsModel(&record, secrets[i])
			if err != nil {
				return nil, err
			}
			result = append(result, public)
		}
		return httpx.JSON(result), nil
	}
	history := c.ArgDefault("history", "") != "0"
	snapshots := map[string]contentSnapshot{}
	for _, record := range records {
		public, err := publicWithContent(ctx, m, &record, snapshots, history)
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
		text := "0.0.0"
		raw, present := record["version"]
		valid := !present || len(raw) > 0 && raw[0] == '"' && jsondecode.Unmarshal(raw, &text) == nil
		record["has_update"], _ = json.Marshal(valid && core.HasUpdate(text))
		var method string
		_ = jsondecode.Unmarshal(record["deployment_method"], &method)
		record["upgrade_available"], _ = json.Marshal(ready && method == "shell")
	}

	return httpx.JSON(result), nil
}
func getContentAgent(c *httpx.Ctx, m *core.Manager) (*httpx.Response, error) {
	record, err := m.ReadAgent(c.Context(), c.Param("agent_id"), true)
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
	record, err := m.ReadAgent(ctx, c.Param("agent_id"), true)
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
		target, targetErr := deploy.NewTypedTarget(record)
		if targetErr != nil {
			return nil, targetErr
		}
		answer = contentChecks.checker(m).PushDiff(ctx, target, source, contentOrigin(record))
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err = jsondecode.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	service := record.ServiceType
	if _, ok, _ := record.MemberJSON("service_type"); !ok {
		service = "mihomo"
	}
	body["service_type"], _ = json.Marshal(service)
	target, targetErr := deploy.NewTypedTarget(record)
	if targetErr != nil {
		return nil, targetErr
	}
	if deploy.HasAppliedContent(target) {
		baseline := struct {
			PushedAt any `json:"pushed_at"`
			Revision any `json:"config_revision"`
		}{rawAgentMember(record, "pushed_at"), rawAgentMember(record, "config_revision")}
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
	record, err := m.ReadAgent(ctx, c.Param("agent_id"), true)
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
	target, targetErr := deploy.NewTypedTarget(record)
	if targetErr != nil {
		return nil, targetErr
	}
	page, err := contentChecks.checker(m).Entries(ctx, target, source, contentOrigin(record), q)
	if errors.Is(err, contentsync.ErrNoEntries) {
		return httpx.Message(404, false, messageContentEntriesMissing), nil
	}
	if err != nil {
		return nil, err
	}
	return httpx.JSON(pushdiff.WithoutStored(page, q.Find)).NoStore(), nil
}

func rawAgentMember(record *model.Agent, key string) json.RawMessage {
	raw, present, err := record.MemberJSON(key)
	if err != nil || !present {
		return json.RawMessage("null")
	}
	return raw
}
