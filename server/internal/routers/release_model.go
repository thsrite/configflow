package routers

import "encoding/json"

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
	Wrote       bool           `json:"-"`
}
