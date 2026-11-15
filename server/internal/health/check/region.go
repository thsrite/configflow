package check

import (
	"sort"
	"strings"
	"sync"
	"time"

	"configflow/internal/jsonstore"
	"configflow/internal/jsonx"
	"configflow/internal/rules"
	"configflow/internal/urlx"
)

// Region check (backend/utils/region_check.py): the Agent only fetches pages
// through a given target and matches markers; every verdict is made here.
// Results that are not dicts, ids that are unhashable and other malformed
// Agent answers return the *PyError (or *KeyError) Python raises.

const (
	RegionRetentionDays = 30
	MaxGroupTargets     = 4
	MaxRegionTargets    = 8
	MaxDomainsPerCheck  = 10
	CheckTimeoutMS      = 8000
)

// ExitRequest is a fresh copy of EXIT_REQUEST.
func ExitRequest() *jsonx.Object {
	return jsonx.Obj("id", "exit", "url", "https://www.cloudflare.com/cdn-cgi/trace",
		"markers", jsonx.Obj("loc", `loc=([A-Z]{2})`, "ip", `ip=([0-9a-fA-F:.]+)`))
}

// GroupTypes is GROUP_TYPES; NonNodeTypes is NON_NODE_TYPES;
// ExcludedTargets is EXCLUDED_TARGETS.
var (
	GroupTypes      = []string{"Selector", "URLTest", "Fallback", "LoadBalance", "Relay", "Smart"}
	NonNodeTypes    = append(append([]string{}, GroupTypes...), "Direct", "Reject", "RejectDrop", "Pass", "PassRule", "Compatible", "Dns")
	ExcludedTargets = []string{"GLOBAL", "ConfigFlow-Region-Probe"}
)

// RegionRule is one entry of REGION_RULES.
type RegionRule struct{ Code, Name, Pattern string }

// RegionRules is REGION_RULES (same as the frontend's lib/regions.ts).
var RegionRules = []RegionRule{
	{"HK", "香港", `港|(?<![A-Za-z])HK(?![A-Za-z])|Hong\s?Kong`},
	{"TW", "台湾", `台湾|台北|(?<![A-Za-z])TW(?![A-Za-z])|Taiwan`},
	{"JP", "日本", `日本|东京|大阪|(?<![A-Za-z])JP(?![A-Za-z])|Japan|Tokyo`},
	{"SG", "新加坡", `新加坡|狮城|(?<![A-Za-z])SG(?![A-Za-z])|Singapore`},
	{"US", "美国", `美国|洛杉矶|硅谷|(?<![A-Za-z])US(?![A-Za-z])|United\s?States|America`},
	{"KR", "韩国", `韩国|首尔|(?<![A-Za-z])KR(?![A-Za-z])|Korea`},
	{"GB", "英国", `英国|伦敦|(?<![A-Za-z])(UK|GB)(?![A-Za-z])|Britain|London`},
	{"DE", "德国", `德国|法兰克福|(?<![A-Za-z])DE(?![A-Za-z])|Germany`},
	{"CA", "加拿大", `加拿大|(?<![A-Za-z])CA(?![A-Za-z])|Canada`},
	{"AU", "澳大利亚", `澳大利亚|澳洲|悉尼|(?<![A-Za-z])AU(?![A-Za-z])|Australia`},
	{"FR", "法国", `法国|巴黎|(?<![A-Za-z])FR(?![A-Za-z])|France`},
	{"NL", "荷兰", `荷兰|阿姆斯特丹|(?<![A-Za-z])NL(?![A-Za-z])|Netherlands`},
	{"IN", "印度", `印度|(?<![A-Za-z])IN(?![A-Za-z])|India`},
	{"MY", "马来西亚", `马来|(?<![A-Za-z])MY(?![A-Za-z])|Malaysia`},
	{"TH", "泰国", `泰国|(?<![A-Za-z])TH(?![A-Za-z])|Thailand`},
	{"VN", "越南", `越南|(?<![A-Za-z])VN(?![A-Za-z])|Vietnam`},
	{"PH", "菲律宾", `菲律宾|(?<![A-Za-z])PH(?![A-Za-z])|Philippines`},
	{"TR", "土耳其", `土耳其|(?<![A-Za-z])TR(?![A-Za-z])|Turkey|Türkiye`},
	{"AR", "阿根廷", `阿根廷|(?<![A-Za-z])AR(?![A-Za-z])|Argentina`},
	{"BR", "巴西", `巴西|(?<![A-Za-z])BR(?![A-Za-z])|Brazil`},
}

