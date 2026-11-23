package routers

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/device"
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
	if v.DNSGuard != nil && v.DNSGuard.Stored == nil {
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
	if v.IPv6PDGuard != nil && v.IPv6PDGuard.Stored == nil {
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
	// A damaged guard (kept whole by the model, Stored) cannot authorize
	// device management and is not projected; the stored record keeps it for
	// repair by the settings owner.

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
			if text, valid := device.RequestString(raw); !valid || text == model.DamagedText {
				return device.Invalid(messageRouterFieldType)
			}
		}
	}
	for _, name := range []string{"ip_rule_sets", "ros_policy_rule_sets"} {
		if raw, present := fields[name]; present {
			if _, valid := idList(raw); !valid {
				return device.Invalid(messageRouterFieldType)
			}
		}
	}
	return nil
}

func pendingProfiles(ctx context.Context, db *repo.Repository) ([]pendingProfile, error) {
	out := []pendingProfile{}
	err := db.View(ctx, func(d *model.Document) error {
		for _, p := range d.Profiles {
			out = append(out, pendingProfile{ID: p.Key(), Name: p.Name, Revision: device.JSON(int64(p.Revision) + int64(d.Shared.Revision))})
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
		return device.Invalid(messageRouterFieldType)
	}
	if err := r.validateIdentity(); err != nil {
		return err
	}
	if r.stored == nil {
		return nil
	}
	// The model keeps an enabled guard with a malformed member, or a guard of
	// another JSON type, whole (Stored). A falsy value is no guard, as in
	// Python (`or {}`); anything else is unknown settings: fail closed.
	for _, stored := range []json.RawMessage{dnsGuardStored(r.stored.DNSGuard), pdGuardStored(r.stored.IPv6PDGuard)} {
		if stored != nil && device.HasStoredState(stored) {
			return device.Invalid(messageStoredGuardInvalid)
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
	text, _ := device.RequestString(fields[member])
	return text
}

func dnsGuardStored(g *model.RouterDNSGuard) json.RawMessage {
	if g == nil {
		return nil
	}
	return g.Stored
}

func pdGuardStored(g *model.RouterPDGuard) json.RawMessage {
	if g == nil {
		return nil
	}
	return g.Stored
}
