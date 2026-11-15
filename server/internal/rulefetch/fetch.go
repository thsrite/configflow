package rulefetch

// Port of backend/utils/rule_fetch.py: shared proxy settings, redirect
// policy, GitHub mirror fallback and bounded retries for server-side rule
// downloads. Rule sources use normal hostname resolution (Fake-IP
// included); only URL syntax is checked, this is not a public-IP gate.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/localhttp"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/urlx"
)

var logger = logx.Get("backend.utils.rule_fetch")

// Client carries the injected network pieces. The zero value is not
// usable; use DefaultClient or set every field.
type Client struct {
	Transport Transport
	// Local is localhttp.Do (in-process dispatch of this server's own API).
	Local func(ctx context.Context, r localhttp.Request) (*localhttp.Response, error)
	// Sleep waits between retries (time.Sleep).
	Sleep func(time.Duration)
}

// DefaultClient uses the network, localhttp and time.Sleep.
var DefaultClient = &Client{Transport: HTTPTransport{}, Local: localhttp.Do, Sleep: time.Sleep}

// Request is the keyword arguments of request_rule.
type Request struct {
	// Method is "get" (default when "") or "head".
	Method string
	// Config is config_data; nil reads get_system_config().
	Config *jsonx.Object
	// MaxRedirects is max_redirects; nil means 3.
	MaxRedirects *int
	// MaxBytes > 0 reads the body (bounded) inside the retry loop; 0 is None.
	MaxBytes int64
	// Stream keeps the body lazy (stream=True).
	Stream                      bool
	ConnectTimeout, ReadTimeout time.Duration
	Insecure                    bool   // verify=False
	CABundle                    string // verify=<path>
	// HostURL is flask.request.host_url when called inside a request ("" outside).
	HostURL string
}

// RuleFetchSettings is rule_fetch_settings(config_data): its system_config.
func RuleFetchSettings(config *jsonx.Object) *jsonx.Object {
	settings := config.GetObject("system_config")
	if settings == nil {
		return jsonx.NewObject()
	}
	return settings
}

func loadConfig(ctx context.Context, config *jsonx.Object) (*jsonx.Object, error) {
	if config != nil {
		return config, nil
	}
	return repo.GetSystemConfig(ctx)
}

type origin struct {
	scheme, host string
	port         int
}

func originOf(u URL) origin {
	port, has, _ := u.Port()
	if !has || port == 0 {
		port = 80
		if u.Scheme == "https" {
			port = 443
		}
	}
	return origin{u.Scheme, u.Hostname(), port}
}

// internalRuleBase is _internal_rule_base: the server origin whose API the
// URL addresses (server_domain, then the current request's host URL).
func internalRuleBase(url string, config *jsonx.Object, hostURL string) (URL, bool) {
	settings := RuleFetchSettings(config)
	parsed, err := SplitURL(url, "")
	if err != nil {
		return URL{}, false
	}
	bases := []any{settings.GetOr("server_domain", "")}
	if hostURL != "" {
		bases = append(bases, hostURL)
	}
	for _, value := range bases {
		base, _ := value.(string)
		if !jsonx.Truthy(value) || base == "" {
			continue
		}
		o, err := SplitURL(base, "")
		if err != nil || originOf(parsed) != originOf(o) {
			continue
		}
		prefix := strings.TrimRight(o.Path, "/") + "/api/"
		if strings.HasPrefix(parsed.Path, prefix) {
			return o, true
		}
	}
	return URL{}, false
}

// IsInternalRuleURL is is_internal_rule_url: requests to ConfigFlow itself
// stay away from the download proxy.
func IsInternalRuleURL(url string, config *jsonx.Object, hostURL string) bool {
	_, ok := internalRuleBase(url, config, hostURL)
	return ok
}

// GetRuleFetchProxy is get_rule_fetch_proxy(config_data).
func GetRuleFetchProxy(config *jsonx.Object) (string, error) {
	return NormalizeRuleFetchProxy(RuleFetchSettings(config).GetOr("rule_fetch_proxy", ""))
}

// RuleRequestProxy is rule_request_options(url, config)['proxies'][scheme]:
// "" (direct) for internal URLs, otherwise the configured proxy.
func RuleRequestProxy(url string, config *jsonx.Object, hostURL string) (string, error) {
	if IsInternalRuleURL(url, config, hostURL) {
		return "", nil
	}
	return GetRuleFetchProxy(config)
}

// GithubHosts is GITHUB_HOSTS.
var GithubHosts = []string{"github.com", "raw.githubusercontent.com", "gist.githubusercontent.com", "api.github.com"}

// IsGithubURL is is_github_url.
func IsGithubURL(url string) bool {
	parsed, err := SplitURL(url, "")
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	for _, h := range GithubHosts {
		if host == h {
			return true
		}
	}
	return false
}

// Candidate is one (label, url) download candidate.
type Candidate struct{ Label, URL string }

