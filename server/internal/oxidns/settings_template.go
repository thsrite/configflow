package oxidns

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/jsondecode"
	"configflow/internal/textx"
)

// templateSettings is the validated template-mode settings object: the values
// ConfigFlow generates a configuration from. Plugin argument sections (args,
// learn_args, api.ssl) are OxiDNS plugin arguments passed through verbatim,
// so they stay YAML trees. Routing selectors, rule DNS actions and device
// policies are read from node, the settings object they share with native
// routing.
type templateSettings struct {
	node *Node

	WorkerThreads, CacheSize, CacheDumpInterval, ProbeCacheSize              int64
	LocalConcurrent, RemoteConcurrent, FallbackConcurrent, FallbackThreshold int64
	CacheLazyTTL, RemoteTTL                                                  int64

	TCPEnabled, PreferIPv4, MetricsEnabled, AggregateRules, AlwaysStandby bool
	CacheEnabled, CacheDumpEnabled, ProbeCacheEnabled                     bool

	LocalDNS, RemoteDNS, FallbackDNS, Listen, CustomHosts, CustomConfig string
	DefaultForward, CustomMatchPosition, CacheDumpFile                  string
	CustomMatches                                                       json.RawMessage
	BlockQtypes                                                         []int64

	API                                                 apiSettings
	Log                                                 logSettings
	QueryRecorder, IPSelector, ROSAddressList, ROSRoute pluginSection
	RuleUpdate                                          pluginSection
	DynamicDomainSet                                    dynamicDomainSettings
	AdGuard                                             adguardSettings
}

// apiSettings is the HTTP API section; SSL passes through to api.http.ssl.
type apiSettings struct {
	Enabled                               bool
	Listen, Username, Password, WebUIRoot string
	SSL                                   *Node
}

type logSettings struct {
	Level, File, Rotation string
	MaxFiles              int64
}

// pluginSection is an optional plugin and its verbatim arguments.
type pluginSection struct {
	Enabled bool
	Args    *Node
}

type dynamicDomainSettings struct {
	Enabled, Learn  bool
	Route           string
	Args, LearnArgs *Node
}

type adguardSettings struct {
	Enabled       bool
	Content, Mode string
	URLs          []string
}

type member[T any] = jsondecode.Member[T]

// texts is a member that must be a list of text values.
type texts = member[[]member[string]]

// textList returns the values of a list of text values (ok false otherwise).
func textList(list texts) ([]string, bool) {
	if !list.OK {
		return nil, false
	}
	out := make([]string, len(list.Value))
	for i, item := range list.Value {
		if !item.OK {
			return nil, false
		}
		out[i] = item.Value
	}
	return out, true
}

// object is a member that must be a JSON object; Raw keeps its members in order.
type object = member[map[string]json.RawMessage]

