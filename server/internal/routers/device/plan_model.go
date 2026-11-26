package device

import "encoding/json"

// CIDRGateway is one static forwarding path that must be proven before withdrawal.
type CIDRGateway struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
}

// NATChecks is the release NAT verification contract shared with source policy.
type NATChecks struct {
	Gateway        string        `json:"gateway"`
	NATBypass      []string      `json:"nat_bypass"`
	NATNotRequired []CIDRGateway `json:"nat_not_required"`
}

// Op is one release operation. The JSON of the concrete type is the driver contract.
type Op interface {
	OpKind() string
	OpSummary() string
}

// OpHead is embedded by every op type: {"kind": …, "summary": …}.
type OpHead struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// OpKind returns the operation discriminator.
func (h OpHead) OpKind() string { return h.Kind }

// OpSummary returns the user-facing operation summary.
func (h OpHead) OpSummary() string { return h.Summary }

// Stage is one ordered release stage (design/r12-release.md §2.2).
type Stage struct {
	Key     string  `json:"key"`
	Title   string  `json:"title"`
	Manual  bool    `json:"manual"`
	Ops     []Op    `json:"ops"`
	Enabled *bool   `json:"enabled,omitempty"`
	Skipped *bool   `json:"skipped,omitempty"`
	Note    *string `json:"note,omitempty"`
}

// NATUpdateOp adopts or extends one existing NAT exemption. Optional fields
// preserve the exact presence of the R12 driver/API contract.
type NATUpdateOp struct {
	OpHead
	ID           string          `json:"id"`
	AddDst       []string        `json:"add_dst"`
	Comment      string          `json:"comment"`
	Groups       *[]string       `json:"groups,omitempty"`
	KeepForeign  bool            `json:"keep_foreign,omitempty"`
	Expected     json.RawMessage `json:"expected,omitempty"`
	OutInterface *string         `json:"out_interface,omitempty"`
}

const (
	// StageReturnRouting prepares the Agent return path.
	StageReturnRouting = "return_routing"
	// StageRoutes writes IPv4 proxy routes or source policy.
	StageRoutes = "routes"
	// StageIPv6FakeIP writes the IPv6 Fake-IP route.
	StageIPv6FakeIP = "ipv6_fakeip"
	// StageIPv6Routes writes public IPv6 proxy routes.
	StageIPv6Routes = "ipv6_routes"
	// StageNATBypass maintains source-NAT exemptions.
	StageNATBypass = "nat_bypass"
	// StageDHCPDNS advertises the verified DNS target.
	StageDHCPDNS = "dhcp_dns"
	// StageIPv6DNS manages IPv6 DNS advertisements.
	StageIPv6DNS = "ipv6_dns"
	// StageDNSGuard enables forced LAN DNS.
	StageDNSGuard = "dns_guard"
	// StageIPv6PDGuard manages prefix-change protection.
	StageIPv6PDGuard = "ipv6_pd_guard"
	// StageReturnCleanup removes the retired Agent return path last.
	StageReturnCleanup = "return_cleanup"
)