// GithubDownloadCandidates is github_download_candidates: the original URL,
// then the configured GitHub mirror for GitHub URLs. A URL already carrying
// exactly that mirror prefix is unwrapped first.
func GithubDownloadCandidates(url string, config *jsonx.Object, hostURL string) ([]Candidate, error) {
	if IsInternalRuleURL(url, config, hostURL) {
		return []Candidate{{"原始地址", url}}, nil
	}
	mirror, isText := RuleFetchSettings(config).GetOr("github_proxy_domain", "").(string)
	if isText && pyStrip(mirror) != "" {
		prefix := pyStrip(mirror)
		if !hasAnyPrefix(prefix, "http://", "https://") {
			prefix = "https://" + prefix
		}
		prefix = strings.TrimRight(prefix, "/") + "/"
		if strings.HasPrefix(url, prefix) {
			original := url[len(prefix):]
			wrapped, err := convert.ApplyGithubProxyDomain(original, config)
			if err != nil {
				return nil, err
			}
			if wrapped == url {
				url = original
			}
		}
	}
	mirrored, err := convert.ApplyGithubProxyDomain(url, config)
	if err != nil {
		return nil, err
	}
	candidates := []Candidate{{"原始地址", url}}
	if mirrored != url {
		candidates = append(candidates, Candidate{"GitHub 代理地址", mirrored})
	}
	return candidates, nil
}

// RuleFetchError is RuleFetchError: both the source and the configured
// GitHub mirror failed. Its message is failure_text of every attempt.
type RuleFetchError struct {
	Attempts  []Attempt
	LastError error
	message   string
}

// Attempt is one failed candidate.
type Attempt struct {
	Label string
	Err   error
}

func (e *RuleFetchError) Error() string { return e.message }

// PythonClass is type(exc).__name__.
func (e *RuleFetchError) PythonClass() string { return "RuleFetchError" }

// Unwrap is exc.last_error (for errors.As on the last cause).
func (e *RuleFetchError) Unwrap() error { return e.LastError }

func newRuleFetchError(attempts []Attempt, secrets []string) *RuleFetchError {
	lines := make([]string, len(attempts))
	for i, a := range attempts {
		lines[i] = a.Label + "：" + PythonClass(a.Err) + ": " + a.Err.Error()
	}
	anySecrets := make([]any, len(secrets))
	for i, s := range secrets {
		anySecrets[i] = s
	}
	return &RuleFetchError{Attempts: attempts, LastError: attempts[len(attempts)-1].Err,
		message: diag.FailureText(strings.Join(lines, "\n"), anySecrets, 0)}
}

var redirectStatuses = map[int]bool{301: true, 302: true, 303: true, 307: true, 308: true}