func mustCompile(pattern string, ignoreCase bool) *rules.PyRegex {
	re, err := rules.CompilePyRegex(pattern, ignoreCase)
	if err != nil {
		panic(err)
	}
	return re
}

var (
	compileOnce    sync.Once
	regionPatterns []*rules.PyRegex
	netflixRegion  *rules.PyRegex
	regionURLHint  *rules.PyRegex
)

func compiled() {
	compileOnce.Do(func() {
		for _, rule := range RegionRules {
			regionPatterns = append(regionPatterns, mustCompile(rule.Pattern, true))
		}
		netflixRegion = mustCompile(`^/([a-z]{2})(?:-[a-z]{2})?/title/`, false)
		regionURLHint = mustCompile(`unavailable|region|country|geo-?block`, true)
	})
}

// regionOfName is region_of_name inside a catch: "" for None.
func regionOfName(name any) string {
	compiled()
	text := ""
	if jsonx.Truthy(name) {
		value, ok := name.(string)
		if !ok {
			raise("TypeError", "expected string or bytes-like object, got '"+TypeName(name)+"'")
		}
		text = value
	}
	for i, pattern := range regionPatterns {
		if pattern.Search(text) {
			return RegionRules[i].Code
		}
	}
	return ""
}

// RegionOfName is region_of_name: the region code of a node or group name,
// or "" (None).
func RegionOfName(name string) string { return regionOfName(name) }

// RegionOfNameValue is region_of_name for any value: the TypeError of a
// truthy non-str name.
func RegionOfNameValue(name any) (code string, err error) {
	defer catch(&err)
	return regionOfName(name), nil
}

// RegionLabel is region_label(code): the Chinese name of a code; an unknown
// code itself, '未知' for a falsy one.
func RegionLabel(code any) any {
	for _, rule := range RegionRules {
		if jsonx.Equal(code, rule.Code) {
			return rule.Name
		}
	}
	if jsonx.Truthy(code) {
		return code
	}
	return "未知"
}

// ---------- results ----------

func regionValue(code any) any {
	if text, ok := code.(string); ok && text == "" {
		return nil
	}
	return code
}

func result(status string, region any, reason string, evidence []any) *jsonx.Object {
	if evidence == nil {
		evidence = []any{}
	}
	return jsonx.Obj("status", status, "region", region, "reason", reason, "evidence", evidence)
}

func evidence(result any) string {
	if !jsonx.Truthy(result) {
		return "没有结果"
	}
	if jsonx.Truthy(get(result, "error", nil)) {
		return jsonx.Str(item(result, "id")) + "：" + jsonx.Str(item(result, "error"))
	}
	text := jsonx.Str(item(result, "id")) + "：HTTP " + jsonx.Str(get(result, "status", nil))
	if jsonx.Truthy(get(result, "final_url", nil)) && jsonx.Truthy(get(result, "redirects", nil)) {
		text += " → " + jsonx.Str(item(result, "final_url"))
	}
	if jsonx.Truthy(get(result, "markers", nil)) {
		text += "，命中 " + sortedJoin("、", item(result, "markers"))
	}
	return text
}

func marker(result any, name string) any {
	if !jsonx.Truthy(result) {
		result = jsonx.NewObject()
	}
	return get(get(result, "markers", jsonx.NewObject()), name, nil)
}

// status is _status: an erroneous request has no trustworthy status code.
func status(result any) any {
	if !jsonx.Truthy(result) || jsonx.Truthy(get(result, "error", nil)) {
		return nil
	}
	return get(result, "status", nil)
}

