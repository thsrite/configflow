package mosdns

// The rule proxy of backend/routes/mosdns.py: devices download a rule file
// through ConfigFlow, converted to MosDNS text and split by part. The HTTP
// glue is in internal/api/mosdns.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"configflow/internal/store"
	"configflow/internal/weburl"
)

var routeLogger = logx.Get("backend.routes.mosdns")

// MaxRuleProxyBytes bounds a proxied download (a variable for tests).
var MaxRuleProxyBytes int64 = 5 * 1024 * 1024

// RequestRule downloads a rule source (tests replace it).
var RequestRule = rulefetch.RequestRule

// Client errors (400) of the rule proxy's download.
var (
	errSizeLimit     = errors.New("Remote response exceeds size limit")
	errContentLength = errors.New("invalid Content-Length")
)

// fetchRemoteFn is fetchRemote (tests replace it).
var fetchRemoteFn fetchFunc = fetchRemote

type fetchFunc func(ctx context.Context, rawURL string, config *jsonx.Object, hostURL string) ([]byte, error)

// fetchRemote downloads url with (3, 10) s timeouts, at most 3 redirects,
// a 2xx status and at most MaxRuleProxyBytes. config nil uses the system
// settings; hostURL is the request's host URL ("" outside a request).
func fetchRemote(ctx context.Context, rawURL string, config *jsonx.Object, hostURL string) ([]byte, error) {
	redirects := 3
	response, err := RequestRule(ctx, rawURL, rulefetch.Request{
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
		n, err := strconv.ParseInt(strings.TrimSpace(length), 10, 64)
		if err != nil {
			return nil, errContentLength
		}
		if n > MaxRuleProxyBytes {
			return nil, errSizeLimit
		}
	}
	content, err := io.ReadAll(io.LimitReader(response, MaxRuleProxyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxRuleProxyBytes {
		return nil, errSizeLimit
	}
	return content, nil
}

// AuthRequest is the credential part of a rule-proxy request.
type AuthRequest struct {
	Internal      bool   // in-process MCP call (authenticated at /mcp)
	Authorization string // Authorization header
	Token         string // ?token=
}

// authorized accepts an in-process MCP call, a valid JWT that is not a
// retired rule-proxy token, or the current rule-proxy or config token in
// the query. internalTokens are the persisted current and retired
// rule-proxy tokens.
func authorized(cfg snapshot, request AuthRequest, internalTokens repo.TokenSet) (bool, error) {
	if request.Internal {
		return true, nil
	}
	current := cfg.SystemConfig.RuleProxyToken.str()
	retired := func(token string) bool { return token != current && internalTokens.Has(token) }
	bearer, _ := auth.ParseBearerToken(request.Authorization, request.Authorization != "")
	if retired(bearer) {
		return false, nil
	}
	if bearer != "" {
		verification, err := auth.VerifyToken(bearer)
		if err != nil {
			return false, err
		}
		if !verification.Failed() {
			return true, nil
		}
	}
	token := request.Token
	if !auth.IsTokenWithinLength(token) {
		token = ""
	}
	if retired(token) {
		return false, nil
	}
	config := cfg.SystemConfig.ConfigToken.str()
	return current != "" && token == current || config != "" && token == config, nil
}

// localContent returns the content of this profile's selected inline
// library source the URL points at (no self-HTTP). A URL Go cannot parse
// is not local; the remote URL check rejects it.
func localContent(raw string, cfg snapshot, hostURL string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	base := strings.TrimSpace(cfg.SystemConfig.ServerDomain.str())
	if base == "" {
		base = strings.TrimRight(hostURL, "/")
	}
	origin, err := url.Parse(base)
	if err != nil {
		origin = &url.URL{}
	}
	// Python compares scheme and netloc (user info included).
	if (parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil) && (parsed.Scheme != origin.Scheme ||
		parsed.Host != origin.Host || parsed.User.String() != origin.User.String()) {
		return "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	prefix := ""
	if cfg.ProfileID.value != "" {
		prefix = "/api/profiles/" + cfg.ProfileID.value
	}
	for _, rule := range cfg.RuleLibrary {
		if !rule.SourceType.is("content") || !cfg.selected(rule.ID) {
			continue
		}
		path := prefix + "/rule-library/content/" + rule.ID.value
		if parsed.Path == path || parsed.Path == strings.TrimRight(origin.Path, "/")+path {
			return rule.Content.str(), true
		}
	}
	return "", false
}

// RuleProxyRequest is a rule-proxy request.
type RuleProxyRequest struct {
	Auth                AuthRequest
	URL, Part, SourceID string // query arguments
	HostURL             string
}

// RuleProxyResponse is the answer: a text body (JSON nil) or a JSON body.
type RuleProxyResponse struct {
	Status  int
	Text    string
	JSON    map[string]any
	NoStore bool // Cache-Control: no-store
	Stale   bool // X-ConfigFlow-Rule-Cache: stale
}

func failure(status int, message string) *RuleProxyResponse {
	return &RuleProxyResponse{Status: status, JSON: map[string]any{"success": false, "message": message}}
}

// RuleProxy serves a device's rule download: the original rule file (or
// the profile's inline source) converted for MosDNS. Declared remote
// sources come from their validated cache; a source that cannot be fetched
// is a 502 rather than an old cache (it could roll devices back). ctx must
// carry the request's repository scope.
func RuleProxy(ctx context.Context, request RuleProxyRequest) *RuleProxyResponse {
	ctx = context.WithoutCancel(ctx) // cache writes and task records finish
	response, err := ruleProxy(ctx, request)
	if err == nil {
		return response
	}
	var remote *rulefetch.RemoteRuleValidationError
	switch {
	case errors.As(err, &remote):
		routeLogger.Warningf("规则源类型校验失败：%s", err.Error())
		return &RuleProxyResponse{Status: 422, JSON: map[string]any{"success": false, "code": "RULE_CONTENT_INVALID",
			"message": err.Error()}}
	case isBadRequest(err):
		routeLogger.Warningf("MosDNS rule-proxy rejected request: %s", safeDetails(err))
		return failure(400, "Invalid remote URL")
	}
	routeLogger.Errorf("MosDNS rule-proxy request failed: %s", safeDetails(err))
	return failure(500, "Failed to process rule proxy request")
}

// isBadRequest reports a failure the request caused (400): an invalid or
// refused URL, an oversized or undecodable response, a rejected value.
// Storage, file-system and download (502) failures are not.
func isBadRequest(err error) bool {
	var repoErr *repo.Error
	var storeErr *store.Error
	var pathErr *fs.PathError
	switch {
	case repo.IsValueError(err), errors.Is(err, errSizeLimit), errors.Is(err, errContentLength),
		errors.Is(err, errNotUTF8), errors.Is(err, rulefetch.ErrInvalidURL):
		return true
	case errors.As(err, &repoErr), errors.As(err, &storeErr), errors.As(err, &pathErr), rulefetch.IsRequestException(err):
		return false
	}
	// TODO(N9): rulefetch reports URL syntax, redirect and size refusals as
	// untyped errors (its "ValueError"); other packages' errors still carry
	// Python class names.
	class := rulefetch.PythonClass(err)
	return class == "ValueError" || class == "UnicodeDecodeError"
}

func ruleProxy(ctx context.Context, request RuleProxyRequest) (*RuleProxyResponse, error) {
	config, err := repo.GetConfig(ctx, "")
	if err != nil {
		return nil, err
	}
	var cfg snapshot
	if err := viaJSON(config, &cfg); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := r.RuleProxyTokensForSanitization(ctx)
	if err != nil {
		return nil, err
	}
	if ok, err := authorized(cfg, request.Auth, tokens); err != nil {
		return nil, err
	} else if !ok {
		return failure(401, "Unauthorized"), nil
	}
	if request.URL == "" {
		return failure(400, "URL parameter is required"), nil
	}
	if request.Part != "" && request.Part != PartDomain && request.Part != PartIP {
		return failure(400, "Invalid part parameter"), nil
	}
	content, found := localContent(request.URL, cfg, request.HostURL)
	stale := false
	if !found {
		// Normal hostname routing, including Fake-IP networks and the
		// download proxy.
		fetchURL, err := convert.ApplyGithubProxyDomain(request.URL, config)
		if err != nil {
			return nil, err
		}
		if _, err := rulefetch.ValidateRuleURL(fetchURL); err != nil {
			return nil, err
		}
		declared, err := rulefetch.ResolveRemoteRuleSource(request.URL, config, request.SourceID)
		if err != nil {
			return nil, err
		}
		var fetchErr error
		if declared != nil {
			if format, err := convert.RulesetFormat(declared, config, ""); err != nil {
				return nil, err
			} else if format == "mrs" {
				return nil, rulefetch.NewRemoteRuleMessage(mrsMessage)
			}
			var raw []byte
			var staleErr error
			raw, staleErr, fetchErr = refresh.ReadRuleSource(ctx, declared, refresh.RuleOptions{Config: config,
				Fetch: func(string) (any, error) { return fetchRemoteFn(ctx, fetchURL, config, request.HostURL) }})
			if fetchErr == nil {
				if content, err = decodeText(raw); err != nil {
					return nil, err
				}
				if staleErr != nil {
					stale = true
					routeLogger.Warningf("规则集「%s」更新失败，使用上一次有效缓存：%s",
						jsonx.Str(declared.GetOr("name", "")), refresh.DescribeFailure(staleErr))
				}
			}
		} else {
			var raw []byte
			raw, fetchErr = fetchRemoteFn(ctx, fetchURL, nil, request.HostURL)
			content = strings.ToValidUTF8(string(raw), "�")
		}
		if fetchErr != nil {
			if !rulefetch.IsRequestException(fetchErr) {
				return nil, fetchErr
			}
			routeLogger.Warningf("在线规则更新失败，保留设备现有规则: %s %s", weburl.SafeForLog(request.URL), safeDetails(fetchErr))
			return &RuleProxyResponse{Status: 502, NoStore: true, JSON: map[string]any{"success": false,
				"code": "RULE_SOURCE_UNAVAILABLE", "message": "规则源暂时不可用，未返回旧缓存；请恢复规则源后重试。"}}, nil
		}
	}
	var converted string
	if request.Part == "" {
		converted, err = ConvertRuleContentForMosDNS(content)
	} else {
		converted, err = ConvertRuleContentForMosDNS(content, request.Part)
	}
	if err != nil {
		return nil, err
	}
	return &RuleProxyResponse{Status: 200, Text: converted, NoStore: true, Stale: stale}, nil
}
