package oxidns

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// BlockQtypesExec answers blocked query types with a cacheable empty
// response; "reject 0" is what older drafts carry.
const BlockQtypesExec = "black_hole nodata short_circuit=true"

var blockQtypesExecs = []string{BlockQtypesExec, "reject 0"}

const defaultSettingsJSON = `{
"config_mode": "template", "native_config": "", "additional_files": [],
"direct_rulesets": [], "proxy_rulesets": [], "direct_rules": [], "proxy_rules": [],
"local_dns": "", "remote_dns": "", "fallback_dns": "",
"default_forward": "forward_remote", "custom_hosts": "", "custom_config": "",
"custom_matches": [], "custom_match_position": "tail", "device_policies": [],
"worker_threads": 2, "listen": "0.0.0.0:53", "tcp_enabled": true,
"prefer_ipv4": true, "block_qtypes": [65, 255], "metrics_enabled": true,
"aggregate_rules": true, "local_concurrent": 2, "remote_concurrent": 1,
"fallback_concurrent": 2, "fallback_threshold": 500, "always_standby": false,
"remote_ttl": 1, "cache_enabled": true, "cache_size": 10240,
"cache_lazy_ttl": 3600, "cache_dump_enabled": false,
"cache_dump_file": "./state/cache.dump", "cache_dump_interval": 300,
"probe_cache_enabled": true, "probe_cache_size": 2048,
"api": {"enabled": true, "listen": "127.0.0.1:9199", "username": "", "password": "", "webui_root": "", "ssl": {}},
"log": {"level": "info", "file": "", "rotation": "daily", "max_files": 7},
"query_recorder": {"enabled": false, "args": {"path": "./state/queries.db", "queue_size": 8192, "batch_size": 256,
  "flush_interval_ms": 200, "memory_tail": 1024, "retention_days": 7, "cleanup_interval_hours": 1, "reader_concurrency": 2}},
"ip_selector": {"enabled": false, "args": {"selection_mode": "background", "probe_methods": ["tcp:443", "tcp:80"],
  "probe_stagger": 200, "probe_timeout": 600, "max_wait": 1000, "top_n": 0, "dnssec_policy": "reorder_only",
  "max_parallel_probes": 64, "cache": {"enabled": true, "size": 4096, "ttl": 3600, "failure_ttl": 60}}},
"dynamic_domain_set": {"enabled": false, "route": "direct", "learn": false,
  "args": {"path": "./state/learned-domains.txt", "bootstrap_rules": [], "queue_size": 1024, "batch_size": 256, "flush_interval_ms": 200},
  "learn_args": {"phase": "after", "questions": "first", "qtypes": ["A", "AAAA"], "success_only": true,
    "answer_required": true, "rule_kind": "full", "async": true, "error_mode": "continue", "timeout": "1s"}},
"adguard": {"enabled": false, "content": "", "urls": [], "mode": "nxdomain"},
"ros_address_list": {"enabled": false, "args": {"address": "", "username": "", "password": "", "async": true,
  "address_list4": "oxidns", "address_list6": "oxidns", "min_ttl": 60, "max_ttl": 3600, "queue_capacity": 16384,
  "comment_prefix": "configflow", "cleanup_on_shutdown": true}},
"ros_route": {"enabled": false, "args": {"address": "", "username": "", "password": "", "async": true,
  "routing_table": "", "gateway4": "", "gateway6": "", "distance": 100, "min_ttl": 60, "max_ttl": 3600,
  "queue_capacity": 16384, "comment_prefix": "configflow", "cleanup_on_shutdown": true}},
"rule_update": {"enabled": false, "args": {"schedule": "30 3 * * *", "timezone": "Asia/Shanghai", "timeout": "60s",
  "outbound": "", "socks5": ""}},
"rule_actions": {}, "rule_dns_follow": [], "rule_dns_parked": {}
}`

var defaultSettingsNode = func() *Node {
	n, err := FromJSON([]byte(defaultSettingsJSON))
	if err != nil {
		panic(err)
	}
	return n
}()

// DefaultSettings is get_default_oxidns_settings(): a fresh copy.
func DefaultSettings() *Node { return clone(defaultSettingsNode) }

// merge is _merge(defaults, value): nested mappings merge, the rest replaces.
func merge(defaults, value *Node) *Node {
	result := clone(defaults)
	pairs(value, func(key string, item *Node) bool {
		if current := get(result, key); isMap(current) && isMap(item) {
			set(result, key, merge(current, item))
		} else {
			set(result, key, clone(item))
		}
		return true
	})
	return result
}

// SafeRelativePath is safe_relative_path: the normalized relative path
// inside the managed directory ("./" removed).
func SafeRelativePath(value *Node, label string) (string, error) {
	return safeRelativePath(str(value), isStr(value), label)
}

