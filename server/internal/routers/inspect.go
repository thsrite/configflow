package routers

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// InspectRequest selects one profile and one driver. No implicit profile context
// is used by worker jobs or cross-profile return ownership checks.
type InspectRequest struct {
	ProfileID string
	Config    json.RawMessage
	Router    *Router
	Driver    Driver
	Loader    ContentLoader
	Agents    AgentSource
	Repo      *repo.Repository
}

// Inspection is one coherent desired state and readback, with an optional Agent.
type Inspection struct {
	Gateway *GatewayAgent
	Desired *Desired
	State   *RouterState
	Report  *Report
}

// IPv6HostRequest is supplied by A23; missing wiring fails closed for this stage.
var IPv6HostRequest = func(context.Context, *GatewayAgent) (IPv6Host, error) {
	return IPv6Host{}, routerError(messageIPv6HostUnavailable)
}

// Inspect reads device and Agent state, never writes to either.
func Inspect(ctx context.Context, req InspectRequest) (*Inspection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, d := req.Router, req.Driver
	if r != nil {
		if err := r.ValidateForDevice(); err != nil {
			return nil, err
		}
	}
	if r == nil || d == nil {
		return nil, routerError(messageOperationFormat)
	}
	var profile model.Profile
	if err := json.Unmarshal(req.Config, &profile); err != nil {
		return nil, err
	}
	// Model fallback is not an ownership proof. Validate every retained manifest
	// before issuing even a read to a device used for ownership decisions.
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var stored model.Router
	if err = json.Unmarshal(b, &stored); err != nil {
		return nil, err
	}
	if ros, ok := d.(*RouterOS); ok {
		defer ros.ReuseAddressReads()()
	}
	var gateway *GatewayAgent
	if req.Agents != nil {
		gateway, err = req.Agents.Agent(ctx, r.ProxyGatewayAgentID)
		if err != nil {
			return nil, err
		}
	} else if r.ProxyGatewayAgentID != "" || r.DNSAgentID != "" {
		return nil, ErrNotPorted
	}
	host := ""
	if gateway != nil && gateway.Agent != nil {
		host = gateway.Host
	}
	desired, err := ComputeDesired(ctx, req.Config, host, r.StaticRuleSets(), req.Loader)
	if err != nil {
		return nil, err
	}
	settings, err := inspectionDNS(ctx, req.Agents, r.DNSAgentID)
	if err != nil {
		return nil, err
	}
	desired.DNSSettings = &settings
	ros, isROS := d.(*RouterOS)
	policy := r.Type == "routeros" && (len(r.RosPolicyRuleSets) > 0 || hasStoredState(r.State("ros_policy_state")))
	guard := DNSGuardRelevant(r)
	if policy || guard {
		if !isROS {
			return nil, routerError(fmt.Sprintf(messageStageNeedsRouterOS, StageRoutes))
		}
	}
	if policy {
		ros.SetPolicyManifest(r.State("ros_policy_state"))
	}
	if guard {
		ros.SetDNSGuardManifest(r.State("dns_guard_state"))
	}
	state, err := d.ReadState(ctx)
	if err != nil {
		return nil, err
	}
	if !r.ManageReturnRouting {
		RetirePolicyRoutes(state)
	}
	if guard {
		desired.DNSGuard = &DesiredDNSGuard{Settings: dnsGuardSettings(r), Previous: r.State("dns_guard_state")}
		state.DNSGuard, err = SnapshotDNSGuard(ctx, ros)
		if err != nil {
			return nil, err
		}
	}
	if policy {
		desired.ROSPolicy, err = InspectROSPolicyDesired(ctx, req.Config, r, desired, req.Loader)
		if err != nil {
			return nil, err
		}
		state.ROSPolicy, err = SnapshotROSPolicy(ctx, ros)
		if err != nil {
			return nil, err
		}
		owned, e := OwnedROSPolicyRows(r.State("ros_policy_state"), state.ROSPolicy)
		if e != nil {
			owned = nil
		}
		ids := []string{}
		for _, v := range owned {
			if v.Path == rosMangle {
				ids = append(ids, v.ID)
			}
		}
		if state.NatContext == nil {
			state.NatContext = &NatContext{}
		}
		state.NatContext.Mangle = []Row{}
		for _, v := range (*state.ROSPolicy)[rosMangle] {
			if !slices.Contains(ids, v.id()) {
				row := cloneAddressRow(v)
				delete(row, "bytes")
				delete(row, "packets")
				state.NatContext.Mangle = append(state.NatContext.Mangle, row)
			}
		}
		state.NatContext.RoutingRules = (*state.ROSPolicy)["/routing/rule"]
		state.NatContext.VRFs = (*state.ROSPolicy)["/ip/vrf"]
		state.NatContext.Routes = (*state.ROSPolicy)[rosRoutes]
	}
	if isROS {
		var ipv6Host *IPv6Host
		hostReport := func() IPv6Host {
			if ipv6Host == nil {
				v := inspectIPv6Host(ctx, req.ProfileID, req.Config, gateway)
				ipv6Host = &v
			}
			return *ipv6Host
		}
		var agent *model.Agent
		if gateway != nil {
			agent = gateway.Agent
		}
		if IPv6FakeIPRelevant(&profile, r) {
			h := IPv6Host{}
			if IPv6FakeIPSettings(&profile).Enabled {
				h = hostReport()
			}
			ros.SetIPv6FakeIPManifest(r.State("ipv6_fakeip_state"))
			state.IPv6FakeIPPlan = InspectIPv6FakeIP(ctx, &profile, r, agent, h, ros)
		}
		if IPv6RoutesRelevant(&profile, r, desired) {
			h := IPv6Host{}
			if IPv6RoutesNeedsHost(&profile, r, desired) {
				h = hostReport()
			}
			state.IPv6RoutesPlan = InspectIPv6Routes(ctx, &profile, r, desired, agent, h, ros)
		}
	} else if len(desired.IPv6CIDRs) > 0 {
		desired.Warnings = append(desired.Warnings, fmt.Sprintf(messageIkuaiIPv6Skipped, len(desired.IPv6CIDRs)))
	}
	if r.DNSAgentID != "" && desired.DNSSettings.Error == "" {
		state.DhcpDNS, err = d.ReadDHCPDNS(ctx)
		if err != nil {
			var re *RouterError
			if !errors.As(err, &re) {
				return nil, err
			}
			desired.DNSSettings.Error = fmt.Sprintf(messageDHCPReadFailed, re.Error())
		}
	}
	if isROS && r.DNSAgentID != "" && desired.DNSSettings.Error == "" {
		dns := BuildDNSReport(*desired.DNSSettings, desired.ProxyGateway, state.DhcpDNS)
		if (dns.Status == "pending" || dns.Status == "synced") && len(dns.Items) == 1 {
			item := dns.Items[0]
			state.IPv6DNSPlan, err = InspectIPv6DNS(ctx, desired, DNSCheck{ID: item.ID, Network: item.Network, Gateway: desired.ProxyGateway, DNS: item.DesiredDNS}, ros)
			if err != nil {
				return nil, err
			}
		}
	}
	if IPv6PDGuardRelevant(r) {
		if !isROS {
			return nil, routerError(fmt.Sprintf(messageStageNeedsRouterOS, StageIPv6PDGuard))
		}
		state.IPv6PDGuardPlan, err = InspectIPv6PDGuard(ctx, r, host, ros)
		if err != nil {
			return nil, err
		}
	}
	report, err := BuildReport(desired, state)
	if err != nil {
		return nil, err
	}
	if isROS {
		report.Routes.ForwardHint, err = ForwardHint(ctx, r, desired, state, report, ros)
		if err != nil {
			return nil, err
		}
	}
	if r.ManageReturnRouting {
		report.ReturnRouting = returnStatus(ctx, req.Agents, gateway)
	} else {
		report.ReturnRouting = &ReturnStatus{Status: "disabled", Message: messageReturnDisabled}
		cleanup, err := returnCleanup(ctx, req, r, gateway)
		if err != nil {
			return nil, err
		}
		if cleanup != nil {
			report.ReturnRouting.Cleanup = cleanup
			report.ReturnRouting.Message = messageReturnCleanupPending
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Inspection{gateway, desired, state, report}, nil
}
func inspectionDNS(ctx context.Context, source AgentSource, id string) (DNSSettings, error) {
	if id == "" {
		return DNSAgentSettings(id, nil), nil
	}
	if source == nil {
		return DNSSettings{}, ErrNotPorted
	}
	a, err := source.Agent(ctx, id)
	if err != nil {
		return DNSSettings{}, err
	}
	if a == nil {
		return DNSAgentSettings(id, nil), nil
	}
	dns := a.DNS
	if dns == nil {
		b, e := json.Marshal(a.Agent)
		if e != nil {
			return DNSSettings{}, e
		}
		if e = jsondecode.Unmarshal(b, &dns); e != nil {
			return DNSSettings{}, e
		}
	}
	if dns.HA != nil && dns.HA.Enabled {
		peer, e := source.Agent(ctx, dns.HA.PeerID)
		if e != nil {
			return DNSSettings{}, e
		}
		var view *DNSAgent
		if peer != nil {
			view = peer.DNS
		}
		return DNSHASettings(id, dns, view), nil
	}
	return DNSAgentSettings(id, dns), nil
}
func inspectIPv6Host(ctx context.Context, profileID string, config json.RawMessage, a *GatewayAgent) IPv6Host {
	var cfg struct {
		ProfileID string `json:"profile_id"`
	}
	_ = jsondecode.Unmarshal(config, &cfg)
	if cfg.ProfileID != "" {
		profileID = cfg.ProfileID
	}
	if a == nil || a.Agent == nil || a.ProfileID != profileID {
		return IPv6Host{Error: messageIPv6ForeignGateway}
	}
	if a.ServiceType != "mihomo" || (a.Enabled != nil && !*a.Enabled) {
		return IPv6Host{Error: messageIPv6InvalidGateway}
	}
	h, err := IPv6HostRequest(ctx, a)
	if err != nil {
		return IPv6Host{Error: releaseErrorText(err)}
	}
	return h
}
func returnStatus(ctx context.Context, source AgentSource, a *GatewayAgent) *ReturnStatus {
	if a == nil || a.Agent == nil {
		return &ReturnStatus{Status: "unlinked", Message: messageReturnUnlinked}
	}
	if a.Status != "online" {
		return &ReturnStatus{Status: "offline", Message: fmt.Sprintf(messageReturnOffline, orText(a.Name, a.ID))}
	}
	if source == nil {
		return &ReturnStatus{Status: "unavailable", Message: ErrNotPorted.Error()}
	}
	s, err := source.ReturnAgent(a).Status(ctx)
	if err != nil {
		return &ReturnStatus{Status: "unavailable", Message: releaseErrorText(err)}
	}
	if s == nil {
		return &ReturnStatus{Status: "unavailable", Message: messageReturnIncomplete}
	}
	s.Status = "ok"
	return s
}
func returnCleanup(ctx context.Context, req InspectRequest, r *Router, a *GatewayAgent) (*ReturnCleanup, error) {
	if a == nil || a.Agent == nil || a.Status != "online" || a.Host == "" || a.Port == 0 {
		return nil, nil
	}
	s := returnStatus(ctx, req.Agents, a)
	if s.Status != "ok" || s.Managed == nil || !effectful(s.Managed.Spec) {
		return nil, nil
	}
	safety := returnSafety(s)
	var ok bool
	var blockers []string
	_ = json.Unmarshal(safety["ok"], &ok)
	_ = json.Unmarshal(safety["blockers"], &blockers)
	if !s.Managed.Active && ok && len(blockers) == 0 {
		return nil, nil
	}
	if req.Repo == nil {
		return nil, ErrNotPorted
	}
	profiles, err := routerProfiles(ctx, req.Repo)
	if err != nil {
		return nil, err
	}
	shared := []string{}
	for _, p := range profiles {
		for _, other := range p.Routers {
			if (p.ID != req.ProfileID || other.ID != r.ID) && other.ManageReturnRouting && other.LinkID("proxy_gateway_agent_id") == a.ID {
				shared = append(shared, orText(other.Name, other.ID))
			}
		}
	}
	return &ReturnCleanup{Spec: s.Managed.Spec, SharedWith: shared, Complete: s.Managed.TablePresent, Safety: s.Safety}, nil
}
func dnsGuardSettings(r *Router) DNSGuard {
	if r.DNSGuard == nil {
		return DefaultDNSGuard()
	}
	return *r.DNSGuard
}

// routersOf is Python's routers_of: a missing or non-object routers member or
// a non-list items member means no routers, and non-object items (null
// included) are skipped. Records decode with Router's R5 fallback, so one
// malformed record only affects its own row.
func routersOf(config json.RawMessage) []*Router {
	var cfg struct {
		Routers json.RawMessage `json:"routers"`
	}
	if jsondecode.Unmarshal(config, &cfg) != nil {
		return nil
	}
	return routerRecords(cfg.Routers)
}

// storedRouters is routersOf for a profile's typed routers member. Only the
// member is encoded, to the bytes it has inside the encoded configuration.
func storedRouters(routers *model.Routers) ([]*Router, error) {
	data, err := model.Marshal(routers)
	if err != nil {
		return nil, err
	}
	return routerRecords(data), nil
}

// routerRecords decodes the items of an encoded routers member like routersOf.
func routerRecords(container json.RawMessage) []*Router {
	var routers struct {
		Items json.RawMessage `json:"items"`
	}
	var items []json.RawMessage
	if jsondecode.Unmarshal(container, &routers) != nil || jsondecode.Unmarshal(routers.Items, &items) != nil {
		return nil
	}
	out := make([]*Router, 0, len(items))
	for _, item := range items {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) != nil || fields == nil {
			continue
		}
		r := &Router{}
		if json.Unmarshal(item, r) != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}
