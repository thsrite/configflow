package monitor

// Port of backend/agents/oxidns_cache.py (A07): flush DNS caches of one
// OxiDNS Agent's core. The Agent discovers `type: cache` plugins in its own
// deployed configuration and accepts only those tags. This bridge targets
// exactly the registered Agent named in the request; core addresses and
// credentials never leave the Agent.

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// Limits of oxidns_cache.py.
const (
	CacheMinAgentVersion   = "1.8.36-go"
	CacheMaxResponseBytes  = 16 * 1024
	CacheMaxTags           = 64
	cacheTagMaxLength      = 64
	cacheSegment           = `[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?`
	cacheRequestTimeoutSec = 40
)

// cacheTagRE is _TAG without its length lookahead (checked separately):
// the Agent's oxMonitorSafeTag.
var cacheTagRE = regexp.MustCompile(`^` + cacheSegment + `(?:\.` + cacheSegment + `)*$`)

// CacheMessages is MESSAGES of oxidns_cache.py.
var CacheMessages = map[string]string{
	"UPGRADE_REQUIRED":        "请将此 Agent 升级到 " + CacheMinAgentVersion + " 或更新版本后清理缓存。",
	"AGENT_NOT_FOUND":         "未找到这台 Agent。",
	"WRONG_SERVICE":           "清理缓存仅适用于 OxiDNS Agent。",
	"AGENT_UNREACHABLE":       "暂时无法连接 Agent，请检查网络后重试。",
	"AGENT_AUTH_FAILED":       "Agent 认证失败，请检查注册状态。",
	"INVALID_REQUEST":         "清理缓存请求无效。",
	"INVALID_RESPONSE":        "缓存操作结果格式异常，请检查 Agent 和 OxiDNS 版本。",
	"API_NOT_CONFIGURED":      "OxiDNS 管理 API 未启用，请在设置中启用并发布一次。",
	"TLS_UNSUPPORTED":         "清理缓存暂不支持当前 OxiDNS API 的 TLS 配置。",
	"CONFIG_UNSUPPORTED":      "当前 OxiDNS 配置无法用于清理缓存，请检查管理 API 配置。",
	"CACHE_UNKNOWN":           "设备当前配置中没有这个缓存实例，请刷新面板后重试。",
	"CACHE_FLUSH_UNSUPPORTED": "当前 OxiDNS 核心不支持清理缓存接口，请升级核心后重试。",
	"CORE_UNREACHABLE":        "暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。",
	"CORE_AUTH_FAILED":        "OxiDNS 管理 API 认证失败，请检查核心配置。",
	"CORE_BUSY":               "OxiDNS 缓存正忙，请稍后重试。",
	"SERVICE_BUSY":            "此设备正在发布、重启或更新，请等待当前操作结束。",
	"CACHE_FLUSH_FAILED":      "清理缓存失败，请检查 OxiDNS 日志后重试。",
}

// CacheFail is oxidns_cache._fail(code, status).
func CacheFail(code string, status int) *Error {
	if status == 0 {
		status = 502
	}
	message, ok := CacheMessages[code]
	if !ok {
		message = CacheMessages["CACHE_FLUSH_FAILED"]
	}
	return &Error{Code: code, Status: status, Message: message}
}

// FlushError is a MonitorError of a failed flush with its `flushed`
// attribute: the caches a partial flush already cleared.
type FlushError struct {
	Err     *Error
	Flushed []any
}

func (e *FlushError) Error() string { return e.Err.Message }
func (e *FlushError) Unwrap() error { return e.Err }

// cacheAgent is oxidns_cache._agent (with the publication history, as
// get_agent_by_id's default).
func cacheAgent(ctx context.Context, m Manager, agentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 {
		return nil, CacheFail("AGENT_NOT_FOUND", 404)
	}
	if !pyEqualStr(agent.Value("service_type"), "oxidns") {
		return nil, CacheFail("WRONG_SERVICE", 400)
	}
	if !versionAtLeast(agent, CacheMinAgentVersion) {
		return nil, CacheFail("UPGRADE_REQUIRED", 409)
	}
	return agent, nil
}

func validCacheTag(value any) bool {
	tag, ok := value.(string)
	return ok && tag != "" && len(tag) <= cacheTagMaxLength && cacheTagRE.MatchString(tag)
}