func is(value any, code int) bool { return jsonx.Equal(value, jsonx.Int(code)) }

// lookup is results.get(id) on the by-id dict.
type byID map[string]any

func (b byID) get(id string) any { return b["s"+id] }

// ---------- services ----------

func evaluateOpenAI(results byID) *jsonx.Object {
	compliance, ios, trace := results.get("openai-compliance"), results.get("openai-ios"), results.get("openai-trace")
	ev := []any{evidence(compliance), evidence(ios)}
	region := marker(trace, "loc")
	if jsonx.Truthy(marker(compliance, "unsupported")) || jsonx.Truthy(marker(ios, "unsupported")) {
		return result("blocked", region, "所在地区不受支持", ev)
	}
	if jsonx.Truthy(marker(ios, "vpn")) {
		return result("blocked", region, "被识别为代理 / VPN", ev)
	}
	if is(status(compliance), 200) && marker(compliance, "consent") != nil {
		reason := ""
		if jsonx.Truthy(marker(ios, "dc")) {
			reason = "App 端可能受限（被识别为数据中心 IP）"
		}
		return result("available", region, reason, ev)
	}
	return result("unknown", region, "响应不符合已知模式", ev)
}

func evaluateClaude(results byID) *jsonx.Object {
	web, trace := results.get("claude-web"), results.get("claude-trace")
	ev := []any{evidence(web)}
	region := marker(trace, "loc")
	if !jsonx.Truthy(web) || jsonx.Truthy(get(web, "error", nil)) {
		return result("unknown", region, "请求失败", ev)
	}
	finalURL := get(web, "final_url", nil)
	if !jsonx.Truthy(finalURL) {
		finalURL = ""
	}
	urls := append([]any{finalURL}, iterate(get(web, "redirects", []any{}))...)
	for _, url := range urls {
		if contains(url, "app-unavailable-in-region") {
			return result("blocked", region, "跳转到「所在地区不可用」页面", ev)
		}
	}
	if is(status(web), 200) {
		return result("available", region, "", ev)
	}
	if is(status(web), 403) {
		return result("unknown", region, "Cloudflare 质询，无法判断", ev)
	}
	return result("unknown", region, "响应不符合已知模式", ev)
}

func splitPath(url any) string {
	text, ok := url.(string)
	if !ok {
		raise("AttributeError", "'"+TypeName(url)+"' object has no attribute 'decode'")
	}
	parts, err := urlx.Split(text)
	if err != nil {
		raise("ValueError", err.Error())
	}
	return parts.Path
}

func netflixRegionOf(result any) any {
	compiled()
	if !jsonx.Truthy(result) || !jsonx.Truthy(get(result, "final_url", nil)) {
		return nil
	}
	path := splitPath(item(result, "final_url"))
	if match, err := netflixRegion.Match(path); err == nil && match != nil {
		code, _ := match.Group(1)
		return strings.ToUpper(code)
	}
	if strings.HasPrefix(path, "/title/") && is(status(result), 200) {
		return "US"
	}
	return nil
}

func evaluateNetflix(results byID) *jsonx.Object {
	licensed := []any{results.get("netflix-licensed-1"), results.get("netflix-licensed-2")}
	original := results.get("netflix-original")
	all := append(append([]any{}, licensed...), original)
	ev := make([]any, len(all))
	for i, entry := range all {
		ev[i] = evidence(entry)
	}
	var region any
	for _, entry := range all {
		if jsonx.Truthy(netflixRegionOf(entry)) {
			region = netflixRegionOf(entry)
			break
		}
	}
	var statuses []any
	for _, entry := range all {
		if status(entry) != nil {
			statuses = append(statuses, status(entry))
		}
	}
	for _, entry := range licensed {
		if is(status(entry), 200) {
			return result("available", region, "完整解锁", ev)
		}
	}
	if is(status(original), 200) {
		return result("partial", region, "仅自制剧", ev)
	}
	every := func(code int) bool {
		for _, value := range statuses {
			if !is(value, code) {
				return false
			}
		}
		return true
	}
	if len(statuses) > 0 && every(403) {
		return result("blocked", region, "Netflix 拒绝访问（403）", ev)
	}
	if len(statuses) > 0 && every(404) {
		return result("blocked", region, "所在地区没有可播放的内容", ev)
	}
	return result("unknown", region, "响应不符合已知模式", ev)
}

