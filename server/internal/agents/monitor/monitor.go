// Package monitor is the read-only OxiDNS monitoring through an
// authenticated, registered Agent: the port of backend/agents/oxidns_monitor.py
// (A06), oxidns_metrics.py (metrics.go), oxidns_dashboard_state.py
// (state.go) and the pure part of oxidns_publication_stats.py
// (publication.go). The ranking history store it reads is A07's
// (history.go, SetRankingHistory).
//
// Every request to an Agent goes through the agents.Client of the manager
// (tests use a fake Agent); the URL is built from the validated origin of
// the registered Agent (AgentOrigin), never from request input.
package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/urlx"
)

// Versions, limits and timings of oxidns_monitor.py.
const (
	MinAgentVersion          = "1.7.0-go"
	MinClientsAgentVersion   = "1.7.1-go"
	MinDashboardAgentVersion = "1.8.14-go"
	ClientSampleLimit        = 10000
	RankingWindowMS          = 24 * 3600 * 1000
	MaxResponseBytes         = 5 * 1024 * 1024
	MaxInteger               = 1<<53 - 1
	BlockRankingMatcherLimit = 32
)

// RankingReuseSeconds and RankingBackoffSeconds are RANKING_REUSE_SECONDS
// and RANKING_BACKOFF_SECONDS (variables so tests can change them like the
// Python fixtures).
var (
	RankingReuseSeconds   = 60.0
	RankingBackoffSeconds = 180.0
)

// rankingBackoffMinutes is RANKING_BACKOFF_SECONDS // 60 (read at call
// time, like the f-string in _rankings_unavailable).
func rankingBackoffMinutes() int64 { return int64(math.Floor(RankingBackoffSeconds / 60)) }

// RcodeFilterText is RCODE_FILTER_TEXT.
var RcodeFilterText = map[string]string{
	"NOERROR":   "No Error",
	"FORMERR":   "Form Error",
	"SERVFAIL":  "Server Failure",
	"NXDOMAIN":  "Non-Existent Domain",
	"NOTIMP":    "Not Implemented",
	"REFUSED":   "Query Refused",
	"YXDOMAIN":  "Name should not exist",
	"YXRRSET":   "RR Set should not exist",
	"NXRRSET":   "RR Set does not exist",
	"NOTAUTH":   "Not authorized",
	"NOTZONE":   "Name not in zone",
	"BADVERS":   "Bad OPT Version",
	"BADSIG":    "TSIG Signature Failure",
	"BADKEY":    "Key not recognized",
	"BADTIME":   "Signature out of time window",
	"BADMODE":   "Bad TKEY mode",
	"BADNAME":   "Duplicate key name",
	"BADALG":    "Algorithm not supported",
	"BADTRUNC":  "Bad truncation",
	"BADCOOKIE": "Bad server cookie",
}

// Messages is MESSAGES.
var Messages = map[string]string{
	"UPGRADE_REQUIRED":        "请将此 Agent 升级到 1.7.0-go 或更新版本后使用 DNS 监控。",
	"API_NOT_CONFIGURED":      "OxiDNS 管理 API 未启用。请在 OxiDNS 设置的「API / 日志」中开启，保存后发布到此 Agent。",
	"RECORDER_NOT_CONFIGURED": "查询记录未启用，请在 OxiDNS 设置中开启查询记录并推送配置。",
	"TLS_UNSUPPORTED":         "此监控连接暂不支持当前 OxiDNS API 的 TLS 配置。",
	"CORE_UNREACHABLE":        "暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。",
	"CORE_AUTH_FAILED":        "OxiDNS 管理 API 认证失败，请检查核心配置中的认证设置。",
	"AGENT_UNREACHABLE":       "暂时无法连接 Agent，请检查网络和 Agent 运行状态。",
	"AGENT_AUTH_FAILED":       "Agent 认证失败，请检查注册状态。",
	"INVALID_RESPONSE":        "监控数据格式异常，请检查 Agent 和 OxiDNS 版本。",
	"INVALID_QUERY":           "查询条件无效，请检查筛选内容。",
	"RECORD_NOT_FOUND":        "这条查询记录已不存在，可能已超过保留期限。",
	"DASHBOARD_STATE_ERROR":   "无法读取或保存 DNS 面板统计起点，请检查数据目录并刷新面板确认状态。",
	"DASHBOARD_CHANGED":       "设备或统计起点已改变，请重新载入 DNS 面板。",
	"SNAPSHOT_CHANGED":        "采集期间核心或配置发生变化，请稍后刷新。",
	"CONFIG_UNSUPPORTED":      "当前核心 API 配置无法用于监控，请检查监听地址与认证设置。",
}

// Error is MonitorError: Code, HTTP Status and the user Message.
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// NewError is MonitorError(code, status, message): status 0 is 502, an
// empty message the code's text (INVALID_RESPONSE's for unknown codes).
func NewError(code string, status int, message string) *Error {
	if status == 0 {
		status = 502
	}
	if message == "" {
		var ok bool
		if message, ok = Messages[code]; !ok {
			message = Messages["INVALID_RESPONSE"]
		}
	}
	return &Error{Code: code, Status: status, Message: message}
}

func upgradeRequired(minVersion string) *Error {
	feature := "DNS 监控"
	switch minVersion {
	case MinClientsAgentVersion:
		feature = "客户端 IP 选择与筛选"
	case MinDashboardAgentVersion:
		feature = "DNS 排行与统计重置"
	}
	return NewError("UPGRADE_REQUIRED", 409, "请将此 Agent 升级到 "+minVersion+" 或更新版本后使用"+feature+"。")
}

// Manager is what the monitor needs of the Agent manager.
type Manager interface {
	GetAgentByID(ctx context.Context, agentID string, history bool) (*jsonx.Object, error)
	DataDir() string
	Client() agents.Client
}

type agentsManager struct{ *agents.Manager }

func (m agentsManager) DataDir() string { return m.Repository().DataDir() }

// FromAgents adapts the registry's Manager.
func FromAgents(m *agents.Manager) Manager { return agentsManager{m} }

// agentFor is _agent: the registered OxiDNS Agent of at least minVersion.
func agentFor(ctx context.Context, m Manager, agentID, minVersion string, history bool) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, history)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 {
		return nil, NewError("AGENT_NOT_FOUND", 404, "未找到这台 Agent。")
	}
	if !pyEqualStr(agent.Value("service_type"), "oxidns") {
		return nil, NewError("WRONG_SERVICE", 400, "DNS 监控仅适用于 OxiDNS Agent。")
	}
	if !versionAtLeast(agent, minVersion) {
		return nil, upgradeRequired(minVersion)
	}
	return agent, nil
}

