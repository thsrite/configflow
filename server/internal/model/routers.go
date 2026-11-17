package model

// Routers is the profile-scoped router collection, including unknown container members.
type Routers struct {
	Items  []Router `json:"items"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes Routers with stored-member preservation and R5 fallback.
func (r *Routers) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Routers is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Routers) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Routers members.
func (r Routers) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// Router is the stored connection and publication record; defaults and public projections belong to consumers.
type Router struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Endpoint            string            `json:"endpoint"`
	Username            string            `json:"username"`
	Password            string            `json:"password"`
	VerifyTLS           *bool             `json:"verify_tls"`
	ProxyGatewayAgentID string            `json:"proxy_gateway_agent_id"`
	DNSAgentID          string            `json:"dns_agent_id"`
	IPRuleSets          []string          `json:"ip_rule_sets"`
	ManageReturnRouting *bool             `json:"manage_return_routing"`
	ROSPolicyRuleSets   []string          `json:"ros_policy_rule_sets"`
	AutoRuleSync        *bool             `json:"auto_rule_sync"`
	RuleSyncInterval    *Int              `json:"rule_sync_interval"`
	DNSGuard            *RouterDNSGuard   `json:"dns_guard"`
	IPv6PDGuard         *RouterPDGuard    `json:"ipv6_pd_guard"`
	LastProbe           *RouterProbe      `json:"last_probe"`
	LastRelease         *RouterRelease    `json:"last_release"`
	ROSPolicyState      *ROSPolicyState   `json:"ros_policy_state"`
	DNSGuardState       *DNSGuardState    `json:"dns_guard_state"`
	IPv6FakeIPState     *IPv6FakeIPState  `json:"ipv6_fakeip_state"`
	IPv6RoutesState     *IPv6RoutesState  `json:"ipv6_routes_state"`
	IPv6PDGuardState    *IPv6PDGuardState `json:"ipv6_pd_guard_state"`
	Fields              Fields
	raw                 Raw
}

// UnmarshalJSON decodes Router with stored-member preservation and R5 fallback.
func (r *Router) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Router is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Router) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Router members.
func (r Router) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterDNSGuard retains explicit settings, including an absent failover default.
type RouterDNSGuard struct {
	Enabled  *bool    `json:"enabled"`
	BlockDoT *bool    `json:"block_dot"`
	Failover *bool    `json:"failover"`
	Bypass   []string `json:"bypass"`
	Fields   Fields
	raw      Raw
}

// UnmarshalJSON decodes RouterDNSGuard with stored-member preservation and R5 fallback.
func (r *RouterDNSGuard) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when RouterDNSGuard is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *RouterDNSGuard) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged RouterDNSGuard members.
func (r RouterDNSGuard) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterPDGuard retains the explicit IPv6 prefix-delegation guard switch.
type RouterPDGuard struct {
	Enabled *bool `json:"enabled"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes RouterPDGuard with stored-member preservation and R5 fallback.
func (r *RouterPDGuard) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when RouterPDGuard is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *RouterPDGuard) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged RouterPDGuard members.
func (r RouterPDGuard) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterProbe records a successful driver identity or a failed probe message.
type RouterProbe struct {
	At           string   `json:"at"`
	OK           bool     `json:"ok"`
	Vendor       *string  `json:"vendor"`
	Version      *string  `json:"version"`
	Model        *string  `json:"model"`
	Capabilities []string `json:"capabilities"`
	Message      *string  `json:"message"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes RouterProbe with stored-member preservation and R5 fallback.
func (r *RouterProbe) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when RouterProbe is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *RouterProbe) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged RouterProbe members.
func (r RouterProbe) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterRelease is the last publication result, not a persistent in-progress release lock.
type RouterRelease struct {
	At          string  `json:"at"`
	OK          bool    `json:"ok"`
	Fingerprint string  `json:"fingerprint"`
	FailedStage *string `json:"failed_stage"`
	Message     string  `json:"message"`
	Fields      Fields
	raw         Raw
}

// UnmarshalJSON decodes RouterRelease with stored-member preservation and R5 fallback.
func (r *RouterRelease) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when RouterRelease is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *RouterRelease) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged RouterRelease members.
func (r RouterRelease) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