func evaluateYouTube(results byID) *jsonx.Object {
	page := results.get("youtube-premium")
	ev := []any{evidence(page)}
	region := marker(page, "gl")
	if jsonx.Truthy(marker(page, "blocked")) {
		return result("blocked", region, "Premium 在所在地区不可用", ev)
	}
	if jsonx.Equal(region, "CN") {
		return result("blocked", region, "识别为中国大陆", ev)
	}
	if jsonx.Truthy(region) {
		return result("available", region, "", ev)
	}
	return result("unknown", nil, "页面里没有地区信息", ev)
}

// Service is one entry of SERVICES.
type Service struct {
	ID, Name string
	Domains  []string
	requests func() []any
	evaluate func(byID) *jsonx.Object
}

// Requests is service['requests'] (fresh copies).
func (s Service) Requests() []any { return s.requests() }

// Services is SERVICES in order.
var Services = []Service{
	{
		ID: "openai", Name: "ChatGPT",
		Domains: []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "sora.com"},
		requests: func() []any {
			return []any{
				jsonx.Obj("id", "openai-compliance", "url", "https://api.openai.com/compliance/cookie_requirements",
					"headers", jsonx.Obj("Authorization", "Bearer null"),
					"markers", jsonx.Obj("unsupported", "unsupported_country", "consent", "cookie_consent_required")),
				jsonx.Obj("id", "openai-ios", "url", "https://ios.chat.openai.com/",
					"markers", jsonx.Obj("unsupported", "unsupported_country", "vpn", "VPN", "dc", `"type"\s*:\s*"dc"`)),
				jsonx.Obj("id", "openai-trace", "url", "https://chatgpt.com/cdn-cgi/trace", "markers", jsonx.Obj("loc", `loc=([A-Z]{2})`)),
			}
		},
		evaluate: evaluateOpenAI,
	},
	{
		ID: "claude", Name: "Claude",
		Domains: []string{"claude.ai", "claude.com", "anthropic.com"},
		requests: func() []any {
			return []any{
				jsonx.Obj("id", "claude-web", "url", "https://claude.ai/"),
				jsonx.Obj("id", "claude-trace", "url", "https://claude.ai/cdn-cgi/trace", "markers", jsonx.Obj("loc", `loc=([A-Z]{2})`)),
			}
		},
		evaluate: evaluateClaude,
	},
	{
		ID: "netflix", Name: "Netflix",
		Domains: []string{"netflix.com", "netflix.net", "nflxext.com", "nflximg.com", "nflximg.net", "nflxso.net",
			"nflxvideo.net"},
		requests: func() []any {
			return []any{
				jsonx.Obj("id", "netflix-licensed-1", "url", "https://www.netflix.com/title/81280792"),
				jsonx.Obj("id", "netflix-licensed-2", "url", "https://www.netflix.com/title/70143836"),
				jsonx.Obj("id", "netflix-original", "url", "https://www.netflix.com/title/80018499"),
			}
		},
		evaluate: evaluateNetflix,
	},
	{
		ID: "youtube", Name: "YouTube Premium",
		Domains: []string{"youtube.com", "googlevideo.com", "ytimg.com", "youtu.be", "youtube-nocookie.com"},
		requests: func() []any {
			return []any{
				jsonx.Obj("id", "youtube-premium", "url", "https://www.youtube.com/premium",
					"headers", jsonx.Obj("Accept-Language", "en"),
					"markers", jsonx.Obj("blocked", "Premium is not available in your country",
						"gl", `"INNERTUBE_CONTEXT_GL"\s*:\s*"([A-Z]{2})"`)),
			}
		},
		evaluate: evaluateYouTube,
	},
}

