package routers

import (
	"configflow/internal/routers/spec"
	"encoding/json"
	"errors"

	"configflow/internal/httpx"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	core "configflow/internal/routers"
	"configflow/internal/yamlx"
)

func workspaceRequest(raw json.RawMessage) (core.WorkspaceRequest, bool) {
	values, ok := fields(raw)
	if !ok || (!exact(values, "revision", "router", "base_url") && !exact(values, "revision", "names_revision", "router", "policies", "base_url")) {
		return core.WorkspaceRequest{}, false
	}
	router, ok := fields(values["router"])
	if !ok || !exact(router, "ip_rule_sets", "ros_policy_rule_sets", "proxy_gateway_agent_id") {
		return core.WorkspaceRequest{}, false
	}
	base, ok := stringValue(values["base_url"])
	if !ok {
		return core.WorkspaceRequest{}, false
	}
	revision, _ := stringValue(values["revision"])
	names, _ := stringValue(values["names_revision"])
	return core.WorkspaceRequest{Revision: revision, NamesRevision: names, Router: values["router"], Policies: values["policies"], BaseURL: base}, true
}
func (h *Handler) workspace(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := h.selectedConfig(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	if r.Type != "routeros" {
		return httpx.Message(400, false, messagePolicyOnly), nil
	}
	return repositoryFailure(c, cfg)(h.workspaceBody(c, s, cfg, r))
}

// repositoryFailure maps ProfileRepositoryError, which Python policy_settings
// catches as 400 together with the router validation errors.
func repositoryFailure(c *httpx.Ctx, cfg *core.ConfigView) func(*httpx.Response, error) (*httpx.Response, error) {
	return func(response *httpx.Response, err error) (*httpx.Response, error) {
		var stored *repo.Error
		if !errors.As(err, &stored) {
			return response, err
		}
		return public(c, cfg, struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{false, stored.Error()}, 400, false)
	}
}
func (h *Handler) workspaceBody(c *httpx.Ctx, s *core.ManagementService, cfg *core.ConfigView, r *spec.Router) (*httpx.Response, error) {
	var err error
	if c.Method() == "PUT" {
		body, ok := workspaceRequest(body(c))
		if !ok {
			return httpx.Message(400, false, messageWorkspaceFormat), nil
		}
		if err = s.SavePolicyWorkspace(c.Context(), c.ProfileID, r, body); err != nil {
			return failure(c, cfg, err, 0, false)
		}
		_, cfg, r, err = h.selectedConfig(c)
		if err != nil {
			return nil, err
		}
		if r == nil {
			return notFound()
		}
	}
	peers, defaultProfile, err := h.workspacePeers(c, s.Repo)
	if err != nil {
		return nil, err
	}
	name, err := profileName(c.Context(), s.Repo, c.ProfileID)
	if err != nil {
		return nil, err
	}
	value, err := core.ReadWorkspace(cfg.Config, r, c.ProfileID, name, defaultProfile, peers)
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	value.ConfigRevision, err = s.Repo.ConfigRevision(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	return public(c, cfg, struct {
		Success   bool           `json:"success"`
		Workspace core.Workspace `json:"workspace"`
	}{true, value}, 200, true)
}
func (h *Handler) previewWorkspace(c *httpx.Ctx) (*httpx.Response, error) {
	values, ok := fields(body(c))
	targets, validTargets := previewTargets(values["targets"])
	if !ok || !validTargets {
		return httpx.Message(400, false, messagePreviewFormat), nil
	}
	seen := map[string]bool{}
	for _, id := range targets {
		if seen[id] {
			return httpx.Message(400, false, messagePreviewFormat), nil
		}
		seen[id] = true
	}
	base := ""
	if raw, exists := values["base_url"]; exists {
		base, ok = stringValue(raw)
		if !ok {
			return httpx.Message(400, false, messagePreviewFormat), nil
		}
	}
	s, err := h.service(c)
	if err != nil {
		return nil, err
	}
	cfg, revision, err := publishSnapshot(c.Context(), s.Repo, c.ProfileID)
	if err != nil {
		return nil, err
	}
	view := &core.ConfigView{Credentials: redact.CollectCredentials(cfg, nil)}
	var r *spec.Router
	for _, row := range selectedRouters(cfg) {
		if row.ID == c.Param("router_id") {
			r = row
			break
		}
	}
	if r == nil {
		return notFound()
	}
	if r.Type != "routeros" {
		return httpx.Message(400, false, messagePolicyOnly), nil
	}
	expected, _ := stringValue(values["revision"])
	actual, err := core.WorkspaceRevision(cfg, r)
	if err != nil {
		return nil, err
	}
	if expected != actual {
		return httpx.Message(409, false, messageDraftStale), nil
	}
	peers, _, err := h.workspacePeers(c, s.Repo)
	if err != nil {
		return nil, err
	}
	byID := map[string]core.WorkspacePeer{}
	for _, a := range peers {
		if a.ServiceType == "oxidns" && a.ProfileID == c.ProfileID {
			status := a.Status
			if status == "" {
				status = "offline"
			}
			byID[a.ID] = core.WorkspacePeer{ID: a.ID, Name: a.Name, Host: a.Host, Status: status}
		}
	}
	selected := []core.WorkspacePeer{}
	for _, id := range targets {
		peer, exists := byID[id]
		if !exists {
			return httpx.Message(409, false, messageTargetsChanged), nil
		}
		selected = append(selected, peer)
	}
	d, inspection, err := h.inspectRouter(c, s.Repo, cfg, r)
	if d != nil {
		defer d.Close()
	}
	if err != nil {
		return failure(c, view, err, 400, false)
	}
	plan, err := core.BuildPlan(c.Context(), r, inspection, d)
	if err != nil {
		return failure(c, view, err, 400, false)
	}
	p, err := oxidns.ProfileFromJSON(cfg)
	if err != nil {
		return nil, err
	}
	content := ""
	if len(targets) > 0 {
		content, err = oxidns.Generate(p, base)
		if err != nil {
			return failure(c, view, err, 400, false)
		}
		node, err := yamlx.Parse([]byte(content))
		if err != nil {
			return nil, err
		}
		if _, err = oxidns.RequireValidDocument(node); err != nil {
			return failure(c, view, err, 400, false)
		}
		encoded, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		secrets, err := redact.ExternalPayloadSecrets(c.Context(), nil)
		if err == nil {
			secrets, err = redact.AgentRecordCredentials(c.Context(), secrets)
		}
		if err != nil {
			return nil, err
		}
		safe := redact.SanitizeCredentials(json.RawMessage(encoded), redact.CollectCredentials(cfg, secrets))
		node, err = yamlx.FromJSON(safe)
		if err != nil {
			return nil, err
		}
		encoded, err = yamlx.Marshal(node)
		if err != nil {
			return nil, err
		}
		content = string(encoded)
	}
	current, err := s.Repo.ConfigRevision(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	if current != revision {
		return httpx.Message(409, false, messagePreviewChanged), nil
	}
	return public(c, view, map[string]any{"success": true, "preview": map[string]any{"revision": expected, "profile_id": c.ProfileID, "config_revision": revision, "oxidns_revision": oxidns.Revision(p.Settings()), "router_plan": plan, "dns_content": content, "targets": selected}}, 200, true)
}

// selectedRouters delegates to the shared R5 decoder via the management view.
// The snapshot is already resolved; this export avoids independent decoder rules.
func selectedRouters(cfg json.RawMessage) []*spec.Router { return core.ManagementRouters(cfg) }

func previewTargets(raw json.RawMessage) ([]string, bool) {
	var rows *[]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return nil, false
	}
	result := make([]string, 0, len(*rows))
	for _, row := range *rows {
		value, ok := stringValue(row)
		if !ok {
			return nil, false
		}
		result = append(result, value)
	}
	return result, true
}
