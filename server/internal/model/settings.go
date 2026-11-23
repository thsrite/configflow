package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
)

// Profile engine settings are plain records (O18 T1c). Members the default
// profile template always wrote are always written; the others are omitzero.

// MihomoSettings stores the editable YAML text and profile-level provider,
// EasyTier and gateway settings; text is parsed only by consumers. RouterSplit,
// LinuxGateway and LinuxGatewayRestorePoint are schema-free objects owned by
// the router-split and Linux gateway features.
type MihomoSettings struct {
	CustomConfig             string            `json:"custom_config" stored:"restrict" falsy:"empty"`
	ProviderUpdateInterval   *Int              `json:"provider_update_interval,omitzero"`
	EasyTier                 *EasyTierSettings `json:"easytier,omitzero"`
	RouterSplit              Dynamic           `json:"router_split,omitzero"`
	LinuxGateway             Dynamic           `json:"linux_gateway,omitzero"`
	LinuxGatewayRestorePoint Dynamic           `json:"linux_gateway_restore_point,omitzero"`
}

// SurgeSettings stores the editable client text and explicit smart-group choices.
type SurgeSettings struct {
	CustomConfig string            `json:"custom_config" stored:"restrict" falsy:"empty"`
	SmartGroups  []SurgeSmartGroup `json:"smart_groups,omitzero"`
}

// SurgeSmartGroup selects a policy group and its client priority expression.
type SurgeSmartGroup struct {
	GroupID        string `json:"group_id"`
	PolicyPriority string `json:"policy_priority,omitzero"`
}

// LoonSettings stores the editable Loon client text.
type LoonSettings struct {
	CustomConfig string `json:"custom_config" stored:"restrict" falsy:"empty"`
}

// EasyTierSettings holds the profile overlay settings without applying defaults during decoding.
type EasyTierSettings struct {
	Enabled   *bool              `json:"enabled,omitzero"`
	Name      *string            `json:"name,omitzero"`
	ApplyTo   *string            `json:"apply_to,omitzero"`
	Routes    []string           `json:"routes,omitzero"`
	MagicDNS  *bool              `json:"magic_dns,omitzero"`
	Keepalive *EasyTierKeepalive `json:"keepalive,omitzero"`
	Options   EasyTierOptions    `json:"options"`
}

// EasyTierKeepalive holds the optional overlay health request.
type EasyTierKeepalive struct {
	Enabled  *bool  `json:"enabled,omitzero"`
	URL      string `json:"url"`
	Interval *Int   `json:"interval,omitzero"`
}

// EasyTierOptions declares every option accepted by Python mihomo_easytier.FIELDS.
type EasyTierOptions struct {
	NetworkName         string   `json:"network-name,omitzero"`
	NetworkSecret       string   `json:"network-secret,omitzero"`
	Hostname            string   `json:"hostname,omitzero"`
	IPv4                string   `json:"ipv4,omitzero"`
	DHCP                *bool    `json:"dhcp,omitzero"`
	Peers               []string `json:"peers,omitzero"`
	SecureMode          *bool    `json:"secure-mode,omitzero"`
	LocalPrivateKey     string   `json:"local-private-key,omitzero"`
	LocalPublicKey      string   `json:"local-public-key,omitzero"`
	Listeners           []string `json:"listeners,omitzero"`
	NoListener          *bool    `json:"no-listener,omitzero"`
	MappedListeners     []string `json:"mapped-listeners,omitzero"`
	ExitNodes           []string `json:"exit-nodes,omitzero"`
	ProxyNetworks       []string `json:"proxy-networks,omitzero"`
	InstanceName        string   `json:"instance-name,omitzero"`
	StateDir            string   `json:"state-dir,omitzero"`
	UDP                 *bool    `json:"udp,omitzero"`
	AcceptDNS           *bool    `json:"accept-dns,omitzero"`
	EnableExitNode      *bool    `json:"enable-exit-node,omitzero"`
	EnableEncryption    *bool    `json:"enable-encryption,omitzero"`
	EncryptionAlgorithm string   `json:"encryption-algorithm,omitzero"`
	PrivateMode         *bool    `json:"private-mode,omitzero"`
	LatencyFirst        *bool    `json:"latency-first,omitzero"`
	DisableP2P          *bool    `json:"disable-p2p,omitzero"`
	EnableKCPProxy      *bool    `json:"enable-kcp-proxy,omitzero"`
	DisableKCPInput     *bool    `json:"disable-kcp-input,omitzero"`
	EnableQUICProxy     *bool    `json:"enable-quic-proxy,omitzero"`
	DisableQUICInput    *bool    `json:"disable-quic-input,omitzero"`
	MTU                 *Int     `json:"mtu,omitzero"`
	TLDDNSZone          string   `json:"tld-dns-zone,omitzero"`
	DialerProxy         string   `json:"dialer-proxy,omitzero"`
	InterfaceName       string   `json:"interface-name,omitzero"`
	RoutingMark         *Int     `json:"routing-mark,omitzero"`
	IPVersion           *string  `json:"ip-version,omitzero"`
}

