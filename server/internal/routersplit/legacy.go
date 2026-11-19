package routersplit

// Only unmigrated Agent/setup/gateway APIs cross this codec. Business decisions
// use model.Profile, Context and YAML nodes; no Python dynamic operations live here.
import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"reflect"

	"configflow/internal/gateway/gwbase"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func legacyOutput(v any) *jsonx.Object {
	b, _ := json.Marshal(v)
	var o jsonx.Object
	_ = json.Unmarshal(b, &o)
	return &o
}
func legacyProfile(v any) *model.Profile {
	switch p := v.(type) {
	case *model.Profile:
		return p
	case *model.Config:
		return p.Profile
	}
	b, err := json.Marshal(v)
	p := &model.Profile{}
	if err == nil {
		_ = json.Unmarshal(b, p)
	}
	return p
}
func legacyContextValue(v any) *Context {
	b, _ := json.Marshal(v)
	c := &Context{}
	_ = json.Unmarshal(b, c)
	return c
}
func legacyNode(v any) (*yamlx.Node, error) {
	if n, ok := v.(*yamlx.Node); ok {
		return n, nil
	}
	return yamlbridge.Encode(v)
}
func legacyNone(v any) bool {
	return v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()
}
func legacyStrings(v []string) []any {
	out := []any{}
	for _, s := range v {
		out = append(out, s)
	}
	return out
}

// FullTun is the legacy object projection of the native full-TUN document.
func FullTun() *jsonx.Object {
	n, _ := fullTunNode(yamlx.Map())
	b, _ := yamlx.ToJSON(yamlx.Get(n, "tun"))
	var o jsonx.Object
	_ = json.Unmarshal(b, &o)
	return &o
}

// ReadMarker adapts the profile's strict marker to legacy callers.
func ReadMarker(config any) (string, *jsonx.Object) {
	p := legacyProfile(config)
	s, m := ReadMarkerProfile(p)
	if m == nil {
		return s, nil
	}
	raw, _ := p.Mihomo.Fields.Get(MarkerKey)
	var out jsonx.Object
	_ = json.Unmarshal(raw, &out)
	out.Set("route_rule_set_ids", legacyStrings(m.RouteRuleSetIDs))
	return s, &out
}

// IsFullTun adapts the native marker reader.
func IsFullTun(config any) bool {
	s, _ := ReadMarkerProfile(legacyProfile(config))
	return s == StateValid
}

// RouteRuleSetIDs returns the marker's selected rules to legacy callers.
func RouteRuleSetIDs(config any) []any {
	s, m := ReadMarkerProfile(legacyProfile(config))
	if s != StateValid {
		return []any{}
	}
	return legacyStrings(m.RouteRuleSetIDs)
}

// TunFilters projects a legacy TUN object once into YAML nodes.
func TunFilters(tun any) []string {
	n, err := legacyNode(tun)
	if err != nil {
		return []string{}
	}
	return nodeTunFilters(n)
}

// FullTunDocument adds the native takeover, preserving its caller's document representation.
func FullTunDocument(document any) (any, error) {
	n, err := legacyNode(document)
	if err != nil || n == nil || n.Kind != yaml.MappingNode {
		return nil, gatewayError(messageDocumentObject)
	}
	out, err := fullTunNode(n)
	if err != nil {
		return nil, err
	}
	if _, ok := document.(*yamlx.Node); ok {
		return out, nil
	}
	return yamlbridge.Value(out)
}

// FullTunBaseYAML is the legacy text boundary; stored-field coercion belongs to model.
func FullTunBaseYAML(text any) (string, error) {
	s, ok := text.(string)
	if !ok {
		return "", gatewayError(messageBaseText)
	}
	return FullTunYAML(s)
}

// IsFullTunDocument validates a generated legacy document.
func IsFullTunDocument(doc any) bool {
	n, err := legacyNode(doc)
	return err == nil && fullTunNodeShape(n) && len(nodeTunFilters(yamlx.Get(n, "tun"))) == 0
}

// CheckGeneration validates a legacy profile before generating.
func CheckGeneration(config any) error { return CheckProfile(legacyProfile(config)) }

// RequireFullTunGenerated validates the actual generated document.
func RequireFullTunGenerated(main any) error { n, _ := legacyNode(main); return RequireDocument(n) }

// NeedsContext determines whether setup/binding context must be loaded.
func NeedsContext(config any) (bool, error) {
	p := legacyProfile(config)
	s, _ := ReadMarkerProfile(p)
	return s == StateAbsent && GatewayEnabled(p), nil
}

// BaseIsFullTun validates the profile's editable YAML.
func BaseIsFullTun(config any) (bool, error) { return BaseFullTun(legacyProfile(config)), nil }

// LegacySource adapts the native recognition result, with null for no match.
func LegacySource(config, c any) (any, error) {
	s, err := LegacyProfileSource(legacyProfile(config), legacyContextValue(c))
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, nil
	}
	return s, nil
}

// LegacyGatewayAgentIDs adapts the native old-gateway selection.
func LegacyGatewayAgentIDs(config, c any) ([]any, error) {
	ids, err := LegacyAgentIDs(legacyProfile(config), legacyContextValue(c))
	return legacyStrings(ids), err
}

// ModeFromContext computes the native state using the caller's locked profile.
func ModeFromContext(config, c any) (any, error) {
	s, err := ProfileState(legacyProfile(config), legacyContextValue(c))
	if err != nil {
		return nil, err
	}
	if s.Mode == nil {
		return nil, nil
	}
	return *s.Mode, nil
}

