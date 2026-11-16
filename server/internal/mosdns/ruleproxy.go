package mosdns

// Port of the rule proxy of backend/routes/mosdns.py (_fetch_remote_content,
// _validate_remote_url, _require_rule_proxy_auth, _local_content_rule,
// _load_cached_rule_content_for_url, mosdns_rule_proxy) and of its
// _cache_settings. The HTTP glue is in internal/api/mosdns.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/urlx"
)

var routeLogger = logx.Get("backend.routes.mosdns")

// MaxRuleProxyBytes is _MAX_RULE_PROXY_BYTES (a variable for tests).
var MaxRuleProxyBytes int64 = 5 * 1024 * 1024

// MaxRuleProxyRedirects is _MAX_RULE_PROXY_REDIRECTS.
const MaxRuleProxyRedirects = 3

// ValidateRemoteURL is _validate_remote_url: validate_rule_url (ValueError).
func ValidateRemoteURL(url string) (string, error) {
	if _, err := rulefetch.ValidateRuleURL(url); err != nil {
		return "", err
	}
	return url, nil
}

// FetchRemoteContent is _fetch_remote_content(url, config_data=, raw=):
// request_rule with (3, 10) s timeouts, at most 3 redirects, a 2xx status
// and at most MaxRuleProxyBytes. The result is []byte (raw) or the text
// decoded with errors='replace'. config nil is None (system settings);
// hostURL is flask.request.host_url inside a request ("" outside).
func FetchRemoteContent(ctx context.Context, url string, config *jsonx.Object, raw bool, hostURL string) (any, error) {
	redirects := MaxRuleProxyRedirects
	response, err := currentEnv().requestRule(ctx, url, rulefetch.Request{
		ConnectTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, Stream: true,
		MaxRedirects: &redirects, Config: config, HostURL: hostURL,
	})
	if err != nil {
		return nil, err
	}
	defer response.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &rulefetch.FetchError{Class: "HTTPError", Status: response.StatusCode,
			Message: fmt.Sprintf("Remote server returned HTTP %d", response.StatusCode)}
	}
	if length, ok := response.HeaderValue("Content-Length"); ok && length != "" {
		n, ok := new(big.Int).SetString(strings.TrimSpace(length), 10)
		if !ok {
			return nil, pyErr("ValueError", "invalid Content-Length "+strconv.Quote(length))
		}
		if n.Cmp(big.NewInt(MaxRuleProxyBytes)) > 0 {
			return nil, pyErr("ValueError", "Remote response exceeds size limit")
		}
	}
	var content []byte
	buffer := make([]byte, 64*1024)
	for {
		n, readErr := response.Read(buffer)
		if n > 0 {
			content = append(content, buffer[:n]...)
			if int64(len(content)) > MaxRuleProxyBytes {
				return nil, pyErr("ValueError", "Remote response exceeds size limit")
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if raw {
		if content == nil {
			content = []byte{}
		}
		return content, nil
	}
	return strings.ToValidUTF8(string(content), "\uFFFD"), nil
}

// AuthRequest is what _require_rule_proxy_auth reads from the request.
type AuthRequest struct {
	// Internal is is_internal_call().
	Internal bool
	// Authorization is the header (Present false when absent).
	Authorization        string
	AuthorizationPresent bool
	// Token is request.args.get('token', '').
	Token string
}

// RuleProxyAuthorized is _require_rule_proxy_auth(): an in-process MCP
// call, a valid JWT that is not a retired rule-proxy token, or the current
// rule-proxy or config token in the query. internalTokens is
// rule_proxy_tokens_for_sanitization(); verify is auth.VerifyToken.
func RuleProxyAuthorized(config *jsonx.Object, request AuthRequest, internalTokens repo.TokenSet,
	verify func(string) (auth.Verification, error)) (ok bool, err error) {
	defer catch(&err)
	if request.Internal {
		return true, nil
	}
	system := get(config, "system_config", jsonx.NewObject())
	configToken := get(system, "config_token", "")
	ruleProxyToken := get(system, "rule_proxy_token", "")
	bearer, _ := auth.ParseBearerToken(request.Authorization, request.AuthorizationPresent)
	hashKey(ruleProxyToken) // internal_tokens - {rule_proxy_token}: TypeError when unhashable
	retired := func(token string) bool {
		if s, isString := ruleProxyToken.(string); isString && s == token {
			return false
		}
		return internalTokens.Has(token)
	}
	if retired(bearer) {
		return false, nil
	}
	if bearer != "" {
		verification, err := verify(bearer)
		if err != nil {
			return false, err
		}
		if !verification.Failed() {
			return true, nil
		}
	}
	urlToken := request.Token
	if !auth.IsTokenWithinLength(urlToken) {
		urlToken = ""
	}
	if retired(urlToken) {
		return false, nil
	}
	return (truthy(ruleProxyToken) && jsonx.Equal(urlToken, ruleProxyToken)) ||
		(truthy(configToken) && jsonx.Equal(urlToken, configToken)), nil
}

// parseURL is urlparse(url) on net/url; a URL net/url refuses is a
// ValueError (Python may accept some of those).
func parseURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		raise(pyErr("ValueError", err.Error()))
	}
	return parsed
}

