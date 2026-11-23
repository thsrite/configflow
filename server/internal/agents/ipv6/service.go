package ipv6

import (
	"configflow/internal/deploy"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/clients"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/routers"
)

// Error is an actionable boundary failure with a stable API classification.
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string                              { return e.Message }
func failure(code string, status int, message string) error { return &Error{code, status, message} }

// Service owns Agent checks; shared publication and device readers are injected
// by composition. It does not own router publication or content comparison.
type Service struct {
	Manager *agents.Manager
	Checker func(*agents.Manager) *contentsync.Checker
	waker   *agents.Waker
}

var operations = lockx.NewKeyed[string](lockx.LevelAgentOperation, "agent-ipv6")
var identityFields = []string{"id", "host", "port", "token", "profile_id", "deployment_method", "service_type", "enabled", "gateway_installation_id"}

func selectFields(d Document, keys ...string) Document {
	out := Document{}
	for _, k := range keys {
		if v, ok := d[k]; ok {
			out[k] = v
		} else {
			out[k] = jsonresp.Value(nil)
		}
	}
	return out
}
func identity(a Document) Document { return selectFields(a, identityFields...) }
func digest(v any) string          { return deploy.CanonicalDigest(jsonresp.Value(v)) }

func linked(config Document, id string) []*routers.Router {
	out := []*routers.Router{}
	for _, r := range routers.FromConfig(jsonresp.Value(config)) {
		if id != "" && r.LinkID("proxy_gateway_agent_id") == id {
			out = append(out, r)
		}
	}
	return out
}
func (s *Service) policy(ctx context.Context, config, a Document) routers.PolicyContext {
	_, doc := ReadConfiguration(config)
	p := routers.PolicyContext{NodeTargets: []routers.PolicyNodeTarget{}, Clients: []routers.PolicyClient{}}
	for _, v := range ProbeTargets(config, doc) {
		if v.Literal {
			p.NodeTargets = append(p.NodeTargets, routers.PolicyNodeTarget{Address: v.Host, Port: v.Port})
		}
	}
	if devices, e := clients.ListDevices(jsonresp.Value(config)); e == nil {
		for _, d := range devices.Devices {
			if d.IP != "" && d.IP != text(a["host"]) && len(p.Clients) < 32 {
				p.Clients = append(p.Clients, routers.PolicyClient{Address: d.IP})
			}
		}
	}
	settings := object(a["ipv6_verification_settings"])
	candidate := s.lookup(ctx, text(settings["client_agent_id"]))
	if candidate != nil && text(candidate["id"]) != text(a["id"]) && text(candidate["profile_id"]) == text(a["profile_id"]) && string(candidate["enabled"]) != "false" && text(candidate["host"]) != text(a["host"]) {
		address, e := netip.ParseAddr(text(candidate["host"]))
		exists := false
		for _, c := range p.Clients {
			exists = exists || c.Address == address.String()
		}
		// Python also compares parsed addresses: another spelling of the gateway
		// address is the gateway itself, never a client source candidate.
		source, sourceError := netip.ParseAddr(text(a["host"]))
		if e == nil && (sourceError != nil || address != source) && !address.IsUnspecified() && !address.IsLoopback() && !address.IsMulticast() && !exists {
			p.Clients = append([]routers.PolicyClient{{Address: address.String(), SourceCandidate: true, SourceNote: msgCandidate}}, p.Clients...)
			if len(p.Clients) > 32 {
				p.Clients = p.Clients[:32]
			}
		}
	}
	dns := field(doc, "dns")
	if boolean(field(dns, "enable")) && scalar(field(dns, "enhanced-mode")) == "fake-ip" {
		p.FakeIPv4Range = scalar(field(dns, "fake-ip-range"))
	}
	return p
}
func (s *Service) inspect(ctx context.Context, a Document, profile string, config Document) (result *Report, resultError error) {
	defer func() {
		if ctx.Err() != nil {
			result = nil
			resultError = ctx.Err()
			return
		}
		if resultError != nil {
			var boundary *Error
			if errors.As(resultError, &boundary) {
				b, e := s.capturedPublic(ctx, config, a, Document{"message": jsonresp.Value(boundary.Message)})
				if e != nil {
					resultError = e
					return
				}
				copy := *boundary
				copy.Message = text(object(b)["message"])
				resultError = &copy
			}
			return
		}
		b, e := s.capturedPublic(ctx, config, a, result)
		if e != nil {
			result = nil
			resultError = e
			return
		}
		var safe Report
		if e = jsondecode.Unmarshal(b, &safe); e != nil {
			result = nil
			resultError = e
			return
		}
		result = &safe
	}()

	settings, _ := ReadConfiguration(config)
	host, e := s.request(ctx, a, "", nil)
	if e != nil {
		return nil, e
	}
	var hostChecks []json.RawMessage
	if json.Unmarshal(host["checks"], &hostChecks) != nil || hostChecks == nil || object(host["persistence"]) == nil {
		return nil, failure("INVALID_RESPONSE", 502, msgHostFields)
	}
	report := &Report{Success: true, AgentID: text(a["id"]), ProfileID: profile, Configuration: settings, Host: jsonresp.Value(host), Checks: []Check{}, Routers: []*RouterReport{}, PrepareInterface: text(host["interface"]), Connectivity: Connectivity{Status: "not_checked", Message: msgConnectivity}}
	status, message := "ok", fmt.Sprintf(msgSummary, onOff(settings.IPv6), onOff(settings.DNSIPv6))
	if len(settings.Errors) > 0 {
		status = "error"
		message = strings.Join(settings.Errors, "；")
	} else if !yes(settings.IPv6) {
		status = "warning"
	}
	report.Checks = append(report.Checks, check("configuration", msgSaved, status, message))
	core := object(host["core"])
	if core != nil && text(core["error"]) == "" {
		defaults := Document{"ipv6": jsonresp.Value(true), "dns.ipv6": jsonresp.Value(false), "tun.enable": jsonresp.Value(false), "dns.fake-ip-range6": jsonresp.Value(nil)}
		for k, v := range core {
			defaults[k] = v
		}
		core = defaults
	}
	var differing []string
	for _, item := range []struct {
		key   string
		value json.RawMessage
	}{{"ipv6", settings.IPv6}, {"dns.ipv6", settings.DNSIPv6}, {"tun.enable", jsonresp.Value(settings.TUNEnabled)}, {"dns.fake-ip-range6", settings.FakeIPv6Range}} {
		v, ok := core[item.key]
		if item.key == "dns.fake-ip-range6" && ok {
			value := text(v)
			if p, e := netip.ParsePrefix(value); e == nil {
				v = jsonresp.Value(p.Masked().String())
			} else {
				v = jsonresp.Value(nil)
			}
		}
		if ok && string(normalRaw(v)) != string(normalRaw(item.value)) {
			differing = append(differing, deployedLabels[item.key])
		}
	}
	if len(differing) > 0 {
		report.Checks = append(report.Checks, check("deployed_config", msgDeviceConfig, "warning", fmt.Sprintf(msgDifferent, strings.Join(differing, "、"))))
	}
	persistence := object(host["persistence"])
	for _, encoded := range hostChecks {
		item := object(encoded)
		if item == nil {
			continue
		}
		key := text(item["id"])
		label := text(item["label"])
		if label == "" {
			label = hostLabels[key]
		}
		if label == "" {
			label = msgHost
		}
		status := map[string]string{"pass": "ok", "warn": "warning", "fail": "error", "info": "unknown"}[text(item["status"])]
		if status == "" {
			status = "unknown"
		}
		message := text(item["message"])
		if message == "" {
			message = msgNoConclusion
		}
		details := []string{}
		if key == "persistence" {
			var evidence []Document
			_ = json.Unmarshal(persistence["evidence"], &evidence)
			for _, v := range evidence {
				if v != nil {
					details = append(details, fmt.Sprintf("%s=%s · %s:%s", display(v["key"]), display(v["value"]), display(v["file"]), display(v["line"])))
				}
			}
			var conflicts []json.RawMessage
			_ = json.Unmarshal(persistence["conflicts"], &conflicts)
			for _, v := range conflicts {
				details = append(details, display(v))
			}
		}
		if key == "forwarding_ra" {
			for k, v := range object(host["runtime"]) {
				if strings.HasPrefix(k, "all.") || strings.HasPrefix(k, text(host["interface"])+".") {
					details = append(details, k+"="+display(v))
				}
			}
		}
		report.Checks = append(report.Checks, check("host_"+key, label, status, message, details...))
	}
	rs := linked(config, report.AgentID)
	blocked := len(settings.Errors) > 0
	policy := s.policy(ctx, config, a)
	var agent model.Agent
	_ = json.Unmarshal(jsonresp.Value(a), &agent)
	for _, r := range rs {
		if err := r.ValidateForDevice(); err != nil {
			return nil, failure("ROUTER_SETTINGS_INVALID", 409, err.Error())
		}
		if r.Type != "routeros" {
			report.Routers = append(report.Routers, &RouterReport{IPv6Readiness: &routers.IPv6Readiness{ID: r.ID, Name: routerName(r), Status: "warning", Checks: []Check{check("manual", msgRouterIPv6, "unknown", msgManualRouter)}}, manual: true})
			continue
		}
		d := routers.NewRouterOS(r.Endpoint, r.Username, r.Password, r.VerifyTLS)
		var h routers.IPv6Host
		_ = jsondecode.Unmarshal(jsonresp.Value(host), &h)
		dns := s.lookup(ctx, r.DNSAgentID)
		result, err := routers.InspectIPv6Readiness(ctx, d, r, &agent, h, routers.IPv6ReadinessConfiguration{PolicyConfiguration: routers.PolicyConfiguration{FakeIPv6Enabled: settings.FakeIPv6Enabled, FakeIPv6Range: text(settings.FakeIPv6Range)}, IPv6: yes(settings.IPv6), FakeIPv6Route: &settings.FakeIPv6Route, TUNEnabled: settings.TUNEnabled}, text(dns["host"]), policy)
		d.Close()
		if err != nil {
			return nil, err
		}
		report.Routers = append(report.Routers, &RouterReport{IPv6Readiness: result})
		blocked = blocked || result.Status == "error"
	}
	if len(rs) == 0 {
		report.Checks = append(report.Checks, check("router", msgRouter, "unknown", msgNoRouter))
	}
	authorized := len(rs) > 0 || yes(object(object(config["mihomo"])["linux_gateway"])["enabled"])
	report.CanPrepare = !blocked && yes(settings.IPv6) && authorized && yes(host["can_prepare"])
	if !authorized {
		report.Checks = append(report.Checks, check("gateway_role", msgRole, "warning", msgRoleWarning))
	}
	fingerprints := []string{}
	for _, r := range report.Routers {
		fingerprints = append(fingerprints, r.Fingerprint)
	}
	report.CheckToken = digest(struct {
		Agent, Config    Document
		Interface, Index json.RawMessage
		Routers          []string
	}{identity(a), prepareConfig(config), host["interface"], host["interface_index"], fingerprints})
	return report, nil
}
func display(b json.RawMessage) string {
	if len(b) == 0 || string(b) == "null" {
		return "null"
	}
	if b[0] == '"' {
		return text(b)
	}
	return string(b)
}
func onOff(b json.RawMessage) string {
	if len(b) > 0 && string(b) != "null" && string(b) != "false" && string(b) != "0" && string(b) != `""` {
		return msgOn
	}
	return msgOff
}

