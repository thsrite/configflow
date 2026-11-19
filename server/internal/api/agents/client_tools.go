package agents

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"

	registry "configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/clientrange"
	"configflow/internal/devicenotes"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

func registerClientTools(r *httpx.Router) {
	r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/client-notes", "agents.client_notes", auth.Require(clientBindingHandler(clientNotes)))
	r.GET("/api/agents/<agent_id>/mihomo/rule-targets", "agents.rule_targets", auth.Require(clientBindingHandler(clientRuleTargets)))
	r.POST("/api/agents/<agent_id>/mihomo/rule-targets/apply", "agents.connection_rule", auth.Require(clientBindingHandler(clientConnectionRule)))
	r.PUT("/api/agents/<agent_id>/domain-discovery", "agents.set_agent_domain_discovery", auth.Require(clientBindingHandler(clientDiscoveryToggle)))
	r.POST("/api/agents/<agent_id>/domain-discovery/clear", "agents.clear_agent_domain_discovery", auth.Require(clientBindingHandler(clientDiscoveryClear)))
	r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/oxidns/device-policies", "agents.oxidns_device_policies", auth.Require(clientBindingHandler(clientPolicies)))
}

// clientTools owns bound-profile edits and discovery cleanup. Its write entry
// points detach request cancellation while retaining server shutdown and values.
type clientTools struct{}

type noteEdit struct {
	ProfileID string
	IP        string
	Name      string
	Previous  string
}
type connectionEdit struct {
	ProfileID string
	RulesetID string
	Target    string
	Value     json.RawMessage
	RuleType  string
}
type policyEdit struct {
	ProfileID     string
	Revision      string
	NotesRevision string
	Policies      json.RawMessage
	BaseURL       string
}

func boundClientProfile(ctx context.Context, id string, mihomoOnly bool) (string, error) {
	agent, err := clientAgent(ctx, id, true)
	if err != nil {
		return "", err
	}
	if agent == nil || (agent.ServiceType != "mihomo" && (mihomoOnly || agent.ServiceType != "oxidns")) {
		return "", repo.NewValidationError(msgClientDevice)
	}
	return agent.ProfileID, nil
}
func checkClientBinding(ctx context.Context, id, profile string, mihomoOnly bool) error {
	current, err := boundClientProfile(ctx, id, mihomoOnly)
	if err != nil {
		return err
	}
	if current != profile {
		return repo.NewInUse(msgClientBinding, nil)
	}
	return nil
}
func clientValidation(message string) error { return repo.NewValidationError(message) }
func rawText(raw json.RawMessage) string    { var s string; _ = jsondecode.Unmarshal(raw, &s); return s }
func stringField(raw json.RawMessage) (string, bool) {
	var s *string
	if jsondecode.Unmarshal(raw, &s) != nil || s == nil {
		return "", false
	}
	return *s, true
}
func bodyFields(c *httpx.Ctx, allowed []string, exact bool) map[string]json.RawMessage {
	fields := clientBody(c)
	if fields == nil {
		return nil
	}
	if exact && len(fields) != len(allowed) {
		return nil
	}
	for key := range fields {
		found := false
		for _, a := range allowed {
			found = found || a == key
		}
		if !found {
			return nil
		}
	}
	return fields
}
func clientNotes(c *httpx.Ctx) (*httpx.Response, error) {
	id := c.Param("agent_id")
	profile, err := boundClientProfile(c.Context(), id, false)
	if err != nil {
		return nil, err
	}
	if c.Method() == "PUT" {
		fields := bodyFields(c, []string{"profile_id", "ip", "name", "previous_name"}, true)
		if fields == nil {
			return nil, clientValidation(msgClientNoteBody)
		}
		if rawText(fields["profile_id"]) != profile {
			return nil, repo.NewInUse(msgClientBinding, nil)
		}
		ip, ok := stringField(fields["ip"])
		if !ok {
			return nil, clientValidation(devicenotes.AddressMessage)
		}
		ip, ok = clientrange.Normalize(ip)
		if !ok {
			return nil, clientValidation(devicenotes.AddressMessage)
		}
		name, ok := stringField(fields["name"])
		if !ok {
			return nil, clientValidation(devicenotes.NameMessage)
		}
		name, err = devicenotes.NormalizeName(name)
		if err != nil {
			return nil, clientValidation(err.Error())
		}
		previous, ok := stringField(fields["previous_name"])
		if !ok {
			return nil, clientValidation(devicenotes.NameMessage)
		}
		previous, err = devicenotes.NormalizeName(previous)
		if err != nil {
			return nil, clientValidation(err.Error())
		}
		if err = (clientTools{}).SaveNote(c.Context(), id, noteEdit{profile, ip, name, previous}); err != nil {
			return nil, err
		}
	}
	payload, err := clientNotesPayload(c.Context(), profile)
	if _, ok := oxidns.IsSettingsError(err); ok {
		return nil, clientValidation(err.Error())
	}
	if err != nil {
		return nil, err
	}
	return httpx.JSON(payload).NoStore(), nil
}