// versionAtLeast is compare_versions(agent.get('version', '0'), v) >= 0
// with the errors of a non-str version counting as unsupported.
func versionAtLeast(agent *jsonx.Object, minVersion string) bool {
	value, present := agent.Get("version")
	if !present {
		value = "0"
	}
	version, ok := value.(string)
	return ok && agents.CompareVersions(version, minVersion) >= 0
}

var hostnameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// AgentOrigin is _agent_origin: the validated http://host:port of a
// registered Agent; never taken from request input.
func AgentOrigin(agent *jsonx.Object) (string, error) {
	value, present := agent.Get("host")
	if !present {
		value = ""
	}
	host, ok := value.(string)
	if !ok || host == "" || strings.ContainsAny(host, "/?#@\\") {
		return "", NewError("AGENT_UNREACHABLE", 0, "")
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if canonical, v6, ok := pyIPAddress(host); ok {
		host = canonical
		if v6 {
			host = "[" + canonical + "]"
		}
	} else if !hostnameRE.MatchString(host) {
		return "", NewError("AGENT_UNREACHABLE", 0, "")
	}
	port := agent.Value("port")
	if !pyIntIn(port, 1, 65535) {
		return "", NewError("AGENT_UNREACHABLE", 0, "")
	}
	return "http://" + host + ":" + strconv.FormatInt(pyInt64(port), 10), nil
}

// query is an ordered str → str dict (request params).
type query [][2]string

func (q query) get(key string) (string, bool) {
	for _, pair := range q {
		if pair[0] == key {
			return pair[1], true
		}
	}
	return "", false
}

func (q query) set(key, value string) query {
	for i, pair := range q {
		if pair[0] == key {
			q[i][1] = value
			return q
		}
	}
	return append(q, [2]string{key, value})
}

// encode is requests' params encoding (urlencode: quote_plus of each key
// and value).
func (q query) encode() string {
	parts := make([]string, len(q))
	for i, pair := range q {
		parts[i] = urlx.QuotePlus(pair[0]) + "=" + urlx.QuotePlus(pair[1])
	}
	return strings.Join(parts, "&")
}

func unreachable() *Error { return NewError("AGENT_UNREACHABLE", 0, "") }

// doRequest is _request (a variable so tests can replace it, like the
// Python tests' monkeypatch of _request).
var doRequest = request

// request is _request: GET <origin>/api/oxidns/<endpoint> with the Agent's
// token, no proxy, no redirects, at most MaxResponseBytes; the answer must
// be a JSON object without an error.
func request(ctx context.Context, m Manager, agent *jsonx.Object, endpoint string, params query, minVersion string) (*jsonx.Object, error) {
	origin, err := AgentOrigin(agent)
	if err != nil {
		return nil, err
	}
	url := origin + "/api/oxidns/" + endpoint
	token, ok := agent.Value("token").(string)
	if !ok {
		return nil, unreachable() // KeyError / TypeError
	}
	// http.client sends header values as latin-1; anything else is a
	// UnicodeEncodeError (a ValueError) and the request is not sent.
	encodedToken, ok := latin1(token)
	if !ok {
		return nil, unreachable()
	}
	if len(params) > 0 {
		url += "?" + params.encode()
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+encodedToken)
	header.Set("Accept", "application/json")
	if endpoint == "queries" {
		// New Agents use the core's SSE feed to enrich list rows.
		header.Set("X-ConfigFlow-Query-Sources", "1")
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: "GET", URL: url, Header: header,
		Timeout: agents.Pair(3, 12), NoProxy: true, NoRedirects: true, MaxBody: MaxResponseBytes})
	if err != nil {
		return nil, unreachable()
	}
	status := response.StatusCode
	if status == 401 || status == 403 {
		return nil, NewError("AGENT_AUTH_FAILED", 0, "")
	}
	if status >= 300 && status < 400 {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	if len(response.Body) > MaxResponseBytes {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	if response.BodyError != nil {
		return nil, unreachable()
	}
	value, ok := loadsBytes(response.Body)
	if !ok {
		if status == 404 {
			return nil, upgradeRequired(minVersion)
		}
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	data, ok := value.(*jsonx.Object)
	if !ok || data == nil {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	if status >= 400 || data.Value("success") == false || data.Value("ok") == false {
		codeValue := data.Value("code")
		if unhashable(codeValue) {
			return nil, unreachable() // TypeError of `code not in MESSAGES`
		}
		code, _ := codeValue.(string)
		if _, known := Messages[code]; !known {
			code = "CORE_UNREACHABLE"
			if status == 404 {
				code = "UPGRADE_REQUIRED"
			}
		}
		errStatus := 502
		switch code {
		case "RECORD_NOT_FOUND":
			errStatus = 404
		case "INVALID_QUERY":
			errStatus = 400
		case "UPGRADE_REQUIRED", "API_NOT_CONFIGURED", "RECORDER_NOT_CONFIGURED", "TLS_UNSUPPORTED",
			"CONFIG_UNSUPPORTED", "SNAPSHOT_CHANGED":
			errStatus = 409
		}
		if code == "UPGRADE_REQUIRED" {
			return nil, upgradeRequired(minVersion)
		}
		return nil, NewError(code, errStatus, "")
	}
	return data, nil
}

// latin1 is s.encode('latin-1') as a Go string of those bytes.
func latin1(s string) (string, bool) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return "", false
		}
		b = append(b, byte(r))
	}
	return string(b), true
}

// loadsBytes is json.loads(bytes); ok false for its ValueError /
// UnicodeError (including the 4300-digit int limit).
func loadsBytes(body []byte) (any, bool) {
	text, ok := agents.DecodeJSONBytes(body)
	if !ok {
		return nil, false
	}
	value, err := jsonx.ParseString(text)
	if err != nil || exceedsDigitLimit(value) {
		return nil, false
	}
	return value, true
}

// pyNumber is _number: a non-negative finite int/float up to MaxInteger,
// otherwise nil.
func pyNumber(value any) any {
	n, ok := asNum(value)
	if !ok {
		return nil
	}
	if n.isNaN() {
		return nil
	}
	if n.less(intNum(0)) || intNum(MaxInteger).less(n) {
		return nil
	}
	if f, err := n.isFinite(); err != nil || !f {
		return nil
	}
	return value
}

// text is _text(value, limit).
func text(value any, limit int) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return pySlice(s, limit)
}

// RankingRanges is RANKING_RANGES.
var RankingRanges = []string{"round", "24h", "7d", "30d"}

// uniqueKeys is `for key in request.args` (each key once, first-seen order).
func uniqueKeys(args [][2]string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, pair := range args {
		if !seen[pair[0]] {
			seen[pair[0]] = true
			keys = append(keys, pair[0])
		}
	}
	return keys
}

func getList(args [][2]string, key string) []string {
	var out []string
	for _, pair := range args {
		if pair[0] == key {
			out = append(out, pair[1])
		}
	}
	return out
}