// cacheTags is _tags(value): a validated tag list of an Agent answer.
func cacheTags(value any) ([]any, error) {
	list, ok := value.([]any)
	if !ok || len(list) > CacheMaxTags {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	seen := map[string]bool{}
	for _, tag := range list {
		if !validCacheTag(tag) {
			return nil, CacheFail("INVALID_RESPONSE", 0)
		}
		seen[tag.(string)] = true
	}
	if len(seen) != len(list) {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	return list, nil
}

// ValidRequestTags is valid_request_tags(value). The error is the
// TypeError of set() on a list holding a list or dict (Python answers 500).
func ValidRequestTags(value any) (bool, error) {
	list, ok := value.([]any)
	if !ok || len(list) == 0 || len(list) > CacheMaxTags {
		return false, nil
	}
	for _, tag := range list {
		if unhashable(tag) {
			return false, typeErrorUnhashable(tag)
		}
	}
	distinct := 0
	for i, tag := range list {
		duplicate := false
		for _, earlier := range list[:i] {
			if jsonx.Equal(earlier, tag) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			distinct++
		}
	}
	if distinct != len(list) {
		return false, nil
	}
	for _, tag := range list {
		if !validCacheTag(tag) {
			return false, nil
		}
	}
	return true, nil
}

// CacheCall is oxidns_cache.call(manager, agent_id, method, tags): GET
// lists the Agent's cache plugins; POST flushes the given tags (tags nil
// is None).
func CacheCall(ctx context.Context, m Manager, agentID, method string, tags any) (*jsonx.Object, error) {
	valid := (method == "GET" && tags == nil) || (method == "POST" && tags != nil)
	if valid && tags != nil {
		ok, err := ValidRequestTags(tags)
		if err != nil {
			return nil, err
		}
		valid = ok
	}
	if !valid {
		return nil, CacheFail("INVALID_REQUEST", 400)
	}
	agent, err := cacheAgent(ctx, m, agentID)
	if err != nil {
		return nil, err
	}
	path := "/api/oxidns/cache"
	if method == "POST" {
		path = "/api/oxidns/cache/flush"
	}
	// The headers are built before the try block: a missing or non-str
	// token is a KeyError / TypeError (500), not AGENT_UNREACHABLE.
	token, err := tokenText(agent)
	if err != nil {
		return nil, err
	}
	encodedToken, ok := latin1(token)
	if !ok {
		return nil, CacheFail("AGENT_UNREACHABLE", 0)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+encodedToken)
	header.Set("Accept", "application/json")
	var body []byte
	if method == "POST" {
		header.Set("Content-Type", "application/json")
		if body, err = jsonx.Default(jsonx.Obj("tags", tags)); err != nil {
			return nil, err
		}
	}
	origin, err := AgentOrigin(agent)
	if err != nil {
		return nil, err
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: origin + path, Header: header, Body: body,
		Timeout: agents.Pair(3, cacheRequestTimeoutSec), NoProxy: true, NoRedirects: true, MaxBody: CacheMaxResponseBytes})
	if err != nil {
		return nil, CacheFail("AGENT_UNREACHABLE", 0)
	}
	status := response.StatusCode
	data, err := agentAnswer(response, CacheMaxResponseBytes, CacheFail)
	if err != nil {
		return nil, err
	}
	answer, ok := data.(*jsonx.Object)
	if !ok || answer == nil {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	if status >= 400 || answer.Value("success") == false {
		code := answer.Value("code")
		text, isStr := code.(string)
		_, known := CacheMessages[text]
		known = known && isStr
		if status == 404 {
			if unhashable(code) {
				return nil, typeErrorUnhashable(code)
			}
			if !known {
				return nil, CacheFail("UPGRADE_REQUIRED", 409)
			}
		}
		if !known {
			text = "CACHE_FLUSH_FAILED"
		}
		errStatus := 502
		switch status {
		case 400, 409, 503, 504:
			errStatus = status
		}
		// A partial flush already changed the core; report what completed.
		flushedValue, present := answer.Get("flushed")
		if !present {
			flushedValue = []any{}
		}
		flushed, ferr := cacheTags(flushedValue)
		if ferr != nil {
			flushed = []any{}
		}
		return nil, &FlushError{Err: CacheFail(text, errStatus), Flushed: flushed}
	}
	if status != 200 || answer.Value("success") != true {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	if method == "GET" {
		caches, err := cacheTags(answer.Value("caches"))
		if err != nil {
			return nil, err
		}
		return jsonx.Obj("success", true, "caches", caches), nil
	}
	flushed, err := cacheTags(answer.Value("flushed"))
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(flushed, tags) {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	return jsonx.Obj("success", true, "flushed", flushed), nil
}

// agentAnswer is the shared read of oxidns_cache / oxidns_rule_update:
// 401/403 → AGENT_AUTH_FAILED, 3xx → INVALID_RESPONSE, more than limit
// bytes → INVALID_RESPONSE, a read failure → AGENT_UNREACHABLE, then
// json.loads of the body (UPGRADE_REQUIRED 409 for a 404 that is not JSON,
// INVALID_RESPONSE otherwise).
func agentAnswer(response *agents.Response, limit int, fail func(string, int) *Error) (any, error) {
	status := response.StatusCode
	if status == 401 || status == 403 {
		return nil, fail("AGENT_AUTH_FAILED", 0)
	}
	if status >= 300 && status < 400 {
		return nil, fail("INVALID_RESPONSE", 0)
	}
	if len(response.Body) > limit {
		return nil, fail("INVALID_RESPONSE", 0)
	}
	if response.BodyError != nil {
		return nil, fail("AGENT_UNREACHABLE", 0)
	}
	value, ok := loadsBytes(response.Body)
	if !ok {
		if status == 404 {
			return nil, fail("UPGRADE_REQUIRED", 409)
		}
		return nil, fail("INVALID_RESPONSE", 0)
	}
	return value, nil
}

// tokenText is 'Bearer ' + agent['token'] outside a try block: KeyError
// when absent, TypeError when not a str.
func tokenText(agent *jsonx.Object) (string, error) {
	value, present := agent.Get("token")
	if !present {
		return "", errors.New("KeyError: 'token'")
	}
	token, ok := value.(string)
	if !ok {
		return "", errors.New("TypeError: can only concatenate str (not " + pyTypeNameOf(value) + ") to str")
	}
	return token, nil
}

// pyTypeNameOf is type(value).__name__ in quotes, including None.
func pyTypeNameOf(value any) string {
	if value == nil {
		return "\"NoneType\""
	}
	return strings.ReplaceAll(pyTypeName(value), "'", "\"")
}
