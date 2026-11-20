// Package host bridges bounded, profile-scoped gateway operations to registered
// Agents. It never executes network commands on the panel or infers host state.
package host

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/gateway"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/netipx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
)

// Minimum versions of the gateway, AIO and standalone forwarding APIs.
const (
	MinVersion        = gateway.MinAgentVersion
	ForwardingVersion = gateway.ForwardingMinAgentVersion
	StandaloneVersion = gateway.StandaloneForwardingMinAgentVersion
	MaxResponseBytes  = 512 * 1024
)

// Manager supplies registered identities and the shared bounded Agent client.
type Manager interface {
	ReadAgent(context.Context, string, bool) (*model.Agent, error)
	Client() agents.Client
}

// Bridge owns profile lookups and network operations; its dependencies are immutable.
type Bridge struct {
	Repository *repo.Repository
	Manager    Manager
}

// Reply preserves future Agent response members without interpreting them.
type Reply map[string]json.RawMessage

func object(raw json.RawMessage) Reply { var o Reply; _ = json.Unmarshal(raw, &o); return o }
func text(raw json.RawMessage) string  { var s string; _ = json.Unmarshal(raw, &s); return s }
func yes(raw json.RawMessage) bool     { return string(raw) == "true" }
func put(o Reply, key string, v any)   { b, _ := json.Marshal(v); o[key] = b }
func fail(code string, status int, message string) error {
	return monitor.NewError(code, status, message)
}

// Supports reports whether a well-formed Agent version implements minimum.
func Supports(a *model.Agent, minimum string) bool {
	if a == nil {
		return false
	}
	switch minimum {
	case MinVersion:
		return gateway.SupportsGateway(a.Version)
	case ForwardingVersion:
		return gateway.SupportsForwarding(a.Version)
	case StandaloneVersion:
		return gateway.SupportsStandaloneForwarding(a.Version)
	}
	return false
}

func (b *Bridge) agent(ctx context.Context, id string) (*model.Agent, *model.Agent, error) {
	a, err := b.Manager.ReadAgent(ctx, id, true)
	return a, a, err
}

func checkAgent(a *model.Agent) error {
	if a.ServiceType != "mihomo" {
		return fail("WRONG_SERVICE", 400, msgWrongService)
	}
	if a.DeploymentMethod == "docker" {
		return fail("UNSUPPORTED_ENVIRONMENT", 409, msgEnvironment)
	}
	if !Supports(a, MinVersion) {
		return fail("UPGRADE_REQUIRED", 409, fmt.Sprintf(msgUpgrade, MinVersion))
	}
	return nil
}

// HasRecord counts unreadable and pending journals as records too.
func HasRecord(status Reply) bool {
	// An empty value is an absent member (PersistForwarding copies the reply's
	// members, present or not); Python reads both as None, i.e. no record.
	raw := status["managed"]
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	managed := object(raw)
	// A malformed journal is evidence of an unreadable record, never evidence
	// that there is nothing to remove (N8-4f fail-closed ownership rule).
	if managed == nil {
		return true
	}
	for _, key := range []string{"active", "pending"} {
		if value, ok := managed[key]; ok && string(value) != "false" && string(value) != "null" {
			return true
		}
	}
	if value, ok := managed["error"]; ok && string(value) != "null" && string(value) != `""` && string(value) != "false" {
		return true
	}
	return false
}

// KeepsForwarding is true only for no record or explicitly persisted forwarding.
func KeepsForwarding(status Reply) bool {
	return !HasRecord(status) || yes(object(status["managed"])["preserves_forwarding"])
}
func recordKept(a *model.Agent, s Reply) bool {
	return !HasRecord(s) || Supports(a, ForwardingVersion) && KeepsForwarding(s)
}