// requestOnce is _request_rule_once: fetch by hostname, choosing the proxy
// again on every redirect hop. An empty proxy setting means direct; an
// explicit proxy never falls back to direct. Internal API requests are
// direct and may only redirect within that origin's API paths.
func (c *Client) requestOnce(ctx context.Context, url string, r *Request, config *jsonx.Object) (*Response, error) {
	method := r.Method
	if method == "" {
		method = "get"
	}
	if method != "get" && method != "head" {
		return nil, errors.New("Rule requests only support GET and HEAD")
	}
	maxRedirects := 3
	if r.MaxRedirects != nil {
		maxRedirects = *r.MaxRedirects
	}
	current := url
	for hop := 0; hop <= maxRedirects; hop++ {
		parsed, err := ValidateRuleURL(current)
		if err != nil {
			return nil, err
		}
		base, internal := internalRuleBase(current, config, r.HostURL)
		proxy, err := RuleRequestProxy(current, config, r.HostURL)
		if err != nil {
			return nil, err
		}
		var response *Response
		if internal && c.Local != nil {
			local, err := c.Local(ctx, localhttp.Request{Method: method, URL: current,
				Prefix: strings.TrimRight(base.Path, "/"), Stream: r.Stream || r.MaxBytes > 0, ReadTimeout: r.ReadTimeout})
			if err != nil {
				return nil, localError(err)
			}
			if local != nil {
				response = &Response{StatusCode: local.StatusCode, Reason: local.Reason, Header: local.Header,
					URL: local.URL, Body: local.Body}
			}
		}
		if response == nil {
			response, err = c.Transport.Do(ctx, TransportRequest{Method: strings.ToUpper(method), URL: current,
				Proxy: proxy, ConnectTimeout: r.ConnectTimeout, ReadTimeout: r.ReadTimeout,
				Insecure: r.Insecure, CABundle: r.CABundle})
			if err != nil {
				return nil, err
			}
		}
		if !redirectStatuses[response.StatusCode] {
			if !r.Stream && r.MaxBytes == 0 {
				if _, err := response.Content(); err != nil {
					return nil, err
				}
			}
			return response, nil
		}
		next, err := func() (string, error) {
			defer response.Close()
			location := response.Header.Get("Location")
			if location == "" {
				return "", errors.New("Redirect without Location")
			}
			target, err := URLJoin(current, location)
			if err != nil {
				return "", err
			}
			targetParts, err := ValidateRuleURL(target)
			if err != nil {
				return "", err
			}
			if internal && (originOf(parsed) != originOf(targetParts) || !IsInternalRuleURL(target, config, r.HostURL)) {
				return "", errors.New("Internal API redirect leaves its origin")
			}
			if hop == maxRedirects {
				return "", errors.New("Too many redirects")
			}
			return target, nil
		}()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return nil, errors.New("Too many redirects")
}

func localError(err error) error {
	switch {
	case errors.Is(err, localhttp.ErrTooManyRedirects):
		return &FetchError{Class: "TooManyRedirects", Message: err.Error(), Err: err}
	case errors.Is(err, localhttp.ErrReadTimeout):
		return &FetchError{Class: "ReadTimeout", Message: err.Error(), Err: err}
	}
	return &FetchError{Class: "InvalidURL", Message: err.Error(), Err: err}
}

// ErrSizeLimit is the ValueError for a body above max_bytes.
var ErrSizeLimit = errors.New("Resource exceeds deployment file size limit")

var retryDelays = []time.Duration{time.Second, 2 * time.Second}

// requestWithRetries is _request_rule_with_retries.
func (c *Client) requestWithRetries(ctx context.Context, url string, r *Request, config *jsonx.Object, checkStatus bool) (*Response, error) {
	for attempt := 0; ; attempt++ {
		response, err := c.requestOnce(ctx, url, r, config)
		if err == nil && checkStatus {
			err = response.RaiseForStatus()
		}
		if err == nil && r.MaxBytes > 0 {
			var body bytes.Buffer
			chunk := make([]byte, 64*1024)
			for {
				n, readErr := response.Read(chunk)
				body.Write(chunk[:n])
				if int64(body.Len()) > r.MaxBytes {
					err = ErrSizeLimit
					break
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					err = readErr
					break
				}
			}
			if err == nil {
				response.Body.Close()
				response.content, response.consumed = body.Bytes(), true
				response.Body = io.NopCloser(bytes.NewReader(response.content))
			}
		}
		if err == nil {
			return response, nil
		}
		if response != nil {
			response.Close()
		}
		var fetch *FetchError
		if !errors.As(err, &fetch) || attempt == len(retryDelays) || !fetch.Transient {
			return nil, err
		}
		logger.Infof("规则下载连接中断（%s），%d 秒后重试第 %d 次：%s", fetch.Class,
			int(retryDelays[attempt]/time.Second), attempt+1, urlx.SafeURLForLog(url))
		c.Sleep(retryDelays[attempt])
	}
}

// RequestRule is request_rule(url, ...): the source first, then its
// configured GitHub mirror on failure, with the same explicit forward proxy
// (or direct transport) for every attempt. Each candidate retries transient
// disconnects after 1 s and 2 s. MaxBytes reads a bounded body inside the
// retry so a broken stream is retried as well; without it a streamed body
// stays lazy and its errors belong to the caller.
func (c *Client) RequestRule(ctx context.Context, url string, r Request) (*Response, error) {
	config, err := loadConfig(ctx, r.Config)
	if err != nil {
		return nil, err
	}
	candidates, err := GithubDownloadCandidates(url, config, r.HostURL)
	if err != nil {
		return nil, err
	}
	var attempts []Attempt
	for index, candidate := range candidates {
		response, err := c.requestWithRetries(ctx, candidate.URL, &r, config, len(candidates) > 1 || r.MaxBytes > 0)
		if err == nil {
			return response, nil
		}
		if !IsRequestException(err) {
			return nil, err
		}
		attempts = append(attempts, Attempt{candidate.Label, err})
		if index+1 < len(candidates) {
			logger.Warningf("规则下载失败（%s），改用%s：%s", PythonClass(err), candidates[index+1].Label, urlx.SafeURLForLog(url))
			continue
		}
		if len(attempts) == 1 {
			return nil, err
		}
		// A mirror embeds the original URL in its path: redact source
		// userinfo there too, not just the outer URL's host.
		var secrets []string
		for _, source := range candidates {
			parsed, _ := SplitURL(source.URL, "")
			user, hasUser, password, hasPassword := parsed.userinfo()
			if hasUser && user != "" {
				secrets = append(secrets, urlx.Unquote(user))
			}
			if hasPassword && password != "" {
				secrets = append(secrets, urlx.Unquote(password))
			}
		}
		return nil, newRuleFetchError(attempts, secrets)
	}
	return nil, fmt.Errorf("no download candidate")
}

// RequestRule is DefaultClient.RequestRule.
func RequestRule(ctx context.Context, url string, r Request) (*Response, error) {
	return DefaultClient.RequestRule(ctx, url, r)
}