// LocalContentRule is _local_content_rule(original_url, config): the
// content of this profile's selected inline library source the URL points
// at, without self-HTTP; found=false is None. hostURL is request.host_url.
func LocalContentRule(originalURL string, config *jsonx.Object, hostURL string) (content any, found bool, err error) {
	defer catch(&err)
	content, found = localContentRule(originalURL, config, hostURL)
	return content, found, nil
}

func localContentRule(originalURL string, config *jsonx.Object, hostURL string) (any, bool) {
	parsed := parseURL(originalURL)
	base := strings.TrimSpace(strOf(get(get(config, "system_config", jsonx.NewObject()), "server_domain", "")))
	if base == "" {
		base = strings.TrimRight(hostURL, "/")
	}
	origin := parseURL(base)
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		if parsed.Scheme != origin.Scheme || hostWithUser(parsed) != hostWithUser(origin) {
			return nil, false
		}
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery {
		return nil, false
	}
	selected := selectedLibraryIDs(config)
	for _, rule := range iterate(get(config, "rule_library", []any{})) {
		if !selected.has(get(rule, "id", nil)) || !jsonx.Equal(get(rule, "source_type", nil), "content") {
			continue
		}
		path := repo.ProfileAPIPath(config, "/rule-library/content/"+str(item(rule, "id")))
		if parsed.Path == path || parsed.Path == strings.TrimRight(origin.Path, "/")+path {
			return get(rule, "content", ""), true
		}
	}
	return nil, false
}