// Request performs one whitelisted, bounded exchange. Transport diagnostics are
// never returned; device error text passes the shared credential sanitizer.
func (b *Bridge) Request(ctx context.Context, raw *model.Agent, operation, method string, payload json.RawMessage) (Reply, error) {
	if method != "GET" {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if allowed[operation] != method {
		return nil, fail("NOT_ALLOWED", 404, msgNotAllowed)
	}
	if raw.Token == "" {
		return nil, fail("AGENT_UNREACHABLE", 502, unreachable(operation))
	}
	origin, err := monitor.Origin(raw)
	if err != nil {
		return nil, fail("AGENT_UNREACHABLE", 502, unreachable(operation))
	}
	header := http.Header{"Authorization": {"Bearer " + raw.Token}, "Accept": {"application/json"}}
	if payload != nil {
		header.Set("Content-Type", "application/json")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := b.Manager.Client().Do(ctx, agents.Request{Method: method, URL: origin + "/api/linux-gateway/" + operation, Header: header, Body: payload, Timeout: agents.Pair(3, 45), NoProxy: true, NoRedirects: true, MaxBody: MaxResponseBytes})
	if err != nil || response == nil {
		return nil, fail("AGENT_UNREACHABLE", 502, unreachable(operation))
	}
	if len(response.Body) > MaxResponseBytes {
		return nil, fail("INVALID_RESPONSE", 502, msgTooLarge)
	}
	if response.BodyError != nil {
		return nil, fail("AGENT_UNREACHABLE", 502, unreachable(operation))
	}
	if response.StatusCode == 404 {
		minimum := MinVersion
		if operation == "forwarding" {
			minimum = ForwardingVersion
		}
		return nil, fail("UPGRADE_REQUIRED", 409, fmt.Sprintf(msgEndpointUpgrade, minimum))
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, fail("AGENT_AUTH_FAILED", 502, msgAuth)
	}
	if !json.Valid(response.Body) {
		return nil, fail("INVALID_RESPONSE", 502, msgParse)
	}
	result := object(response.Body)
	if result == nil {
		return nil, fail("INVALID_RESPONSE", 502, msgShape)
	}
	// The raw API has extensible status fields. Use the common redactor rather
	// than introducing a second credential grammar at this boundary.
	safe := redact.SanitizeCredentials(json.RawMessage(response.Body), redact.NewSecrets([]string{raw.Token}))
	if result = object(safe); result == nil {
		return nil, fail("INVALID_RESPONSE", 502, msgShape)
	}
	if response.StatusCode >= 400 {
		message := text(result["message"])
		if message == "" {
			message = text(result["error"])
		}
		if message == "" {
			message = msgIncomplete
		}
		r := []rune(message)
		if len(r) > 1000 {
			message = string(r[:1000])
		}
		status := response.StatusCode
		if status != 400 && status != 409 && status != 422 {
			status = 502
		}
		return nil, fail("GATEWAY_ERROR", status, message)
	}
	return result, nil
}

var allowed = map[string]string{"status": "GET", "preview": "POST", "apply": "POST", "remove": "POST", "forwarding": "POST"}

func unreachable(operation string) string {
	switch operation {
	case "apply", "remove":
		return msgUncertain
	case "forwarding":
		return msgForwardUncertain
	}
	return msgConnect
}

// Spec is the only network configuration accepted by preview and apply.
type Spec struct {
	Interface       string `json:"interface"`
	Address         string `json:"address"`
	LANCIDR         string `json:"lan_cidr"`
	UpstreamGateway string `json:"upstream_gateway"`
	ProfileID       string `json:"profile_id,omitempty"`
}

var devicePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`)

// ParseSpec validates exact fields and native IPv4 address/network semantics.
func ParseSpec(raw json.RawMessage) (Spec, error) {
	o := object(raw)
	var s Spec
	if len(o) != 4 || o["interface"] == nil || o["address"] == nil || o["lan_cidr"] == nil || o["upstream_gateway"] == nil {
		return s, fail("INVALID_REQUEST", 400, msgSpecFields)
	}
	for _, v := range o {
		str := text(v)
		if str == "" || utf8.RuneCountInString(str) > 64 {
			return s, fail("INVALID_REQUEST", 400, msgSpecText)
		}
	}
	_ = jsondecode.Unmarshal(raw, &s)
	if !devicePattern.MatchString(s.Interface) || s.Interface == "lo" {
		return s, fail("INVALID_REQUEST", 400, msgInterface)
	}
	addr, e1 := netip.ParseAddr(s.Address)
	up, e2 := netip.ParseAddr(s.UpstreamGateway)
	network, networkOK := netipx.ParseNetwork4(s.LANCIDR, true)
	if e1 != nil || e2 != nil || !networkOK || !addr.Is4() || !up.Is4() || !network.Addr().Is4() || network != network.Masked() {
		return s, fail("INVALID_REQUEST", 400, msgIPv4)
	}
	if network.Bits() < 8 || network.Bits() > 30 || addr == up {
		return s, fail("INVALID_REQUEST", 400, msgLAN)
	}
	start := network.Addr().As4()
	n := uint32(start[0])<<24 | uint32(start[1])<<16 | uint32(start[2])<<8 | uint32(start[3])
	last := n | uint32((uint64(1)<<uint(32-network.Bits()))-1)
	for _, a := range []netip.Addr{addr, up} {
		v := a.As4()
		i := uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
		if !network.Contains(a) || i == n || i == last || a.IsMulticast() || a.IsUnspecified() || a.IsLoopback() || a.IsLinkLocalUnicast() {
			return s, fail("INVALID_REQUEST", 400, msgLAN)
		}
	}
	s.Address = addr.String()
	s.UpstreamGateway = up.String()
	s.LANCIDR = network.String()
	return s, nil
}

func (b *Bridge) mode(ctx context.Context, id string, config *model.Config) (string, error) {
	marker, _ := routersplit.ReadMarkerProfile(config.Profile)
	if marker != "absent" {
		if marker == "valid" {
			return "full_tun", nil
		}
		return "invalid", nil
	}
	if !gateway.NewConfig(config).Enabled() {
		return "", nil
	}
	legacy, err := routersplit.ReadContext(ctx, b.Repository, id)
	if err != nil {
		return "", err
	}
	mode, err := routersplit.ProfileState(config.Profile, legacy)
	if err != nil {
		return "", err
	}
	m := ""
	if mode.Mode != nil {
		m = *mode.Mode
	}
	if m == "" {
		full := routersplit.BaseFullTun(config.Profile)
		source, err := routersplit.LegacyProfileSource(config.Profile, legacy)
		if err != nil {
			return "", err
		}
		if full && source != "" {
			m = "legacy_full_tun"
		}
	}
	return m, nil
}

// Call validates captured profile binding and guards all host-changing operations.
// Writes detach at the owner, retaining Runtime shutdown cancellation.
func (b *Bridge) Call(ctx context.Context, id, operation, method, profileID string, body json.RawMessage) (Reply, error) {
	if method != "GET" {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if allowed[operation] != method {
		return nil, fail("NOT_ALLOWED", 404, msgNotAllowed)
	}
	a, raw, err := b.agent(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fail("AGENT_NOT_FOUND", 404, msgMissing)
	}
	if profileID == "" || a.ProfileID != profileID {
		return nil, fail("PROFILE_CHANGED", 409, msgProfileChanged)
	}
	if err = checkAgent(a); err != nil {
		return nil, err
	}
	var payload json.RawMessage
	mode := ""
	if operation == "apply" || operation == "remove" || operation == "forwarding" {
		config, err := b.Repository.Config(ctx, profileID, repo.ResourceRoots{})
		if err != nil {
			return nil, err
		}
		mode, err = b.mode(ctx, profileID, config)
		if err != nil {
			return nil, err
		}
		payload, mode, err = b.guard(ctx, a, raw, config, operation, profileID, mode, body)
		if err != nil {
			return nil, err
		}
		if operation == "apply" {
			settings, err := gateway.SettingsOf(config)
			if err != nil {
				return nil, err
			}
			if !settings.Enabled {
				return nil, fail("GATEWAY_DISABLED", 409, msgDisabled)
			}
		}
	}
	if operation == "preview" || operation == "apply" {
		s, err := ParseSpec(body)
		if err != nil {
			return nil, err
		}
		s.ProfileID = profileID
		payload, _ = json.Marshal(s)
	}
	result, err := b.Request(ctx, raw, operation, method, payload)
	if err != nil {
		return nil, err
	}
	if operation == "status" {
		if v, ok := result["forward_filter"]; ok {
			filter := SanitizeForwardFilter(v)
			if filter == nil {
				delete(result, "forward_filter")
			} else {
				put(result, "forward_filter", filter)
			}
		}
	}
	if operation == "remove" && yes(result["success"]) {
		if message := removedMessages[mode]; message != "" {
			put(result, "message", message)
		}
	}
	return result, nil
}
func emptyBody(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null" || len(object(raw)) == 0 && strings.TrimSpace(string(raw)) == "{}"
}
func (b *Bridge) guard(ctx context.Context, a *model.Agent, raw *model.Agent, config *model.Config, op, profile, mode string, body json.RawMessage) (json.RawMessage, string, error) {
	invalid := func(message string) (json.RawMessage, string, error) {
		return nil, mode, fail("INVALID_REQUEST", 400, message)
	}
	o := object(body)
	confirmed := len(o) == 1 && yes(o["confirm_router_split_teardown"])
	if op == "remove" && !emptyBody(body) && !confirmed {
		return invalid(msgRemoveBody)
	}
	splitError := func(code string) (json.RawMessage, string, error) {
		return nil, mode, fail(code, 409, routersplit.Messages[code])
	}
	switch op {
	case "apply":
		if mode == "full_tun" {
			return splitError(routersplit.CodeFullTun)
		}
		if mode == "invalid" {
			return splitError(routersplit.CodeInvalid)
		}
		return nil, mode, nil
	case "remove":
		if mode == "legacy" && !confirmed {
			return splitError(routersplit.CodeLegacyTeardown)
		}
		if mode == "full_tun" || mode == "legacy_full_tun" {
			if mode == "full_tun" && !Supports(a, ForwardingVersion) {
				return nil, mode, fail("UPGRADE_REQUIRED", 409, fmt.Sprintf(msgUpgrade, ForwardingVersion))
			}
			status, err := b.Request(ctx, raw, "status", "GET", nil)
			if err != nil {
				return nil, mode, err
			}
			if !HasRecord(status) {
				mode = "full_tun_none"
			} else if recordKept(a, status) {
				mode = "full_tun"
			} else if mode == "legacy_full_tun" && confirmed {
				mode = "legacy"
			} else {
				message := ForwardingNotKept("")
				if mode == "legacy_full_tun" {
					message = msgLegacyRemove
					if !Supports(a, ForwardingVersion) {
						return nil, mode, fail("UPGRADE_REQUIRED", 409, msgLegacyUpgrade)
					}
				}
				return nil, mode, fail("FORWARDING_NOT_PERSISTED", 409, message)
			}
		}
		return json.RawMessage(`{}`), mode, nil
	default:
		if !Supports(a, ForwardingVersion) {
			return nil, mode, fail("UPGRADE_REQUIRED", 409, fmt.Sprintf(msgUpgrade, ForwardingVersion))
		}
		if mode == "full_tun" {
			if !emptyBody(body) && (len(o) != 1 || (string(o["prepare_tun"]) != "true" && string(o["prepare_tun"]) != "false")) {
				return invalid(msgPrepareBody)
			}
		} else if len(ProxyRouters(config, a.ID)) > 0 {
			if !emptyBody(body) {
				return invalid(msgProxyBody)
			}
		} else {
			return nil, mode, fail("ROUTER_SPLIT_REQUIRED", 409, msgSplitRequired)
		}
		if err := b.requireLayout(ctx, a, raw); err != nil {
			return nil, mode, err
		}
		if emptyBody(body) {
			body = json.RawMessage(`{}`)
		}
		return body, mode, nil
	}
}

func upgradeNeeded(a *model.Agent, status Reply) bool {
	f := object(status["ipv4_forwarding"])
	return f == nil || string(f["supported"]) == "false" && yes(status["supported"]) && !Supports(a, StandaloneVersion)
}
func (b *Bridge) requireLayout(ctx context.Context, a *model.Agent, raw *model.Agent) error {
	if Supports(a, StandaloneVersion) {
		return nil
	}
	status, err := b.Request(ctx, raw, "status", "GET", nil)
	if err != nil {
		return err
	}
	if upgradeNeeded(a, status) {
		return fail("UPGRADE_REQUIRED", 409, msgStandalone)
	}
	return nil
}

// GatewayOffRefusal explains why disabling a legacy gateway would interrupt forwarding.
func (b *Bridge) GatewayOffRefusal(ctx context.Context, profile string, ids []string) (string, error) {
	for _, id := range ids {
		a, _, err := b.agent(ctx, id)
		if err != nil {
			return "", err
		}
		if a == nil || a.ServiceType != "mihomo" || a.DeploymentMethod == "docker" {
			continue
		}
		name := a.Name
		if name == "" {
			name = id
		}
		status, err := b.RecordStatus(ctx, id, profile)
		if err != nil {
			var e *monitor.Error
			if errors.As(err, &e) {
				return fmt.Sprintf(msgOffUnknown, name, strings.TrimRight(e.Message, "。")), nil
			}
			return "", err
		}
		if !recordKept(a, status) {
			if Supports(a, ForwardingVersion) {
				return fmt.Sprintf(msgOffNotKept, name), nil
			}
			return fmt.Sprintf(msgOffUpgrade, name), nil
		}
	}
	return "", nil
}

// RecordStatus treats old Agents and a missing endpoint as having no journal.
func (b *Bridge) RecordStatus(ctx context.Context, id, profile string) (Reply, error) {
	a, _, err := b.agent(ctx, id)
	if err != nil || !Supports(a, MinVersion) {
		return nil, err
	}
	s, err := b.Call(ctx, id, "status", "GET", profile, nil)
	var e *monitor.Error
	if errors.As(err, &e) && e.Code == "UPGRADE_REQUIRED" {
		return nil, nil
	}
	return s, err
}

// RequireForwardingKept guards full-TUN publication before any upload.
func (b *Bridge) RequireForwardingKept(ctx context.Context, id, profile string) error {
	s, err := b.Call(ctx, id, "status", "GET", profile, nil)
	if err != nil {
		return publishError(err)
	}
	if !KeepsForwarding(s) {
		return &gateway.Error{Message: ForwardingNotKept(msgPublishAction)}
	}
	return nil
}

// RequireGatewayOffKept guards gateway-off publication of a legacy split.
func (b *Bridge) RequireGatewayOffKept(ctx context.Context, id, profile string) error {
	a, _, err := b.agent(ctx, id)
	if err != nil {
		return err
	}
	s, err := b.RecordStatus(ctx, id, profile)
	if err != nil {
		return publishError(err)
	}
	if !recordKept(a, s) {
		return &gateway.Error{Message: msgPublishOff}
	}
	return nil
}

// RequireGatewayAgent guards a generated gateway configuration before upload.
func RequireGatewayAgent(a *model.Agent) error {
	version := ""
	if a != nil {
		version = a.Version
	}
	return gateway.RequireGatewayAgent(version)
}

type guardFailure struct {
	reason *gateway.Error
	cause  error
}

func (e *guardFailure) Error() string   { return e.reason.Error() }
func (e *guardFailure) Unwrap() []error { return []error{e.reason, e.cause} }
func publishError(err error) error {
	var monitorErr *monitor.Error
	if !errors.As(err, &monitorErr) {
		return err
	}
	return &guardFailure{reason: &gateway.Error{Message: fmt.Sprintf(msgPublishUnknown, monitorErr.Message)}, cause: err}
}