// ServiceByID is SERVICES_BY_ID[id].
func ServiceByID(id string) (Service, bool) {
	for _, service := range Services {
		if service.ID == id {
			return service, true
		}
	}
	return Service{}, false
}

// ServiceRequests is service_requests(): the exit request, then every
// service request.
func ServiceRequests() []any {
	out := []any{ExitRequest()}
	for _, service := range Services {
		out = append(out, service.Requests()...)
	}
	return out
}

func indexByID(results []any) byID {
	out := byID{}
	for _, entry := range results {
		out[hashKey(item(entry, "id"))] = entry
	}
	return out
}

// Evaluate is SERVICES_BY_ID[id]['evaluate'](by_id) for a by-id dict.
func (s Service) Evaluate(results *jsonx.Object) (out *jsonx.Object, err error) {
	defer catch(&err)
	index := byID{}
	for key, value := range results.All() {
		index["s"+key] = value
	}
	return s.evaluate(index), nil
}

// EvaluateServices is evaluate_services(results): service id → verdict.
func EvaluateServices(results []any) (out *jsonx.Object, err error) {
	defer catch(&err)
	index := indexByID(results)
	out = jsonx.NewObject()
	for _, service := range Services {
		out.Set(service.ID, service.evaluate(index))
	}
	return out, nil
}

// ExitInfo is exit_info(results).
func ExitInfo(results []any) (out *jsonx.Object, err error) {
	defer catch(&err)
	var exit any
	for _, entry := range results {
		if jsonx.Equal(item(entry, "id"), "exit") {
			exit = entry
			break
		}
	}
	var errorValue any
	if exit != nil {
		errorValue = get(exit, "error", nil)
	}
	if !jsonx.Truthy(errorValue) {
		errorValue = nil
	}
	return jsonx.Obj("loc", marker(exit, "loc"), "ip", marker(exit, "ip"), "error", errorValue), nil
}

// PublicServices is public_services().
func PublicServices() []any {
	out := make([]any, len(Services))
	for i, service := range Services {
		out[i] = jsonx.Obj("id", service.ID, "name", service.Name, "domains", jsonx.ListOf(service.Domains))
	}
	return out
}

// ---------- arbitrary domains ----------

// BlockPattern is BLOCK_PATTERN (matched by the Agent).
const BlockPattern = `(?i)(not|isn't|is not) available in your (country|region|location)` +
	`|unavailable in your (country|region|location)` +
	`|not available in (this|the) (country|region)` +
	`|地区不可用|所在的?地区|所在国家`

// DomainRequest is domain_request(request_id, url).
func DomainRequest(requestID, url string) *jsonx.Object {
	return jsonx.Obj("id", requestID, "url", url, "markers", jsonx.Obj("block", BlockPattern))
}

func lessThan500(value any) bool {
	switch v := value.(type) {
	case bool:
		return true
	case jsonx.Number:
		return less(numberOf(v), intNum(500))
	}
	raise("TypeError", "'<' not supported between instances of '"+TypeName(value)+"' and 'int'")
	return false
}

