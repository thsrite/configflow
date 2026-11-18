package routers

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"encoding/json"
)

// routerFromModel is a narrow runtime projection. The shared codec owns scalar
// rules and raw provenance; only router-owned settings get runtime defaults.
func routerFromModel(v model.Router) Router {
	r := Router{RouterSettings: RouterSettings{ID: v.ID, Name: v.Name, Type: v.Type, Endpoint: v.Endpoint, Username: v.Username, ProxyGatewayAgentID: v.ProxyGatewayAgentID, DNSAgentID: v.DNSAgentID, IPRuleSets: v.IPRuleSets, RosPolicyRuleSets: v.ROSPolicyRuleSets, VerifyTLS: true, RuleSyncInterval: 60}, Password: v.Password, Extra: map[string]json.RawMessage{}}
	if v.VerifyTLS != nil {
		r.VerifyTLS = *v.VerifyTLS
	}
	if v.RuleSyncInterval != nil {
		r.RuleSyncInterval = int(*v.RuleSyncInterval)
	}
	if v.ManageReturnRouting != nil {
		r.ManageReturnRouting = *v.ManageReturnRouting
	}
	if v.AutoRuleSync != nil {
		r.AutoRuleSync = *v.AutoRuleSync
	}
	if v.DNSGuard != nil {
		g := DefaultDNSGuard()
		if v.DNSGuard.Enabled != nil {
			g.Enabled = *v.DNSGuard.Enabled
		}
		if v.DNSGuard.BlockDoT != nil {
			g.BlockDoT = *v.DNSGuard.BlockDoT
		}
		if v.DNSGuard.Failover != nil {
			g.Failover = *v.DNSGuard.Failover
		}
		if v.DNSGuard.Bypass != nil {
			g.Bypass = v.DNSGuard.Bypass
		}
		r.DNSGuard = &g
	}
	if v.IPv6PDGuard != nil {
		g := PDGuard{}
		if v.IPv6PDGuard.Enabled != nil {
			g.Enabled = *v.IPv6PDGuard.Enabled
		}
		r.IPv6PDGuard = &g
	}
	raw, err := model.Marshal(&v)
	if err != nil {
		return r
	}
	_ = json.Unmarshal(raw, &r.Extra)
	// Selection IDs are identities, not display text. A numeric member cannot
	// become the name of an actual rule; retain its raw field and use R5 fallback.
	if value, present := r.Extra["ip_rule_sets"]; present {
		if ids, valid := idList(value); valid {
			r.IPRuleSets = ids
		} else {
			r.IPRuleSets = nil
		}
	}
	if value, present := r.Extra["ros_policy_rule_sets"]; present {
		if ids, valid := idList(value); valid {
			r.RosPolicyRuleSets = ids
		} else {
			r.RosPolicyRuleSets = nil
		}
	}
	// A partially invalid guard cannot authorize device management. The stored
	// record still preserves every nested field for repair by the settings owner.
	issues := model.Issues(v)
	if model.IssueUnder(issues, "dns_guard") {
		r.DNSGuard = nil
	}
	if model.IssueUnder(issues, "ipv6_pd_guard") {
		r.IPv6PDGuard = nil
	}

	for _, key := range append([]string{"id", "password"}, EditableFields...) {
		delete(r.Extra, key)
	}
	r.source = raw
	r.stored = &v
	r.baseline, _ = json.Marshal(struct {
		RouterSettings
		Password string `json:"password"`
	}{r.RouterSettings, r.Password})
	return r
}

// routerProfiles reads only the profile identity and router subtree. It never
// copies unrelated nodes, YAML, or rule-library data per router.
func routerProfiles(ctx context.Context, db *repo.Repository) ([]routerProfile, error) {
	out := []routerProfile{}
	err := db.View(ctx, func(d *model.Document) error {
		for _, p := range d.Profiles {
			row := routerProfile{ID: p.Key(), Name: p.Name}
			for _, v := range p.Routers.Items {
				r := routerFromModel(v)
				row.Routers = append(row.Routers, &r)
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}

type routerProfile struct {
	ID, Name string
	Routers  []*Router
}

func (r *Router) validateIdentity() error {
	if len(r.source) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(r.source, &fields); err != nil {
		return err
	}
	for _, name := range []string{"id", "type", "endpoint", "username", "password", "proxy_gateway_agent_id", "dns_agent_id"} {
		if raw, ok := fields[name]; ok {
			if (name == "proxy_gateway_agent_id" || name == "dns_agent_id") && string(raw) == "null" {
				continue
			}
			if _, valid := requestString(raw); !valid {
				return invalid(messageRouterFieldType)
			}
		}
	}
	for _, name := range []string{"ip_rule_sets", "ros_policy_rule_sets"} {
		if raw, present := fields[name]; present {
			if _, valid := idList(raw); !valid {
				return invalid(messageRouterFieldType)
			}
		}
	}
	return nil
}

func pendingProfiles(ctx context.Context, db *repo.Repository) ([]pendingProfile, error) {
	out := []pendingProfile{}
	err := db.View(ctx, func(d *model.Document) error {
		for _, p := range d.Profiles {
			out = append(out, pendingProfile{ID: p.Key(), Name: p.Name, Revision: mustPolicyJSON(int64(p.Revision) + int64(d.Shared.Revision))})
		}
		return nil
	})
	return out, err
}

// ValidateForDevice checks original consumed identities and active guard settings.
// Settings PUT/DELETE deliberately do not call it: corrupt fields must remain
// repairable and removable through the UI under the normal job lock.
func (r *Router) ValidateForDevice() error {
	if r == nil {
		return invalid(messageRouterFieldType)
	}
	if err := r.validateIdentity(); err != nil {
		return err
	}
	if r.stored == nil {
		return nil
	}
	issues := model.Issues(r.stored)
	for _, guard := range []struct{ setting, state string }{{"dns_guard", "dns_guard_state"}, {"ipv6_pd_guard", "ipv6_pd_guard_state"}} {
		if !model.IssueUnder(issues, guard.setting) {
			continue
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(r.source, &fields)
		var switches struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.Unmarshal(fields[guard.setting], &switches)
		if switches.Enabled || hasStoredState(r.Extra[guard.state]) {
			return invalid(messageStoredGuardInvalid)
		}
	}
	return nil
}

// LinkID returns a stored Agent link only when its original token is text.
// Optional missing/null/malformed links are absent, never fabricated text IDs.
func (r *Router) LinkID(member string) string {
	if r == nil {
		return ""
	}
	fallback := ""
	switch member {
	case "proxy_gateway_agent_id":
		fallback = r.ProxyGatewayAgentID
	case "dns_agent_id":
		fallback = r.DNSAgentID
	default:
		return ""
	}
	if len(r.source) == 0 || r.explicit[member] {
		return fallback
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.source, &fields) != nil {
		return ""
	}
	text, _ := requestString(fields[member])
	return text
}