// templateWire is the settings object as sent: every checked member keeps its
// raw value so decodeTemplate reports the first invalid one in the original order.
type templateWire struct {
	WorkerThreads       member[int64]           `json:"worker_threads"`
	CacheSize           member[int64]           `json:"cache_size"`
	CacheDumpInterval   member[int64]           `json:"cache_dump_interval"`
	ProbeCacheSize      member[int64]           `json:"probe_cache_size"`
	LocalConcurrent     member[int64]           `json:"local_concurrent"`
	RemoteConcurrent    member[int64]           `json:"remote_concurrent"`
	FallbackConcurrent  member[int64]           `json:"fallback_concurrent"`
	FallbackThreshold   member[int64]           `json:"fallback_threshold"`
	CacheLazyTTL        member[int64]           `json:"cache_lazy_ttl"`
	RemoteTTL           member[int64]           `json:"remote_ttl"`
	TCPEnabled          member[bool]            `json:"tcp_enabled"`
	PreferIPv4          member[bool]            `json:"prefer_ipv4"`
	MetricsEnabled      member[bool]            `json:"metrics_enabled"`
	AggregateRules      member[bool]            `json:"aggregate_rules"`
	AlwaysStandby       member[bool]            `json:"always_standby"`
	CacheEnabled        member[bool]            `json:"cache_enabled"`
	CacheDumpEnabled    member[bool]            `json:"cache_dump_enabled"`
	ProbeCacheEnabled   member[bool]            `json:"probe_cache_enabled"`
	DirectRulesets      texts                   `json:"direct_rulesets"`
	ProxyRulesets       texts                   `json:"proxy_rulesets"`
	DirectRules         texts                   `json:"direct_rules"`
	ProxyRules          texts                   `json:"proxy_rules"`
	LocalDNS            member[string]          `json:"local_dns"`
	RemoteDNS           member[string]          `json:"remote_dns"`
	FallbackDNS         member[string]          `json:"fallback_dns"`
	Listen              member[string]          `json:"listen"`
	CustomHosts         member[string]          `json:"custom_hosts"`
	CustomConfig        member[string]          `json:"custom_config"`
	DefaultForward      member[string]          `json:"default_forward"`
	CustomMatchPosition member[string]          `json:"custom_match_position"`
	CustomMatches       member[[]member[match]] `json:"custom_matches"`
	BlockQtypes         member[[]int64]         `json:"block_qtypes"`
	CacheDumpFile       member[string]          `json:"cache_dump_file"`
	API                 member[apiWire]         `json:"api"`
	Log                 member[logWire]         `json:"log"`
	QueryRecorder       member[sectionWire]     `json:"query_recorder"`
	IPSelector          member[sectionWire]     `json:"ip_selector"`
	DynamicDomainSet    member[dynamicWire]     `json:"dynamic_domain_set"`
	AdGuard             member[adguardWire]     `json:"adguard"`
	ROSAddressList      member[sectionWire]     `json:"ros_address_list"`
	ROSRoute            member[sectionWire]     `json:"ros_route"`
	RuleUpdate          member[sectionWire]     `json:"rule_update"`
}

// match is one custom match entry; matches is a list or newline-separated text.
type match struct {
	Enabled member[bool]            `json:"enabled"`
	Matches member[json.RawMessage] `json:"matches"`
	Exec    member[string]          `json:"exec"`
}

type sectionWire struct {
	Enabled member[bool] `json:"enabled"`
	Args    object       `json:"args"`
}

type apiWire struct {
	sectionWire
	Listen    member[string] `json:"listen"`
	Username  member[string] `json:"username"`
	Password  member[string] `json:"password"`
	WebUIRoot member[string] `json:"webui_root"`
	SSL       member[struct {
		Cert member[string] `json:"cert"`
		Key  member[string] `json:"key"`
	}] `json:"ssl"`
}

type logWire struct {
	sectionWire
	Level    member[string] `json:"level"`
	File     member[string] `json:"file"`
	Rotation member[string] `json:"rotation"`
	MaxFiles member[int64]  `json:"max_files"`
}

type dynamicWire struct {
	sectionWire
	Route     member[string] `json:"route"`
	Learn     member[bool]   `json:"learn"`
	LearnArgs member[struct {
		Phase     member[string] `json:"phase"`
		Questions member[string] `json:"questions"`
		RuleKind  member[string] `json:"rule_kind"`
		ErrorMode member[string] `json:"error_mode"`
	}] `json:"learn_args"`
}

type adguardWire struct {
	sectionWire
	Content member[string]           `json:"content"`
	URLs    member[[]member[string]] `json:"urls"`
	Mode    member[string]           `json:"mode"`
}

// recorderArgs, selectorArgs, dynamicArgs, rosArgs and updateArgs are the
// checked members of plugin arguments; other members pass through unchecked.
type recorderArgs struct {
	QueueSize            member[int64]  `json:"queue_size"`
	BatchSize            member[int64]  `json:"batch_size"`
	FlushIntervalMS      member[int64]  `json:"flush_interval_ms"`
	RetentionDays        member[int64]  `json:"retention_days"`
	CleanupIntervalHours member[int64]  `json:"cleanup_interval_hours"`
	ReaderConcurrency    member[int64]  `json:"reader_concurrency"`
	MemoryTail           member[int64]  `json:"memory_tail"`
	Path                 member[string] `json:"path"`
}

