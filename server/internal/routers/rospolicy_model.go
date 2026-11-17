package routers

import (
	"configflow/internal/devhttp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"sort"

	"configflow/internal/netipx"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

const (
	rosTables    = "/routing/table"
	rosSettings  = "/routing/settings"
	rosListNames = rosAddresses + "#names"
	// ROSPolicyPrefix identifies the owner comment, never an ownership proof.
	ROSPolicyPrefix = "cf-ros-policy-"
)

var policyPaths = []string{rosAddresses, rosTables, rosMangle, rosRoutes, rosNAT, "/routing/rule", rosFilter, "/ip/firewall/raw", "/ip/vrf"}
var policyNamePatterns = []string{"CF_Source_Proxy_IP_[0-9]+", "CF_Destination_Proxy_IP_[0-9]+", "CF_Bypass_IP_[0-9]+"}

// ROSPolicySnapshot contains complete raw configuration and runtime evidence.
type ROSPolicySnapshot map[string][]Row

// ROSPolicyDesired separates selected destinations from ordinary static routes.
type ROSPolicyDesired struct {
	Selected       []string        `json:"selected"`
	Sources        []string        `json:"sources"`
	CIDRs          []ProxyCIDR     `json:"cidrs"`
	Error          string          `json:"error"`
	Owner          string          `json:"owner"`
	Previous       json.RawMessage `json:"previous"`
	Warnings       []string        `json:"warnings,omitempty"`
	SourceRevision string          `json:"source_revision,omitempty"`
}

// MarshalJSON keeps the selected-policy empty warnings array; a withdrawn
// policy has no source_revision or warnings member, as in the inspection API.
func (d ROSPolicyDesired) MarshalJSON() ([]byte, error) {
	type plain ROSPolicyDesired
	if d.SourceRevision != "" {
		return json.Marshal(struct {
			plain
			Warnings []string `json:"warnings"`
		}{plain(d), append([]string{}, d.Warnings...)})
	}
	return json.Marshal(plain(d))
}

// ROSPolicySpec records the exact scope of one deployed bundle.
type ROSPolicySpec struct {
	Sources         []string `json:"sources"`
	Destinations    []string `json:"destinations"`
	Gateway         string   `json:"gateway"`
	NamingVersion   int      `json:"naming_version"`
	PolicyVersion   int      `json:"policy_version"`
	NATInterface    string   `json:"nat_interface"`
	EgressInterface string   `json:"egress_interface"`
}

// ROSPolicyManifest is the private proof of ownership, including device defaults.
type ROSPolicyManifest struct {
	Spec      *ROSPolicySpec `json:"spec"`
	Resources []ROSResource  `json:"resources"`
}

// Manifest distinguishes an untouched driver attribute from an explicit null.
type Manifest struct {
	Set   bool
	Value json.RawMessage
}

// Manifests carries feature-owned resource proofs to release persistence.
type Manifests struct {
	Policy, DNSGuard, IPv6FakeIP, IPv6Routes, IPv6PDGuard Manifest
}

// ManifestHolder exposes the per-release manifest updates (RouterOS only).
type ManifestHolder interface{ Manifests() Manifests }

// Manifests returns independent copies so callers cannot mutate driver proofs.
func (d *RouterOS) Manifests() Manifests {
	return Manifests{IPv6PDGuard: Manifest{d.ipv6PDGuardManifestSet, slices.Clone(d.ipv6PDGuardManifest)}, IPv6FakeIP: Manifest{d.ipv6FakeIPManifestSet, slices.Clone(d.ipv6FakeIPManifest)}, IPv6Routes: Manifest{d.ipv6RoutesManifestSet, slices.Clone(d.ipv6RoutesManifest)}, Policy: Manifest{d.policyManifestSet, slices.Clone(d.policyManifest)}, DNSGuard: Manifest{d.dnsGuardManifestSet, slices.Clone(d.dnsGuardManifest)}}
}

// ROSPolicyCheck is the post-release proof, including migrated old objects.
type ROSPolicyCheck struct {
	Spec         *ROSPolicySpec   `json:"spec"`
	Previous     json.RawMessage  `json:"previous"`
	Migrations   []ROSResource    `json:"migrations"`
	Replacements []ROSReplacement `json:"replacements"`
}

// ROSPolicyReplace prepares a new bundle before retiring the old entry point.
// PrepareStatic is supplied by the release planner, never decoded client input.
type ROSPolicyReplace struct {
	OpHead
	Bundle          []ROSReplacement  `json:"bundle"`
	Previous        json.RawMessage   `json:"previous"`
	Owned           []ROSResource     `json:"owned"`
	Migrations      []ROSResource     `json:"migrations"`
	Replacements    []ROSReplacement  `json:"replacements"`
	Spec            *ROSPolicySpec    `json:"spec"`
	Evidence        ROSPolicySnapshot `json:"evidence"`
	StaticChecks    []CIDRGateway     `json:"static_checks"`
	PrepareStatic   []Op              `json:"prepare_static,omitempty"`
	StaticNATChecks *NATChecks        `json:"static_nat_checks,omitempty"`
}

// MarshalJSON emits prepare_static whenever the planner set it, including an
// empty list (static checks whose replacement routes already exist); nil means
// the key is absent, as in Python's op dict.
func (op ROSPolicyReplace) MarshalJSON() ([]byte, error) {
	type plain ROSPolicyReplace
	if op.PrepareStatic == nil {
		return json.Marshal(plain(op))
	}
	return json.Marshal(struct {
		plain
		PrepareStatic []Op `json:"prepare_static"`
	}{plain(op), op.PrepareStatic})
}

// ROSPolicyItem is one destination shown in the report.
type ROSPolicyItem struct {
	ProxyCIDR
	Status   string   `json:"status"`
	Detail   string   `json:"detail"`
	RouteIDs []string `json:"route_ids"`
	Mode     string   `json:"mode"`
}

// ROSPolicyPlan is the guarded operation and its complete preview evidence.
type ROSPolicyPlan struct {
	Ops      []Op            `json:"ops"`
	Blockers []string        `json:"blockers"`
	Warnings []string        `json:"warnings"`
	Items    []ROSPolicyItem `json:"items"`
	Check    *ROSPolicyCheck `json:"check"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
	Request  json.RawMessage `json:"request,omitempty"`
}

// ROSPolicyDigest is a Go-native opaque revision for the policy workspace.
// Object keys are sorted by encoding/json; formatting is not a stored contract.
func ROSPolicyDigest(value json.RawMessage) (string, error) {
	var decoded any
	if err := devhttp.DecodeJSON(value, &decoded); err != nil {
		return "", fmt.Errorf("decode policy revision: %w", err)
	}
	data, err := json.Marshal(decoded)
	if err != nil {
		return "", fmt.Errorf("encode policy revision: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ROSPolicyConfigRow copies configuration fields, preserving unknown matchers.
func ROSPolicyConfigRow(row Row) Row { return configROSRow(row) }

// ROSPolicyNetworkOverlap reuses the shared collapsed, binary-search index.
func ROSPolicyNetworkOverlap(networks []netip.Prefix) func(netip.Prefix) bool {
	return prefixOverlap(networks)
}

// SubtractNetworks removes sorted disjoint exclusions with a sweep. No
// address-range identities are converted into CIDRs here. Each address family
// is subtracted on its own (an IPv6 exclusion never removes IPv4 space), so the
// result is never an empty list standing in for an input error; invalid
// prefixes are ignored.
func SubtractNetworks(from, minus []netip.Prefix) []netip.Prefix {
	out := []netip.Prefix{}
	for _, is4 := range []bool{true, false} {
		out = append(out, subtractFamily(prefixFamily(from, is4), prefixFamily(minus, is4))...)
	}
	return out
}

func prefixFamily(prefixes []netip.Prefix, is4 bool) []netip.Prefix {
	out := []netip.Prefix{}
	for _, p := range prefixes {
		if p.IsValid() && p.Addr().Is4() == is4 {
			out = append(out, p)
		}
	}
	return out
}

// subtractFamily is the sweep over one address family; Collapse cannot fail
// for single-family input.
func subtractFamily(from, minus []netip.Prefix) []netip.Prefix {
	from, _ = netipx.Collapse(from)
	minus, _ = netipx.Collapse(minus)
	out := []netip.Prefix{}
	first := 0
	for _, network := range from {
		for first < len(minus) && netipx.Broadcast(minus[first]).Less(network.Addr()) {
			first++
		}
		pieces := []netip.Prefix{network}
		for i := first; i < len(minus) && !netipx.Broadcast(network).Less(minus[i].Addr()); i++ {
			parts := []netip.Prefix{}
			for _, piece := range pieces {
				if !piece.Overlaps(minus[i]) {
					parts = append(parts, piece)
				} else if !subnetOf(piece, minus[i]) {
					parts = append(parts, addressExclude(piece, minus[i])...)
				}
			}
			pieces = parts
		}
		out = append(out, pieces...)
	}
	out, _ = netipx.Collapse(out)
	if out == nil {
		out = []netip.Prefix{}
	}
	return out
}

// SmartROSPolicySources validates OxiDNS rows through its shared validator and
// applies longest-prefix smart/direct exceptions, excluding the gateway.
func SmartROSPolicySources(rows json.RawMessage, gateway string) ([]string, error) {
	node, err := yamlx.FromJSON(rows)
	if err != nil {
		return nil, err
	}
	normalized, err := oxidns.NormalizePolicies(node)
	if err != nil {
		return nil, err
	}
	type source struct {
		prefix netip.Prefix
		mode   string
	}
	entries := []source{}
	for _, row := range normalized.Content {
		prefix, ok := netipx.ParseNetwork4(yamlx.Get(row, "ip").Value, false)
		if ok {
			entries = append(entries, source{prefix, yamlx.Get(row, "mode").Value})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].prefix.Bits() < entries[j].prefix.Bits() })
	networks := []netip.Prefix{}
	for _, entry := range entries {
		if entry.mode == "smart" {
			networks, _ = netipx.Collapse(append(networks, entry.prefix))
		} else {
			networks = SubtractNetworks(networks, []netip.Prefix{entry.prefix})
		}
	}
	address, ok := netipx.ParseAddr4(gateway)
	if !ok {
		return nil, routerError(messagePolicyGatewayInvalid)
	}
	networks = SubtractNetworks(networks, []netip.Prefix{netip.PrefixFrom(address, 32)})
	if len(networks) > 4096 {
		return nil, routerError(messagePolicyTooManySources)
	}
	result := []string{}
	for _, network := range networks {
		result = append(result, network.String())
	}
	return result, nil
}

// InspectROSPolicyDesired reads only explicitly selected sets and saved device
// policies. Missing content blocks replacing an existing working bundle.
func InspectROSPolicyDesired(ctx context.Context, config json.RawMessage, router *Router, desired *Desired, loader ContentLoader) (*ROSPolicyDesired, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := ROSPolicyDigest(mustPolicyJSON(router.ID))
	if err != nil {
		return nil, err
	}
	result := &ROSPolicyDesired{Selected: append([]string{}, router.RosPolicyRuleSets...), Sources: []string{}, CIDRs: []ProxyCIDR{}, Owner: ROSPolicyPrefix + owner[:12], Previous: router.State("ros_policy_state")}
	if len(result.Selected) == 0 {
		return result, nil
	}
	policy, err := ComputeDesired(ctx, config, desired.ProxyGateway, result.Selected, loader)
	if err != nil {
		return nil, err
	}
	for _, entry := range policy.ProxyCIDRs {
		if !slices.Equal(entry.Sources, []string{"fake-ip"}) {
			result.CIDRs = append(result.CIDRs, entry)
		}
	}
	result.Warnings = []string{}
	for _, warning := range policy.Warnings {
		if !slices.Contains(policy.BlockingWarnings, warning) {
			result.Warnings = append(result.Warnings, warning)
		}
	}
	if len(policy.BlockingWarnings) > 0 {
		result.Error = messagePolicySelectionBlocked + joinPolicyMessages(policy.BlockingWarnings)
	}
	profile, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return nil, err
	}
	settings := profile.Settings()
	if settings == nil || settings.Tag == "!!null" {
		settings = yamlx.Map()
	}
	result.SourceRevision = oxidns.Revision(settings)
	view, err := oxidns.ReadPolicies(settings, profile)
	if err == nil && !view.Editable {
		err = routerError(view.Reason)
	}
	if err == nil {
		result.Sources, err = SmartROSPolicySources(oxidns.ToJSON(view.Policies), desired.ProxyGateway)
	}
	if err != nil {
		result.Error = err.Error()
	} else if len(result.Sources) == 0 {
		result.Error = messagePolicyNoSources
	}
	return result, nil
}

// mustPolicyJSON is used only for typed, JSON-encodable model values.
func mustPolicyJSON[T any](v T) json.RawMessage { data, _ := json.Marshal(v); return data }