func invalidQuery() *Error { return NewError("INVALID_QUERY", 400, "") }

// dashboardParams is _dashboard_params.
func dashboardParams(args [][2]string) (query, error) {
	var result query
	for _, key := range uniqueKeys(args) {
		values := getList(args, key)
		if len(values) != 1 || key == "recorder" && !ValidRecorderTag(values[0]) ||
			key == "range" && !contains(RankingRanges, values[0]) || key != "recorder" && key != "range" {
			return nil, invalidQuery()
		}
		result = result.set(key, values[0])
	}
	return result, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// target is _target: a digest of the registered target and token.
func target(agent *jsonx.Object) (string, error) {
	identity := make([]any, 0, 5)
	for _, key := range []string{"id", "host", "port", "token", "service_type"} {
		identity = append(identity, agent.Value(key))
	}
	encoded, err := jsonx.Encode(identity, jsonx.Options{EnsureASCII: true, Compact: true})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// hasControl is any(ord(c) < 32 or 127 <= ord(c) <= 159 for c in value).
func hasControl(value string) bool {
	for _, r := range value {
		if r < 32 || (r >= 127 && r <= 159) {
			return true
		}
	}
	return false
}

// displayUpstream is _display_upstream: an upstream label without
// credentials, query or fragment.
func displayUpstream(value any) string {
	s := text(value, 2048)
	if hasControl(s) {
		return ""
	}
	if strings.Contains(s, "://") {
		parsed, err := urlx.Split(s)
		if err != nil {
			return "上游服务器"
		}
		host := parsed.Hostname()
		if host == "" {
			return "上游服务器"
		}
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		port, hasPort, err := parsed.Port()
		if err != nil {
			return "上游服务器"
		}
		netloc := host
		if hasPort {
			netloc += ":" + strconv.Itoa(port)
		}
		return urlx.Unsplit(parsed.Scheme, netloc, parsed.Path, "", "")
	}
	s, _, _ = strings.Cut(s, "?")
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// publicUpstreams is _public_upstreams.
func publicUpstreams(rows []any, labels []any) ([]any, error) {
	addresses := map[string]any{}
	for _, item := range labels {
		label := item.(*jsonx.Object)
		key, _ := jsonx.Compact([]any{label.Value("plugin_tag"), label.Value("upstream")})
		addresses[string(key)] = label.Value("address")
	}
	out := make([]any, 0, len(rows))
	for _, item := range rows {
		row := item.(*jsonx.Object)
		pair := []any{row.Value("tag"), row.Value("upstream")}
		encoded, err := jsonx.Encode(pair, jsonx.Options{EnsureASCII: true, Compact: true})
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(encoded)
		key, _ := jsonx.Compact(pair)
		address, ok := addresses[string(key)]
		if !ok {
			address = ""
		}
		public := row.Clone()
		public.Set("id", hex.EncodeToString(digest[:]))
		public.Set("tag", displayUpstream(row.Value("tag")))
		public.Set("upstream", displayUpstream(row.Value("upstream")))
		public.Set("address", displayUpstream(address))
		out = append(out, public)
	}
	return out, nil
}

var (
	instanceRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	reasonRE   = regexp.MustCompile(`^[a-z_]{1,64}$`)
	recorderRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?)*$`)
	cursorRE   = regexp.MustCompile(`^[0-9]{1,16}:[1-9][0-9]{0,15}$`)
	digits16RE = regexp.MustCompile(`^[0-9]{1,16}$`)
	qtypeRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	rcodeRE    = regexp.MustCompile(`^[A-Za-z0-9 _-]{1,64}$`)
	recordIDRE = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
)

// snapshotResult is _overview_snapshot's tuple.
type snapshotResult struct {
	data         *jsonx.Object
	core         *jsonx.Object
	recorders    []any
	observations *jsonx.Object
	labels       []any
}

func validRevision(value any) bool { return strMatches(value, hex64) }

// overviewSnapshot is _overview_snapshot.
func overviewSnapshot(ctx context.Context, m Manager, agent *jsonx.Object, dashboard bool) (*snapshotResult, error) {
	minVersion := MinAgentVersion
	if dashboard {
		minVersion = MinDashboardAgentVersion
	}
	data, err := doRequest(ctx, m, agent, "overview", nil, minVersion)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	health, okH := data.Value("health").(*jsonx.Object)
	capabilities, okC := data.Value("capabilities").(*jsonx.Object)
	metricsText, okT := data.Value("metrics").(string)
	if !okH || !okC || !okT || health == nil || capabilities == nil {
		return nil, invalid
	}
	recordersValue, present := capabilities.Get("query_recorders")
	if !present {
		recordersValue = []any{}
	}
	recorders, ok := recordersValue.([]any)
	if !ok || len(recorders) > 64 {
		return nil, invalid
	}
	for _, item := range recorders {
		if _, ok := item.(string); !ok {
			return nil, invalid
		}
	}
	observations, err := ParseMetrics(metricsText)
	if err != nil {
		return nil, invalid
	}
	core := jsonx.Obj("version", text(health.Value("version"), 64), "status", text(health.Value("status"), 32),
		"uptime_ms", pyNumber(health.Value("uptime_ms")), "instance_id", text(health.Value("instance_id"), 128))
	labelsValue, present := data.Get("upstream_labels")
	if !present {
		labelsValue = []any{}
	}
	labels, ok := labelsValue.([]any)
	if !ok || len(labels) > ClientSampleLimit {
		return nil, invalid
	}
	seen := map[string]bool{}
	for _, item := range labels {
		label, ok := item.(*jsonx.Object)
		if !ok || label == nil {
			return nil, invalid
		}
		for _, key := range []string{"plugin_tag", "upstream", "address"} {
			s, ok := label.Value(key).(string)
			if !ok || runeLen(s) > 16_384 {
				return nil, invalid
			}
		}
		id, _ := jsonx.Compact([]any{label.Value("plugin_tag"), label.Value("upstream")})
		if seen[string(id)] {
			return nil, invalid
		}
		seen[string(id)] = true
	}
	if dashboard {
		for _, tag := range recorders {
			if !ValidRecorderTag(tag) {
				return nil, invalid
			}
		}
		if !strMatches(health.Value("instance_id"), instanceRE) ||
			!pyIntIn(data.Value("collected_at_ms"), 0, 253402300799999) ||
			!validRevision(data.Value("source_revision")) || !validRevision(data.Value("runtime_revision")) {
			return nil, invalid
		}
	}
	return &snapshotResult{data, core, recorders, observations, labels}, nil
}

// blockedReasons is BLOCKED_REASONS.
var blockedReasons = map[string]string{"standard_adguard": "标准 AdGuard 拦截路径", "rule_dns": "规则拦截（拒绝/黑洞）",
	"standard_adguard_rule_dns": "标准 AdGuard 与规则拦截"}

// rankingsUnavailable is _rankings_unavailable(code, **fields).
func rankingsUnavailable(code string, fields ...any) *jsonx.Object {
	reasons := map[string]string{
		"upgrade_required":          "请将 Agent 升级到 " + MinDashboardAgentVersion + " 或更新版本以查看排行并重置统计。",
		"recorder_not_configured":   "未启用查询记录，无法计算域名和客户端排行。",
		"unsupported_configuration": "当前配置未包含可确认的拦截路径（标准 AdGuard 或规则「拒绝/黑洞」）。",
		"too_many_block_rules": fmt.Sprintf("拦截规则分组超过 %d 个，暂不统计被拦截域名排行；", BlockRankingMatcherLimit) +
			"请合并拒绝/黑洞规则后再查看。",
		"snapshot_changed": "采集期间核心或配置发生变化，请稍后刷新。",
		"rankings_paused":  fmt.Sprintf("上次统计排行超时，已暂停 %d 分钟后再试，以免影响查询记录。", rankingBackoffMinutes()),
	}
	reason, ok := reasons[code]
	if !ok {
		if reason, ok = Messages[strings.ToUpper(code)]; !ok {
			reason = "暂时无法获取排行，请稍后刷新。"
		}
	}
	result := jsonx.Obj("available", false, "success", false, "code", code, "reason", reason,
		"sample_size", 0, "rows", []any{}, "at_limit", false)
	for i := 0; i+1 < len(fields); i += 2 {
		result.Set(fields[i].(string), fields[i+1])
	}
	return result
}

// rankingSection is _ranking_section(value, kind) with at most 10 rows.
func rankingSection(value any, kind string) (*jsonx.Object, error) {
	return rankingSectionRows(value, kind, 10)
}

// rankingSectionRows is _ranking_section(value, kind, rows=limit).
func rankingSectionRows(value any, kind string, limit int) (*jsonx.Object, error) {
	invalid := NewError("INVALID_RESPONSE", 0, "")
	section, ok := value.(*jsonx.Object)
	if !ok || section == nil {
		return nil, invalid
	}
	sampleValue, rowsValue, atLimitValue := section.Value("sample_size"), section.Value("rows"), section.Value("at_limit")
	rows, okRows := rowsValue.([]any)
	atLimit, okAt := atLimitValue.(bool)
	if !pyIntIn(sampleValue, 0, ClientSampleLimit) || !okRows || len(rows) > limit || !okAt {
		return nil, invalid
	}
	sample := pyInt64(sampleValue)
	if atLimit != (sample == ClientSampleLimit) {
		return nil, invalid
	}
	factor := int64(65535)
	if kind == "clients" {
		factor = 1
	}
	type entry struct {
		key   string
		count int64
	}
	var result []entry
	seen := map[string]bool{}
	total := int64(0)
	for _, item := range rows {
		row, ok := item.(*jsonx.Object)
		if !ok || row == nil {
			return nil, invalid
		}
		key, ok := row.Value("key").(string)
		if !ok {
			return nil, invalid
		}
		count := row.Value("count")
		if key == "" || runeLen(key) > 1024 || hasControl(key) || seen[key] || sample*factor < 1 ||
			!pyIntIn(count, 1, sample*factor) {
			return nil, invalid
		}
		if kind == "clients" && !isPlainIP(key) {
			return nil, invalid
		}
		seen[key] = true
		total += pyInt64(count)
		result = append(result, entry{key, pyInt64(count)})
	}
	if total > sample*factor {
		return nil, invalid
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].count != result[j].count {
			return result[i].count > result[j].count
		}
		return result[i].key < result[j].key
	})
	out := make([]any, len(result))
	for i, e := range result {
		out[i] = jsonx.Obj("key", e.key, "count", e.count, "share", jsonx.Float(float64(e.count)/float64(sample)))
	}
	return jsonx.Obj("sample_size", sample, "rows", out, "at_limit", atLimit), nil
}

// kept is one entry of _rankings_kept.
type kept struct {
	key      string
	result   *jsonx.Object
	at       float64
	failedAt *float64
}

var (
	rankingsMu   sync.Mutex
	rankingsKept = map[string]*kept{} // (data dir, Agent id) → kept rankings
	clockStart   = time.Now()
	// monotonic is time.monotonic() (a variable for tests).
	monotonic = func() float64 { return time.Since(clockStart).Seconds() }
)

// ResetRankingsCache empties _rankings_kept (tests).
func ResetRankingsCache() {
	rankingsMu.Lock()
	rankingsKept = map[string]*kept{}
	rankingsMu.Unlock()
}

func compactKey(parts ...any) string {
	encoded, _ := jsonx.Compact(parts)
	return string(encoded)
}

// keptRankings is _kept_rankings: dashboardRankings reused for a minute and
// paused after a timeout, per Agent for one statistics round.
func keptRankings(ctx context.Context, m Manager, agent *jsonx.Object, params query, snapshot, state *jsonx.Object,
	recorders []any, manual bool) (*jsonx.Object, error) {
	identity := compactKey(m.DataDir(), agent.Value("id"))
	recorderParam, _ := params.get("recorder")
	key := compactKey(recorderParam, recorders, state.Value("generation"), state.Value("instance_id"),
		state.Value("source_revision"), state.Value("runtime_revision"))
	now := monotonic()
	rankingsMu.Lock()
	k := rankingsKept[identity]
	if k != nil && k.key != key {
		k = nil
	}
	rankingsMu.Unlock()
	if k != nil && !manual {
		if k.result != nil && k.failedAt == nil && 0 <= now-k.at && now-k.at < RankingReuseSeconds {
			return k.result, nil
		}
		if k.failedAt != nil && 0 <= now-*k.failedAt && now-*k.failedAt < RankingBackoffSeconds {
			if k.result != nil {
				return k.result, nil
			}
			return rankingsUnavailable("rankings_paused"), nil
		}
	}
	result, err := dashboardRankings(ctx, m, agent, params, snapshot, state, recorders)
	if err != nil {
		var monitorErr *Error
		if errors.As(err, &monitorErr) && (monitorErr.Code == "CORE_UNREACHABLE" || monitorErr.Code == "AGENT_UNREACHABLE") {
			var previous *jsonx.Object
			at := now
			if k != nil {
				previous, at = k.result, k.at
			}
			failed := now
			rankingsMu.Lock()
			rankingsKept[identity] = &kept{key: key, result: previous, at: at, failedAt: &failed}
			rankingsMu.Unlock()
			if previous != nil && !manual {
				return previous, nil // this round's last answer, rather than an error
			}
		}
		return nil, err
	}
	rankingsMu.Lock()
	rankingsKept[identity] = &kept{key: key, result: result, at: now}
	rankingsMu.Unlock()
	return result, nil
}

func containsAny(list []any, value any) bool {
	for _, item := range list {
		if jsonx.Equal(item, value) {
			return true
		}
	}
	return false
}

// dashboardRankings is _dashboard_rankings.
func dashboardRankings(ctx context.Context, m Manager, agent *jsonx.Object, params query, snapshot, state *jsonx.Object,
	recorders []any) (*jsonx.Object, error) {
	started := pyInt64(state.Value("started_at_ms"))
	collected := pyInt64(snapshot.Value("collected_at_ms"))
	since := collected - RankingWindowMS
	if started > since {
		since = started
	}
	bounds := []any{"since_ms", since, "until_ms", collected}
	if recorder, ok := params.get("recorder"); ok && recorder != "" && !containsAny(recorders, recorder) {
		return nil, invalidQuery()
	}
	if len(recorders) == 0 {
		return rankingsUnavailable("recorder_not_configured", bounds...), nil
	}
	requestParams := append(query{}, params...)
	requestParams = requestParams.set("since_ms", strconv.FormatInt(since, 10))
	requestParams = requestParams.set("until_ms", strconv.FormatInt(collected, 10))
	data, err := doRequest(ctx, m, agent, "rankings", requestParams, MinDashboardAgentVersion)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	recorder := data.Value("recorder")
	recorderParam, hasRecorderParam := params.get("recorder")
	retention, hasRetention := data.Get("retention_days")
	if data.Value("success") != true || !ValidRecorderTag(recorder) || !containsAny(recorders, recorder) ||
		hasRecorderParam && !pyEqualStr(recorder, recorderParam) ||
		!pyIntIn(data.Value("collected_at_ms"), 0, 253402300799999) ||
		!pyNumberEqual(data.Value("sample_limit"), jsonx.Int(ClientSampleLimit)) || !isPyInt(data.Value("sample_limit")) ||
		hasRetention && retention != nil && !pyIntIn(retention, 0, 36500) {
		return nil, invalid
	}
	if !jsonx.Equal(data.Value("instance_id"), state.Value("instance_id")) ||
		!jsonx.Equal(data.Value("source_revision"), state.Value("source_revision")) ||
		!jsonx.Equal(data.Value("runtime_revision"), state.Value("runtime_revision")) ||
		pyInt64(data.Value("collected_at_ms")) < collected {
		return nil, NewError("SNAPSHOT_CHANGED", 409, "")
	}
	blocked, ok := data.Value("blocked").(*jsonx.Object)
	if !ok || blocked == nil {
		return nil, invalid
	}
	available, ok := blocked.Value("available").(bool)
	if !ok {
		return nil, invalid
	}
	var blockedResult *jsonx.Object
	if available {
		codeValue, present := blocked.Get("reason")
		if !present {
			codeValue = "standard_adguard"
		}
		code, ok := codeValue.(string)
		if !ok || !reasonRE.MatchString(code) {
			return nil, invalid
		}
		label, ok := blockedReasons[code]
		if !ok {
			label = "拦截路径"
		}
		section, err := rankingSection(blocked, "domains")
		if err != nil {
			return nil, err
		}
		blockedResult = jsonx.Obj("available", true, "code", code, "reason", label)
		blockedResult.Update(section)
		if tag, present := blocked.Get("matcher_tag"); present {
			if !ValidRecorderTag(tag) {
				return nil, invalid
			}
			blockedResult.Set("matcher_tag", tag)
		}
		if tagsValue, present := blocked.Get("matcher_tags"); present {
			tags, ok := tagsValue.([]any)
			if !ok || len(tags) == 0 || len(tags) > BlockRankingMatcherLimit {
				return nil, invalid
			}
			for _, tag := range tags {
				if !ValidRecorderTag(tag) {
					return nil, invalid
				}
			}
			blockedResult.Set("matcher_tags", append([]any{}, tags...))
		}
	} else {
		reason, ok := blocked.Value("reason").(string)
		if !ok || !reasonRE.MatchString(reason) {
			reason = "unsupported_configuration"
		}
		blockedResult = rankingsUnavailable(reason)
	}
	clients, err := rankingSection(data.Value("clients"), "clients")
	if err != nil {
		return nil, err
	}
	domains, err := rankingSection(data.Value("domains"), "domains")
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("available", true, "success", true, "recorder", recorder,
		"collected_at_ms", data.Value("collected_at_ms"), "instance_id", data.Value("instance_id"),
		"source_revision", data.Value("source_revision"), "runtime_revision", data.Value("runtime_revision"),
		"retention_days", retention, "sample_limit", ClientSampleLimit,
		"since_ms", since, "until_ms", collected, "clients", clients, "domains", domains, "blocked", blockedResult), nil
}

// floorDiv and floorMod are Python's // and % for ints.
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

// historyRankings is _history_rankings: complete rankings added up from the
// stored periods, or nil to fall back to the live sample.
func historyRankings(m Manager, agent *jsonx.Object, recorder any, rangeKey string, snapshot, state *jsonx.Object,
	recorders []any) (*jsonx.Object, error) {
	recorderText, isStr := recorder.(string)
	if len(recorders) == 0 || !containsAny(recorders, recorder) || !HistorySupported(agent) {
		return nil, nil
	}
	_ = isStr
	now := floorDiv(pyInt64(snapshot.Value("collected_at_ms")), 1000)
	var since int64
	if rangeKey == "round" {
		since = floorDiv(pyInt64(state.Value("started_at_ms")), 1000)
	} else {
		since = now - HistoryRanges[rangeKey]
	}
	since -= floorMod(since, HistoryHour)
	until := now - floorMod(now, HistoryHour) + HistoryHour
	agentID := jsonx.Str(agent.Value("id"))
	stored, err := getHistory(m.DataDir()).Query(agentID, recorderText, since, until)
	if err != nil {
		return nil, err
	}
	if stored.Slots == 0 {
		return nil, nil
	}
	first, last := stored.First, stored.Last
	expected := floorDiv(last-first, HistorySlot)
	if expected < 1 {
		expected = 1
	}
	section := func(kind string, total int64) *jsonx.Object {
		rows := []any{}
		for _, row := range stored.Rows[kind] {
			var share any = jsonx.Int(0)
			if total != 0 {
				f, _ := intNum(row.Count).trueDiv(intNum(total))
				share = jsonx.Float(f)
			}
			rows = append(rows, jsonx.Obj("key", row.Key, "count", row.Count, "share", share))
		}
		return jsonx.Obj("sample_size", total, "at_limit", false, "rows", rows)
	}
	var blocked *jsonx.Object
	if stored.BlockedSlots != 0 {
		blocked = jsonx.Obj("available", true, "code", "history", "reason", "拦截路径")
		blocked.Update(section("blocked", stored.BlockedRecords))
	} else {
		blocked = rankingsUnavailable("unsupported_configuration")
	}
	missing := expected - stored.Slots
	if missing < 0 {
		missing = 0
	}
	return jsonx.Obj("available", true, "success", true, "source", "history", "range", rangeKey, "recorder", recorder,
		"collected_at_ms", last*1000, "instance_id", state.Value("instance_id"),
		"source_revision", state.Value("source_revision"), "runtime_revision", state.Value("runtime_revision"),
		"retention_days", HistoryRetentionDay, "sample_limit", ClientSampleLimit,
		"since_ms", since*1000, "until_ms", last*1000, "first_ms", first*1000,
		"slot_seconds", HistorySlot, "missing_slots", missing, "partial_slots", stored.PartialSlots,
		"clients", section("clients", stored.Records), "domains", section("domains", stored.Records), "blocked", blocked), nil
}

// historyStatus is _history_status.
func historyStatus(m Manager, agent *jsonx.Object, recorders []any, recorder any) (*jsonx.Object, error) {
	status := jsonx.Obj("history_version", HistoryAgentVersion)
	if !HistorySupported(agent) {
		status.Set("history", "upgrade_required")
		return status, nil
	}
	stored := false
	if containsAny(recorders, recorder) {
		recorderText, _ := recorder.(string)
		var err error
		if stored, err = getHistory(m.DataDir()).HasAny(jsonx.Str(agent.Value("id")), recorderText); err != nil {
			return nil, err
		}
	}
	if stored {
		status.Set("history", "pending_range")
	} else {
		status.Set("history", "collecting")
	}
	return status, nil
}

func verifyTarget(ctx context.Context, m Manager, agentID, expected string) error {
	current, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return err
	}
	if current == nil {
		return NewError("DASHBOARD_CHANGED", 409, "")
	}
	digest, err := target(current)
	if err != nil {
		return err
	}
	if digest != expected {
		return NewError("DASHBOARD_CHANGED", 409, "")
	}
	return nil
}

func agentSummary(agent *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, key := range []string{"id", "name", "host", "profile_id", "version", "status"} {
		out.Set(key, agent.Value(key))
	}
	return out
}

// nowUTC is datetime.now(timezone.utc) (a variable for tests).
var nowUTC = func() time.Time { return time.Now().UTC() }

// isoUTC is datetime.isoformat() of an aware UTC time.
func isoUTC(t time.Time) string {
	text := t.Format("2006-01-02T15:04:05")
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text + "+00:00"
}

// fromTimestampMS is datetime.fromtimestamp(ms / 1000, timezone.utc): the
// float seconds, microseconds rounded half to even.
func fromTimestampMS(ms int64) time.Time {
	seconds := float64(ms) / 1000
	whole, frac := math.Modf(seconds)
	micro := math.RoundToEven(frac * 1e6)
	if micro >= 1e6 {
		whole++
		micro -= 1e6
	} else if micro < 0 {
		whole--
		micro += 1e6
	}
	return time.Unix(int64(whole), int64(micro)*1000).UTC()
}

func firstRecorder(recorders []any) any {
	if len(recorders) == 0 {
		return nil
	}
	return recorders[0]
}

// mapStateErrors is the except clauses of _dashboard_overview.
func mapStateErrors(err error) error {
	var stateErr *StateError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPublicationPending):
		return NewError("SNAPSHOT_CHANGED", 409, "")
	case errors.As(err, &stateErr):
		return NewError("DASHBOARD_STATE_ERROR", 500, "")
	case errors.Is(err, ErrStateConflict):
		return NewError("DASHBOARD_CHANGED", 409, "")
	}
	return err
}

// dashboardOverview is _dashboard_overview.
func dashboardOverview(ctx context.Context, m Manager, agentID string, params query, manual bool) (*jsonx.Object, error) {
	minVersion := MinAgentVersion
	if manual {
		minVersion = MinDashboardAgentVersion
	}
	agent, err := agentFor(ctx, m, agentID, minVersion, false)
	if err != nil {
		return nil, err
	}
	if !versionAtLeast(agent, MinDashboardAgentVersion) {
		snap, err := overviewSnapshot(ctx, m, agent, false)
		if err != nil {
			return nil, err
		}
		summary := SummarizeObservations(snap.observations)
		upstreams, err := publicUpstreams(summary.GetList("upstreams"), snap.labels)
		if err != nil {
			return nil, err
		}
		summary.Set("upstreams", upstreams)
		result := jsonx.Obj("success", true, "agent", agentSummary(agent), "collected_at", isoUTC(nowUTC()),
			"core", snap.core, "recorders", snap.recorders)
		result.Update(summary)
		result.Set("statistics", nil)
		result.Set("rankings", rankingsUnavailable("upgrade_required"))
		return result, nil
	}
	result, err := dashboardWithState(ctx, m, agent, agentID, params, manual)
	return result, mapStateErrors(err)
}

func dashboardWithState(ctx context.Context, m Manager, agent *jsonx.Object, agentID string, params query, manual bool) (*jsonx.Object, error) {
	store := NewStateStore(m.DataDir())
	previous, err := store.Read(agentID)
	if err != nil {
		return nil, err
	}
	snap, err := overviewSnapshot(ctx, m, agent, true)
	if err != nil {
		return nil, err
	}
	digest, err := target(agent)
	if err != nil {
		return nil, err
	}
	if err := verifyTarget(ctx, m, agentID, digest); err != nil {
		return nil, err
	}
	current, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	snapshot := snap.data
	state, err := Advance(previous, snapshot, snap.core, snap.observations, digest, current, manual)
	if err != nil {
		return nil, err
	}
	if collectedLess(snapshot.Value("collected_at_ms"), state.Value("started_at_ms")) {
		return nil, NewError("SNAPSHOT_CHANGED", 409, "")
	}
	adjusted, partial, err := Totals(state, snap.observations)
	if err != nil {
		return nil, err
	}
	summary := SummarizeObservations(adjusted)
	upstreams, err := publicUpstreams(summary.GetList("upstreams"), snap.labels)
	if err != nil {
		return nil, err
	}
	summary.Set("upstreams", upstreams)
	rankings, err := overviewRankings(ctx, m, agent, params, snapshot, state, snap.recorders, manual)
	if err != nil {
		return nil, err
	}
	expected := ""
	if previous != nil {
		expected, _ = previous.Value("generation").(string)
	}
	var replacement *jsonx.Object
	if previous == nil || !jsonx.Equal(state, previous) {
		replacement = state
	}
	if _, err := store.CompareAndSet(agentID, expected, replacement, func() error {
		return verifyTarget(ctx, m, agentID, digest)
	}); err != nil {
		return nil, err
	}
	statistics := jsonx.Obj("generation", state.Value("generation"), "started_at_ms", state.Value("started_at_ms"),
		"reason", state.Value("reason"), "partial", partial)
	if count := state.Value("publication_count"); jsonx.Truthy(count) {
		statistics.Set("publication_count", count)
	}
	result := jsonx.Obj("success", true, "agent", agentSummary(agent),
		"collected_at", isoUTC(fromTimestampMS(pyInt64(snapshot.Value("collected_at_ms")))),
		"core", snap.core, "recorders", snap.recorders)
	result.Update(summary)
	result.Set("statistics", statistics)
	result.Set("rankings", rankings)
	return result, nil
}

// overviewRankings is the inner try of _dashboard_overview: history, then
// the kept live sample; errors other than INVALID_QUERY (or any error of a
// manual reset) become an unavailable section.
func overviewRankings(ctx context.Context, m Manager, agent *jsonx.Object, params query, snapshot, state *jsonx.Object,
	recorders []any, manual bool) (*jsonx.Object, error) {
	rankings, err := func() (*jsonx.Object, error) {
		// A pushed configuration may remove the UI's selected recorder.
		var effective query
		if recorder, ok := params.get("recorder"); ok {
			effective = query{{"recorder", recorder}}
		}
		if recorder, ok := params.get("recorder"); !manual && (!ok || !containsAny(recorders, recorder)) {
			effective = nil
		}
		var selected any
		if recorder, ok := effective.get("recorder"); ok && recorder != "" {
			selected = recorder
		} else {
			selected = firstRecorder(recorders)
		}
		rangeKey, ok := params.get("range")
		if !ok {
			rangeKey = "round"
		}
		rankings, err := historyRankings(m, agent, selected, rangeKey, snapshot, state, recorders)
		if err != nil || rankings != nil {
			return rankings, err
		}
		rankings, err = keptRankings(ctx, m, agent, effective, snapshot, state, recorders, manual)
		if err != nil {
			return nil, err
		}
		if rankings.Value("success") == true {
			status, err := historyStatus(m, agent, recorders, selected)
			if err != nil {
				return nil, err
			}
			merged := rankings.Clone()
			merged.Update(status)
			rankings = merged
		}
		return rankings, nil
	}()
	if err != nil {
		var monitorErr *Error
		if !errors.As(err, &monitorErr) {
			return nil, err
		}
		if manual || monitorErr.Code == "INVALID_QUERY" {
			return nil, err
		}
		return rankingsUnavailable(strings.ToLower(monitorErr.Code)), nil
	}
	return rankings, nil
}

// Overview is overview(manager, agent_id, args).
func Overview(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	params, err := dashboardParams(args)
	if err != nil {
		return nil, err
	}
	return dashboardOverview(ctx, m, agentID, params, false)
}

// ResetDashboard is reset_dashboard(manager, agent_id, body).
func ResetDashboard(ctx context.Context, m Manager, agentID string, body any) (*jsonx.Object, error) {
	object, ok := body.(*jsonx.Object)
	if !ok || object == nil {
		return nil, invalidQuery()
	}
	for _, key := range object.Keys() {
		if key != "recorder" {
			return nil, invalidQuery()
		}
	}
	var params query
	if value, present := object.Get("recorder"); present {
		if !ValidRecorderTag(value) {
			return nil, invalidQuery()
		}
		params = query{{"recorder", value.(string)}}
	}
	return dashboardOverview(ctx, m, agentID, params, true)
}

// QueryParams is query_params(args, detail).
func QueryParams(args [][2]string, detail bool) (query, error) {
	allowed := map[string]bool{"recorder": true}
	if !detail {
		for _, key := range []string{"limit", "cursor", "qname", "qtype", "client_ip", "rcode", "status", "since_ms",
			"until_ms", "matcher_tag"} {
			allowed[key] = true
		}
	}
	var result query
	for _, key := range uniqueKeys(args) {
		values := getList(args, key)
		if !allowed[key] || len(values) != 1 {
			return nil, invalidQuery()
		}
		value := values[0]
		if len(value) > 1024 || strings.ContainsFunc(value, func(r rune) bool { return r < 32 }) {
			return nil, invalidQuery()
		}
		value = pyStrip(value)
		if value == "" {
			continue
		}
		switch key {
		case "limit", "since_ms", "until_ms":
			if !digits16RE.MatchString(value) {
				return nil, invalidQuery()
			}
			n, _ := strconv.ParseInt(value, 10, 64)
			if n > MaxInteger || key == "limit" && (n < 1 || n > 100) {
				return nil, invalidQuery()
			}
			value = strconv.FormatInt(n, 10)
		case "cursor":
			if !cursorRE.MatchString(value) {
				return nil, invalidQuery()
			}
			for _, part := range strings.Split(value, ":") {
				if n, _ := strconv.ParseInt(part, 10, 64); n > MaxInteger {
					return nil, invalidQuery()
				}
			}
		case "status":
			if !contains([]string{"all", "error", "has_response", "no_response"}, value) {
				return nil, invalidQuery()
			}
		case "recorder", "matcher_tag":
			if runeLen(value) > 256 || strings.ContainsAny(value, "/?#\\") {
				return nil, invalidQuery()
			}
		case "client_ip":
			if strings.Contains(value, "%") {
				return nil, invalidQuery()
			}
			canonical, _, ok := pyIPAddress(value)
			if !ok {
				return nil, invalidQuery()
			}
			value = canonical
		case "qtype":
			if !qtypeRE.MatchString(value) {
				return nil, invalidQuery()
			}
			value = strings.ToUpper(value)
		case "rcode":
			if !rcodeRE.MatchString(value) {
				return nil, invalidQuery()
			}
			if mapped, ok := RcodeFilterText[strings.ToUpper(value)]; ok {
				value = mapped
			}
		}
		result = result.set(key, value)
	}
	since, okSince := result.get("since_ms")
	until, okUntil := result.get("until_ms")
	if okSince && okUntil {
		s, _ := strconv.ParseInt(since, 10, 64)
		u, _ := strconv.ParseInt(until, 10, 64)
		if s > u {
			return nil, invalidQuery()
		}
	}
	if !detail {
		if _, ok := result.get("limit"); !ok {
			result = result.set("limit", "50")
		}
	}
	return result, nil
}

var resolutionSources = map[string]bool{"local": true, "remote": true, "fallback": true, "pinned": true, "cache": true,
	"hosts": true, "blocked": true, "rejected": true, "unknown": true, "unrecorded": true, "error": true,
	"no_response": true, "ipv4_preferred": true}

var recordFields = []string{"id", "created_at_ms", "elapsed_ms", "client_ip", "questions_json", "error", "has_response",
	"rcode", "answer_count", "authority_count", "additional_count", "answers_json",
	"authorities_json", "additionals_json", "signature_json", "request_id", "req_rd", "req_cd",
	"req_ad", "req_opcode", "resp_aa", "resp_tc", "resp_ra", "resp_ad", "resp_cd"}

// record is _record.
func record(value any, detail bool) (*jsonx.Object, error) {
	invalid := NewError("INVALID_RESPONSE", 0, "")
	row, ok := value.(*jsonx.Object)
	if !ok || row == nil || !pyIntIn(row.Value("id"), 1, MaxInteger) {
		return nil, invalid
	}
	out := jsonx.NewObject()
	for _, key := range recordFields {
		if v, present := row.Get(key); present {
			out.Set(key, v)
		}
	}
	for _, key := range []string{"created_at_ms", "elapsed_ms"} {
		if pyNumber(out.Value(key)) == nil {
			return nil, invalid
		}
	}
	if _, ok := out.Value("questions_json").([]any); !ok {
		return nil, invalid
	}
	if _, ok := out.Value("answers_json").([]any); !ok {
		return nil, invalid
	}
	if detail {
		steps, ok := row.Value("steps").([]any)
		if !ok {
			return nil, invalid
		}
		out.Set("steps", steps)
	}
	if source, ok := row.Value("resolution_source").(string); ok && resolutionSources[source] {
		out.Set("resolution_source", source)
	}
	return out, nil
}

// Queries is queries(manager, agent_id, args).
func Queries(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	params, err := QueryParams(args, false)
	if err != nil {
		return nil, err
	}
	minVersion := MinAgentVersion
	if ip, _ := params.get("client_ip"); ip != "" {
		minVersion = MinClientsAgentVersion
	}
	agent, err := agentFor(ctx, m, agentID, minVersion, false)
	if err != nil {
		return nil, err
	}
	data, err := doRequest(ctx, m, agent, "queries", params, minVersion)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	records, ok := data.Value("records").([]any)
	cursor := data.Value("next_cursor")
	if data.Value("ok") != true || !ok || len(records) > 100 {
		return nil, invalid
	}
	if cursor != nil && !strMatches(cursor, cursorRE) {
		return nil, invalid
	}
	out := make([]any, 0, len(records))
	for _, row := range records {
		r, err := record(row, false)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	recorder, _ := params.get("recorder")
	return jsonx.Obj("success", true, "recorder", recorder, "records", out, "next_cursor", cursor), nil
}

// Query is query(manager, agent_id, record_id, args).
func Query(ctx context.Context, m Manager, agentID, recordID string, args [][2]string) (*jsonx.Object, error) {
	params, err := QueryParams(args, true)
	if err != nil {
		return nil, err
	}
	if !recordIDRE.MatchString(recordID) {
		return nil, invalidQuery()
	}
	if n, _ := strconv.ParseInt(recordID, 10, 64); n > MaxInteger {
		return nil, invalidQuery()
	}
	agent, err := agentFor(ctx, m, agentID, MinAgentVersion, false)
	if err != nil {
		return nil, err
	}
	data, err := doRequest(ctx, m, agent, "queries/"+recordID, params, MinAgentVersion)
	if err != nil {
		return nil, err
	}
	if data.Value("ok") != true {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	r, err := record(data.Value("record"), true)
	if err != nil {
		return nil, err
	}
	recorder, _ := params.get("recorder")
	return jsonx.Obj("success", true, "recorder", recorder, "record", r), nil
}

// ClientParams is client_params(args): client choices are independent of
// the current record filters.
func ClientParams(args [][2]string) (query, error) {
	var result query
	for _, key := range uniqueKeys(args) {
		values := getList(args, key)
		if (key != "recorder" && key != "since_ms" && key != "until_ms" && key != "search") || len(values) != 1 {
			return nil, invalidQuery()
		}
		value := values[0]
		if hasControl(value) {
			return nil, invalidQuery()
		}
		switch key {
		case "search":
			if len(value) > 64 {
				return nil, invalidQuery()
			}
		case "recorder":
			if !ValidRecorderTag(value) {
				return nil, invalidQuery()
			}
		default:
			if !digits16RE.MatchString(value) {
				return nil, invalidQuery()
			}
			if n, _ := strconv.ParseInt(value, 10, 64); n > MaxInteger {
				return nil, invalidQuery()
			}
		}
		value = pyStrip(value)
		if value != "" {
			result = result.set(key, value)
		}
	}
	since, okSince := result.get("since_ms")
	until, okUntil := result.get("until_ms")
	if okSince && okUntil {
		s, _ := strconv.ParseInt(since, 10, 64)
		u, _ := strconv.ParseInt(until, 10, 64)
		if s > u {
			return nil, invalidQuery()
		}
	}
	return result, nil
}

// ValidRecorderTag is _valid_recorder_tag.
func ValidRecorderTag(value any) bool {
	s, ok := value.(string)
	return ok && runeLen(s) > 0 && runeLen(s) <= 64 && recorderRE.MatchString(s)
}

// Clients is clients(manager, agent_id, args).
func Clients(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	params, err := ClientParams(args)
	if err != nil {
		return nil, err
	}
	agent, err := agentFor(ctx, m, agentID, MinClientsAgentVersion, false)
	if err != nil {
		return nil, err
	}
	data, err := doRequest(ctx, m, agent, "clients", params, MinClientsAgentVersion)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	recorder, rowsValue, sampleValue := data.Value("recorder"), data.Value("rows"), data.Value("sample_size")
	rows, okRows := rowsValue.([]any)
	recorderParam, hasParam := params.get("recorder")
	if data.Value("ok") != true || !ValidRecorderTag(recorder) || hasParam && !pyEqualStr(recorder, recorderParam) ||
		!pyIntIn(sampleValue, 0, ClientSampleLimit) || !okRows || len(rows) > ClientSampleLimit {
		return nil, invalid
	}
	sample := pyInt64(sampleValue)
	result := []any{}
	seen := map[string]bool{}
	total := int64(0)
	for _, item := range rows {
		row, ok := item.(*jsonx.Object)
		if !ok || row == nil {
			return nil, invalid
		}
		ip, ok := row.Value("key").(string)
		if !ok || !isPlainIP(ip) {
			return nil, invalid
		}
		count := row.Value("count")
		if seen[ip] || sample < 1 || !pyIntIn(count, 1, sample) {
			return nil, invalid
		}
		seen[ip] = true
		total += pyInt64(count)
		result = append(result, jsonx.Obj("ip", ip, "count", count))
	}
	if total != sample {
		return nil, invalid
	}
	return jsonx.Obj("success", true, "recorder", recorder, "clients", result,
		"sample_size", sample, "sample_limited", sample == ClientSampleLimit), nil
}