type selectorArgs struct {
	SelectionMode     member[string] `json:"selection_mode"`
	TopN              member[int64]  `json:"top_n"`
	ProbeStagger      member[int64]  `json:"probe_stagger"`
	ProbeTimeout      member[int64]  `json:"probe_timeout"`
	MaxWait           member[int64]  `json:"max_wait"`
	MaxParallelProbes member[int64]  `json:"max_parallel_probes"`
	DNSSECPolicy      member[string] `json:"dnssec_policy"`
	ProbeMethods      texts          `json:"probe_methods"`
}

type dynamicArgs struct {
	Path            member[string] `json:"path"`
	QueueSize       member[int64]  `json:"queue_size"`
	BatchSize       member[int64]  `json:"batch_size"`
	FlushIntervalMS member[int64]  `json:"flush_interval_ms"`
	BootstrapRules  texts          `json:"bootstrap_rules"`
}

type rosArgs struct {
	Address      member[string] `json:"address"`
	Username     member[string] `json:"username"`
	Password     member[string] `json:"password"`
	RoutingTable member[string] `json:"routing_table"`
	Gateway4     member[string] `json:"gateway4"`
	Gateway6     member[string] `json:"gateway6"`
	MinTTL       member[int64]  `json:"min_ttl"`
	MaxTTL       member[int64]  `json:"max_ttl"`
	TLS          member[struct {
		CA       member[string] `json:"ca"`
		Insecure member[bool]   `json:"insecure"`
	}] `json:"tls"`
}

type updateArgs struct {
	Schedule member[string] `json:"schedule"`
}

// maxInt32 is the upper bound of every count and duration setting.
const maxInt32 = 2147483647

// intIn checks one integer member; the message names the member without its section.
func intIn(name string, value member[int64], minimum, maximum int64) error {
	if !value.OK || value.Value < minimum || value.Value > maximum {
		return fail(fmt.Sprintf("%s 必须为 %d–%d 的整数", name, minimum, maximum))
	}
	return nil
}

func oneOf(value member[string], options ...string) bool {
	return value.OK && slices.Contains(options, value.Value)
}

// argsOf decodes the checked members of a section's arguments.
func argsOf[T any](section sectionWire) T {
	var args T
	if section.Args.OK {
		_ = jsondecode.Unmarshal(section.Args.Raw, &args) // members never fail
	}
	return args
}

// nodeOf is the YAML tree of a passed-through member (nil when absent).
func nodeOf(raw json.RawMessage) *Node {
	if raw == nil {
		return nil
	}
	node, err := FromJSON(raw)
	if err != nil {
		return nil
	}
	return node
}

func stateDir(value member[string], message string) error {
	if p, err := safeRelativePath(value.Value, value.OK, PathLabel); err != nil {
		return err
	} else if !strings.HasPrefix(p, "state/") {
		return fail(message)
	}
	return nil
}