func routerName(r *routers.Router) string {
	if r.Name != "" {
		return r.Name
	}
	return r.ID
}

// Host reads the captured gateway Agent for router inspection. The router owner
// already holds its device lock, so this leaf read never takes the Agent lock.
func (s *Service) Host(ctx context.Context, agent *model.Agent) (routers.IPv6Host, error) {
	if agent == nil {
		return routers.IPv6Host{}, failure("AGENT_NOT_FOUND", 404, msgMissing)
	}
	config, _, e := s.snapshot(ctx, agent.ProfileID)
	if e != nil {
		return routers.IPv6Host{}, e
	}
	a := object(jsonresp.Value(agent))
	data, e := s.request(ctx, a, "", nil)
	if e != nil {
		var boundary *Error
		if errors.As(e, &boundary) {
			b, err := s.capturedPublic(ctx, config, a, Document{"message": jsonresp.Value(boundary.Message)})
			if err != nil {
				return routers.IPv6Host{}, err
			}
			copy := *boundary
			copy.Message = text(object(b)["message"])
			e = &copy
		}
		return routers.IPv6Host{}, e
	}
	b, e := s.capturedPublic(ctx, config, a, data)
	if e != nil {
		return routers.IPv6Host{}, e
	}
	var host routers.IPv6Host
	if jsondecode.Unmarshal(b, &host) != nil {
		return host, failure("INVALID_RESPONSE", 502, msgResponseShape)
	}
	return host, nil
}

func normalRaw(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return jsonresp.Value(nil)
	}
	return b
}
