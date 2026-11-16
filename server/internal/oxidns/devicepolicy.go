package oxidns

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// The parts of backend/common/oxidns_device_policy.py (module C26) that the
// settings normalizer (C05) calls. C26 adds the rest of the module here.

var (
	clientAddressPattern = regexp.MustCompile(`^[0-9a-fA-F:.]+$`)
	clientPrefixPattern  = regexp.MustCompile(`^[0-9]{1,3}$`)
)

const clientRangeMessage = "请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。"

// NormalizeClientRange is normalize_client_range(raw): a host or CIDR in
// canonical text (IPv4-mapped peers as IPv4), or OxidnsSettingsError.
func NormalizeClientRange(raw any) (text string, err error) {
	defer catch(&err)
	return normalizeClientRange(raw), nil
}

func normalizeClientRange(raw any) string {
	text, ok := raw.(string)
	if !ok {
		fail(clientRangeMessage)
	}
	parts := strings.Split(pystr.Strip(text), "/")
	if len(parts) > 2 || !clientAddressPattern.MatchString(parts[0]) {
		fail(clientRangeMessage)
	}
	address, ok := convert.ParseIPAddress(parts[0])
	if !ok {
		fail(clientRangeMessage)
	}
	prefix := address.BitLen()
	if len(parts) == 2 {
		if !clientPrefixPattern.MatchString(parts[1]) {
			fail(clientRangeMessage)
		}
		prefix, _ = strconv.Atoi(parts[1])
	}
	if prefix > address.BitLen() {
		fail(clientRangeMessage)
	}
	// OxiDNS sees IPv4-mapped peers as the same IPv4 client.
	if address.Is4In6() {
		if prefix < 96 {
			fail(clientRangeMessage)
		}
		address, prefix = address.Unmap(), prefix-96
	}
	network := netip.PrefixFrom(address, prefix).Masked()
	if network.Addr().IsMulticast() && lastAddress(network).IsMulticast() {
		fail(clientRangeMessage)
	}
	if prefix == address.BitLen() {
		if address.IsUnspecified() {
			fail(clientRangeMessage)
		}
		return address.String()
	}
	return network.Addr().String() + "/" + strconv.Itoa(prefix)
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

// NormalizePolicies is normalize_policies(value): the validated device
// policy rows ({ip, name, mode}).
func NormalizePolicies(value any) (rows []any, err error) {
	defer catch(&err)
	return normalizePolicies(value), nil
}

func normalizePolicies(value any) []any {
	list, ok := value.([]any)
	if !ok || len(list) > 512 {
		fail("设备策略必须为列表，最多 512 条策略。")
	}
	result := []any{}
	seen := map[string]bool{}
	for _, raw := range list {
		row, ok := raw.(*jsonx.Object)
		if ok && row != nil {
			for _, key := range row.Keys() {
				if !strIn(key, "ip", "name", "mode") {
					ok = false
				}
			}
		}
		if !ok || row == nil {
			fail("设备策略仅支持 IP / 网段、备注和策略。")
		}
		ip := normalizeClientRange(row.Value("ip"))
		if seen[ip] {
			fail("客户端 IP / 网段重复：" + ip)
		}
		seen[ip] = true
		name, mode := row.GetOr("name", ""), row.Value("mode")
		text, isText := name.(string)
		if !isText || runeLen(text) > 80 || hasControl(text) {
			fail("设备备注最多 80 个字符，不能包含换行或控制字符。")
		}
		if !strIn(mode, "smart", "direct") {
			fail("设备策略必须为智能分流或直连。")
		}
		// Older versions accepted DEL in names; drop it so the row stays editable.
		result = append(result, jsonx.Obj("ip", ip, "name", pystr.Strip(strings.ReplaceAll(text, "\x7f", "")), "mode", mode))
	}
	return result
}