// decodeTemplate validates a merged template-mode settings object in the
// order of the editor's checks and decodes it. It stores the deduplicated
// selector lists back into s.
func decodeTemplate(s *Node) (*templateSettings, error) {
	var w templateWire
	if err := jsondecode.Unmarshal(ToJSON(s), &w); err != nil {
		return nil, fail("OxiDNS 设置必须是对象")
	}
	for _, check := range []struct {
		name    string
		value   member[int64]
		minimum int64
		maximum int64
	}{
		{"worker_threads", w.WorkerThreads, 1, 256}, {"cache_size", w.CacheSize, 1, maxInt32},
		{"cache_dump_interval", w.CacheDumpInterval, 1, maxInt32}, {"probe_cache_size", w.ProbeCacheSize, 1, maxInt32},
		{"local_concurrent", w.LocalConcurrent, 1, maxInt32}, {"remote_concurrent", w.RemoteConcurrent, 1, maxInt32},
		{"fallback_concurrent", w.FallbackConcurrent, 1, maxInt32}, {"fallback_threshold", w.FallbackThreshold, 1, maxInt32},
		{"cache_lazy_ttl", w.CacheLazyTTL, 0, maxInt32}, {"remote_ttl", w.RemoteTTL, 0, maxInt32},
	} {
		if err := intIn(check.name, check.value, check.minimum, check.maximum); err != nil {
			return nil, err
		}
	}
	for _, check := range []struct {
		name  string
		value member[bool]
	}{
		{"tcp_enabled", w.TCPEnabled}, {"prefer_ipv4", w.PreferIPv4}, {"metrics_enabled", w.MetricsEnabled},
		{"aggregate_rules", w.AggregateRules}, {"always_standby", w.AlwaysStandby}, {"cache_enabled", w.CacheEnabled},
		{"cache_dump_enabled", w.CacheDumpEnabled}, {"probe_cache_enabled", w.ProbeCacheEnabled},
	} {
		if !check.value.OK {
			return nil, fail(check.name + " 必须为布尔值")
		}
	}
	selectors := map[string][]string{}
	for _, list := range []struct {
		name  string
		value texts
	}{{"direct_rulesets", w.DirectRulesets}, {"proxy_rulesets", w.ProxyRulesets}, {"direct_rules", w.DirectRules}, {"proxy_rules", w.ProxyRules}} {
		ids, ok := textList(list.value)
		if !ok {
			return nil, fail(list.name + " 必须为规则 ID 数组")
		}
		selectors[list.name] = uniqueStrings(ids)
		set(s, list.name, strs(selectors[list.name]))
	}
	for _, suffix := range []string{"rules", "rulesets"} {
		for _, id := range selectors["direct_"+suffix] {
			if slices.Contains(selectors["proxy_"+suffix], id) {
				return nil, fail("同一规则不能同时分配给直连和代理")
			}
		}
	}
	for _, text := range []struct {
		name  string
		value member[string]
	}{{"local_dns", w.LocalDNS}, {"remote_dns", w.RemoteDNS}, {"fallback_dns", w.FallbackDNS}, {"listen", w.Listen}, {"custom_hosts", w.CustomHosts}, {"custom_config", w.CustomConfig}} {
		if !text.value.OK {
			return nil, fail(text.name + " 必须为文本")
		}
	}
	if strings.TrimSpace(w.Listen.Value) == "" {
		return nil, fail("DNS 监听地址不能为空")
	}
	if !oneOf(w.DefaultForward, "forward_local", "forward_remote") {
		return nil, fail("默认转发目标无效")
	}
	if !oneOf(w.CustomMatchPosition, "head", "tail") {
		return nil, fail("自定义匹配执行顺序无效")
	}
	if err := checkMatches(w.CustomMatches); err != nil {
		return nil, err
	}
	if !w.BlockQtypes.OK || slices.ContainsFunc(w.BlockQtypes.Value, func(code int64) bool { return code < 1 || code > 65535 }) {
		return nil, fail("屏蔽查询类型必须为 1–65535 的整数数组")
	}
	if w.CacheDumpEnabled.Value {
		if _, err := safeRelativePath(w.CacheDumpFile.Value, w.CacheDumpFile.OK, "缓存文件"); err != nil {
			return nil, err
		}
	}
	for _, section := range []struct {
		name  string
		ok    bool
		value sectionWire
	}{
		{"api", w.API.OK, w.API.Value.sectionWire}, {"log", w.Log.OK, w.Log.Value.sectionWire},
		{"query_recorder", w.QueryRecorder.OK, w.QueryRecorder.Value}, {"ip_selector", w.IPSelector.OK, w.IPSelector.Value},
		{"dynamic_domain_set", w.DynamicDomainSet.OK, w.DynamicDomainSet.Value.sectionWire}, {"adguard", w.AdGuard.OK, w.AdGuard.Value.sectionWire},
		{"ros_address_list", w.ROSAddressList.OK, w.ROSAddressList.Value}, {"ros_route", w.ROSRoute.OK, w.ROSRoute.Value},
		{"rule_update", w.RuleUpdate.OK, w.RuleUpdate.Value},
	} {
		if !section.ok {
			return nil, fail(section.name + " 必须为对象")
		}
		if section.name != "log" && !section.value.Enabled.OK {
			return nil, fail(section.name + ".enabled 必须为布尔值")
		}
		if section.value.Args.Present() && !section.value.Args.OK {
			return nil, fail(section.name + ".args 必须为对象")
		}
	}
	if err := checkAPI(w.API.Value); err != nil {
		return nil, err
	}
	if err := checkLog(w.Log.Value); err != nil {
		return nil, err
	}
	if err := checkFeatures(w); err != nil {
		return nil, err
	}
	t := &templateSettings{node: s,
		WorkerThreads: w.WorkerThreads.Value, CacheSize: w.CacheSize.Value, CacheDumpInterval: w.CacheDumpInterval.Value,
		ProbeCacheSize: w.ProbeCacheSize.Value, LocalConcurrent: w.LocalConcurrent.Value, RemoteConcurrent: w.RemoteConcurrent.Value,
		FallbackConcurrent: w.FallbackConcurrent.Value, FallbackThreshold: w.FallbackThreshold.Value,
		CacheLazyTTL: w.CacheLazyTTL.Value, RemoteTTL: w.RemoteTTL.Value,
		TCPEnabled: w.TCPEnabled.Value, PreferIPv4: w.PreferIPv4.Value, MetricsEnabled: w.MetricsEnabled.Value,
		AggregateRules: w.AggregateRules.Value, AlwaysStandby: w.AlwaysStandby.Value, CacheEnabled: w.CacheEnabled.Value,
		CacheDumpEnabled: w.CacheDumpEnabled.Value, ProbeCacheEnabled: w.ProbeCacheEnabled.Value,
		LocalDNS: w.LocalDNS.Value, RemoteDNS: w.RemoteDNS.Value, FallbackDNS: w.FallbackDNS.Value, Listen: w.Listen.Value,
		CustomHosts: w.CustomHosts.Value, CustomConfig: w.CustomConfig.Value, DefaultForward: w.DefaultForward.Value,
		CustomMatchPosition: w.CustomMatchPosition.Value, CacheDumpFile: w.CacheDumpFile.Value,
		CustomMatches: w.CustomMatches.Raw, BlockQtypes: w.BlockQtypes.Value,
	}
	api := w.API.Value
	t.API = apiSettings{api.Enabled.Value, api.Listen.Value, api.Username.Value, api.Password.Value, api.WebUIRoot.Value, nodeOf(api.SSL.Raw)}
	t.Log = logSettings{w.Log.Value.Level.Value, w.Log.Value.File.Value, w.Log.Value.Rotation.Value, w.Log.Value.MaxFiles.Value}
	section := func(v sectionWire) pluginSection { return pluginSection{v.Enabled.Value, nodeOf(v.Args.Raw)} }
	t.QueryRecorder, t.IPSelector = section(w.QueryRecorder.Value), section(w.IPSelector.Value)
	t.ROSAddressList, t.ROSRoute, t.RuleUpdate = section(w.ROSAddressList.Value), section(w.ROSRoute.Value), section(w.RuleUpdate.Value)
	dynamic := w.DynamicDomainSet.Value
	t.DynamicDomainSet = dynamicDomainSettings{dynamic.Enabled.Value, dynamic.Learn.Value, dynamic.Route.Value, nodeOf(dynamic.Args.Raw), nodeOf(dynamic.LearnArgs.Raw)}
	adguard := w.AdGuard.Value
	t.AdGuard = adguardSettings{Enabled: adguard.Enabled.Value, Content: adguard.Content.Value, Mode: adguard.Mode.Value}
	for _, url := range adguard.URLs.Value {
		t.AdGuard.URLs = append(t.AdGuard.URLs, url.Value)
	}
	return t, nil
}

