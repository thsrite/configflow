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
// when stored with a wrong type (switch:"off").

// Routers is the profile-scoped router collection.
type Routers struct {
	Items []Router `json:"items"`
}

// Router is the stored connection and publication record; defaults and public projections belong to consumers.
type Router struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name,omitzero"`
	Type                string            `json:"type"`
	Endpoint            string            `json:"endpoint"`
	Username            string            `json:"username"`
	Password            string            `json:"password"`
	VerifyTLS           *bool             `json:"verify_tls,omitzero" switch:"on"`
	ProxyGatewayAgentID string            `json:"proxy_gateway_agent_id"`
	DNSAgentID          string            `json:"dns_agent_id"`
	IPRuleSets          []string          `json:"ip_rule_sets"`
	ManageReturnRouting *bool             `json:"manage_return_routing,omitzero" switch:"off"`
	ROSPolicyRuleSets   []string          `json:"ros_policy_rule_sets"`
	AutoRuleSync        *bool             `json:"auto_rule_sync,omitzero" switch:"off"`
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
}

// RouterDNSGuard holds the explicit DNS guard settings; an absent member keeps
// its default. An enabled guard stored with a malformed member, another JSON
// type or undeclared members is kept whole (Stored): its settings are unknown,
// so device management refuses it (fail closed) and writes keep it for the
// settings owner to repair.
type RouterDNSGuard struct {
	Enabled  *bool    `json:"enabled,omitzero" switch:"off"`
	BlockDoT *bool    `json:"block_dot,omitzero"`
	Failover *bool    `json:"failover,omitzero"`
	Bypass   []string `json:"bypass,omitzero"`
	// Stored keeps a damaged stored guard; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// RouterPDGuard holds the explicit IPv6 prefix-delegation guard switch, kept
// whole when damaged like RouterDNSGuard.
type RouterPDGuard struct {
	Enabled *bool `json:"enabled,omitzero" switch:"off"`
	// Stored keeps a damaged stored guard; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

func (r *RouterDNSGuard) keepDamaged(raw []byte) { *r = RouterDNSGuard{Stored: bytes.Clone(raw)} }

func (r *RouterDNSGuard) keepOnIssues(issues []FieldIssue) bool {
	return enabledGuardIssue(r.Enabled, issues)
}

func (r *RouterPDGuard) keepDamaged(raw []byte) { *r = RouterPDGuard{Stored: bytes.Clone(raw)} }

func (r *RouterPDGuard) keepOnIssues(issues []FieldIssue) bool {
	return enabledGuardIssue(r.Enabled, issues)
}

// enabledGuardIssue reports an enabled guard with an issue on another member
// than its switch (a switch of the wrong type reads as off: nothing to keep).
func enabledGuardIssue(enabled *bool, issues []FieldIssue) bool {
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
