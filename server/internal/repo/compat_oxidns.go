// TODO(port): replace with package #11 oxidns editor, rule DNS
// (backend/common/oxidns_device_policy.py::normalize_client_range, backend/common/rule_dns.py::normalize_action,
// hydrate_ruleset), package #15 resource routes A (backend/common/device_notes.py::normalize_notes) and
// package #38 clients (client_fixed_ip.validate_bindings now delegates to internal/routerdoc).
// These packages import repo, so the replacements have to live in a package repo can import.

package repo

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/routerdoc"
)

func oxidnsError(message string) error { return helperError(ClassOxidnsSettings, message) }

// ---- oxidns_device_policy.py ----

var (
	clientAddressPattern = regexp.MustCompile(`^[0-9a-fA-F:.]+$`)
	clientPrefixPattern  = regexp.MustCompile(`^[0-9]{1,3}$`)
)

const clientRangeMessage = "请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。"

// pyIPAddress is ipaddress.ip_address(text) for text made of hex digits,
// colons and dots (no scope id).
func pyIPAddress(text string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(text)
	if err != nil {
		return netip.Addr{}, false
	}
	return address, true
}

// normalizeClientRange is normalize_client_range: hosts and CIDRs in
// canonical text, IPv4-mapped peers as IPv4.
func normalizeClientRange(raw any) (string, error) {
	invalid := oxidnsError(clientRangeMessage)
	text, ok := raw.(string)
	if !ok {
		return "", invalid
	}
	parts := strings.Split(pyStrip(text), "/")
	if len(parts) > 2 || !clientAddressPattern.MatchString(parts[0]) {
		return "", invalid
	}
	address, ok := pyIPAddress(parts[0])
	if !ok {
		return "", invalid
	}
	prefix := address.BitLen()
	if len(parts) == 2 {
		if !clientPrefixPattern.MatchString(parts[1]) {
			return "", invalid
		}
		prefix, _ = strconv.Atoi(parts[1])
	}
	if prefix > address.BitLen() {
		return "", invalid
	}
	// OxiDNS sees IPv4-mapped peers as the same IPv4 client.
	if address.Is4In6() {
		if prefix < 96 {
			return "", invalid
		}
		address, prefix = address.Unmap(), prefix-96
	}
	network := netip.PrefixFrom(address, prefix).Masked()
	first := network.Addr()
	if first.IsMulticast() && lastAddress(network).IsMulticast() {
		return "", invalid
	}
	if prefix == address.BitLen() {
		if address.IsUnspecified() {
			return "", invalid
		}
		return address.String(), nil
	}
	return first.String() + "/" + strconv.Itoa(prefix), nil
}

// lastAddress is network.broadcast_address.
func lastAddress(network netip.Prefix) netip.Addr {
	bytes := network.Addr().AsSlice()
	for bit := network.Bits(); bit < len(bytes)*8; bit++ {
		bytes[bit/8] |= 1 << (7 - bit%8)
	}
	address, _ := netip.AddrFromSlice(bytes)
	return address
}

// ---- device_notes.py ----

const maxDeviceNotes = 2048

// normalizeName is normalize_name.
func normalizeName(value any) (string, error) {
	text, ok := value.(string)
	valid := ok && utf8.RuneCountInString(text) <= 80
	if valid {
		for _, r := range text {
			if r < 32 || r == 127 {
				valid = false
				break
			}
		}
	}
	if !valid {
		return "", oxidnsError("设备备注最多 80 个字符，不能包含换行或控制字符。")
	}
	return pyStrip(text), nil
}

// normalizeNotes is normalize_notes: canonical address -> stripped name.
func normalizeNotes(value any) (*jsonx.Object, error) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil || object.Len() > maxDeviceNotes {
		return nil, oxidnsError("设备备注必须为对象，最多 " + strconv.Itoa(maxDeviceNotes) + " 条。")
	}
	result := jsonx.NewObject()
	for address, name := range object.All() {
		ip, err := normalizeClientRange(address)
		if err != nil {
			return nil, err
		}
		if result.Has(ip) {
			return nil, oxidnsError("设备备注地址重复：" + ip)
		}
		normalized, err := normalizeName(name)
		if err != nil {
			return nil, err
		}
		result.Set(ip, normalized)
	}
	return result, nil
}

