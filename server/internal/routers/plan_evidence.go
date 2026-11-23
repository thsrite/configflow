package routers

import "encoding/json"

type natPlanEvidence struct {
	Context     *NatContext         `json:"context"`
	AddressSets map[string][]string `json:"address_sets"`
	NATList     *NATListEvidence    `json:"nat_list,omitempty"`
}
type policyPlanEvidence struct {
	NAT     any             `json:"nat"`
	Policy  json.RawMessage `json:"ros_policy"`
	Request json.RawMessage `json:"request"`
}
type fakePlanEvidence struct {
	NAT   any             `json:"nat"`
	Fake  IPv6EvidenceSet `json:"ipv6_fakeip"`
	Check IPv6FakeIPCheck `json:"ipv6_check"`
}
type routes6PlanEvidence struct {
	IPv4   any              `json:"v4"`
	Routes IPv6EvidenceSet  `json:"ipv6_routes"`
	Check  *IPv6RoutesCheck `json:"ipv6_routes_check"`
	Skip   []string         `json:"ipv6_routes_skip"`
}
type dns6PlanEvidence struct {
	Routing any              `json:"routing"`
	DNS     *IPv6DNSEvidence `json:"ipv6_dns"`
	Check   *IPv6DNSCheck    `json:"ipv6_dns_check"`
}
type pdPlanEvidence struct {
	Base   any             `json:"base"`
	PD     *IPv6PDEvidence `json:"ipv6_pd_guard"`
	Status string          `json:"ipv6_pd_guard_status"`
}
type dhcpPlanEvidence struct {
	AgentID   string        `json:"agent_id"`
	TargetDNS string        `json:"target_dns"`
	Gateway   string        `json:"gateway"`
	State     *DhcpDnsState `json:"state"`
	HA        *DNSHA        `json:"ha,omitempty"`
}

func planEvidence(r *Router, d *Desired, state *RouterState, listed *NATListPlan, dns DNSReport) (any, any) {
	var evidence any
	if state.NatContext != nil {
		v := natPlanEvidence{Context: state.NatContext, AddressSets: state.AddressSets}
		if listed != nil {
			v.NATList = listed.Evidence
		}
		evidence = v
	}
	if v := state.ROSPolicyPlan; v != nil {
		evidence = policyPlanEvidence{evidence, v.Evidence, v.Request}
	}
	if v := state.IPv6FakeIPPlan; v != nil {
		evidence = fakePlanEvidence{evidence, v.Evidence, v.Check}
	}
	if v := state.IPv6RoutesPlan; v != nil {
		evidence = routes6PlanEvidence{evidence, v.Evidence, v.Check, v.Skip}
	}
	if v := state.IPv6DNSPlan; v != nil {
		evidence = dns6PlanEvidence{evidence, v.Evidence, v.Check}
	}
	if v := state.IPv6PDGuardPlan; v != nil {
		evidence = pdPlanEvidence{evidence, v.Evidence, v.Status}
	}
	var dhcp any
	if r.DNSAgentID != "" {
		v := dhcpPlanEvidence{AgentID: r.DNSAgentID, TargetDNS: dns.TargetDNS, Gateway: d.ProxyGateway, State: state.DhcpDNS}
		if d.DNSSettings != nil {
			v.HA = d.HA
		}
		dhcp = v
	}
	return evidence, dhcp
}
