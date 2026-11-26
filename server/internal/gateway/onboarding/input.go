// Package onboarding prepares and validates profile-scoped Mihomo/OxiDNS drafts.
// It never contacts devices or changes host networking. Persistence belongs to Service.
package onboarding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/gateway"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

const (
	// DefaultLocalDNS is the first-run encrypted direct resolver.
	DefaultLocalDNS = "https://223.5.5.5/dns-query"
	// DNSTarget is the local Mihomo DNS endpoint used by OxiDNS.
	DNSTarget = "127.0.0.1:1053"
)

// Request is a validated wizard form. Optional booleans retain absence.
type Request struct {
	RuleSetIDs                []string     `json:"rule_set_ids"`
	DirectRuleSetIDs          []string     `json:"direct_rule_set_ids"`
	ProxyRuleSetIDs           []string     `json:"proxy_rule_set_ids"`
	LocalDNS                  string       `json:"local_dns"`
	QueryRecorder             *bool        `json:"query_recorder,omitempty"`
	ConfirmTemplateConversion bool         `json:"confirm_template_conversion,omitempty"`
	ExpectedRevision          *json.Number `json:"expected_config_revision,omitempty"`
	DefaultForward            string       `json:"default_forward,omitempty"`
}

func invalid(message string) error { return &gateway.Error{Message: message} }

// ParseRequest rejects unsupported shapes and validates exact field names.
func ParseRequest(raw json.RawMessage) (Request, error) {
	return parseRequest(raw, false)
}

func parseRequest(raw json.RawMessage, fullTUN bool) (Request, error) {
	var body map[string]json.RawMessage
	var result Request
	if !utf8.Valid(raw) || json.Unmarshal(raw, &body) != nil || body == nil {
		return result, invalid(msgFields)
	}
	allowed := []string{"rule_set_ids", "direct_rule_set_ids", "proxy_rule_set_ids", "local_dns", "query_recorder", "confirm_template_conversion", "expected_config_revision", "default_forward"}
	for key := range body {
		if !slices.Contains(allowed, key) {
			return result, invalid(msgFields)
		}
	}
	for _, field := range []struct {
		key string
		dst *[]string
	}{{"rule_set_ids", &result.RuleSetIDs}, {"direct_rule_set_ids", &result.DirectRuleSetIDs}, {"proxy_rule_set_ids", &result.ProxyRuleSetIDs}} {
		value := bytes.TrimSpace(body[field.key])
		if len(value) == 0 || value[0] != '[' || json.Unmarshal(value, field.dst) != nil || len(*field.dst) > 1024 {
			return result, invalid(fmt.Sprintf(fmtIDs, field.key))
		}
		out := make([]string, 0, len(*field.dst))
		seen := map[string]bool{}
		for _, id := range *field.dst {
			if id == "" || utf8.RuneCountInString(id) > 256 || strings.ContainsFunc(id, func(c rune) bool { return c < 32 || c == 127 }) {
				return result, invalid(fmt.Sprintf(fmtIDs, field.key))
			}
			if !seen[id] {
				out = append(out, id)
				seen[id] = true
			}
		}
		*field.dst = out
	}
	for _, id := range result.DirectRuleSetIDs {
		if slices.Contains(result.ProxyRuleSetIDs, id) {
			return result, invalid(msgOverlap)
		}
	}
	for _, key := range []string{"query_recorder", "confirm_template_conversion"} {
		if value, ok := body[key]; ok {
			if string(value) != "true" && string(value) != "false" {
				return result, invalid(fmt.Sprintf(fmtBoolean, key))
			}
			b := string(value) == "true"
			if key == "query_recorder" {
				result.QueryRecorder = &b
			} else {
				result.ConfirmTemplateConversion = b
			}
		}
	}
	if value, ok := body["expected_config_revision"]; ok {
		// JSON integer literals are not restricted to int64 at the HTTP boundary.
		// The revision guard compares them numerically without rounding.
		literal := string(value)
		integer, ok := new(big.Int).SetString(literal, 10)
		if !ok || integer.Sign() < 0 || strings.ContainsAny(literal, `.eE+"`) {
			return result, invalid(msgRevision)
		}
		number := json.Number(literal)
		result.ExpectedRevision = &number
	}

	result.DefaultForward = "forward_remote"
	if value, ok := body["default_forward"]; ok {
		var selected *string
		if json.Unmarshal(value, &selected) != nil || selected == nil {
			return result, invalid(msgDefaultForward)
		}
		result.DefaultForward = *selected
	}

	if result.DefaultForward != "forward_local" && result.DefaultForward != "forward_remote" {
		return result, invalid(msgDefaultForward)
	}
	_ = json.Unmarshal(body["local_dns"], &result.LocalDNS)
	if _, err := localUpstreams(result.LocalDNS, fullTUN, nil); err != nil {
		return result, err
	}
	return result, nil
}

