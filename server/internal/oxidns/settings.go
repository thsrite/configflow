package oxidns

import (
	"math/big"
	"strings"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Port of backend/common/oxidns_settings.py (module C05). The defaults
// (get_default_oxidns_settings) live in repo.GetDefaultOxidnsSettings because
// the repository needs them; everything else is here.

// BlockQtypesExec is BLOCK_QTYPES_EXEC: the exec that answers blocked query
// types with an empty, cacheable response.
const BlockQtypesExec = "black_hole nodata short_circuit=true"

// BlockQtypesExecs is BLOCK_QTYPES_EXECS: the current exec and the one older
// drafts still carry.
var BlockQtypesExecs = []string{BlockQtypesExec, "reject 0"}

// DefaultSettings is get_default_oxidns_settings(): a fresh copy.
func DefaultSettings() *jsonx.Object { return repo.GetDefaultOxidnsSettings() }

// Merge is _merge(defaults, value): a deep copy of defaults with value laid
// over it; nested dicts merge recursively, everything else replaces (deep
// copied). value must be a dict.
func Merge(defaults, value *jsonx.Object) *jsonx.Object {
	result := defaults.DeepCopy()
	for key, item := range value.All() {
		current, isObject := result.Value(key).(*jsonx.Object)
		itemObject, itemIsObject := item.(*jsonx.Object)
		if isObject && current != nil && itemIsObject && itemObject != nil {
			result.Set(key, Merge(current, itemObject))
		} else {
			result.Set(key, jsonx.DeepCopy(item))
		}
	}
	return result
}

// SafeRelativePath is safe_relative_path(value, label): the normalized
// POSIX form of a relative path inside the managed directory ("./" prefix
// removed), or OxidnsSettingsError "<label>无效" (not a non-empty string, a
// backslash or a control character) / "<label>必须为安全的相对路径"
// (absolute, empty, "." or ".." segments). Python's default label is
// DefaultPathLabel.
func SafeRelativePath(value any, label string) (path string, err error) {
	defer catch(&err)
	return safeRelativePath(value, label), nil
}

// DefaultPathLabel is safe_relative_path's default label.
const DefaultPathLabel = "文件路径"

func safeRelativePath(value any, label string) string {
	text, ok := value.(string)
	if !ok || text == "" || strings.Contains(text, `\`) || hasControl(text) {
		fail(label + "无效")
	}
	normalized := strings.TrimPrefix(text, "./")
	if strings.HasPrefix(normalized, "/") {
		fail(label + "必须为安全的相对路径")
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			fail(label + "必须为安全的相对路径")
		}
	}
	// PurePosixPath(normalized).as_posix() of a path without empty, "." or
	// ".." segments is the path itself.
	return normalized
}

// NormalizeOptions are the keyword arguments of normalize_oxidns_settings.
type NormalizeOptions struct {
	// SkipNativeArguments is validate_native_arguments=False: a native draft
	// is only checked for safe YAML (load_native_yaml), not expanded and
	// validated (native_document).
	SkipNativeArguments bool
}

// NormalizeSettings is normalize_oxidns_settings(value,
// validate_native_arguments=!opts.SkipNativeArguments): the profile's
// `oxidns` value merged over the defaults and validated. value nil is
// Python's None (= {}).
//
// It returns a new object; value is not modified. Errors: *Error
// (OxidnsSettingsError, or OxidnsValidationError from the native document
// validation), *convert.PyError where Python crashes on a malformed value,
// *PendingError where a not yet ported OxiDNS function is needed.
//
// Template mode validates every template field; native mode validates the
// shared fields (device policies, attachments, rule DNS fields) and the
// native YAML, and returns the template draft fields untouched.
func NormalizeSettings(value any, opts NormalizeOptions) (settings *jsonx.Object, err error) {
	defer catch(&err)
	return normalizeSettings(value, !opts.SkipNativeArguments), nil
}

const int32Max = 1<<31 - 1

func normalizeSettings(value any, validateNativeArguments bool) *jsonx.Object {
	if value == nil {
		value = jsonx.NewObject()
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		fail("OxiDNS 设置必须是对象")
	}
	s := Merge(DefaultSettings(), object)
	s.Set("device_policies", normalizePolicies(s.Value("device_policies")))
	if !strIn(s.Value("config_mode"), "template", "native") {
		fail("OxiDNS 配置模式必须为 template 或 native")
	}
	if !isStr(s.Value("native_config")) {
		fail("OxiDNS 完整配置必须为 YAML 文本")
	}
	managedFiles(s.Value("additional_files"))
	normalizeRuleDNS(s)
	if s.Value("config_mode") == "native" {
		if validateNativeArguments {
			nativeDocument(s, true, nil)
		} else {
			// A stored draft may be incomplete; only enforce safe YAML and
			// bounded, explicitly managed attachment data at this boundary.
			loadNativeYAML(s.Value("native_config"), DefaultNativeLabel)
		}
		return s
	}

	for _, key := range []string{"worker_threads", "cache_size", "cache_dump_interval", "probe_cache_size",
		"local_concurrent", "remote_concurrent", "fallback_concurrent", "fallback_threshold"} {
		maximum := int64(int32Max)
		if key == "worker_threads" {
			maximum = 256
		}
		checkInteger(s, key, 1, maximum)
	}
	for _, key := range []string{"cache_lazy_ttl", "remote_ttl"} {
		checkInteger(s, key, 0, int32Max)
	}
	for _, key := range []string{"tcp_enabled", "prefer_ipv4", "metrics_enabled", "aggregate_rules", "always_standby",
		"cache_enabled", "cache_dump_enabled", "probe_cache_enabled"} {
		if _, ok := s.Value(key).(bool); !ok {
			fail(key + " 必须为布尔值")
		}
	}
	for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
		ids, ok := stringList(s.Value(key))
		if !ok {
			fail(key + " 必须为规则 ID 数组")
		}
		s.Set(key, jsonx.ListOf(dedupe(ids)))
	}
	for _, suffix := range []string{"rules", "rulesets"} {
		direct := map[string]bool{}
		for _, id := range s.GetList("direct_" + suffix) {
			direct[id.(string)] = true
		}
		for _, id := range s.GetList("proxy_" + suffix) {
			if direct[id.(string)] {
				fail("同一规则不能同时分配给直连和代理")
			}
		}
	}
	for _, key := range []string{"local_dns", "remote_dns", "fallback_dns", "listen", "custom_hosts", "custom_config"} {
		if !isStr(s.Value(key)) {
			fail(key + " 必须为文本")
		}
	}
	if strip(s.Value("listen")) == "" {
		fail("DNS 监听地址不能为空")
	}
	if !strIn(s.Value("default_forward"), "forward_local", "forward_remote") {
		fail("默认转发目标无效")
	}
	if !strIn(s.Value("custom_match_position"), "head", "tail") {
		fail("自定义匹配执行顺序无效")
	}
	entries, ok := s.Value("custom_matches").([]any)
	if !ok {
		fail("自定义匹配必须为数组")
	}
	for _, raw := range entries {
		entry, ok := raw.(*jsonx.Object)
		if !ok || entry == nil {
			fail("自定义匹配条目必须为对象")
		}
		if !truthy(entry.GetOr("enabled", true)) {
			continue
		}
		matches := entry.GetOr("matches", []any{})
		if text, ok := matches.(string); ok {
			matches = jsonx.ListOf(convert.PySplitlines(text))
		}
		list, isList := matches.([]any)
		anyCondition := false
		for _, m := range list {
			if text, ok := m.(string); ok && strip(text) != "" {
				anyCondition = true
				break
			}
		}
		if !isList || !anyCondition {
			fail("启用的自定义匹配需要至少一个条件")
		}
		allText := true
		for _, m := range list {
			if !isStr(m) {
				allText = false
			}
		}
		exec, execIsText := entry.Value("exec").(string)
		if !allText || !execIsText || strip(exec) == "" {
			fail("自定义匹配条件和执行动作必须为非空文本")
		}
	}
	qtypes, ok := s.Value("block_qtypes").([]any)
	if ok {
		for _, v := range qtypes {
			if !isInt(v) || !inRange(v.(jsonx.Number), 1, 65535) {
				ok = false
				break
			}
		}
	}
	if !ok {
		fail("屏蔽查询类型必须为 1–65535 的整数数组")
	}
	if s.Value("cache_dump_enabled") == true {
		safeRelativePath(s.Value("cache_dump_file"), "缓存文件")
	}
	for _, name := range []string{"api", "log", "query_recorder", "ip_selector", "dynamic_domain_set", "adguard",
		"ros_address_list", "ros_route", "rule_update"} {
		section, ok := s.Value(name).(*jsonx.Object)
		if !ok || section == nil {
			fail(name + " 必须为对象")
		}
		if _, isBool := section.Value("enabled").(bool); name != "log" && !isBool {
			fail(name + ".enabled 必须为布尔值")
		}
		if args, present := section.Get("args"); present {
			if a, isObject := args.(*jsonx.Object); !isObject || a == nil {
				fail(name + ".args 必须为对象")
			}
		}
	}
	api := s.GetObject("api")
	for _, key := range []string{"listen", "username", "password", "webui_root"} {
		if !isStr(api.Value(key)) {
			fail("api." + key + " 必须为文本")
		}
	}
	if api.Value("enabled") == true {
		if strip(api.Value("listen")) == "" {
			fail("API 监听地址不能为空")
		}
		if truthy(api.Value("username")) != truthy(api.Value("password")) {
			fail("API 用户名和密码必须同时设置")
		}
		ssl, ok := item(api, "ssl").(*jsonx.Object)
		if !ok || ssl == nil {
			fail("API TLS 设置必须为对象")
		}
		if truthy(ssl.Value("cert")) != truthy(ssl.Value("key")) {
			fail("API TLS 证书和私钥必须同时设置")
		}
	}
	log := s.GetObject("log")
	if !isStr(item(log, "file")) {
		fail("日志文件路径必须为文本")
	}
	if !strIn(item(log, "level"), "off", "trace", "debug", "info", "warn", "error") {
		fail("日志级别无效")
	}
	if !strIn(item(log, "rotation"), "never", "minutely", "hourly", "daily", "weekly") {
		fail("日志轮转方式无效")
	}
	checkInteger(log, "max_files", 0, int32Max)
	recorder := s.GetObject("query_recorder")
	if recorder.Value("enabled") == true {
		args := item(recorder, "args").(*jsonx.Object)
		for _, key := range []string{"queue_size", "batch_size", "flush_interval_ms", "retention_days", "cleanup_interval_hours", "reader_concurrency"} {
			checkInteger(args, key, 1, int32Max)
		}
		checkInteger(args, "memory_tail", 0, int32Max)
		if !strings.HasPrefix(safeRelativePath(item(args, "path"), DefaultPathLabel), "state/") {
			fail("查询记录请放在 state/ 下，避免部署覆盖运行数据")
		}
	}
	selector := s.GetObject("ip_selector")
	if selector.Value("enabled") == true {
		args := item(selector, "args").(*jsonx.Object)
		if !strIn(args.Value("selection_mode"), "first_success", "best_within_budget", "background") {
			fail("IP 优选模式无效")
		}
		checkInteger(args, "top_n", 0, int32Max)
		for _, key := range []string{"probe_stagger", "probe_timeout", "max_wait", "max_parallel_probes"} {
			minimum := int64(1)
			if key == "probe_stagger" {
				minimum = 0
			}
			checkInteger(args, key, minimum, int32Max)
		}
		if !strIn(args.Value("dnssec_policy"), "reorder_only", "skip") {
			fail("IP 优选 DNSSEC 策略无效")
		}
		methods, ok := args.Value("probe_methods").([]any)
		if ok {
			_, ok = stringList(methods)
		}
		if !ok || len(methods) == 0 {
			fail("IP 探测方式必须为非空文本数组")
		}
	}
	dynamic := s.GetObject("dynamic_domain_set")
	if dynamic.Value("enabled") == true {
		if !strIn(item(dynamic, "route"), "direct", "proxy") {
			fail("动态域名集路由或学习设置无效")
		}
		if _, learnIsBool := item(dynamic, "learn").(bool); !learnIsBool {
			fail("动态域名集路由或学习设置无效")
		}
		args := item(dynamic, "args").(*jsonx.Object)
		if !strings.HasPrefix(safeRelativePath(item(args, "path"), DefaultPathLabel), "state/") {
			fail("动态规则请放在 state/ 下，避免部署覆盖运行数据")
		}
		learn, ok := item(dynamic, "learn_args").(*jsonx.Object)
		if !ok || learn == nil {
			fail("学习参数必须为对象")
		}
		for _, key := range []string{"queue_size", "batch_size", "flush_interval_ms"} {
			checkInteger(args, key, 1, int32Max)
		}
		if _, ok := stringList(args.Value("bootstrap_rules")); !ok {
			fail("动态域名初始规则必须为文本数组")
		}
		if !strIn(learn.Value("phase"), "before", "after") || !strIn(learn.Value("questions"), "first", "all") {
			fail("动态学习阶段或问题范围无效")
		}
		if !strIn(learn.Value("rule_kind"), "full", "domain") || !strIn(learn.Value("error_mode"), "continue", "stop", "fail") {
			fail("动态学习规则类型或错误行为无效")
		}
	}
	adguard := s.GetObject("adguard")
	if adguard.Value("enabled") == true {
		if !strIn(item(adguard, "mode"), "nxdomain", "nodata", "null", "refused") {
			fail("AdGuard 拦截响应模式无效")
		}
		content, contentIsText := item(adguard, "content").(string)
		if !contentIsText {
			fail("AdGuard 规则必须为文本和订阅 URL 数组")
		}
		urls, urlsIsList := item(adguard, "urls").([]any)
		if !urlsIsList {
			fail("AdGuard 规则必须为文本和订阅 URL 数组")
		}
		if strip(content) == "" && len(urls) == 0 {
			fail("启用 AdGuard 时请填写规则或订阅 URL")
		}
		for _, url := range urls {
			text, ok := url.(string)
			if !ok || !(strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")) {
				fail("AdGuard 订阅必须为 HTTP(S) URL")
			}
		}
	}
	for _, name := range []string{"ros_address_list", "ros_route"} {
		section := s.GetObject(name)
		if section.Value("enabled") != true {
			continue
		}
		args := item(section, "args").(*jsonx.Object)
		for _, key := range []string{"address", "username", "password"} {
			text, ok := args.Value(key).(string)
			if !ok || strip(text) == "" {
				fail(name + " 需要 RouterOS 地址、用户名和密码")
			}
		}
		if tlsValue, present := args.Get("tls"); present {
			tls, ok := tlsValue.(*jsonx.Object)
			if !ok || tls == nil {
				fail("RouterOS TLS 必须为对象，关闭时删除 tls 字段")
			}
			if truthy(tls.Value("ca")) && truthy(tls.Value("insecure")) {
				fail("RouterOS TLS 不能同时设置 CA 和跳过验证")
			}
		}
		checkInteger(args, "min_ttl", 0, int32Max)
		checkInteger(args, "max_ttl", 0, int32Max)
		minTTL, _ := args.GetNumber("min_ttl")
		maxTTL, _ := args.GetNumber("max_ttl")
		if bigOf(minTTL).Cmp(bigOf(maxTTL)) > 0 {
			fail("RouterOS 最小 TTL 不能大于最大 TTL")
		}
		if name == "ros_route" && (!truthy(args.Value("routing_table")) ||
			!(truthy(args.Value("gateway4")) || truthy(args.Value("gateway6")))) {
			fail("RouterOS 路由需要已存在的路由表和至少一个网关")
		}
	}
	update := s.GetObject("rule_update")
	if update.Value("enabled") == true {
		args := item(update, "args").(*jsonx.Object)
		schedule, ok := args.GetOr("schedule", "").(string)
		if !ok || len(pySplit(schedule)) != 5 {
			fail("规则更新需要五段 cron 表达式")
		}
	}
	return s
}

// inRange is minimum <= n <= maximum for an int Number.
func inRange(n jsonx.Number, minimum, maximum int64) bool {
	b := bigOf(n)
	return b.Cmp(big.NewInt(minimum)) >= 0 && b.Cmp(big.NewInt(maximum)) <= 0
}

// checkInteger is normalize_oxidns_settings' integer(obj, key, minimum,
// maximum).
func checkInteger(obj *jsonx.Object, key string, minimum, maximum int64) {
	value := get(obj, key, nil)
	if !isInt(value) || !inRange(value.(jsonx.Number), minimum, maximum) {
		fail(key + " 必须为 " + big.NewInt(minimum).String() + "–" + big.NewInt(maximum).String() + " 的整数")
	}
}

// normalizeRuleDNS is _normalize_rule_dns: shape and disjointness of the
// per-rule DNS fields (rule_actions, rule_dns_parked, rule_dns_follow), in
// both modes. A null action deletes its key.
func normalizeRuleDNS(s *jsonx.Object) {
	for _, field := range []struct {
		key    string
		parked bool
	}{{"rule_actions", false}, {"rule_dns_parked", true}} {
		value, ok := item(s, field.key).(*jsonx.Object)
		if !ok || value == nil {
			fail(field.key + " 必须为以规则 ID 为键的对象")
		}
		normalized := jsonx.NewObject()
		for identity, action := range value.All() {
			if action == nil {
				continue
			}
			normalized.Set(identity, normalizeAction(action, field.parked))
		}
		s.Set(field.key, normalized)
	}
	follow, ok := stringList(item(s, "rule_dns_follow"))
	if !ok {
		fail("rule_dns_follow 必须为规则 ID 数组")
	}
	follow = dedupe(follow)
	s.Set("rule_dns_follow", jsonx.ListOf(follow))
	actions := s.GetObject("rule_actions")
	parked := s.GetObject("rule_dns_parked")
	for _, identity := range follow {
		if action, ok := actions.Get(identity); ok {
			label := RouteLabels[action.(*jsonx.Object).GetString("action")]
			fail("规则 " + identity + " 已设置 DNS 处理「" + label + "」，不能同时跟随策略")
		}
		if parked.Has(identity) {
			fail("规则 " + identity + " 已暂存停用前的 DNS 处理，不能同时跟随策略")
		}
	}
}

// runeLen is len(s) in code points.
func runeLen(s string) int { return utf8.RuneCountInString(s) }