func checkMatches(entries member[[]member[match]]) error {
	if !entries.OK {
		return fail("自定义匹配必须为数组")
	}
	for _, entry := range entries.Value {
		if !entry.OK {
			return fail("自定义匹配条目必须为对象")
		}
		if enabled := entry.Value.Enabled; enabled.OK && !enabled.Value {
			continue
		}
		conditions, ok := []member[string]{}, true
		if raw := entry.Value.Matches; raw.Present() {
			var text string
			if jsondecode.Unmarshal(raw.Raw, &text) == nil {
				for _, line := range textx.SplitLines(text, false) {
					conditions = append(conditions, member[string]{Value: line, OK: true})
				}
			} else {
				ok = jsondecode.Unmarshal(raw.Raw, &conditions) == nil
			}
		}
		if !ok || !slices.ContainsFunc(conditions, func(c member[string]) bool { return c.OK && strings.TrimSpace(c.Value) != "" }) {
			return fail("启用的自定义匹配需要至少一个条件")
		}
		if slices.ContainsFunc(conditions, func(c member[string]) bool { return !c.OK }) || !entry.Value.Exec.OK || strings.TrimSpace(entry.Value.Exec.Value) == "" {
			return fail("自定义匹配条件和执行动作必须为非空文本")
		}
	}
	return nil
}

