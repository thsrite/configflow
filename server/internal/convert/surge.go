package convert

// Port of backend/converters/surge.py (C19): the Surge configuration of a
// profile. Manual node strings are parsed with
// the built-in subs.ParseURIList exactly as Python's parse_uri_list
// (unknown schemes are dropped silently; switching Surge/Loon to Sub-Store
// is a follow-up after parity, packages.md).

import (
	"configflow/internal/logx"
)

var surgeLog = logx.Get("backend.converters.surge")

// SurgeBuiltinPolicies are validate_rule_policies' Surge built-ins.
var SurgeBuiltinPolicies = []string{"DIRECT", "REJECT", "REJECT-DROP", "REJECT-TINYGIF", "REJECT-NO-DROP",
	"CELLULAR", "CELLULAR-ONLY", "HYBRID", "NO-HYBRID"}

// SurgeDefaultGeneral is the [General] section used without a custom one.
var SurgeDefaultGeneral = []string{
	"[General]",
	"loglevel = notify",
	"internet-test-url = https://www.gstatic.com/generate_204",
	"proxy-test-url = https://www.gstatic.com/generate_204",
	"test-timeout = 3",
	"skip-proxy = localhost, *.local, injections.adguard.org, local.adguard.org, captive.apple.com, guzzoni.apple.com, 0.0.0.0/8, 10.0.0.0/8, 17.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.168.0.0/16, 192.88.99.0/24, 198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4, 255.255.255.255/32",
	"dns-server = 223.5.5.5, 119.29.29.29, system",
	"ipv6 = true",
	"allow-wifi-access = true",
	"wifi-access-http-port = 6152",
	"wifi-access-socks5-port = 6153",
	"http-listen = 0.0.0.0:6152",
	"socks5-listen = 0.0.0.0:6153",
	"exclude-simple-hostnames = true",
}

const defaultTestURL = "https://www.gstatic.com/generate_204"

// surgeRuleTypeMap is surge_rule_type_map (mihomo/Clash → Surge), in order.
var surgeRuleTypeMap = [][2]string{{"SRC-IP-CIDR", "SRC-IP"}, {"DST-PORT", "DEST-PORT"}}
