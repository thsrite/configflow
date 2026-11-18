package routers

import (
	"encoding/json"

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
	raw, err := releaseRouterRecord(r)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	delete(fields, "last_probe")
	p, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Router map[string]json.RawMessage `json:"router"`
		Oxidns string                     `json:"oxidns"`
	}{fields, oxidns.Revision(p.Settings())})
	if err != nil {
		return "", err
	}
	return ROSPolicyDigest(data)
}

// ReadWorkspace reads policy metadata without contacting the router.
func ReadWorkspace(config json.RawMessage, r *Router, profile, name, defaultProfile string, agents []WorkspaceAgent) (Workspace, error) {
	p, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return Workspace{}, err
	}
	policies, err := oxidns.ReadPolicies(p.Settings(), p)
	if err != nil {
		return Workspace{}, err
	}
	revision, err := WorkspaceRevision(config, r)
	if err != nil {
		return Workspace{}, err
	}
	names, err := oxidns.PolicyNamesRevision(p)
	if err != nil {
		return Workspace{}, err
	}
	out := Workspace{ProfileID: profile, ProfileName: name, Revision: revision, NamesRevision: names, Router: r.View(), Sources: WorkspaceSources{policies, oxidns.ToJSON(policies.Policies)}, SourceAgents: []WorkspacePeer{}, Gateways: []WorkspaceGateway{}, RuleSets: []WorkspaceRule{}, OxidnsRevision: oxidns.Revision(p.Settings())}
	for _, a := range agents {
		n := orText(a.Name, a.ID)
		if a.ServiceType == "oxidns" && orText(a.ProfileID, defaultProfile) == profile {
			out.SourceAgents = append(out.SourceAgents, WorkspacePeer{a.ID, n, a.Host, orText(a.Status, "offline")})
		}
		if a.ServiceType == "mihomo" {
			out.Gateways = append(out.Gateways, WorkspaceGateway{a.ID, n, a.Host})
		}
	}
	var doc struct {
		Rules []struct {
			WorkspaceRule
			ItemType string `json:"itemType"`
		} `json:"rule_configs"`
	}
	if err = json.Unmarshal(config, &doc); err != nil {
		return Workspace{}, err
	}
	for _, rule := range doc.Rules {
		if rule.ItemType == "ruleset" {
			rule.Name = orText(rule.Name, rule.ID)
			out.RuleSets = append(out.RuleSets, rule.WorkspaceRule)
		}
	}
	return out, nil
}

// SaveWorkspace changes the router and optional source policies atomically in
// the caller's transaction document. Unknown configuration fields are retained.
func SaveWorkspace(config json.RawMessage, id string, body WorkspaceRequest) (json.RawMessage, error) {
	r := findSyncRouter(config, id)
	if r == nil || r.Type != "routeros" {
		return nil, rosError(KindConflict, messageWorkspaceStale)
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
		return nil, rosError(KindConflict, messageWorkspaceStale)
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