func checkAPI(api apiWire) error {
	for _, text := range []struct {
		name  string
		value member[string]
	}{{"listen", api.Listen}, {"username", api.Username}, {"password", api.Password}, {"webui_root", api.WebUIRoot}} {
		if !text.value.OK {
			return fail("api." + text.name + " 必须为文本")
		}
	}
	if !api.Enabled.Value {
		return nil
	}
	if strings.TrimSpace(api.Listen.Value) == "" {
		return fail("API 监听地址不能为空")
	}
	if (api.Username.Value != "") != (api.Password.Value != "") {
		return fail("API 用户名和密码必须同时设置")
	}
	if !api.SSL.OK {
		return fail("API TLS 设置必须为对象")
	}
	if ssl := api.SSL.Value; (ssl.Cert.Value != "") != (ssl.Key.Value != "") {
		return fail("API TLS 证书和私钥必须同时设置")
	}
	return nil
}

func checkLog(log logWire) error {
	if !log.File.OK {
		return fail("日志文件路径必须为文本")
	}
	if !oneOf(log.Level, "off", "trace", "debug", "info", "warn", "error") {
		return fail("日志级别无效")
	}
	if !oneOf(log.Rotation, "never", "minutely", "hourly", "daily", "weekly") {
		return fail("日志轮转方式无效")
	}
	return intIn("max_files", log.MaxFiles, 0, maxInt32)
}