// ClassifyDomainResult is classify_domain_result(result, original_url):
// ok / restricted / denied / error for one target.
func ClassifyDomainResult(result any, originalURL string) (out *jsonx.Object, err error) {
	defer catch(&err)
	compiled()
	if !jsonx.Truthy(result) || jsonx.Truthy(get(result, "error", nil)) {
		var detail any = "没有结果"
		if jsonx.Truthy(result) {
			detail = get(result, "error", nil)
		}
		return jsonx.Obj("state", "error", "detail", detail, "status", nil, "final_url", nil), nil
	}
	statusValue := get(result, "status", nil)
	finalURL := get(result, "final_url", nil)
	if !jsonx.Truthy(finalURL) {
		finalURL = ""
	}
	detail := "HTTP " + jsonx.Str(statusValue)
	moved := false
	if jsonx.Truthy(finalURL) && regionURLHint.Search(splitPath(finalURL)) {
		moved = !regionURLHint.Search(splitPath(originalURL))
	}
	var state string
	switch {
	case is(statusValue, 451):
		state, detail = "restricted", "HTTP 451（因法律原因不可用）"
	case jsonx.Truthy(marker(result, "block")) && (is(statusValue, 200) || is(statusValue, 403)):
		state, detail = "restricted", "HTTP "+jsonx.Str(statusValue)+"，页面提示："+jsonx.Str(marker(result, "block"))
	case moved:
		state, detail = "restricted", "跳转到 "+jsonx.Str(finalURL)
	case is(statusValue, 403):
		state = "denied"
	case jsonx.Truthy(statusValue) && lessThan500(statusValue):
		state = "ok"
	default:
		state = "error"
	}
	return jsonx.Obj("state", state, "detail", detail, "status", statusValue, "final_url", finalURL), nil
}

// SummarizeDomain is summarize_domain(per_target).
func SummarizeDomain(perTarget *jsonx.Object) (out *jsonx.Object, err error) {
	defer catch(&err)
	restricted, usable := []any{}, []any{}
	total := 0
	for name, entry := range perTarget.All() {
		total++
		state := item(entry, "state")
		if jsonx.Equal(state, "restricted") {
			restricted = append(restricted, name)
		}
		if jsonx.Equal(state, "ok") {
			usable = append(usable, name)
		}
	}
	verdict := "unreachable"
	switch {
	case len(restricted) > 0 && len(usable) > 0:
		verdict = "suspected"
	case len(restricted) > 0 && len(restricted) == total:
		verdict = "all_restricted"
	case len(usable) > 0 && len(restricted) == 0:
		verdict = "no_difference"
	}
	return jsonx.Obj("verdict", verdict, "restricted", restricted, "usable", usable), nil
}

// ---------- target choice ----------

func inSet(value any, set []string) bool {
	hashKey(value)
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, element := range set {
		if element == text {
			return true
		}
	}
	return false
}

// PickTargets is pick_targets(targets, preferred_group): DIRECT, up to 4
// strategy groups (preferred first) and one live node per region (at most 8
// regions). preferredGroup nil is None.
func PickTargets(targets []any, preferredGroup any) (picked []any, err error) {
	defer catch(&err)
	compiled()
	picked = []any{}
	names := map[string]bool{}
	for _, target := range targets {
		names[hashKey(item(target, "name"))] = true
	}
	if names["sDIRECT"] {
		picked = append(picked, jsonx.Obj("name", "DIRECT", "kind", "direct", "region_hint", nil))
	}
	var groups []any
	for _, target := range targets {
		if inSet(get(target, "type", nil), GroupTypes) && !inSet(item(target, "name"), ExcludedTargets) {
			groups = append(groups, target)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return jsonx.Equal(item(groups[i], "name"), preferredGroup) && !jsonx.Equal(item(groups[j], "name"), preferredGroup)
	})
	if len(groups) > MaxGroupTargets {
		groups = groups[:MaxGroupTargets]
	}
	for _, target := range groups {
		name := item(target, "name")
		picked = append(picked, jsonx.Obj("name", name, "kind", "group", "region_hint", regionValue(regionOfName(name))))
	}
	byRegion := map[string]any{}
	for _, target := range targets {
		if inSet(get(target, "type", nil), NonNodeTypes) || !jsonx.Truthy(get(target, "alive", true)) {
			continue
		}
		if code := regionOfName(item(target, "name")); code != "" {
			if _, seen := byRegion[code]; !seen {
				byRegion[code] = target
			}
		}
	}
	count := 0
	for _, rule := range RegionRules {
		target, ok := byRegion[rule.Code]
		if !ok || count >= MaxRegionTargets {
			continue
		}
		count++
		picked = append(picked, jsonx.Obj("name", item(target, "name"), "kind", "node", "region_hint", rule.Code))
	}
	return picked, nil
}

