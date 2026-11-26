package convert

// Port of backend/converters/loon.py (C20): the Loon configuration of a
// profile (https://nsloon.app/docs/intro).
//   - manual nodes go to [Proxy]
//   - subscriptions / aggregations are [Remote Proxy] entries pointing at this
//     server's ?format=loon endpoints
//   - a group's node-name regex becomes a [Remote Filter] NameRegex filter
//   - rule sets go to [Remote Rule]
// Manual node strings use subs.ParseURIList exactly as Python (see surge.go).

import (
	"regexp"
	"strings"

	"configflow/internal/logx"
)

var loonLog = logx.Get("backend.converters.loon")

// LoonDefaultTestURL is DEFAULT_TEST_URL.
const LoonDefaultTestURL = "https://www.gstatic.com/generate_204"

// LoonBuiltinPolicies is LOON_BUILTIN_POLICIES (https://nsloon.app/docs/Policy/).
var LoonBuiltinPolicies = []string{"DIRECT", "REJECT", "REJECT-DROP", "REJECT-IMG", "REJECT-DICT", "REJECT-ARRAY"}

// LoonRuleTypes is LOON_RULE_TYPES: the local rule types Loon documents;
// others are commented out.
var LoonRuleTypes = []string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6", "GEOIP", "IP-ASN",
	"SRC-PORT", "DEST-PORT", "URL-REGEX", "USER-AGENT", "PROTOCOL", "AND", "OR", "NOT"}

// loonRuleTypeMap is LOON_RULE_TYPE_MAP (mihomo/Clash → Loon).
var loonRuleTypeMap = [][2]string{{"DST-PORT", "DEST-PORT"}}

// loadBalanceAlgorithms is LOAD_BALANCE_ALGORITHMS.
var loadBalanceAlgorithms = map[string]string{"consistent-hashing": "pcc", "sticky-sessions": "pcc", "round-robin": "round-robin"}

// LoonVmessCiphers is VMESS_CIPHERS.
var LoonVmessCiphers = []string{"none", "auto", "aes-128-cfb", "aes-128-gcm", "chacha20-ietf-poly1305"}

// LoonDefaultGeneral is DEFAULT_GENERAL. bypass-tun must not contain
// 198.18.0.0/15, Loon's fake IP range.
var LoonDefaultGeneral = []string{
	"[General]",
	"ip-mode = dual",
	"dns-server = system,223.5.5.5,119.29.29.29",
	"doh-server = https://dns.alidns.com/dns-query",
	"hijack-dns = *:53",
	"proxy-test-url = https://www.gstatic.com/generate_204",
	"internet-test-url = http://wifi.vivo.com.cn/generate_204",
	"test-timeout = 5",
	"interface-mode = auto",
	"sni-sniffing = true",
	"udp-fallback-mode = DIRECT",
	"allow-wifi-access = false",
	"wifi-access-http-port = 7222",
	"wifi-access-socks5-port = 7221",
	"skip-proxy = 192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,localhost,*.local,captive.apple.com",
	"bypass-tun = 10.0.0.0/8,100.64.0.0/10,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.0.0.0/24,192.0.2.0/24,192.88.99.0/24,192.168.0.0/16,198.51.100.0/24,203.0.113.0/24,224.0.0.0/4,255.255.255.255/32",
}

var (
	loonSubTypeRe = regexp.MustCompile(`\([\s\x{0085}\p{Z}]*([A-Z0-9-]+)[\s\x{0085}\p{Z}]*,`)
)

// loonRulesetURL selects the publisher's client-specific rule variant.
func loonRulesetURL(address string) string {
	if strings.Contains(address, "blackmatrix7/ios_rule_script") && strings.Contains(address, "/rule/Clash/") {
		return strings.ReplaceAll(address, "/rule/Clash/", "/rule/Loon/")
	}
	return address
}
