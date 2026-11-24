package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/spec"
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
	Router    *spec.Router
	Driver    Driver
	Loader    spec.ContentLoader
	Rules     RuleContentLoader
	IPv6Hosts IPv6HostFetcher
	Agents    AgentSource
	Repo      *repo.Repository
}

// Inspection is one coherent desired state and readback, with an optional Agent.
type Inspection struct {
	Gateway *GatewayAgent
	Desired *spec.Desired
	State   *RouterState
	Report  *Report
}

func fetchIPv6Host(ctx context.Context, fetcher IPv6HostFetcher, agent *GatewayAgent) (ipv6.IPv6Host, error) {
	if fetcher == nil {
		return ipv6.IPv6Host{}, device.Error(messageIPv6HostUnavailable)
	}
	return fetcher(ctx, agent)
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
		return nil, device.Error(device.MessageOperationFormat)
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
	if rosDriver, ok := d.(*RouterOS); ok {
		defer rosDriver.ReuseAddressReads()()
	}
	var gateway *GatewayAgent
	if req.Agents != nil {
		gateway, err = req.Agents.Agent(ctx, r.ProxyGatewayAgentID)
		if err != nil {
			return nil, err
		}
	} else if r.ProxyGatewayAgentID != "" || r.DNSAgentID != "" {
		return nil, device.ErrNotPorted
	}
	host := ""
	if gateway != nil && gateway.Agent != nil {
		host = gateway.Host
	}
	loader := req.Loader
	if loader == nil && req.Rules != nil {
		rules := req.Rules
		loader = func(item spec.RuleSet, library *spec.LibraryRule) (string, error) {
			return rules(ctx, req.Config, item, library)
		}
	}
	desired, err := spec.ComputeDesired(ctx, req.Config, host, r.StaticRuleSets(), loader)
	if err != nil {
		return nil, err
	}
	settings, err := inspectionDNS(ctx, req.Agents, r.DNSAgentID)
	if err != nil {
		return nil, err
	}
	desired.DNSSettings = &settings
	rosDriver, isROS := d.(*RouterOS)
	policy := r.Type == "routeros" && (len(r.RosPolicyRuleSets) > 0 || device.HasStoredState(r.State("ros_policy_state")))
	guard := dnsguard.Relevant(r)
	if policy || guard {
		if !isROS {
			return nil, device.Error(fmt.Sprintf(messageStageNeedsRouterOS, device.StageRoutes))
		}
	}
	if policy {
		rosDriver.SetPolicyManifest(r.State("ros_policy_state"))
	}
	if guard {
		rosDriver.SetDNSGuardManifest(r.State("dns_guard_state"))
	}
	state, err := d.ReadState(ctx)
	if err != nil {
		return nil, err
	}
	if !r.ManageReturnRouting {
		RetirePolicyRoutes(state)
	}
	if guard {
		desired.DNSGuard = &spec.DesiredDNSGuard{Settings: dnsGuardSettings(r), Previous: r.State("dns_guard_state")}
		state.DNSGuard, err = dnsguard.Read(ctx, rosDriver.Client)
		if err != nil {
			return nil, err
		}
	}
	if policy {
		desired.ROSPolicy, err = rospolicy.InspectROSPolicyDesired(ctx, req.Config, r, desired, loader)
		if err != nil {
			return nil, err
		}
		state.ROSPolicy, err = rospolicy.SnapshotROSPolicy(ctx, rosDriver.Client)
		if err != nil {
			return nil, err
		}
		owned, e := rospolicy.OwnedROSPolicyRows(r.State("ros_policy_state"), state.ROSPolicy)
		if e != nil {
			owned = nil
		}
		ids := []string{}
		for _, v := range owned {
			if v.Path == ros.PathMangle {
				ids = append(ids, v.ID)
			}
		}
		if state.NatContext == nil {
			state.NatContext = &ros.NatContext{}
		}
		state.NatContext.Mangle = []ros.Row{}
		for _, v := range (*state.ROSPolicy)[ros.PathMangle] {
			if !slices.Contains(ids, v.ID()) {
				row := ros.CloneRow(v)
				delete(row, "bytes")
				delete(row, "packets")
				state.NatContext.Mangle = append(state.NatContext.Mangle, row)
			}
		}
		state.NatContext.RoutingRules = (*state.ROSPolicy)["/routing/rule"]
		state.NatContext.VRFs = (*state.ROSPolicy)["/ip/vrf"]
		state.NatContext.Routes = (*state.ROSPolicy)[ros.PathRoutes]
	}
	if isROS {
		var ipv6Host *ipv6.IPv6Host
		hostReport := func() ipv6.IPv6Host {
			if ipv6Host == nil {
				v := inspectIPv6Host(ctx, req.ProfileID, req.Config, gateway, req.IPv6Hosts)
				ipv6Host = &v
			}
			return *ipv6Host
		}
		var agent *model.Agent
		if gateway != nil {
			agent = gateway.Agent
		}
		if ipv6.IPv6FakeIPRelevant(&profile, r) {
			h := ipv6.IPv6Host{}
			if ipv6.IPv6FakeIPSettings(&profile).Enabled {
				h = hostReport()
			}
			rosDriver.SetIPv6FakeIPManifest(r.State("ipv6_fakeip_state"))
			state.IPv6FakeIPPlan = ipv6.InspectIPv6FakeIP(ctx, &profile, r, agent, h, rosDriver.Client)
		}
		if ipv6.IPv6RoutesRelevant(&profile, r, desired) {
			h := ipv6.IPv6Host{}
			if ipv6.IPv6RoutesNeedsHost(&profile, r, desired) {
				h = hostReport()
			}
			state.IPv6RoutesPlan = ipv6.InspectIPv6Routes(ctx, &profile, r, desired, agent, h, rosDriver.Client)
		}
	} else if len(desired.IPv6CIDRs) > 0 {
		desired.Warnings = append(desired.Warnings, fmt.Sprintf(messageIkuaiIPv6Skipped, len(desired.IPv6CIDRs)))
	}
	if r.DNSAgentID != "" && desired.DNSSettings.Error == "" {
		state.DhcpDNS, err = d.ReadDHCPDNS(ctx)
		if err != nil {
			var re *device.RouterError
			if !errors.As(err, &re) {
				return nil, err
			}
			desired.DNSSettings.Error = fmt.Sprintf(messageDHCPReadFailed, re.Error())
		}
	}
	if isROS && r.DNSAgentID != "" && desired.DNSSettings.Error == "" {
		dns := device.BuildDNSReport(*desired.DNSSettings, desired.ProxyGateway, state.DhcpDNS)
		if (dns.Status == "pending" || dns.Status == "synced") && len(dns.Items) == 1 {
			item := dns.Items[0]
			state.IPv6DNSPlan, err = ipv6.InspectIPv6DNS(ctx, desired, device.DNSCheck{ID: item.ID, Network: item.Network, Gateway: desired.ProxyGateway, DNS: item.DesiredDNS}, rosDriver.Client)
			if err != nil {
				return nil, err
			}
		}
	}
	if ipv6.IPv6PDGuardRelevant(r) {
		if !isROS {
			return nil, device.Error(fmt.Sprintf(messageStageNeedsRouterOS, device.StageIPv6PDGuard))
		}
		state.IPv6PDGuardPlan, err = ipv6.InspectIPv6PDGuard(ctx, r, host, rosDriver.Client)
		if err != nil {
			return nil, err
		}
	}
	report, err := BuildReport(desired, state)
	if err != nil {
		return nil, err
	}
	if isROS {
		report.Routes.ForwardHint, err = ipv6.ForwardHint(ctx, r, desired, ipv6.ForwardInputs{Guard: state.DNSGuard, NAT: state.NatContext, Policy: state.ROSPolicy, PolicyPlan: state.ROSPolicyPlan}, report.Routes, rosDriver.Client)
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
func inspectionDNS(ctx context.Context, source AgentSource, id string) (device.DNSSettings, error) {
	if id == "" {
		return device.DNSAgentSettings(id, nil), nil
	}
	if source == nil {
		return device.DNSSettings{}, device.ErrNotPorted
	}
	a, err := source.Agent(ctx, id)
	if err != nil {
		return device.DNSSettings{}, err
	}
	if a == nil {
		return device.DNSAgentSettings(id, nil), nil
	}
	dns := a.DNS
	if dns == nil {
		b, e := json.Marshal(a.Agent)
		if e != nil {
			return device.DNSSettings{}, e
		}
		if e = jsondecode.Unmarshal(b, &dns); e != nil {
			return device.DNSSettings{}, e
		}
	}
	if dns.HA != nil && dns.HA.Enabled {
		peer, e := source.Agent(ctx, dns.HA.PeerID)
		if e != nil {
			return device.DNSSettings{}, e
		}
		var view *device.DNSAgent
		if peer != nil {
			view = peer.DNS
		}
		return device.DNSHASettings(id, dns, view), nil
	}
	return device.DNSAgentSettings(id, dns), nil
}
func inspectIPv6Host(ctx context.Context, profileID string, config json.RawMessage, a *GatewayAgent, fetcher IPv6HostFetcher) ipv6.IPv6Host {
	var cfg struct {
		ProfileID string `json:"profile_id"`
	}
	_ = jsondecode.Unmarshal(config, &cfg)
	if cfg.ProfileID != "" {
		profileID = cfg.ProfileID
	}
	if a == nil || a.Agent == nil || a.ProfileID != profileID {
		return ipv6.IPv6Host{Error: messageIPv6ForeignGateway}
	}
	if a.ServiceType != "mihomo" || (a.Enabled != nil && !*a.Enabled) {
		return ipv6.IPv6Host{Error: messageIPv6InvalidGateway}
	}
	h, err := fetchIPv6Host(ctx, fetcher, a)
	if err != nil {
		return ipv6.IPv6Host{Error: releaseErrorText(err)}
	}
	return h
}
func returnStatus(ctx context.Context, source AgentSource, a *GatewayAgent) *ReturnStatus {
	if a == nil || a.Agent == nil {
		return &ReturnStatus{Status: "unlinked", Message: messageReturnUnlinked}
	}
	if a.Status != "online" {
		return &ReturnStatus{Status: "offline", Message: fmt.Sprintf(messageReturnOffline, device.OrText(a.Name, a.ID))}
	}
	if source == nil {
		return &ReturnStatus{Status: "unavailable", Message: device.ErrNotPorted.Error()}
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
func returnCleanup(ctx context.Context, req InspectRequest, r *spec.Router, a *GatewayAgent) (*ReturnCleanup, error) {
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
		return nil, device.ErrNotPorted
	}
	profiles, err := spec.RouterProfiles(ctx, req.Repo)
	if err != nil {
		return nil, err
	}
	shared := []string{}
	for _, p := range profiles {
		for _, other := range p.Routers {
			if (p.ID != req.ProfileID || other.ID != r.ID) && other.ManageReturnRouting && other.LinkID("proxy_gateway_agent_id") == a.ID {
				shared = append(shared, device.OrText(other.Name, other.ID))
			}
		}
	}
	return &ReturnCleanup{Spec: s.Managed.Spec, SharedWith: shared, Complete: s.Managed.TablePresent, Safety: s.Safety}, nil
}
func dnsGuardSettings(r *spec.Router) spec.DNSGuard {
	if r.DNSGuard == nil {
		return spec.DefaultDNSGuard()
	}
	return *r.DNSGuard
}
