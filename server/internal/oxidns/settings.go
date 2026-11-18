package oxidns

import (
	"fmt"
	"math/big"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
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
	text := str(value)
	if !isStr(value) || text == "" || strings.ContainsRune(text, '\\') || hasControl(text) {
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

var intLimit = big.NewInt(2147483647)

func checkInt(obj *Node, key string, minimum int64, maximum *big.Int) error {
	value, ok := intOf(get(obj, key))
	if !ok || value.Cmp(big.NewInt(minimum)) < 0 || value.Cmp(maximum) > 0 {
		return fail(fmt.Sprintf("%s 必须为 %d–%s 的整数", key, minimum, maximum))
	}
	return nil
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
	if err := requestTypeError(value); err != nil {
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
	if !isOneOf(get(s, "config_mode"), "template", "native") {
		return nil, fail("OxiDNS 配置模式必须为 template 或 native")
	}
	if !isStr(get(s, "native_config")) {
		return nil, fail("OxiDNS 完整配置必须为 YAML 文本")
	}
	if _, err := ManagedFiles(get(s, "additional_files")); err != nil {
		return nil, err
	}
	if err := normalizeRuleDNS(s); err != nil {
		return nil, err
	}
	if str(get(s, "config_mode")) == "native" {
		if validateNative {
			_, err = NativeDocument(s, true, nil)
		} else {
			_, err = loadNative(get(s, "native_config"), NativeLabel)
		}
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	if err := checkTemplate(s); err != nil {
		return nil, err
	}
	return s, nil
}

func checkTemplate(s *Node) error {
	for _, key := range []string{"worker_threads", "cache_size", "cache_dump_interval", "probe_cache_size",
		"local_concurrent", "remote_concurrent", "fallback_concurrent", "fallback_threshold"} {
		maximum := intLimit
		if key == "worker_threads" {
			maximum = big.NewInt(256)
		}
		if err := checkInt(s, key, 1, maximum); err != nil {
			return err
		}
	}
	for _, key := range []string{"cache_lazy_ttl", "remote_ttl"} {
		if err := checkInt(s, key, 0, intLimit); err != nil {
			return err
		}
	}
	for _, key := range []string{"tcp_enabled", "prefer_ipv4", "metrics_enabled", "aggregate_rules", "always_standby",
		"cache_enabled", "cache_dump_enabled", "probe_cache_enabled"} {
		if !isBool(get(s, key)) {
			return fail(key + " 必须为布尔值")
		}
	}
	for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
		if !allStrings(get(s, key)) {
			return fail(key + " 必须为规则 ID 数组")
		}
		set(s, key, unique(get(s, key)))
	}
	for _, suffix := range []string{"rules", "rulesets"} {
		for _, id := range items(get(s, "direct_"+suffix)) {
			if contains(get(s, "proxy_"+suffix), id) {
				return fail("同一规则不能同时分配给直连和代理")
			}
		}
	}
	for _, key := range []string{"local_dns", "remote_dns", "fallback_dns", "listen", "custom_hosts", "custom_config"} {
		if !isStr(get(s, key)) {
			return fail(key + " 必须为文本")
		}
	}
	if strings.TrimSpace(str(get(s, "listen"))) == "" {
		return fail("DNS 监听地址不能为空")
	}
	if !isOneOf(get(s, "default_forward"), "forward_local", "forward_remote") {
		return fail("默认转发目标无效")
	}
	if !isOneOf(get(s, "custom_match_position"), "head", "tail") {
		return fail("自定义匹配执行顺序无效")
	}
	if !isSeq(get(s, "custom_matches")) {
		return fail("自定义匹配必须为数组")
	}
	for _, entry := range items(get(s, "custom_matches")) {
		if !isMap(entry) {
			return fail("自定义匹配条目必须为对象")
		}
		if enabled := get(entry, "enabled"); has(entry, "enabled") && same(enabled, B(false)) {
			continue
		}
		matches := get(entry, "matches")
		if !has(entry, "matches") {
			matches = L()
		}
		if isStr(matches) {
			matches = strs(splitLines(matches.Value, false))
		}
		if !isSeq(matches) || len(filter(matches.Content, func(m *Node) bool { return isStr(m) && strings.TrimSpace(m.Value) != "" })) == 0 {
			return fail("启用的自定义匹配需要至少一个条件")
		}
		if !allStrings(matches) || !isStr(get(entry, "exec")) || strings.TrimSpace(str(get(entry, "exec"))) == "" {
			return fail("自定义匹配条件和执行动作必须为非空文本")
		}
	}
	blocked := get(s, "block_qtypes")
	if !isSeq(blocked) {
		return fail("屏蔽查询类型必须为 1–65535 的整数数组")
	}
	for _, v := range blocked.Content {
		if n, ok := int64Of(v); !ok || n < 1 || n > 65535 {
			return fail("屏蔽查询类型必须为 1–65535 的整数数组")
		}
	}
	if isTrue(get(s, "cache_dump_enabled")) {
		if _, err := SafeRelativePath(get(s, "cache_dump_file"), "缓存文件"); err != nil {
			return err
		}
	}
	for _, name := range []string{"api", "log", "query_recorder", "ip_selector", "dynamic_domain_set", "adguard",
		"ros_address_list", "ros_route", "rule_update"} {
		section := get(s, name)
		if !isMap(section) {
			return fail(name + " 必须为对象")
		}
		if name != "log" && !isBool(get(section, "enabled")) {
			return fail(name + ".enabled 必须为布尔值")
		}
		if has(section, "args") && !isMap(get(section, "args")) {
			return fail(name + ".args 必须为对象")
		}
	}
	api := get(s, "api")
	for _, key := range []string{"listen", "username", "password", "webui_root"} {
		if !isStr(get(api, key)) {
			return fail("api." + key + " 必须为文本")
		}
	}
	if isTrue(get(api, "enabled")) {
		if strings.TrimSpace(str(get(api, "listen"))) == "" {
			return fail("API 监听地址不能为空")
		}
		if (str(get(api, "username")) != "") != (str(get(api, "password")) != "") {
			return fail("API 用户名和密码必须同时设置")
		}
		ssl := get(api, "ssl")
		if !isMap(ssl) {
			return fail("API TLS 设置必须为对象")
		}
		if (str(get(ssl, "cert")) != "") != (str(get(ssl, "key")) != "") {
			return fail("API TLS 证书和私钥必须同时设置")
		}
	}
	log := get(s, "log")
	if !isStr(get(log, "file")) {
		return fail("日志文件路径必须为文本")
	}
	if !isOneOf(get(log, "level"), "off", "trace", "debug", "info", "warn", "error") {
		return fail("日志级别无效")
	}
	if !isOneOf(get(log, "rotation"), "never", "minutely", "hourly", "daily", "weekly") {
		return fail("日志轮转方式无效")
	}
	if err := checkInt(log, "max_files", 0, intLimit); err != nil {
		return err
	}
	if recorder := get(s, "query_recorder"); isTrue(get(recorder, "enabled")) {
		args := get(recorder, "args")
		for _, key := range []string{"queue_size", "batch_size", "flush_interval_ms", "retention_days", "cleanup_interval_hours", "reader_concurrency"} {
			if err := checkInt(args, key, 1, intLimit); err != nil {
				return err
			}
		}
		if err := checkInt(args, "memory_tail", 0, intLimit); err != nil {
			return err
		}
		if p, err := SafeRelativePath(get(args, "path"), PathLabel); err != nil {
			return err
		} else if !strings.HasPrefix(p, "state/") {
			return fail("查询记录请放在 state/ 下，避免部署覆盖运行数据")
		}
	}
	if selector := get(s, "ip_selector"); isTrue(get(selector, "enabled")) {
		args := get(selector, "args")
		if !isOneOf(get(args, "selection_mode"), "first_success", "best_within_budget", "background") {
			return fail("IP 优选模式无效")
		}
		if err := checkInt(args, "top_n", 0, intLimit); err != nil {
			return err
		}
		for _, key := range []string{"probe_stagger", "probe_timeout", "max_wait", "max_parallel_probes"} {
			minimum := int64(1)
			if key == "probe_stagger" {
				minimum = 0
			}
			if err := checkInt(args, key, minimum, intLimit); err != nil {
				return err
			}
		}
		if !isOneOf(get(args, "dnssec_policy"), "reorder_only", "skip") {
			return fail("IP 优选 DNSSEC 策略无效")
		}
		if methods := get(args, "probe_methods"); !allStrings(methods) || len(methods.Content) == 0 {
			return fail("IP 探测方式必须为非空文本数组")
		}
	}
	if dynamic := get(s, "dynamic_domain_set"); isTrue(get(dynamic, "enabled")) {
		if !isOneOf(get(dynamic, "route"), "direct", "proxy") || !isBool(get(dynamic, "learn")) {
			return fail("动态域名集路由或学习设置无效")
		}
		args := get(dynamic, "args")
		if p, err := SafeRelativePath(get(args, "path"), PathLabel); err != nil {
			return err
		} else if !strings.HasPrefix(p, "state/") {
			return fail("动态规则请放在 state/ 下，避免部署覆盖运行数据")
		}
		learn := get(dynamic, "learn_args")
		if !isMap(learn) {
			return fail("学习参数必须为对象")
		}
		for _, key := range []string{"queue_size", "batch_size", "flush_interval_ms"} {
			if err := checkInt(args, key, 1, intLimit); err != nil {
				return err
			}
		}
		if !allStrings(get(args, "bootstrap_rules")) {
			return fail("动态域名初始规则必须为文本数组")
		}
		if !isOneOf(get(learn, "phase"), "before", "after") || !isOneOf(get(learn, "questions"), "first", "all") {
			return fail("动态学习阶段或问题范围无效")
		}
		if !isOneOf(get(learn, "rule_kind"), "full", "domain") || !isOneOf(get(learn, "error_mode"), "continue", "stop", "fail") {
			return fail("动态学习规则类型或错误行为无效")
		}
	}
	if adguard := get(s, "adguard"); isTrue(get(adguard, "enabled")) {
		if !isOneOf(get(adguard, "mode"), "nxdomain", "nodata", "null", "refused") {
			return fail("AdGuard 拦截响应模式无效")
		}
		content, urls := get(adguard, "content"), get(adguard, "urls")
		if !isStr(content) || !isSeq(urls) {
			return fail("AdGuard 规则必须为文本和订阅 URL 数组")
		}
		if strings.TrimSpace(content.Value) == "" && len(urls.Content) == 0 {
			return fail("启用 AdGuard 时请填写规则或订阅 URL")
		}
		for _, url := range urls.Content {
			if !isStr(url) || !httpURL.MatchString(url.Value) {
				return fail("AdGuard 订阅必须为 HTTP(S) URL")
			}
		}
	}
	for _, name := range []string{"ros_address_list", "ros_route"} {
		section := get(s, name)
		if !isTrue(get(section, "enabled")) {
			continue
		}
		args := get(section, "args")
		for _, key := range []string{"address", "username", "password"} {
			if strings.TrimSpace(str(get(args, key))) == "" {
				return fail(name + " 需要 RouterOS 地址、用户名和密码")
			}
		}
		if has(args, "tls") {
			tls := get(args, "tls")
			if !isMap(tls) {
				return fail("RouterOS TLS 必须为对象，关闭时删除 tls 字段")
			}
			if (str(get(tls, "ca")) != "") && (isTrue(get(tls, "insecure"))) {
				return fail("RouterOS TLS 不能同时设置 CA 和跳过验证")
			}
		}
		if err := checkInt(args, "min_ttl", 0, intLimit); err != nil {
			return err
		}
		if err := checkInt(args, "max_ttl", 0, intLimit); err != nil {
			return err
		}
		lo, _ := intOf(get(args, "min_ttl"))
		hi, _ := intOf(get(args, "max_ttl"))
		if lo.Cmp(hi) > 0 {
			return fail("RouterOS 最小 TTL 不能大于最大 TTL")
		}
		if name == "ros_route" && (!(str(get(args, "routing_table")) != "") || !((str(get(args, "gateway4")) != "") || (str(get(args, "gateway6")) != ""))) {
			return fail("RouterOS 路由需要已存在的路由表和至少一个网关")
		}
	}
	if update := get(s, "rule_update"); isTrue(get(update, "enabled")) {
		schedule := get(get(update, "args"), "schedule")
		if !has(get(update, "args"), "schedule") {
			schedule = S("")
		}
		if !isStr(schedule) || len(strings.Fields(schedule.Value)) != 5 {
			return fail("规则更新需要五段 cron 表达式")
		}
	}
	return nil
}

// splitLines is str.splitlines (keepEnds optional): Python's line boundaries.
func splitLines(s string, keepEnds bool) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		end := i + width
		switch r {
		case '\r':
			if end < len(s) && s[end] == '\n' {
				end++
			}
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
		default:
			i = end
			continue
		}
		if keepEnds {
			out = append(out, s[start:end])
		} else {
			out = append(out, s[start:i])
		}
		start, i = end, end
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// posixParent and posixName are str(PurePosixPath(p).parent) / .name.
func posixParent(p string) string { return path.Dir(path.Clean(p)) }

func posixName(p string) string {
	if base := path.Base(path.Clean(p)); base != "." && base != "/" {
		return base
	}
	return ""
}
