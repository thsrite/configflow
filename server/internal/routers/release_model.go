package routers

import (
	"encoding/json"
	"slices"
)

// Plan is the reviewable, ordered release and its readback requirements.
type Plan struct {
	Stages       []Stage  `json:"stages"`
	Checks       *Checks  `json:"checks,omitempty"`
	Blockers     []string `json:"blockers"`
	Warnings     []string `json:"warnings"`
	Script       string   `json:"script"`
	Fingerprint  string   `json:"fingerprint"`
	Empty        bool     `json:"empty"`
	SyncBaseline *bool    `json:"sync_baseline,omitempty"`
}

// PolicyRouteCheck is the destination set of a source-aware route.
type PolicyRouteCheck struct {
	Comment string   `json:"comment"`
	Gateway string   `json:"gateway"`
	Dst     []string `json:"dst"`
}

// Checks carries exactly the checks applicable to this release. Pointers to
// slices preserve the difference between absent and a present empty array.
type Checks struct {
	ipv6DNSPresent   bool
	rosPolicyPresent bool
	NATChecks
	Routes              []CIDRGateway              `json:"routes"`
	RemovedRoutes       []string                   `json:"removed_routes"`
	PolicyRoutes        *[]PolicyRouteCheck        `json:"policy_routes,omitempty"`
	RestoredRoutes      *[]CIDRGateway             `json:"restored_routes,omitempty"`
	RemovedPolicyRoutes *[]string                  `json:"removed_policy_routes,omitempty"`
	IPv6FakeIP          *IPv6FakeIPCheck           `json:"ipv6_fakeip,omitempty"`
	IPv6Routes          *IPv6RoutesCheck           `json:"ipv6_routes,omitempty"`
	NATList             *NATListCheck              `json:"nat_list,omitempty"`
	ROSPolicy           *ROSPolicyCheck            `json:"ros_policy,omitempty"`
	ReturnRouting       *ReturnSpec                `json:"return_routing,omitempty"`
	DNSGuard            *DNSGuardCheck             `json:"dns_guard,omitempty"`
	IkuaiGroups         map[string]GroupStageCheck `json:"ikuai_groups,omitempty"`
	DHCPDNS             *DNSCheck                  `json:"dhcp_dns,omitempty"`
	IPv6DNS             *IPv6DNSCheck              `json:"ipv6_dns,omitempty"`
	IPv6PDGuard         json.RawMessage            `json:"ipv6_pd_guard,omitempty"`
}

// RouteAddOp creates a static route.
type RouteAddOp struct {
	OpHead
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	Comment string `json:"comment"`
}

// RouteCommentOp adopts one existing static route.
type RouteCommentOp struct {
	OpHead
	ID      string `json:"id"`
	CIDR    string `json:"cidr"`
	Comment string `json:"comment"`
}

// RouteDeleteOp withdraws one existing static route.
type RouteDeleteOp struct {
	OpHead
	ID   string `json:"id"`
	CIDR string `json:"cidr"`
}

// PolicyRouteOp creates or updates a source-aware route.
type PolicyRouteOp struct {
	OpHead
	ID          string    `json:"id,omitempty"`
	Gateway     string    `json:"gateway"`
	Dst         []string  `json:"dst"`
	Comment     string    `json:"comment"`
	Groups      *[]string `json:"groups,omitempty"`
	KeepForeign bool      `json:"keep_foreign,omitempty"`
}

// PolicyRouteDeleteOp withdraws a source-aware route.
type PolicyRouteDeleteOp struct {
	OpHead
	ID string `json:"id"`
}

// NATAddOp creates a source NAT exemption.
type NATAddOp struct {
	OpHead
	Src          []string  `json:"src"`
	Dst          []string  `json:"dst"`
	Comment      string    `json:"comment"`
	Groups       *[]string `json:"groups,omitempty"`
	OutInterface *string   `json:"out_interface,omitempty"`
}

// DHCPDNSUpdateOp replaces only the selected DHCP network's DNS list.
type DHCPDNSUpdateOp struct {
	OpHead
	ID       string          `json:"id"`
	DNS      []string        `json:"dns"`
	Expected json.RawMessage `json:"expected"`
	Scope    DNSScope        `json:"scope"`
}