// LoadCachedRuleContentForURL is _load_cached_rule_content_for_url(
// original_url, config_data): the validated local cache of a rule source
// with that URL, "" when there is none. config nil reads get_config().
func LoadCachedRuleContentForURL(ctx context.Context, originalURL string, config *jsonx.Object) (out string, err error) {
	defer catch(&err)
	if config == nil {
		if config, err = repo.GetConfig(ctx, ""); err != nil {
			return "", err
		}
	}
	if originalURL == "" {
		return "", nil
	}
	libraryValue := get(config, "rule_library", []any{})
	library := iterate(libraryValue)
	byID := map[string]any{}
	for _, rule := range library {
		byID[hashKey(get(rule, "id", nil))] = rule
	}
	var candidates []any
	for _, rule := range iterate(get(config, "rule_configs", []any{})) {
		if jsonx.Equal(get(rule, "itemType", nil), "ruleset") && jsonx.Equal(get(rule, "url", nil), originalURL) {
			candidate := byID[hashKey(get(rule, "library_rule_id", nil))]
			if !truthy(candidate) {
				candidate = rule
			}
			candidates = append(candidates, candidate)
		}
	}
	for _, rule := range library {
		if jsonx.Equal(get(rule, "url", nil), originalURL) {
			candidates = append(candidates, rule)
		}
	}
	path := parseURL(originalURL).Path
	const marker = "/rule-library/content/"
	if strings.Contains(path, marker) {
		_, rest, _ := strings.Cut(path, marker)
		ruleID, _, _ := strings.Cut(strings.Trim(rest, "/"), "/")
		if rule, present := byID[hashKey(ruleID)]; present {
			candidates = append(candidates, rule)
		}
	}
	libraryList, _ := libraryValue.([]any)
	if libraryList == nil {
		libraryList = []any{}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		source, ok := candidate.(*jsonx.Object)
		if !ok {
			raise(attributeError(candidate, "get"))
		}
		filepath, err := rulefetch.RuleCacheReadPath(ctx, source, libraryList)
		if err != nil {
			raise(err)
		}
		if seen[filepath] {
			continue
		}
		seen[filepath] = true
		content, err := rulefetch.ReadValidatedRuleCache(ctx, source, config)
		if err != nil {
			if isOSError(err) {
				routeLogger.Warningf("读取本地规则缓存失败 %s: %s", filepath, err.Error())
				continue
			}
			raise(err)
		}
		if content != nil {
			text, err := decodeUTF8Sig(content)
			if err != nil {
				routeLogger.Warningf("读取本地规则缓存失败 %s: %s", filepath, err.Error())
				continue
			}
			return text, nil
		}
	}
	return "", nil
}

// hostWithUser is the netloc of u.
func hostWithUser(u *url.URL) string {
	if u.User != nil {
		return u.User.String() + "@" + u.Host
	}
	return u.Host
}

// strOf requires a string (AttributeError otherwise, like .strip()).
func strOf(v any) string {
	text, ok := v.(string)
	if !ok {
		raise(attributeError(v, "strip"))
	}
	return text
}

func isOSError(err error) bool {
	switch pythonClass(err) {
	case "OSError", "FileNotFoundError", "PermissionError", "IsADirectoryError", "NotADirectoryError",
		"ConfigStorageError":
		return true
	}
	return false
}

// RuleProxyRequest is what mosdns_rule_proxy reads from the request.
type RuleProxyRequest struct {
	Auth AuthRequest
	// URL, Part and SourceID are request.args url/part/source_id ("" when
	// missing or empty).
	URL, Part, SourceID string
	// HostURL is request.host_url.
	HostURL string
}

// RuleProxyResponse is the view's return value: a text body (JSON nil) or
// a JSON body, the status and the extra headers.
type RuleProxyResponse struct {
	Status int
	Text   string
	JSON   *jsonx.Object
	// NoStore adds Cache-Control: no-store (always set for text bodies).
	NoStore bool
	// Stale adds X-ConfigFlow-Rule-Cache: stale.
	Stale bool
}

// ruleProxyDeps are the collaborators of mosdns_rule_proxy (replaced by
// the recorded-suite replay).
type ruleProxyDeps struct {
	getConfig        func(ctx context.Context) (*jsonx.Object, error)
	authorized       func(ctx context.Context, config *jsonx.Object, request AuthRequest) (bool, error)
	localContentRule func(url string, config *jsonx.Object, hostURL string) (any, bool, error)
	applyGithubProxy func(url string, config *jsonx.Object) (string, error)
	validateRemote   func(url string) error
}

func defaultRuleProxyDeps() ruleProxyDeps {
	return ruleProxyDeps{
		getConfig: func(ctx context.Context) (*jsonx.Object, error) { return repo.GetConfig(ctx, "") },
		authorized: func(ctx context.Context, _ *jsonx.Object, request AuthRequest) (bool, error) {
			if request.Internal {
				return true, nil
			}
			// get_config() again: the request's cached snapshot.
			config, err := repo.GetConfig(ctx, "")
			if err != nil {
				return false, err
			}
			r, err := repo.GetRepository(ctx)
			if err != nil {
				return false, err
			}
			tokens, err := r.RuleProxyTokensForSanitization(ctx)
			if err != nil {
				return false, err
			}
			return RuleProxyAuthorized(config, request, tokens, auth.VerifyToken)
		},
		localContentRule: LocalContentRule,
		applyGithubProxy: convert.ApplyGithubProxyDomain,
		validateRemote: func(url string) error {
			_, err := ValidateRemoteURL(url)
			return err
		},
	}
}

