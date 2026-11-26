package agents

// This file captures typed Agent records at the HTTP boundary; comparison,
// paging and scheduling use the typed deploy/contentsync interfaces.
import (
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
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

type contentSnapshot struct {
	source *deploy.Source
	err    error
}

// contentSources remembers the content sources of one request and, through
// the derived view cache, of the registry generation that request read.
type contentSources struct {
	local map[string]contentSnapshot
	gen   core.Generation
	// the generation's stored Agent credentials, read once per request
	stored     redact.Secrets
	storedErr  error
	storedRead bool
}

func loadContentSource(ctx context.Context, m *core.Manager, record *model.Agent, cache *contentSources) (*deploy.Source, error) {
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
	if cache == nil {
		saved := readContentSource(ctx, m, profile)
		return saved.source, saved.err
	}
	if saved, ok := cache.local[profile]; ok {
		return saved.source, saved.err
	}
	saved := views.source(m.Repository(), cache.gen, profile, func() contentSnapshot { return readContentSource(ctx, m, profile) })
	cache.local[profile] = saved
	return saved.source, saved.err
}

func readContentSource(ctx context.Context, m *core.Manager, profile string) contentSnapshot {
	config, err := m.Repository().Config(ctx, profile, nil)
	if err != nil {
		return contentSnapshot{nil, err}
	}
	// The borrowed Config is an immutable snapshot: the source keeps it as its
	// typed view instead of encoding and decoding it again.
	source, err := deploy.SourceFromConfig(config, nil)
	return contentSnapshot{source, err}
}
func contentOrigin(record *model.Agent) string {
	if record.AppliedContent == nil {
		return ""
	}
	return record.AppliedContent.BaseURL
}

// publicWithContent is the public view of a live record read from gen at
// position index, with its content sync status.
func publicWithContent(ctx context.Context, m *core.Manager, record *model.Agent, index int, cache *contentSources, history bool) (map[string]json.RawMessage, error) {
	var gen core.Generation // nil cache: nothing is cached
	if cache != nil {
		gen = cache.gen
	}
	public, err := views.public(m.Repository(), gen, viewKey{index, record.ID, viewFull}, record, func() (map[string]json.RawMessage, error) {
		secrets, err := liveAgentSecrets(ctx, m, cache, record)
		if err != nil {
			return nil, err
		}
		return publicAgentView(record, secrets)
	})
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

type contentHandler struct {
	deps Deps
}

func (h *contentHandler) listContentAgents(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	m, err := h.deps.manager(ctx)
	if err != nil {
		return nil, err
	}
	summary := c.ArgDefault("summary", "") == "1"
	records, gen, err := m.ReadAgentsAt(ctx, !summary, nil)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]json.RawMessage, 0, len(records))
	for i := range records {
		if !core.FreshHeartbeat(records[i].LastHeartbeat, nil) {
			records[i].Status = "offline"
		}
	}
	repository := m.Repository()
	if summary {
		request := &contentSources{gen: gen}
		for i := range records {
			record := &records[i]
			public, err := views.public(repository, gen, viewKey{i, record.ID, viewSummary}, record, func() (map[string]json.RawMessage, error) {
				secrets, err := liveAgentSecrets(ctx, m, request, record)
				if err != nil {
					return nil, err
				}
				return publicAgentView(record, secrets)
			})
			if err != nil {
				return nil, err
			}
			result = append(result, public)
		}
		return httpx.JSON(result), nil
	}
	history := c.ArgDefault("history", "") != "0"
	sources := &contentSources{local: map[string]contentSnapshot{}, gen: gen}
	for i := range records {
		public, err := publicWithContent(ctx, m, &records[i], i, sources, history)
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
func (h *contentHandler) getContentAgent(c *httpx.Ctx, m *core.Manager) (*httpx.Response, error) {
	record, index, gen, err := m.ReadAgentAt(c.Context(), c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return httpx.Message(404, false, "Agent not found"), nil
	}
	public, err := publicWithContent(c.Context(), m, record, index, &contentSources{local: map[string]contentSnapshot{}, gen: gen}, true)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(public), nil
}

func (h *contentHandler) register(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/push-diff", "agents.agent_push_diff", auth.Require(h.agentPushDiff))
	r.GET("/api/agents/<agent_id>/push-diff/entries", "agents.agent_push_diff_entries", auth.Require(h.agentPushDiffEntries))
}
func (h *contentHandler) agentPushDiff(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	m, err := h.deps.manager(ctx)
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
	if service == "" {
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
	find, err := redact.StoredSecretFinder(ctx, m.Repository())
	if err != nil {
		return nil, err
	}
	return httpx.JSON(pushdiff.WithoutStored(body, find)).NoStore(), nil
}
func (h *contentHandler) agentPushDiffEntries(c *httpx.Ctx) (*httpx.Response, error) {
	q, err := contentPageQuery(c.ArgDefault("version", ""), c.ArgDefault("file", ""), c.ArgDefault("change", ""), c.ArgDefault("offset", ""), c.ArgDefault("limit", ""), c.ArgDefault("q", ""))
	if err != nil {
		return httpx.Message(400, false, messageInvalidContentPage), nil
	}
	ctx := c.Context()
	m, err := h.deps.manager(ctx)
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
	q.Find, err = redact.StoredSecretFinder(ctx, m.Repository())
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
	raw, present, err := model.EncodeMember(record, key)
	if err != nil || !present {
		return json.RawMessage("null")
	}
	return raw
}
