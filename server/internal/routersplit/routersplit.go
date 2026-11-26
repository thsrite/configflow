// Package routersplit validates full-TUN takeover and recognizes older router
// split profiles. It does not write routers or own device locks.
package routersplit

import (
	"configflow/internal/jsondecode"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/gateway/gwbase"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// MarkerKey is MARKER_KEY.
const MarkerKey = "router_split"

// GatewayMarker is GATEWAY_MARKER.
const GatewayMarker = "x-configflow-linux-gateway"

// RouteKeysRemoved is ROUTE_KEYS_REMOVED.
var RouteKeysRemoved = []string{"route-address", "inet4-route-address", "inet6-route-address",
	"route-address-set", "route-exclude-address-set"}

// Marker states returned by ReadMarker.
const (
	StateAbsent  = "absent"
	StateValid   = "valid"
	StateInvalid = "invalid"
)

// RouterSplitError is router_split.RouterSplitError, a LinuxGatewayError so
// existing 400 handlers still apply: errors.As(err, **gwbase.LinuxGatewayError)
// finds the embedded error.
type RouterSplitError struct {
	gwbase.LinuxGatewayError
	Code   string
	Status int
}

// NewRouterSplitError is RouterSplitError(message, code, status); status 0
// means the default 409.
func NewRouterSplitError(message, code string, status int) *RouterSplitError {
	if status == 0 {
		status = 409
	}
	return &RouterSplitError{LinuxGatewayError: gwbase.LinuxGatewayError{Message: message}, Code: code, Status: status}
}

// As lets errors.As find the embedded *gwbase.LinuxGatewayError (Python's
// isinstance(error, LinuxGatewayError)).
func (e *RouterSplitError) As(target any) bool {
	if t, ok := target.(**gwbase.LinuxGatewayError); ok {
		*t = &e.LinuxGatewayError
		return true
	}
	return false
}

func gatewayError(message string) error { return &gwbase.LinuxGatewayError{Message: message} }

// ValidationError identifies an unusable generated takeover document.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// Marker is the strict stored ownership declaration. Unlike display text,
// its version and identities must never be coerced or defaulted into validity.
type Marker struct {
	Version         json.Number `json:"version"`
	Takeover        string      `json:"takeover"`
	RouteRuleSetIDs []string    `json:"route_rule_set_ids"`
	Origin          string      `json:"origin"`
}

// ReadMarkerProfile validates the original marker, including unknown members.
func ReadMarkerProfile(p *model.Profile) (string, *Marker) {
	if p == nil {
		return StateAbsent, nil
	}
	raw := p.Mihomo.RouterSplit.JSON()
	if raw == nil {
		return StateAbsent, nil
	}
	var fields map[string]json.RawMessage
	var marker Marker
	if jsondecode.Unmarshal(raw, &fields) != nil || len(fields) != 4 || jsondecode.Unmarshal(raw, &marker) != nil || marker.Version != "1" || string(fields["version"]) != "1" || marker.Takeover != "full_tun" || marker.Origin != "setup" || marker.RouteRuleSetIDs == nil || len(marker.RouteRuleSetIDs) > 1024 {
		return StateInvalid, nil
	}
	for _, key := range []string{"version", "takeover", "route_rule_set_ids", "origin"} {
		if _, ok := fields[key]; !ok {
			return StateInvalid, nil
		}
	}
	ids := []string{}
	for _, id := range marker.RouteRuleSetIDs {
		if id == "" || utf8.RuneCountInString(id) > 256 || strings.ContainsFunc(id, func(r rune) bool { return r < 32 || r == 127 }) {
			return StateInvalid, nil
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	marker.RouteRuleSetIDs = ids
	return StateValid, &marker
}

// GatewayEnabled reads the shared gateway switch; a switch of another type
// than a boolean reads as off.
func GatewayEnabled(p *model.Profile) bool {
	if p == nil {
		return false
	}
	var settings struct {
		Enabled bool `json:"enabled" stored:"gate"`
	}
	_, _ = model.DecodeStored(p.Mihomo.LinuxGateway.JSON(), &settings, "")
	return settings.Enabled
}

// CheckProfile stops generation if a marker is invalid or takeover modes conflict.
func CheckProfile(p *model.Profile) error {
	state, _ := ReadMarkerProfile(p)
	if state == StateInvalid {
		return NewRouterSplitError(Messages[CodeInvalid], CodeInvalid, 0)
	}
	if state == StateValid && GatewayEnabled(p) {
		return NewRouterSplitError(Messages[CodeFullTun], CodeFullTun, 0)
	}
	return nil
}

// FullTunYAML adds the takeover to editable base YAML while keeping opaque nodes.
func FullTunYAML(text string) (string, error) {
	source := text
	if strings.TrimSpace(text) == "" {
		data, err := yamlx.Marshal(convert.MihomoTemplate())
		if err != nil {
			return "", err
		}
		source = string(data)
	}
	root, err := yamlx.Parse([]byte(source))
	if err != nil {
		return "", gatewayError(messageBaseParse)
	}
	if root.Kind != yaml.MappingNode {
		return "", gatewayError(messageBaseObject)
	}
	root, err = fullTunNode(root)
	if err != nil {
		return "", err
	}
	data, err := yamlx.Edit([]byte(source), func(n *yamlx.Node) error { *n = *root; return nil })
	return string(data), err
}

func baseNode(p *model.Profile) *yamlx.Node {
	if p == nil {
		return nil
	}
	root, _ := yamlx.Parse([]byte(p.Mihomo.CustomConfig))
	return root
}

// BaseFullTun reports whether the editable base already captures all traffic.
func BaseFullTun(p *model.Profile) bool {
	n := baseNode(p)
	return fullTunNodeShape(n) && len(nodeTunFilters(yamlx.Get(n, "tun"))) == 0
}

// RequireDocument validates the actual generated YAML before publishing.
func RequireDocument(n *yamlx.Node) error {
	if !fullTunNodeShape(n) {
		return &ValidationError{GeneratedTunMissing}
	}
	if filters := nodeTunFilters(yamlx.Get(n, "tun")); len(filters) > 0 {
		return &ValidationError{strings.Replace(GeneratedTunFiltered, "{keys}", strings.Join(filters, "、tun."), 1)}
	}
	return nil
}

// Context is the pre-transaction setup/binding snapshot used for legacy recognition.
type Context struct {
	SessionGoal       *string       `json:"session_goal"`
	SessionID         *model.String `json:"session_id"`
	SessionCompleted  bool          `json:"session_completed" stored:"gate"`
	HasGatewaySession bool          `json:"has_gateway_session" stored:"gate"`
	MihomoAgentIDs    []string      `json:"mihomo_agent_ids"`
	invalidIdentity   bool
}

type contextWire Context

// UnmarshalJSON reads a legacy context leniently; an identity of the wrong
// type marks it invalid, a flag of another type than a boolean is false.
func (c *Context) UnmarshalJSON(b []byte) error {
	if err := model.DecodeLenient(b, (*contextWire)(c)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	if raw, present := fields["mihomo_agent_ids"]; present && string(raw) != "null" {
		var ids []*string
		if json.Unmarshal(raw, &ids) != nil {
			c.invalidIdentity = true
			c.MihomoAgentIDs = nil
		} else {
			for _, id := range ids {
				if id == nil || *id == "" {
					c.invalidIdentity = true
					c.MihomoAgentIDs = nil
				}
			}
		}
	}
	if raw, present := fields["session_id"]; present && string(raw) != "null" {
		var id string
		if json.Unmarshal(raw, &id) != nil {
			c.invalidIdentity = true
			c.SessionID = nil
		}
	}
	return nil
}

// Session is the consumed setup-session view; opaque session data stays with setup.
type Session struct {
	ID        *model.String `json:"id"`
	Goal      *string       `json:"goal"`
	Abandoned bool          `json:"abandoned" stored:"gate"`
	Completed bool          `json:"completed" stored:"gate"`
	invalidID bool
}

type sessionWire Session

// UnmarshalJSON reads the setup-session view leniently; an ID of the wrong
// type marks it invalid, a flag of another type than a boolean is false.
func (s *Session) UnmarshalJSON(b []byte) error {
	if err := model.DecodeLenient(b, (*sessionWire)(s)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	if raw, present := fields["id"]; present && string(raw) != "null" {
		var id string
		if json.Unmarshal(raw, &id) != nil {
			s.invalidID = true
			s.ID = nil
		}
	}
	return nil
}

// SessionContext selects the newest non-abandoned session and historical gateway purpose.
func SessionContext(rows []Session) (*Context, error) {
	out := &Context{MihomoAgentIDs: []string{}}
	found := false
	for _, s := range rows {
		if s.Abandoned {
			continue
		}
		if !found {
			if s.invalidID {
				return nil, identityError(messageTopologyIdentity)
			}
			out.SessionGoal, out.SessionID, out.SessionCompleted = s.Goal, s.ID, s.Completed
			found = true
		}
		if s.Goal != nil && *s.Goal == "gateway" {
			out.HasGatewaySession = true
		}
	}
	return out, nil
}

// LegacyProfileSource recognizes a wizard purpose or a router bound to a Mihomo Agent.
func legacyProfileSource(p *model.Profile, c *Context) string {
	if p == nil || c == nil {
		return ""
	}
	if state, _ := ReadMarkerProfile(p); state != StateAbsent {
		return ""
	}
	if c.SessionGoal != nil && *c.SessionGoal == "router" {
		return "setup"
	}
	if c.HasGatewaySession {
		return ""
	}
	if len(profileRouterIDs(p, c.MihomoAgentIDs)) > 0 {
		return "topology"
	}
	return ""
}
func profileRouterIDs(p *model.Profile, ids []string) []string {
	out := []string{}
	if p != nil {
		for _, r := range p.Routers.Items {
			if id, link, valid := topologyRouter(r); valid && link != "" && slices.Contains(ids, link) {
				out = append(out, id)
			}
		}
	}
	return out
}

// LegacyAgentIDs identifies the old gateway records that a publication may replace.
func legacyAgentIDs(p *model.Profile, c *Context) []string {
	out := []string{}
	if c == nil {
		return out
	}
	switch legacyProfileSource(p, c) {
	case "setup":
		return append(out, c.MihomoAgentIDs...)
	case "topology":
		proxies := map[string]bool{}
		for _, r := range p.Routers.Items {
			if _, link, _ := topologyRouter(r); link != "" {
				proxies[link] = true
			}
		}
		for _, id := range c.MihomoAgentIDs {
			if proxies[id] {
				out = append(out, id)
			}
		}
	}
	return out
}

// SplitState is the UI's takeover state; absent modes and sources are JSON null.
type SplitState struct {
	Mode           *string       `json:"mode"`
	Source         *string       `json:"source"`
	MihomoAgentIDs []string      `json:"mihomo_agent_ids"`
	RouterIDs      []string      `json:"router_ids"`
	SetupSessionID *model.String `json:"setup_session_id"`
	Issues         []string      `json:"issues"`
}

// ProfileState computes takeover status without repository access or writes.
func profileState(p *model.Profile, c *Context) SplitState {
	if c == nil {
		c = &Context{}
	}
	out := SplitState{MihomoAgentIDs: append([]string{}, c.MihomoAgentIDs...), RouterIDs: profileRouterIDs(p, c.MihomoAgentIDs), SetupSessionID: c.SessionID, Issues: []string{}}
	marker, _ := ReadMarkerProfile(p)
	switch marker {
	case StateValid:
		out.Mode, out.Source = new("full_tun"), new("marker")
		base := baseNode(p)
		if len(nodeTunFilters(yamlx.Get(base, "tun"))) > 0 {
			out.Issues = append(out.Issues, "tun_filtered")
		}
		if !fullTunNodeShape(base) {
			out.Issues = append(out.Issues, "tun_missing")
		}
		if GatewayEnabled(p) {
			out.Issues = append(out.Issues, "gateway_enabled")
		}
	case StateInvalid:
		out.Mode, out.Source = new("invalid"), new("marker")
	default:
		if GatewayEnabled(p) && !BaseFullTun(p) {
			if source := legacyProfileSource(p, c); source != "" {
				out.Mode, out.Source = new("legacy"), new(source)
			}
		}
	}
	return out
}

// topologyRouter reads only the original router identity/link tokens. Invalid
// optional links are absent, so an unrelated record cannot block a whole profile.
func topologyRouter(r model.Router) (id, link string, validID bool) {
	raw, err := model.Marshal(&r)
	if err != nil {
		return "", "", false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", "", false
	}
	var identity, connection *string
	// A damaged stored identity or link (model.DamagedText) is not text.
	if json.Unmarshal(fields["proxy_gateway_agent_id"], &connection) != nil || connection == nil || *connection == model.DamagedText {
		return "", "", false
	}
	if json.Unmarshal(fields["id"], &identity) == nil && identity != nil && *identity != "" && *identity != model.DamagedText {
		id, validID = *identity, true
	}
	return id, *connection, validID
}

// ValidateTopology checks only identities actually used for legacy recognition.
// A valid/invalid marker takes its own path and never requires legacy topology.
func ValidateTopology(p *model.Profile, c *Context) error {
	if state, _ := ReadMarkerProfile(p); state != StateAbsent {
		return nil
	}
	if c != nil && c.invalidIdentity {
		return identityError(messageTopologyIdentity)
	}
	if p == nil || c == nil {
		return nil
	}
	for _, r := range p.Routers.Items {
		_, link, validID := topologyRouter(r)
		if link != "" && slices.Contains(c.MihomoAgentIDs, link) && !validID {
			return identityError(messageTopologyIdentity)
		}
	}
	return nil
}

// LegacyProfileSource validates identities and recognizes the old takeover purpose.
func LegacyProfileSource(p *model.Profile, c *Context) (string, error) {
	if err := ValidateTopology(p, c); err != nil {
		return "", err
	}
	return legacyProfileSource(p, c), nil
}

// LegacyAgentIDs validates original identities before choosing gateway records.
func LegacyAgentIDs(p *model.Profile, c *Context) ([]string, error) {
	if err := ValidateTopology(p, c); err != nil {
		return nil, err
	}
	return legacyAgentIDs(p, c), nil
}

// ProfileState validates identities and computes the complete takeover state.
func ProfileState(p *model.Profile, c *Context) (SplitState, error) {
	if err := ValidateTopology(p, c); err != nil {
		return SplitState{}, err
	}
	return profileState(p, c), nil
}

func identityError(message string) error { return NewRouterSplitError(message, CodeInvalid, 409) }