// RuleProxy is mosdns_rule_proxy(): fetch the original rule file (or the
// profile's inline source), convert it for MosDNS and split it by part.
// Every failure becomes a JSON response like Python's handlers. ctx must
// carry the request's repository scope; work that writes the rule cache or
// the task status is never cancelled.
func RuleProxy(ctx context.Context, request RuleProxyRequest) *RuleProxyResponse {
	return ruleProxy(context.WithoutCancel(ctx), request, defaultRuleProxyDeps())
}

func jsonFailure(status int, message string) *RuleProxyResponse {
	return &RuleProxyResponse{Status: status, JSON: jsonx.Obj("success", false, "message", message)}
}

func ruleProxy(ctx context.Context, request RuleProxyRequest, deps ruleProxyDeps) *RuleProxyResponse {
	response, err := ruleProxyBody(ctx, request, deps)
	if err == nil {
		return response
	}
	var remote *rulefetch.RemoteRuleValidationError
	switch {
	case errors.As(err, &remote) && pythonClass(err) == "RemoteRuleValidationError":
		routeLogger.Warningf("规则源类型校验失败：%s", err.Error())
		return &RuleProxyResponse{Status: 422, JSON: jsonx.Obj("success", false, "code", "RULE_CONTENT_INVALID",
			"message", err.Error())}
	case isValueError(err):
		routeLogger.Warningf("MosDNS rule-proxy rejected request: %s", safeExceptionDetails(err))
		return jsonFailure(400, "Invalid remote URL")
	}
	routeLogger.Errorf("MosDNS rule-proxy request failed: %s", safeExceptionDetails(err))
	return jsonFailure(500, "Failed to process rule proxy request")
}