// GatewayWasUsed checks the native restore marker and the Agent installation proof.
func GatewayWasUsed(config, agent, source any) (bool, error) {
	if source == "setup" {
		return true, nil
	}
	p := legacyProfile(config)
	if p.Mihomo.Fields.Has(gwbase.RestoreKey) {
		return true, nil
	}
	var a struct {
		InstallationID *string `json:"gateway_installation_id"`
	}
	b, _ := json.Marshal(agent)
	_ = jsondecode.Unmarshal(b, &a)
	return a.InstallationID != nil && *a.InstallationID != "", nil
}

// HookMissingError means the setup-session provider was not registered.
type HookMissingError struct{ Hook, Provider string }

func (e *HookMissingError) Error() string {
	return "common.router_split: " + e.Hook + " is not available (provided by " + e.Provider + ")"
}

// ProfileSessionsFunc is the pending setup owner migration boundary.
type ProfileSessionsFunc func(context.Context, *repo.Repository, string) ([]*jsonx.Object, error)

var profileSessions ProfileSessionsFunc

// SetProfileSessions registers setup's existing session reader.
func SetProfileSessions(fn ProfileSessionsFunc) func() {
	old := profileSessions
	profileSessions = fn
	return func() { profileSessions = old }
}

var (
	agentBindings = func(ctx context.Context, r *repo.Repository, id string) (*jsonx.Object, error) {
		return r.GetProfileAgentBindings(ctx, id)
	}
	getProfile = func(ctx context.Context, r *repo.Repository, id string) (*jsonx.Object, error) {
		p, e := r.Profile(ctx, id)
		if e != nil {
			return nil, e
		}
		return legacyOutput(p), nil
	}
	sessionsOf = func(ctx context.Context, r *repo.Repository, id string) ([]*jsonx.Object, error) {
		if profileSessions == nil {
			return nil, &HookMissingError{"setup_flow.profile_sessions", "config C36 (common/setup_flow.py)"}
		}
		return profileSessions(ctx, r, id)
	}
)

func sessionContext(ctx context.Context, r *repo.Repository, id string) (*Context, error) {
	rows, err := sessionsOf(ctx, r, id)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	if err = json.Unmarshal(encoded, &sessions); err != nil {
		return nil, err
	}
	return SessionContext(sessions)
}

// LegacyContext reads collaborators before any profile transaction begins.
func LegacyContext(ctx context.Context, r *repo.Repository, id string) (*jsonx.Object, error) {
	bindings, err := agentBindings(ctx, r, id)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	encoded, err := json.Marshal(bindings)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(encoded, &fields); err != nil {
		return nil, identityError(messageBindingsInvalid)
	}
	var agents []map[string]json.RawMessage
	if raw, ok := fields["agents"]; !ok || json.Unmarshal(raw, &agents) != nil || agents == nil {
		return nil, identityError(messageBindingsInvalid)
	}
	for _, a := range agents {
		if a == nil {
			return nil, identityError(messageBindingsInvalid)
		}
		var service string
		_ = json.Unmarshal(a["service_type"], &service)
		if service == "mihomo" {
			var identity *string
			if json.Unmarshal(a["id"], &identity) != nil || identity == nil || *identity == "" {
				return nil, identityError(messageBindingsInvalid)
			}
		}
	}
	out, err := sessionContext(ctx, r, id)
	if err != nil {
		return nil, err
	}
	var view struct {
		Agents []model.Agent `json:"agents"`
		Raw    model.Raw
	}
	b, err := json.Marshal(bindings)
	if err != nil {
		return nil, err
	}
	if err = model.DecodeRecord(b, &view, &view.Raw); err != nil {
		return nil, err
	}
	for _, a := range view.Agents {
		if a.ServiceType == "mihomo" && a.ID != "" {
			out.MihomoAgentIDs = append(out.MihomoAgentIDs, a.ID)
		}
	}
	// Context.Raw is provenance only, never a response member.
	return legacyOutput(struct {
		SessionGoal       *string       `json:"session_goal"`
		SessionID         *model.String `json:"session_id"`
		SessionCompleted  bool          `json:"session_completed"`
		HasGatewaySession bool          `json:"has_gateway_session"`
		MihomoAgentIDs    []string      `json:"mihomo_agent_ids"`
	}{out.SessionGoal, out.SessionID, out.SessionCompleted, out.HasGatewaySession, out.MihomoAgentIDs}), nil
}

// PublishLegacySource reads setup sessions only for an unmarked, named profile.
func PublishLegacySource(ctx context.Context, r *repo.Repository, id string, config, agentID any) (any, error) {
	p := legacyProfile(config)
	s, _ := ReadMarkerProfile(p)
	if s != StateAbsent || id == "" {
		return nil, nil
	}
	c, err := sessionContext(ctx, r, id)
	if err != nil {
		return nil, err
	}
	var a string
	b, _ := json.Marshal(agentID)
	if json.Unmarshal(b, &a) != nil || a == "" {
		return nil, identityError(messageTopologyIdentity)
	}
	c.MihomoAgentIDs = []string{a}
	source, err := LegacyProfileSource(p, c)
	if err != nil {
		return nil, err
	}
	if source == "" {
		return nil, nil
	}
	return source, nil
}

// RouterSplitState adapts repository reads and the native state projection.
func RouterSplitState(ctx context.Context, r *repo.Repository, id string, config, c any) (*jsonx.Object, error) {
	if legacyNone(config) {
		v, err := getProfile(ctx, r, id)
		if err != nil {
			return nil, err
		}
		config = v
	}
	if legacyNone(c) {
		v, err := LegacyContext(ctx, r, id)
		if err != nil {
			return nil, err
		}
		c = v
	}
	state, err := ProfileState(legacyProfile(config), legacyContextValue(c))
	if err != nil {
		return nil, err
	}
	return legacyOutput(state), nil
}