// checkFeatures checks the arguments of the enabled optional plugins.
func checkFeatures(w templateWire) error {
	if recorder := w.QueryRecorder.Value; recorder.Enabled.Value {
		args := argsOf[recorderArgs](recorder)
		for _, check := range []struct {
			name  string
			value member[int64]
		}{{"queue_size", args.QueueSize}, {"batch_size", args.BatchSize}, {"flush_interval_ms", args.FlushIntervalMS},
			{"retention_days", args.RetentionDays}, {"cleanup_interval_hours", args.CleanupIntervalHours}, {"reader_concurrency", args.ReaderConcurrency}} {
			if err := intIn(check.name, check.value, 1, maxInt32); err != nil {
				return err
			}
		}
		if err := intIn("memory_tail", args.MemoryTail, 0, maxInt32); err != nil {
			return err
		}
		if err := stateDir(args.Path, "查询记录请放在 state/ 下，避免部署覆盖运行数据"); err != nil {
			return err
		}
	}
	if selector := w.IPSelector.Value; selector.Enabled.Value {
		args := argsOf[selectorArgs](selector)
		if !oneOf(args.SelectionMode, "first_success", "best_within_budget", "background") {
			return fail("IP 优选模式无效")
		}
		if err := intIn("top_n", args.TopN, 0, maxInt32); err != nil {
			return err
		}
		for _, check := range []struct {
			name    string
			value   member[int64]
			minimum int64
		}{{"probe_stagger", args.ProbeStagger, 0}, {"probe_timeout", args.ProbeTimeout, 1}, {"max_wait", args.MaxWait, 1}, {"max_parallel_probes", args.MaxParallelProbes, 1}} {
			if err := intIn(check.name, check.value, check.minimum, maxInt32); err != nil {
				return err
			}
		}
		if !oneOf(args.DNSSECPolicy, "reorder_only", "skip") {
			return fail("IP 优选 DNSSEC 策略无效")
		}
		if methods, ok := textList(args.ProbeMethods); !ok || len(methods) == 0 {
			return fail("IP 探测方式必须为非空文本数组")
		}
	}
	if dynamic := w.DynamicDomainSet.Value; dynamic.Enabled.Value {
		if !oneOf(dynamic.Route, "direct", "proxy") || !dynamic.Learn.OK {
			return fail("动态域名集路由或学习设置无效")
		}
		args := argsOf[dynamicArgs](dynamic.sectionWire)
		if err := stateDir(args.Path, "动态规则请放在 state/ 下，避免部署覆盖运行数据"); err != nil {
			return err
		}
		if !dynamic.LearnArgs.OK {
			return fail("学习参数必须为对象")
		}
		for _, check := range []struct {
			name  string
			value member[int64]
		}{{"queue_size", args.QueueSize}, {"batch_size", args.BatchSize}, {"flush_interval_ms", args.FlushIntervalMS}} {
			if err := intIn(check.name, check.value, 1, maxInt32); err != nil {
				return err
			}
		}
		if _, ok := textList(args.BootstrapRules); !ok {
			return fail("动态域名初始规则必须为文本数组")
		}
		learn := dynamic.LearnArgs.Value
		if !oneOf(learn.Phase, "before", "after") || !oneOf(learn.Questions, "first", "all") {
			return fail("动态学习阶段或问题范围无效")
		}
		if !oneOf(learn.RuleKind, "full", "domain") || !oneOf(learn.ErrorMode, "continue", "stop", "fail") {
			return fail("动态学习规则类型或错误行为无效")
		}
	}
	if adguard := w.AdGuard.Value; adguard.Enabled.Value {
		if !oneOf(adguard.Mode, "nxdomain", "nodata", "null", "refused") {
			return fail("AdGuard 拦截响应模式无效")
		}
		if !adguard.Content.OK || !adguard.URLs.OK {
			return fail("AdGuard 规则必须为文本和订阅 URL 数组")
		}
		if strings.TrimSpace(adguard.Content.Value) == "" && len(adguard.URLs.Value) == 0 {
			return fail("启用 AdGuard 时请填写规则或订阅 URL")
		}
		for _, url := range adguard.URLs.Value {
			if !url.OK || !httpURL.MatchString(url.Value) {
				return fail("AdGuard 订阅必须为 HTTP(S) URL")
			}
		}
	}
	for _, section := range []struct {
		name  string
		value sectionWire
	}{{"ros_address_list", w.ROSAddressList.Value}, {"ros_route", w.ROSRoute.Value}} {
		if !section.value.Enabled.Value {
			continue
		}
		if err := checkROS(section.name, argsOf[rosArgs](section.value)); err != nil {
			return err
		}
	}
	if update := w.RuleUpdate.Value; update.Enabled.Value {
		if schedule := argsOf[updateArgs](update).Schedule; !schedule.OK || len(strings.Fields(schedule.Value)) != 5 {
			return fail("规则更新需要五段 cron 表达式")
		}
	}
	return nil
}

func checkROS(name string, args rosArgs) error {
	for _, value := range []member[string]{args.Address, args.Username, args.Password} {
		if strings.TrimSpace(value.Value) == "" {
			return fail(name + " 需要 RouterOS 地址、用户名和密码")
		}
	}
	if args.TLS.Present() {
		if !args.TLS.OK {
			return fail("RouterOS TLS 必须为对象，关闭时删除 tls 字段")
		}
		if tls := args.TLS.Value; tls.CA.Value != "" && tls.Insecure.Value {
			return fail("RouterOS TLS 不能同时设置 CA 和跳过验证")
		}
	}
	if err := intIn("min_ttl", args.MinTTL, 0, maxInt32); err != nil {
		return err
	}
	if err := intIn("max_ttl", args.MaxTTL, 0, maxInt32); err != nil {
		return err
	}
	if args.MinTTL.Value > args.MaxTTL.Value {
		return fail("RouterOS 最小 TTL 不能大于最大 TTL")
	}
	if name == "ros_route" && (args.RoutingTable.Value == "" || args.Gateway4.Value == "" && args.Gateway6.Value == "") {
		return fail("RouterOS 路由需要已存在的路由表和至少一个网关")
	}
	return nil
}

// uniqueStrings keeps the first occurrence of each value.
func uniqueStrings(values []string) []string {
	out, seen := []string{}, map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