// ---- client_fixed_ip.py ----

// validateBindings is validate_bindings (internal/routerdoc holds the check).
func validateBindings(value any) error {
	if message := routerdoc.ValidateBindings(value); message != "" {
		return oxidnsError(message)
	}
	return nil
}

// ---- rule_dns.py ----

var (
	ruleDNSActions     = []string{"refused", "nxdomain", "nodata", "blackhole", "upstream"}
	ruleDNSListRoutes  = []string{"direct", "proxy"}
	ruleDNSSources     = []string{"local", "remote", "fallback"}
	rulesetSourceField = []string{"name", "url", "behavior", "content", "source_type", "format"}

	// routeLabels is ROUTE_LABELS.
	routeLabels = map[string]string{"direct": "直连 DNS", "proxy": "代理 DNS", "refused": "拒绝", "nxdomain": "不存在",
		"nodata": "空应答", "blackhole": "黑洞", "upstream": "指定上游"}
)

func containsString(list []string, value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, item := range list {
		if item == text {
			return true
		}
	}
	return false
}

// hasExactKeys is set(obj) == {keys...}.
func hasExactKeys(obj *jsonx.Object, keys ...string) bool {
	if obj.Len() != len(keys) {
		return false
	}
	for _, key := range keys {
		if !obj.Has(key) {
			return false
		}
	}
	return true
}

// normalizeAction is normalize_action: the validated canonical copy of one
// rule_actions (or, with parked, rule_dns_parked) value.
func normalizeAction(value any, parked bool) (*jsonx.Object, error) {
	allowed := ruleDNSActions
	if parked {
		allowed = append(append([]string{}, ruleDNSListRoutes...), ruleDNSActions...)
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil || !containsString(allowed, object.Value("action")) {
		return nil, oxidnsError("规则 DNS 处理无效：" + strings.Join(allowed, "、"))
	}
	action := object.GetString("action")
	if action != "upstream" {
		if !hasExactKeys(object, "action") {
			return nil, oxidnsError("规则 DNS 处理「" + routeLabels[action] + "」不能包含其他参数")
		}
		return jsonx.Obj("action", action), nil
	}
	if !hasExactKeys(object, "action", "source", "upstream") || !containsString(ruleDNSSources, object.Value("source")) {
		return nil, oxidnsError("指定上游需要 source（local、remote 或 fallback）和 upstream")
	}
	upstream, ok := object.Value("upstream").(*jsonx.Object)
	if !ok || upstream == nil || !nonBlankString(upstream.Value("addr")) {
		return nil, oxidnsError("指定上游需要非空的 addr 地址")
	}
	return jsonx.Obj("action", "upstream", "source", object.Value("source"), "upstream", upstream.DeepCopy()), nil
}

// hydrateRuleset is hydrate_ruleset: it copies a shared rule-library source
// onto a profile ruleset reference in place.
func hydrateRuleset(item, libraryEntry *jsonx.Object, profileID string) error {
	for _, key := range rulesetSourceField {
		item.Delete(key)
	}
	item.Delete("library_enabled")
	for _, key := range rulesetSourceField {
		if value, ok := libraryEntry.Get(key); ok {
			item.Set(key, jsonx.DeepCopy(value))
		}
	}
	item.SetDefault("behavior", "domain")
	item.Set("library_enabled", jsonx.DeepCopy(libraryEntry.GetOr("enabled", true)))
	if jsonx.Equal(libraryEntry.Value("source_type"), "content") {
		id, ok := libraryEntry.Get("id")
		if !ok {
			return helperError(ClassKeyError, "'id'")
		}
		if profileID == "" {
			profileID = "default"
		}
		item.Set("url", "/api/profiles/"+profileID+"/rule-library/content/"+jsonx.Str(id))
	}
	return nil
}