// MosDNSSettings stores editor settings and raw YAML text without parsing or
// normalizing a draft. The cache numbers are int:"text": Python's editor read
// them with int(), so input may give them as ASCII decimal text.
type MosDNSSettings struct {
	CustomConfig        string        `json:"custom_config" stored:"restrict" falsy:"empty"`
	DirectRulesets      []string      `json:"direct_rulesets"`
	ProxyRulesets       []string      `json:"proxy_rulesets"`
	DirectRules         []string      `json:"direct_rules"`
	ProxyRules          []string      `json:"proxy_rules"`
	CustomMatches       []MosDNSMatch `json:"custom_matches"`
	CustomMatchPosition *string       `json:"custom_match_position,omitzero"`
	LocalDNS            string        `json:"local_dns"`
	RemoteDNS           string        `json:"remote_dns"`
	FallbackDNS         string        `json:"fallback_dns"`
	DefaultForward      *string       `json:"default_forward,omitzero"`
	CustomHosts         string        `json:"custom_hosts"`
	LogEnabled          *bool         `json:"log_enabled,omitzero"`
	LogLevel            *string       `json:"log_level,omitzero"`
	LogFile             *string       `json:"log_file,omitzero"`
	APIEnabled          *bool         `json:"api_enabled,omitzero"`
	APIAddress          *string       `json:"api_address,omitzero"`
	APIAddr             *string       `json:"api_addr,omitzero"`
	ListenAddress       *string       `json:"listen_address,omitzero"`
	CacheEnabled        *bool         `json:"cache_enabled,omitzero"`
	CacheSize           *Int          `json:"cache_size,omitzero" int:"text"`
	CacheLazyTTL        *Int          `json:"cache_lazy_ttl,omitzero" int:"text"`
	CacheDumpEnabled    *bool         `json:"cache_dump_enabled,omitzero"`
	CacheDumpFile       *string       `json:"cache_dump_file,omitzero"`
	CacheDumpInterval   *Int          `json:"cache_dump_interval,omitzero" int:"text"`
}

// MosDNSMatch is an editor match: an object, or the legacy scalar text form
// (Text != nil) that is stored and displayed but ignored by generators. It
// decodes itself because a stored string is a valid value, not a wrong type.
type MosDNSMatch struct {
	// Text retains the legacy scalar form for the editor; generators ignore it.
	Text    *string   `json:"-"`
	ID      string    `json:"id"`
	Enabled *bool     `json:"enabled,omitzero"`
	Matches TextLines `json:"matches"`
	Exec    string    `json:"exec"`
}

type mosDNSMatchWire MosDNSMatch

// UnmarshalJSON decodes the scalar text form or, leniently, the object form.
func (r *MosDNSMatch) UnmarshalJSON(data []byte) error {
	text := bytes.TrimSpace(data)
	if len(text) > 0 && text[0] == '"' {
		var value string
		if err := json.Unmarshal(text, &value); err != nil {
			return err
		}
		*r = MosDNSMatch{Text: &value}
		return nil
	}
	return decodePlain(data, (*mosDNSMatchWire)(r))
}

// storedIssues reports the object form's members of the wrong type and the
// members it does not declare (the text form has none).
func (r *MosDNSMatch) storedIssues(raw []byte, prefix string) []FieldIssue {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var wire mosDNSMatchWire
	issues, _ := DecodeStored(raw, &wire, prefix)
	return issues
}

// MarshalJSON writes the scalar text form or the object members.
func (r MosDNSMatch) MarshalJSON() ([]byte, error) {
	if r.Text != nil {
		return encodeUnit(*r.Text)
	}
	return marshalPlain((*mosDNSMatchWire)(&r))
}

// DomainDiscovery holds profile discovery choices, user-keyed notes and applied-rule history.
type DomainDiscovery struct {
	DirectRuleset          *string             `json:"direct_ruleset,omitzero"`
	ProxyRuleset           *string             `json:"proxy_ruleset,omitzero"`
	Ignored                []string            `json:"ignored,omitzero"`
	AutoProbe              *bool               `json:"auto_probe,omitzero"`
	AutoApply              *bool               `json:"auto_apply,omitzero" stored:"gate"`
	AutoApplyMinConfidence *Int                `json:"auto_apply_min_confidence,omitzero"`
	History                []DiscoveryHistory  `json:"history,omitzero"`
	RegionCheckEnabled     *bool               `json:"region_check_enabled,omitzero"`
	RegionProbePort        *Int                `json:"region_probe_port,omitzero"`
	RegionalRulesets       *OrderedMap[string] `json:"regional_rulesets,omitzero"`
	WebsiteNotes           OrderedMap[string]  `json:"website_notes,omitzero"`
}