// ---------- store ----------

var regionLock sync.Mutex

// RegionStore keeps, per Agent, the latest service matrix and the region
// differences of each domain (RegionStore).
type RegionStore struct{ files *jsonstore.Store }

// NewRegionStore is RegionStore(data_dir).
func NewRegionStore(dir string) *RegionStore { return &RegionStore{jsonstore.New(dir)} }

// Path is the file of an Agent.
func (s *RegionStore) Path(agentID string) string { return s.files.Path(agentID) }

func (s *RegionStore) read(agentID string) *jsonx.Object {
	data := s.files.Read(agentID)
	if data == nil {
		data = jsonx.NewObject()
	}
	data.SetDefault("services", nil)
	if _, ok := data.Value("domains").(*jsonx.Object); !ok {
		data.Set("domains", jsonx.NewObject())
	}
	if _, ok := data.Value("websites").(*jsonx.Object); !ok {
		data.Set("websites", jsonx.NewObject())
	}
	return data
}

// NaiveISOSeconds is a naive local datetime's isoformat(timespec='seconds').
func NaiveISOSeconds(t time.Time) string { return t.Format("2006-01-02T15:04:05") }

// oldest is (now - timedelta(days)).isoformat(timespec='seconds') on a naive
// local datetime (wall-clock arithmetic, no DST adjustment).
func oldest(now time.Time, days int) string {
	wall := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.UTC)
	return NaiveISOSeconds(wall.Add(-time.Duration(days) * 24 * time.Hour))
}

func checkedAtAtLeast(entry any, bound string) bool {
	value := get(entry, "checked_at", "")
	text, ok := value.(string)
	if !ok {
		raise("TypeError", "'>=' not supported between instances of '"+TypeName(value)+"' and 'str'")
	}
	return text >= bound
}

// Update is update(agent_id, mutate, now): mutate the stored data, drop
// domain and website entries older than 30 days, keep the newest 1000
// websites, write. A zero now is datetime.now().
func (s *RegionStore) Update(agentID string, mutate func(*jsonx.Object) error, now time.Time) (err error) {
	if now.IsZero() {
		now = time.Now()
	}
	regionLock.Lock()
	defer regionLock.Unlock()
	defer catch(&err)
	data := s.read(agentID)
	if err := mutate(data); err != nil {
		return err
	}
	bound := oldest(now, RegionRetentionDays)
	domains := jsonx.NewObject()
	for host, entry := range iterItems(data.Value("domains")) {
		if checkedAtAtLeast(entry, bound) {
			domains.Set(host, entry)
		}
	}
	data.Set("domains", domains)
	type pair struct {
		host  string
		entry any
	}
	var entries []pair
	for host, entry := range iterItems(data.Value("websites")) {
		if checkedAtAtLeast(entry, bound) {
			entries = append(entries, pair{host, entry})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return item(entries[i].entry, "checked_at").(string) < item(entries[j].entry, "checked_at").(string)
	})
	if len(entries) > 1000 {
		entries = entries[len(entries)-1000:]
	}
	websites := jsonx.NewObject()
	for _, p := range entries {
		websites.Set(p.host, p.entry)
	}
	data.Set("websites", websites)
	_, err = s.files.Write(agentID, data, false)
	return err
}

// iterItems is .items() of a value mutate left in place: a dict, or the
// AttributeError of anything else.
func iterItems(v any) func(func(string, any) bool) {
	object, ok := v.(*jsonx.Object)
	if !ok || object == nil {
		raise("AttributeError", "'"+TypeName(v)+"' object has no attribute 'items'")
	}
	return object.All()
}

// Load is load(agent_id).
func (s *RegionStore) Load(agentID string) *jsonx.Object {
	regionLock.Lock()
	defer regionLock.Unlock()
	return s.read(agentID)
}

// Delete is delete(agent_id).
func (s *RegionStore) Delete(agentID string) error {
	regionLock.Lock()
	defer regionLock.Unlock()
	return s.files.Remove(agentID)
}