// safeRelativePath checks a path that must be text (ok false otherwise).
func safeRelativePath(text string, ok bool, label string) (string, error) {
	if !ok || text == "" || strings.ContainsRune(text, '\\') || hasControl(text) {
		return "", fail(label + "无效")
	}
	normalized := strings.TrimPrefix(text, "./")
	if strings.HasPrefix(normalized, "/") {
		return "", fail(label + "必须为安全的相对路径")
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fail(label + "必须为安全的相对路径")
		}
	}
	return normalized, nil
}

func safePath(text, label string) (string, error) { return SafeRelativePath(S(text), label) }

func hasControl(s string) bool {
	for _, r := range s {
		if r < 32 {
			return true
		}
	}
	return false
}

// normalizeRuleDNS is _normalize_rule_dns.
func normalizeRuleDNS(settings *Node) error {
	for _, key := range []string{"rule_actions", "rule_dns_parked"} {
		value := get(settings, key)
		if !isMap(value) {
			return fail(key + " 必须为以规则 ID 为键的对象")
		}
		result := M()
		var err error
		pairs(value, func(identity string, action *Node) bool {
			if isNone(action) {
				return true
			}
			var normalized *Node
			if normalized, err = NormalizeAction(action, key == "rule_dns_parked"); err != nil {
				return false
			}
			set(result, identity, normalized)
			return true
		})
		if err != nil {
			return err
		}
		set(settings, key, result)
	}
	follow := get(settings, "rule_dns_follow")
	if !allStrings(follow) {
		return fail("rule_dns_follow 必须为规则 ID 数组")
	}
	follow = unique(follow)
	set(settings, "rule_dns_follow", follow)
	for _, identity := range items(follow) {
		if action := get(get(settings, "rule_actions"), identity.Value); action != nil {
			return fail(fmt.Sprintf("规则 %s 已设置 DNS 处理「%s」，不能同时跟随策略", identity.Value, RouteLabels[str(get(action, "action"))]))
		}
		if has(get(settings, "rule_dns_parked"), identity.Value) {
			return fail(fmt.Sprintf("规则 %s 已暂存停用前的 DNS 处理，不能同时跟随策略", identity.Value))
		}
	}
	return nil
}

// allStrings: a list of strings.
func allStrings(n *Node) bool {
	if !isSeq(n) {
		return false
	}
	for _, item := range n.Content {
		if !isStr(item) {
			return false
		}
	}
	return true
}

// unique is list(dict.fromkeys(list)) for strings.
func unique(list *Node) *Node {
	out, seen := L(), map[string]bool{}
	for _, item := range items(list) {
		if !seen[item.Value] {
			seen[item.Value] = true
			out.Content = append(out.Content, item)
		}
	}
	return out
}

func isOneOf(n *Node, options ...string) bool {
	if !isStr(n) {
		return false
	}
	for _, o := range options {
		if n.Value == o {
			return true
		}
	}
	return false
}

var httpURL = regexp.MustCompile(`^https?://`)

// NormalizeSettings is normalize_oxidns_settings. value nil means {}.
// validateNative false only checks the native YAML is safe (stored drafts
// may be incomplete).
func NormalizeSettings(value *Node, validateNative bool) (*Node, error) {
	if err := checkRequestTypes(value); err != nil {
		return nil, err
	}
	if isNone(value) {
		value = M()
	}
	if !isMap(value) {
		return nil, fail("OxiDNS 设置必须是对象")
	}
	s := merge(defaultSettingsNode, value)
	policies, err := NormalizePolicies(get(s, "device_policies"))
	if err != nil {
		return nil, err
	}
	set(s, "device_policies", policies)
	view := readSettingsView(s)
	if !view.ConfigMode.OK || (view.ConfigMode.Value != "template" && view.ConfigMode.Value != "native") {
		return nil, fail("OxiDNS 配置模式必须为 template 或 native")
	}
	if !view.NativeConfig.OK {
		return nil, fail("OxiDNS 完整配置必须为 YAML 文本")
	}
	if _, err := ManagedFiles(get(s, "additional_files")); err != nil {
		return nil, err
	}
	if err := normalizeRuleDNS(s); err != nil {
		return nil, err
	}
	if view.isNative() {
		if validateNative {
			_, err = NativeDocument(s, true, nil)
		} else {
			_, err = view.loadNative(NativeLabel)
		}
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	if _, err := decodeTemplate(s); err != nil {
		return nil, err
	}
	return s, nil
}

// posixParent and posixName are str(PurePosixPath(p).parent) / .name.
func posixParent(p string) string { return path.Dir(path.Clean(p)) }

func posixName(p string) string {
	if base := path.Base(path.Clean(p)); base != "." && base != "/" {
		return base
	}
	return ""
}