// SaveNote commits a display-only edit after rechecking the binding and previous name.
func (clientTools) SaveNote(ctx context.Context, id string, edit noteEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	return updateClientNote(ctx, id, edit)
}
func clientRuleTargets(c *httpx.Ctx) (*httpx.Response, error) {
	profile, err := boundClientProfile(c.Context(), c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	payload, err := clientTargetsPayload(c.Context(), profile)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(payload).NoStore(), nil
}
func clientConnectionRule(c *httpx.Ctx) (*httpx.Response, error) {
	id := c.Param("agent_id")
	profile, err := boundClientProfile(c.Context(), id, true)
	if err != nil {
		return nil, err
	}
	f := bodyFields(c, []string{"profile_id", "ruleset_id", "target", "value", "rule_type"}, true)
	if f == nil {
		return nil, clientValidation(msgConnectionBody)
	}
	if rawText(f["profile_id"]) != profile {
		return nil, repo.NewInUse(msgClientBinding, nil)
	}
	target, ruleset := rawText(f["target"]), rawText(f["ruleset_id"])
	if (target != "direct" && target != "proxy") || ruleset == "" {
		return nil, clientValidation(msgConnectionTarget)
	}
	edit := connectionEdit{profile, ruleset, target, f["value"], rawText(f["rule_type"])}
	result, err := (clientTools{}).ApplyConnection(c.Context(), id, edit)
	if err != nil {
		var validation *rules.ProfileValidationError
		if errors.As(err, &validation) {
			return nil, clientValidation(err.Error())
		}
		return nil, err
	}
	return httpx.JSON(result).NoStore(), nil
}

// ApplyConnection owns guarded shared-rule adoption and its cache/history writes.
func (clientTools) ApplyConnection(ctx context.Context, id string, edit connectionEdit) (any, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if edit.RuleType == "IP-CIDR" || edit.RuleType == "IP-CIDR6" {
		dest, err := rules.ParseDestination(rawText(edit.Value), edit.RuleType)
		if err != nil {
			return nil, err
		}
		return applyClientIP(ctx, id, edit, dest)
	}
	// Preserve the explicit type, including null/non-string, at the shared parser.
	entry := map[string]any{"target": edit.Target, "value": edit.Value, "rule_type": edit.RuleType}
	raw, _ := json.Marshal([]any{entry})
	entries, err := discovery.ParseApplyItems(raw)
	if err != nil {
		return nil, err
	}
	guard := func(tx context.Context, infos map[string]*discovery.RulesetInfo) error {
		if err := checkClientBinding(tx, id, edit.ProfileID, true); err != nil {
			return err
		}
		settings, err := freshClientDiscovery(tx, edit.ProfileID)
		if err != nil {
			return err
		}
		selected := settings.DirectRuleset
		if edit.Target == "proxy" {
			selected = settings.ProxyRuleset
		}
		if selected == nil || *selected != edit.RulesetID || (infos != nil && (infos[edit.Target] == nil || infos[edit.Target].ID != edit.RulesetID)) {
			return repo.NewInUse(msgConnectionDefaults, nil)
		}
		return nil
	}
	if err := guard(ctx, nil); err != nil {
		return nil, err
	}
	result, err := discovery.ApplyDomains(ctx, edit.ProfileID, entries, "manual", nil, discovery.ApplyOptions{BeforeWrite: guard})
	if err != nil {
		return nil, err
	}
	return struct {
		Success bool `json:"success"`
		discovery.ApplyResult
	}{true, result}, nil
}
func clientDiscoveryToggle(c *httpx.Ctx) (*httpx.Response, error) {
	f := clientBody(c)
	enabled, ok := f["enabled"]
	if !ok || (string(enabled) != "true" && string(enabled) != "false") {
		return httpx.Message(400, false, msgDiscoveryBoolean), nil
	}
	record, err := (clientTools{}).SetDiscovery(c.Context(), c.Param("agent_id"), string(enabled) == "true")
	if err != nil {
		var value *registry.ValueError
		var conflict *registry.DeploymentConflict
		if errors.As(err, &value) {
			return httpx.Message(400, false, err.Error()), nil
		}
		if errors.As(err, &conflict) {
			return httpx.Message(409, false, err.Error()), nil
		}
		return nil, err
	}
	if record == nil {
		return httpx.Message(404, false, "Agent not found"), nil
	}
	public, err := publicAgentModel(record)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool                       `json:"success"`
		Data    map[string]json.RawMessage `json:"data"`
	}{true, public}), nil
}