// DiscoveryHistory records one applied discovery result and its optional
// recheck. Python wrote verdict, confidence and recheck on every entry (null
// when unknown), so they are always written.
type DiscoveryHistory struct {
	Value      string            `json:"value"`
	RuleType   string            `json:"rule_type"`
	Target     string            `json:"target"`
	RulesetID  string            `json:"ruleset_id"`
	Source     string            `json:"source"`
	AppliedAt  string            `json:"applied_at"`
	Verdict    *string           `json:"verdict"`
	Confidence *Int              `json:"confidence"`
	Recheck    *DiscoveryRecheck `json:"recheck"`
}

// DiscoveryRecheck records a periodic check of an applied discovery rule.
type DiscoveryRecheck struct {
	At            string  `json:"at"`
	Verdict       *string `json:"verdict"`
	SuggestRemove *bool   `json:"suggest_remove" null:"keep"`
	Reason        string  `json:"reason"`
}

// ClientFixedIP is a tracked DHCP reservation keyed by client IP, with ownership text kept unchanged.
type ClientFixedIP struct {
	RouterID  string  `json:"router_id"`
	Vendor    string  `json:"vendor"`
	ID        string  `json:"id"`
	IP        string  `json:"ip"`
	MAC       string  `json:"mac"`
	Comment   string  `json:"comment"`
	Server    *string `json:"server,omitzero"`
	Interface *string `json:"interface,omitzero"`
	Gateway   *string `json:"gateway,omitzero"`
}

// DeviceNotes maps client address/range text to display names in stored order.
// Null and NonTextName mark stored notes that Python refused to extend
// (stored as null; a name that is not text, read as absent). Reads ignore
// them; a notes write refuses such notes with Python's message instead of
// replacing them. Stored notes of another JSON type (a list) read as empty
// notes, not damaged (O18 T1a).
type DeviceNotes struct {
	OrderedMap[string]
	Null        bool
	NonTextName bool
}

// MarshalJSON writes the notes in order.
func (n DeviceNotes) MarshalJSON() ([]byte, error) { return n.OrderedMap.MarshalJSON() }

// IsZero reports empty notes.
func (n DeviceNotes) IsZero() bool { return n.OrderedMap.IsZero() }

// UnmarshalJSON decodes stored notes leniently (see decodeStoredNotes).
func (n *DeviceNotes) UnmarshalJSON(data []byte) error {
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true))
	return decodeStoredNotes(dec, &issueSink{}, n)
}

// UnmarshalJSONFrom decodes notes given as input: an object of text names
// only (null or another value is a type error).
func (n *DeviceNotes) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() != '{' {
		return fmt.Errorf("%w: device notes must be an object", ErrType)
	}
	var notes DeviceNotes
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	for dec.PeekKind() != '}' {
		key, err := dec.ReadToken()
		if err != nil {
			return err
		}
		name := key.String()
		if dec.PeekKind() != '"' {
			return fmt.Errorf("%w: device note %q must be text", ErrType, name)
		}
		value, err := dec.ReadToken()
		if err != nil {
			return err
		}
		notes.Set(name, value.String())
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	*n = notes
	return nil
}

// decodeStoredNotes reads stored notes: null is empty notes marked Null, a
// name that is not text is left out and marked NonTextName, another JSON kind
// is empty notes; each with an issue.
func decodeStoredNotes(dec *jsontext.Decoder, sink *issueSink, n *DeviceNotes) error {
	*n = DeviceNotes{}
	switch dec.PeekKind() {
	case 'n':
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		n.Null = true
		sink.add(dec, IssueType)
		return nil
	case '{':
	default:
		return skipInvalid(dec, sink, func() {})
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	for dec.PeekKind() != '}' {
		key, err := dec.ReadToken()
		if err != nil {
			return err
		}
		name := key.String() // the token is voided by the next read
		if dec.PeekKind() != '"' {
			if err := dec.SkipValue(); err != nil {
				return err
			}
			n.NonTextName = true
			sink.add(dec, IssueType)
			continue
		}
		value, err := dec.ReadToken()
		if err != nil {
			return err
		}
		n.Set(name, value.String())
	}
	_, err := dec.ReadToken()
	return err
}

// ClientFixedIPs maps client IPv4 text to tracked reservations in stored order.
type ClientFixedIPs = OrderedMap[ClientFixedIP]