// LocalUpstreams validates the direct DNS endpoints for the selected takeover.
// Parsing and feature validation are shared with the OxiDNS generator.
func LocalUpstreams(value string, fullTUN bool) (*yamlx.Node, error) {
	return localUpstreams(value, fullTUN, nil)
}

func localUpstreams(value string, fullTUN bool, localAddresses []netip.Addr) (*yamlx.Node, error) {
	unsafeMessage := msgUnsafeDNS
	if fullTUN {
		unsafeMessage = "直连 DNS 请填写 IPv4 地址的 UDP、TCP、DoH 或 DoT 上游，避免指向本机 DNS 或代理出口"
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > 16384 {
		return nil, invalid(msgLocalDNS)
	}
	upstreams, err := oxidns.ForwardUpstreams(value, nil)
	if err != nil {
		return nil, invalid(err.Error())
	}
	if len(upstreams.Content) > 16 {
		return nil, invalid(msgLocalLimit)
	}
	for _, upstream := range upstreams.Content {
		raw := text(yamlx.Get(upstream, "addr"))
		if fullTUN && !strings.Contains(raw, "://") {
			raw = "udp://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, invalid(unsafeMessage)
		}
		plain := fullTUN && (u.Scheme == "udp" || u.Scheme == "tcp")
		if plain && (u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Host, ":")) {
			return nil, invalid(unsafeMessage)
		}
		if u.User != nil || !plain && u.Scheme != "https" && u.Scheme != "tls" || u.Scheme == "https" && u.Path == "" {
			return nil, invalid(unsafeMessage)
		}
		if !validUpstreamIPv4(u.Hostname(), plain, localAddresses) || !validUpstreamPort(u.Port(), plain) {
			return nil, invalid(unsafeMessage)
		}
		if plain {
			if dial := text(field(upstream, "dial_addr")); dial != "" && !validUpstreamIPv4(dial, true, localAddresses) {
				return nil, invalid(unsafeMessage)
			}
			if override := field(upstream, "port"); override != nil && !validUpstreamPort(override.Value, true) {
				return nil, invalid(unsafeMessage)
			}
		}
		for _, key := range []string{"socks5", "outbound"} {
			if n := yamlx.Get(upstream, key); n != nil && n.Tag != "!!null" && n.Value != "" && n.Value != "false" {
				return nil, invalid(unsafeMessage)
			}
		}
	}
	return upstreams, nil
}

func validUpstreamIPv4(host string, plain bool, localAddresses []netip.Addr) bool {
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Is4() && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !(plain && slices.Contains(localAddresses, ip))
}

func validUpstreamPort(port string, plain bool) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 0 && n <= 65535 && n != 1053 && (plain && n != 0 || !plain && n != 53)
}

// ErrSetupUnavailable means C36 cannot safely adopt this profile write yet.
var ErrSetupUnavailable = errors.New(msgWaitingSetup)