// SetDiscovery updates the switch and returns the live runtime overlay.
func (clientTools) SetDiscovery(ctx context.Context, id string, enabled bool) (*model.Agent, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manager, err := registry.Get(ctx)
	if err != nil {
		return nil, err
	}
	return manager.SetDomainDiscoveryModel(ctx, id, enabled)
}
func clientDiscoveryClear(c *httpx.Ctx) (*httpx.Response, error) {
	found, err := (clientTools{}).ClearDiscovery(c.Context(), c.Param("agent_id"))
	if err != nil {
		return nil, err
	}
	if !found {
		return httpx.Message(404, false, "Agent not found"), nil
	}
	return httpx.JSON(struct {
		Success bool `json:"success"`
	}{true}), nil
}

// ClearDiscovery deletes only collected traffic, probes and regions.
func (clientTools) ClearDiscovery(ctx context.Context, id string) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return clearClientDiscovery(ctx, id)
}
func policyError(status int, message string) *httpx.Response {
	return httpx.JSONStatus(status, struct {
		Message string `json:"message"`
	}{message}).NoStore()
}
func clientPolicies(c *httpx.Ctx) (*httpx.Response, error) {
	id := c.Param("agent_id")
	agent, err := clientAgent(c.Context(), id, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return policyError(404, msgPolicyMissing), nil
	}
	if agent.ServiceType != "oxidns" {
		return policyError(400, msgPolicyService), nil
	}
	profile := agent.ProfileID
	if c.Method() == "PUT" {
		f := bodyFields(c, []string{"profile_id", "revision", "notes_revision", "policies", "base_url"}, false)
		base := ""
		valid := true
		if b, present := f["base_url"]; present {
			base, valid = stringField(b)
		}
		if f == nil || !valid {
			return policyError(400, msgPolicyBody), nil
		}
		if rawText(f["profile_id"]) != profile {
			return policyError(409, msgPolicyBinding), nil
		}
		err = (clientTools{}).SavePolicies(c.Context(), id, policyEdit{profile, rawText(f["revision"]), rawText(f["notes_revision"]), f["policies"], base})
	}
	if err == nil {
		var payload any
		payload, err = clientPolicyPayload(c.Context(), profile, agent)
		if err == nil {
			return httpx.JSON(payload).NoStore(), nil
		}
	}
	if errors.Is(err, repo.ErrInUse) {
		return policyError(409, err.Error()), nil
	}
	if _, ok := oxidns.IsSettingsError(err); ok || errors.Is(err, repo.ErrRepository) {
		return policyError(400, err.Error()), nil
	}
	return nil, err
}

// SavePolicies commits a draft with binding, settings and notes revision guards.
func (clientTools) SavePolicies(ctx context.Context, id string, edit policyEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	return updateClientPolicies(ctx, id, edit)
}
