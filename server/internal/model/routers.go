package model

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Router records are plain records (O18 T1c): ordinary structs decoded with
// the stored-data rules and written member by member. The members Python's
// router normalization always writes are always written; the others are
// omitzero. Switches that make a publication write to the device read as off
// when stored with a wrong type (stored:"gate").

// Routers is the profile-scoped router collection.
type Routers struct {
	Items []Router `json:"items"`
}

// Router is the stored connection and publication record; defaults and public projections belong to consumers.
type Router struct {
	// The connection identity (ID, type, endpoint, credentials, Agent links)
	// stored as anything but text reads as DamagedText (stored:"restrict";
	// null too for the required members, null:"damaged") and the router is
	// refused for device management, as Python and the pre-O18 T1c Go
	// refused it; written back, the damage stays (HR O18 T1c, M3 ruling).
	// Two routers stored as 1 and "1" do not become the same router.
	ID                  string            `json:"id" stored:"restrict" null:"damaged"`
	Name                string            `json:"name,omitzero"`
	Type                string            `json:"type" stored:"restrict" null:"damaged"`
	Endpoint            string            `json:"endpoint" stored:"restrict" null:"damaged"`
	Username            string            `json:"username" stored:"restrict" null:"damaged"`
	Password            string            `json:"password" stored:"restrict" null:"damaged"`
	VerifyTLS           *bool             `json:"verify_tls,omitzero" switch:"on"`
	ProxyGatewayAgentID string            `json:"proxy_gateway_agent_id" stored:"restrict"`
	DNSAgentID          string            `json:"dns_agent_id" stored:"restrict"`
	IPRuleSets          []string          `json:"ip_rule_sets" stored:"selection"`
	ManageReturnRouting *bool             `json:"manage_return_routing,omitzero" stored:"gate"`
	ROSPolicyRuleSets   []string          `json:"ros_policy_rule_sets" stored:"selection"`
	AutoRuleSync        *bool             `json:"auto_rule_sync,omitzero" stored:"gate"`
	RuleSyncInterval    *Int              `json:"rule_sync_interval,omitzero"`
	DNSGuard            *RouterDNSGuard   `json:"dns_guard,omitzero"`
	IPv6PDGuard         *RouterPDGuard    `json:"ipv6_pd_guard,omitzero"`
	LastProbe           *RouterProbe      `json:"last_probe,omitzero"`
	LastRelease         *RouterRelease    `json:"last_release,omitzero"`
	ROSPolicyState      *ROSPolicyState   `json:"ros_policy_state,omitzero"`
	DNSGuardState       *DNSGuardState    `json:"dns_guard_state,omitzero"`
	IPv6FakeIPState     *IPv6FakeIPState  `json:"ipv6_fakeip_state,omitzero"`
	IPv6RoutesState     *IPv6RoutesState  `json:"ipv6_routes_state,omitzero"`
	IPv6PDGuardState    *IPv6PDGuardState `json:"ipv6_pd_guard_state,omitzero"`
	StateRestoredAt     *string           `json:"state_restored_at,omitzero"`
}

// RouterStateMembers are the members of a Router record that hold run-time
// publication state outside the configuration rows (agent_state table under
// the router member prefix). Every other Router member stays in the
// configuration row.
var RouterStateMembers = []string{
	"last_release",
	"state_restored_at",
}

// RouterDNSGuard holds the explicit DNS guard settings; an absent member keeps
// its default. An enabled guard stored with a malformed member, another JSON
// type or undeclared members is kept whole (Stored): its settings are unknown,
// so device management refuses it (fail closed) and writes keep it for the
// settings owner to repair.
type RouterDNSGuard struct {
	Enabled  *bool    `json:"enabled,omitzero" stored:"gate"`
	BlockDoT *bool    `json:"block_dot,omitzero"`
	Failover *bool    `json:"failover,omitzero"`
	Bypass   []string `json:"bypass,omitzero"`
	// Stored keeps a damaged stored guard; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// RouterPDGuard holds the explicit IPv6 prefix-delegation guard switch, kept
// whole when damaged like RouterDNSGuard.
type RouterPDGuard struct {
	Enabled *bool `json:"enabled,omitzero" stored:"gate"`
	// Stored keeps a damaged stored guard; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

func (r *RouterDNSGuard) keepDamaged(raw []byte) { *r = RouterDNSGuard{Stored: bytes.Clone(raw)} }

func (r *RouterDNSGuard) keepOnIssues(issues []FieldIssue, raw []byte) bool {
	return enabledGuardIssue(r.Enabled, issues, raw)
}

func (r *RouterPDGuard) keepDamaged(raw []byte) { *r = RouterPDGuard{Stored: bytes.Clone(raw)} }

func (r *RouterPDGuard) keepOnIssues(issues []FieldIssue, raw []byte) bool {
	return enabledGuardIssue(r.Enabled, issues, raw)
}

// enabledGuardIssue reports a guard to keep whole: an enabled guard with an
// issue on another member, or a guard whose switch is stored with another
// type that Python read as on ("yes", 1). The switch is a device-write gate
// and reads as off, but for a guard off means withdrawing it from the device
// on the next publication: a damaged switch must not remove a guard Python
// kept, so device management refuses it (fail closed, as before O18 T1c). A
// falsy switch of another type (0, "") is off in Python too.
func enabledGuardIssue(enabled *bool, issues []FieldIssue, raw []byte) bool {
	for _, issue := range issues {
		if issue.Kind == IssueType && (issue.Path == "enabled" || strings.HasSuffix(issue.Path, ".enabled")) {
			var guard struct {
				Enabled json.RawMessage `json:"enabled"`
			}
			if json.Unmarshal(raw, &guard) != nil {
				return true
			}
			return Switch(guard.Enabled, false)
		}
	}
	if enabled == nil || !*enabled {
		return false
	}
	for _, issue := range issues {
		if !strings.HasSuffix(issue.Path, "enabled") {
			return true
		}
	}
	return false
}

// RouterProbe records a successful driver identity or a failed probe message.
type RouterProbe struct {
	At           string   `json:"at"`
	OK           bool     `json:"ok"`
	Vendor       *string  `json:"vendor,omitzero"`
	Version      *string  `json:"version,omitzero"`
	Model        *string  `json:"model,omitzero"`
	Capabilities []string `json:"capabilities,omitzero"`
	Message      *string  `json:"message,omitzero"`
}

// RouterRelease is the last publication result, not a persistent in-progress release lock.
type RouterRelease struct {
	At          string  `json:"at"`
	OK          bool    `json:"ok"`
	Fingerprint string  `json:"fingerprint"`
	FailedStage *string `json:"failed_stage,omitzero"`
	Message     string  `json:"message"`
}