// DNSScope identifies a DHCP network without including its DNS values.
type DNSScope struct {
	ID      string `json:"id"`
	Network string `json:"network"`
	Gateway string `json:"gateway"`
}

// ReturnApplyOp applies a negotiated Agent spec and preserves its old raw spec.
type ReturnApplyOp struct {
	OpHead
	Spec     *ReturnSpec     `json:"spec"`
	Previous json.RawMessage `json:"previous"`
}

// ReturnRemoveOp removes only resources whose ownership the Agent proves.
type ReturnRemoveOp struct {
	OpHead
	RestoreLegacy bool            `json:"restore_legacy"`
	Previous      json.RawMessage `json:"previous,omitempty"`
}

// OpKind returns the group operation discriminator.
func (op GroupOp) OpKind() string { return op.Kind }

// OpSummary returns the group operation's user-facing summary.
func (op GroupOp) OpSummary() string { return op.Summary }

// ReleaseEntry is one execution or compensation outcome.
type ReleaseEntry struct {
	Stage   string `json:"stage"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

// Result records partial success even when shutdown stops further device I/O.
type Result struct {
	OK          bool           `json:"ok"`
	FailedStage *string        `json:"failed_stage,omitempty"`
	Log         []ReleaseEntry `json:"log"`
	Rollback    []ReleaseEntry `json:"rollback"`
	Aborted     bool           `json:"-"`
	// Wrote is set as soon as an operation is attempted, not when it is
	// proven: a write may land before shutdown cancels its proof.
	Wrote bool `json:"-"`
}

// MarshalJSON preserves explicit empty and null feature checks independently
// of the native verifier pointer used at execution time.
func (c Checks) MarshalJSON() ([]byte, error) {
	type plain Checks
	b, err := json.Marshal(plain(c))
	if err != nil {
		return nil, err
	}
	newNATRule := false
	for _, stage := range c.IkuaiGroups {
		newNATRule = newNATRule || slices.ContainsFunc(stage.Rules, isNewNATRuleCheck)
	}
	if !(c.ipv6DNSPresent && c.IPv6DNS == nil) && !(c.rosPolicyPresent && c.ROSPolicy == nil) && !newNATRule {
		return b, nil
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	if newNATRule {
		if fields["ikuai_groups"], err = releaseGroupChecks(c.IkuaiGroups); err != nil {
			return nil, err
		}
	}
	if c.ipv6DNSPresent && c.IPv6DNS == nil {
		fields["ipv6_dns"] = json.RawMessage(`{}`)
	}
	if c.rosPolicyPresent && c.ROSPolicy == nil {
		fields["ros_policy"] = json.RawMessage(`null`)
	}
	return json.Marshal(fields)
}

// isNewNATRuleCheck is the check of a NAT rule this release creates.
func isNewNATRuleCheck(r GroupRuleCheck) bool { return r.Func == "nat_rule" && r.ID == "" }

// releaseGroupChecks is the release view of the iKuai group checks. Python's
// _grouped_nat_ops writes "id": null for a NAT rule it creates, while the
// shared planner check echoes the rules it is given (policy checks have no id),
// so the explicit null belongs to the plan, not to GroupRuleCheck's own JSON.
func releaseGroupChecks(stages map[string]GroupStageCheck) (json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(stages))
	for key, stage := range stages {
		b, err := json.Marshal(stage)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(stage.Rules, isNewNATRuleCheck) {
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(b, &fields); err != nil {
				return nil, err
			}
			rules := make([]json.RawMessage, len(stage.Rules))
			for i, r := range stage.Rules {
				if rules[i], err = json.Marshal(r); err != nil {
					return nil, err
				}
				if isNewNATRuleCheck(r) {
					var rule map[string]json.RawMessage
					if err = json.Unmarshal(rules[i], &rule); err != nil {
						return nil, err
					}
					rule["id"] = json.RawMessage("null")
					if rules[i], err = json.Marshal(rule); err != nil {
						return nil, err
					}
				}
			}
			if fields["rules"], err = json.Marshal(rules); err != nil {
				return nil, err
			}
			if b, err = json.Marshal(fields); err != nil {
				return nil, err
			}
		}
		out[key] = b
	}
	return json.Marshal(out)
}