func ruleProxyBody(ctx context.Context, request RuleProxyRequest, deps ruleProxyDeps) (out *RuleProxyResponse, err error) {
	defer catch(&err)
	config, err := deps.getConfig(ctx)
	if err != nil {
		return nil, err
	}
	authorized, err := deps.authorized(ctx, config, request.Auth)
	if err != nil {
		return nil, err
	}
	if !authorized {
		return jsonFailure(401, "Unauthorized"), nil
	}
	originalURL := request.URL
	if originalURL == "" {
		return jsonFailure(400, "URL parameter is required"), nil
	}
	part := request.Part
	if part != "" && !inList(part, RuleParts) {
		return jsonFailure(400, "Invalid part parameter"), nil
	}
	var staleErr error
	original, found, err := deps.localContentRule(originalURL, config, request.HostURL)
	if err != nil {
		return nil, err
	}
	if !found {
		// Use normal hostname routing, including Fake-IP networks and the
		// download proxy.
		fetchURL, err := deps.applyGithubProxy(originalURL, config)
		if err != nil {
			return nil, err
		}
		if err := deps.validateRemote(fetchURL); err != nil {
			return nil, err
		}
		declared, err := rulefetch.ResolveRemoteRuleSource(originalURL, config, request.SourceID)
		if err != nil {
			return nil, err
		}
		if declared != nil {
			format, err := convert.RulesetFormat(declared, config, "")
			if err != nil {
				return nil, err
			}
			if format == "mrs" {
				return nil, rulefetch.NewRemoteRuleMessage("OxiDNS/MosDNS 不支持 MRS 二进制规则，请使用该订阅的 Text 或 YAML 版本。")
			}
		}
		fetched, fetchErr := func() (any, error) {
			if declared != nil && jsonx.Equal(declared.GetOr("source_type", "url"), "url") {
				value, stale, err := refresh.ReadRuleSource(ctx, declared, refresh.RuleOptions{Config: config,
					Fetch: func(string) (any, error) {
						return currentEnv().fetchRemoteContent(ctx, fetchURL, config, true, request.HostURL)
					}})
				if err != nil {
					return nil, err
				}
				text, err := decodeUTF8Sig(value)
				if err != nil {
					return nil, err
				}
				staleErr = stale
				if stale != nil {
					routeLogger.Warningf("规则集「%s」更新失败，使用上一次有效缓存：%s",
						str(declared.GetOr("name", "")), refresh.DescribeFailure(stale))
				}
				return text, nil
			}
			return currentEnv().fetchRemoteContent(ctx, fetchURL, nil, false, request.HostURL)
		}()
		if fetchErr != nil {
			if isRequestException(fetchErr) {
				routeLogger.Warningf("在线规则更新失败，保留设备现有规则: %s %s",
					urlx.SafeURLForLog(originalURL), safeExceptionDetails(fetchErr))
				// Runtime downloaders treat HTTP 200 as a successful refresh;
				// the shared cache could silently roll devices back.
				return &RuleProxyResponse{Status: 502, NoStore: true, JSON: jsonx.Obj("success", false,
					"code", "RULE_SOURCE_UNAVAILABLE", "message", "规则源暂时不可用，未返回旧缓存；请恢复规则源后重试。")}, nil
			}
			return nil, fetchErr
		}
		original = fetched
	}
	var partArg any
	if part != "" {
		partArg = part
	}
	converted := convertForArchive(original, partArg).(string)
	return &RuleProxyResponse{Status: 200, Text: converted, NoStore: true, Stale: staleErr != nil}, nil
}

// CacheSettings is _cache_settings(data): the cache fields of a whole or a
// legacy cache save, normalized the same way.
func CacheSettings(data any) (out *jsonx.Object, err error) {
	defer catch(&err)
	// int(value), falling back to the default on TypeError, ValueError
	// and OverflowError.
	toInt := func(value any, def int64) any {
		switch v := value.(type) {
		case bool:
			if v {
				return jsonx.Int(1)
			}
			return jsonx.Int(0)
		case jsonx.Number:
			if v.IsInt() {
				return v
			}
			f := v.Float64()
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return jsonx.Int(def)
			}
			n, _ := new(big.Float).SetFloat64(math.Trunc(f)).Int(nil)
			return jsonx.BigInt(n)
		case string:
			text := strings.ReplaceAll(strings.TrimSpace(v), "_", "")
			if n, ok := new(big.Int).SetString(strings.TrimPrefix(text, "+"), 10); ok && text != "" &&
				!strings.HasPrefix(v, "_") && !strings.HasSuffix(strings.TrimSpace(v), "_") && !strings.Contains(v, "__") {
				return jsonx.BigInt(n)
			}
		}
		return jsonx.Int(def)
	}
	interval := toInt(get(data, "cache_dump_interval", jsonx.Int(300)), 300)
	if n, _ := new(big.Int).SetString(interval.(jsonx.Number).String(), 10); n == nil || n.Sign() <= 0 {
		interval = jsonx.Int(300)
	}
	dumpFile := get(data, "cache_dump_file", "./cache.dump")
	var dumpText any = "./cache.dump"
	if dumpFile != nil {
		dumpText = str(dumpFile)
	}
	return jsonx.Obj(
		"cache_enabled", truthy(get(data, "cache_enabled", true)),
		"cache_dump_enabled", truthy(get(data, "cache_dump_enabled", true)),
		"cache_size", toInt(get(data, "cache_size", jsonx.Int(10240)), 10240),
		"cache_lazy_ttl", toInt(get(data, "cache_lazy_ttl", jsonx.Int(21600)), 21600),
		"cache_dump_interval", interval,
		"cache_dump_file", dumpText,
	), nil
}
