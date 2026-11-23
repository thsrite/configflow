package routers

import (
	"configflow/internal/routers/device"
	"encoding/json"

	"configflow/internal/model"
	"configflow/internal/oxidns"
)

// WorkspaceAgent is the registry projection needed by the policy editor.
type WorkspaceAgent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Status      string `json:"status"`
	ServiceType string `json:"service_type"`
	ProfileID   string `json:"profile_id"`
}

// WorkspacePeer exposes no transport credentials.
type WorkspacePeer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	Status string `json:"status"`
}

// WorkspaceGateway is a Mihomo selection, including devices bound elsewhere.
type WorkspaceGateway struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

// WorkspaceRule is a selectable rule set.
type WorkspaceRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Policy        string `json:"policy"`
	LibraryRuleID string `json:"library_rule_id"`
}

// Workspace holds one profile draft and its optimistic edit revisions.
type Workspace struct {
	ProfileID      string             `json:"profile_id"`
	ProfileName    string             `json:"profile_name"`
	Revision       string             `json:"revision"`
	NamesRevision  string             `json:"names_revision"`
	Router         RouterView         `json:"router"`
	Sources        WorkspaceSources   `json:"sources"`
	SourceAgents   []WorkspacePeer    `json:"source_agents"`
	Gateways       []WorkspaceGateway `json:"gateways"`
	RuleSets       []WorkspaceRule    `json:"rule_sets"`
	OxidnsRevision string             `json:"oxidns_revision"`
	ConfigRevision int64              `json:"config_revision"`
}

// WorkspaceSources keeps C26's complete policy projection, including rows.
type WorkspaceSources struct {
	oxidns.PolicyView
	Policies json.RawMessage `json:"policies"`
}

// WorkspaceRequest is a checked draft edit; Policies nil means router-only.
type WorkspaceRequest struct {
	Revision      string          `json:"revision"`
	NamesRevision string          `json:"names_revision"`
	Router        json.RawMessage `json:"router"`
	Policies      json.RawMessage `json:"policies"`
	BaseURL       string          `json:"base_url"`
}

// WorkspaceRevision omits probe-only changes and uses D2 native JSON encoding.
func WorkspaceRevision(config json.RawMessage, r *Router) (string, error) {
	p, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return "", err
	}
	return workspaceRevision(oxidns.Revision(p.Settings()), r)
}

// workspaceRevision digests the router record with the OxiDNS settings
// revision oxidnsRevision.
func workspaceRevision(oxidnsRevision string, r *Router) (string, error) {
	raw, err := releaseRouterRecord(r)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	delete(fields, "last_probe")
	data, err := json.Marshal(struct {
		Router map[string]json.RawMessage `json:"router"`
		Oxidns string                     `json:"oxidns"`
	}{fields, oxidnsRevision})
	if err != nil {
		return "", err
	}
	return ROSPolicyDigest(data)
}

// ReadWorkspace reads policy metadata without contacting the router. config
// is a borrowed resolved view and is only read.
func ReadWorkspace(config *model.Config, r *Router, profile, name, defaultProfile string, agents []WorkspaceAgent) (Workspace, error) {
	p, err := oxidns.NewTypedProfile(config)
	if err != nil {
		return Workspace{}, err
	}
	policies, err := oxidns.ReadPolicies(p.Settings(), p)
	if err != nil {
		return Workspace{}, err
	}
	settingsRevision := oxidns.Revision(p.Settings())
	revision, err := workspaceRevision(settingsRevision, r)
	if err != nil {
		return Workspace{}, err
	}
	names, err := oxidns.PolicyNamesRevision(p)
	if err != nil {
		return Workspace{}, err
	}
	out := Workspace{ProfileID: profile, ProfileName: name, Revision: revision, NamesRevision: names, Router: r.View(), Sources: WorkspaceSources{policies, oxidns.ToJSON(policies.Policies)}, SourceAgents: []WorkspacePeer{}, Gateways: []WorkspaceGateway{}, RuleSets: []WorkspaceRule{}, OxidnsRevision: settingsRevision}
	for _, a := range agents {
		n := device.OrText(a.Name, a.ID)
		if a.ServiceType == "oxidns" && device.OrText(a.ProfileID, defaultProfile) == profile {
			out.SourceAgents = append(out.SourceAgents, WorkspacePeer{a.ID, n, a.Host, device.OrText(a.Status, "offline")})
		}
		if a.ServiceType == "mihomo" {
			out.Gateways = append(out.Gateways, WorkspaceGateway{a.ID, n, a.Host})
		}
	}
	rules, err := workspaceRuleSets(config.Profile)
	if err != nil {
		return Workspace{}, err
	}
	out.RuleSets = append(out.RuleSets, rules...)
	return out, nil
}

// workspaceRuleSets lists the profile's ruleset rows from their typed fields.
func workspaceRuleSets(profile *model.Profile) ([]WorkspaceRule, error) {
	var out []WorkspaceRule
	for i := range profile.RuleConfigs {
		rule := &profile.RuleConfigs[i]
		if rule.ItemType != "ruleset" {
			continue
		}
		row := WorkspaceRule{ID: rule.ID, Name: device.OrText(rule.Name, rule.ID), LibraryRuleID: rule.LibraryRuleID}
		if rule.Policy != nil {
			row.Policy = *rule.Policy
		}
		out = append(out, row)
	}
	return out, nil
}

// SaveWorkspace changes the router and optional source policies atomically in
// the caller's transaction document. Unknown configuration fields are retained.
func SaveWorkspace(config json.RawMessage, id string, body WorkspaceRequest) (json.RawMessage, error) {
	r := findSyncRouter(config, id)
	if r == nil || r.Type != "routeros" {
		return nil, device.KindError(KindConflict, messageWorkspaceStale)
	}
	rev, err := WorkspaceRevision(config, r)
	if err != nil {
		return nil, err
	}
	p, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return nil, err
	}
	names, err := oxidns.PolicyNamesRevision(p)
	if err != nil {
		return nil, err
	}
	if rev != body.Revision || body.Policies != nil && names != body.NamesRevision {
		return nil, device.KindError(KindConflict, messageWorkspaceStale)
	}
	updated, err := Normalize(body.Router, r)
	if err != nil {
		return nil, err
	}
	config, err = replaceManagementRouter(config, id, updated)
	if err != nil {
		return nil, err
	}
	return saveWorkspacePolicies(config, body.Policies, body.BaseURL)
}
