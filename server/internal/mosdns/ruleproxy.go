package mosdns

// The rule proxy of backend/routes/mosdns.py: devices download a rule file
// through ConfigFlow, converted to MosDNS text and split by part. The HTTP
// glue is in internal/api/mosdns.

import (
	"configflow/internal/lifecycle"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"configflow/internal/auth"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rulesetfmt"
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

type nativeFetchFunc func(context.Context, string, *model.Config, string) ([]byte, error)

func fetchNative(ctx context.Context, rawURL string, config *model.Config, hostURL string) ([]byte, error) {
	redirects := 3
	response, err := requestCaptured(ctx, rawURL, config, rulefetch.Request{
		ConnectTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, Stream: true,
		MaxRedirects: &redirects, HostURL: hostURL,
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
	current := cfg.SystemConfig.RuleProxyToken
	retired := func(token string) bool {
		matched := 0
		for candidate := range internalTokens {
			matched |= credentialMatch(token, candidate)
		}
		return matched == 1 && !sameCredential(token, current)
	}
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
	config := cfg.SystemConfig.ConfigToken
	return current != "" && sameCredential(token, current) || config != "" && sameCredential(token, config), nil
}

// localContent returns the content of this profile's selected inline
// library source the URL points at (no self-HTTP). A URL Go cannot parse
// is not local; the remote URL check rejects it.
func localContent(raw string, cfg snapshot, hostURL string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	base := strings.TrimSpace(cfg.SystemConfig.ServerDomain)
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
	if cfg.ProfileID != "" {
		prefix = "/api/profiles/" + cfg.ProfileID
	}
	for _, rule := range cfg.RuleLibrary {
		if textValue(rule.SourceType, "url") != "content" || !cfg.selected(rule.ID) {
			continue
		}
		path := prefix + "/rule-library/content/" + rule.ID
		if parsed.Path == path || parsed.Path == strings.TrimRight(origin.Path, "/")+path {
			return textValue(rule.Content, ""), true
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
	ctx = lifecycle.Detach(ctx) // cache writes and task records finish
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
	var linkErr *os.LinkError
	var download *rulefetch.DownloadError
	var fetch *rulefetch.FetchError
	var attempts *rulefetch.RuleFetchError
	switch {
	case errors.As(err, &linkErr), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Native receipt/rename failures and owner cancellation are server
		// failures, never URL validation, even through wrapped error chains.
		return false
	case repo.IsValueError(err), errors.Is(err, errSizeLimit), errors.Is(err, errContentLength),
		errors.Is(err, errNotUTF8), errors.Is(err, rulefetch.ErrInvalidURL),
		errors.Is(err, rulefetch.ErrInvalidProxy), errors.Is(err, rulefetch.ErrProxyNotString),
		errors.Is(err, rulefetch.ErrInternalRedirect), errors.Is(err, rulefetch.ErrRedirectEncoding), errors.Is(err, rulefetch.ErrSizeLimit):
		return true
	case errors.As(err, &repoErr), errors.As(err, &storeErr), errors.As(err, &pathErr),
		errors.As(err, &download), errors.As(err, &fetch), errors.As(err, &attempts):
		return false
	}
	// Remaining plain validation errors keep the existing 400 contract.
	return true
}

func ruleProxy(ctx context.Context, request RuleProxyRequest) (*RuleProxyResponse, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	id, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, err
	}
	config, err := r.Config(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	cfg := snapshotOf(config)
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
		fetchURL, err := proxySourceURL(request.URL, config)
		if err != nil {
			return nil, err
		}
		if _, err := rulefetch.ValidateRuleURL(fetchURL); err != nil {
			return nil, err
		}
		declared, err := resolveLegacySource(request.URL, config, request.SourceID)
		if err != nil {
			return nil, err
		}
		var fetchErr error
		if declared != nil {
			if format, err := rulesetfmt.RuleFormat(model.RuleConfig{LibraryRuleID: declared.ID, Name: declared.Name}, config.RuleLibrary, ""); err != nil {
				return nil, err
			} else if format == "mrs" {
				return nil, rulefetch.NewRemoteRuleMessage(mrsMessage)
			}
			var raw []byte
			var staleErr error
			raw, staleErr, fetchErr = readCapturedSource(ctx, *declared, config, func(string) (any, error) { return fetchCaptured(ctx, fetchURL, config, request.HostURL) })
			if fetchErr == nil {
				if content, err = decodeText(raw); err != nil {
					return nil, err
				}
				if staleErr != nil {
					stale = true
					routeLogger.Warningf("规则集「%s」更新失败，使用上一次有效缓存：%s",
						declared.Name, refresh.DescribeFailure(staleErr))
				}
			}
		} else {
			var raw []byte
			raw, fetchErr = fetchCaptured(ctx, fetchURL, nil, request.HostURL)
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

func sameCredential(a, b string) bool {
	return credentialMatch(a, b) == 1
}

func credentialMatch(a, b string) int {
	left, right := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(left[:], right[:])
}
