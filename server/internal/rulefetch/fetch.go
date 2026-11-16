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
	"net/url"
	"strings"
	"time"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/localhttp"
	"configflow/internal/logx"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/weburl"
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

// originOf is _origin(parsed): (scheme, hostname, port or the default
// port); a port above 65535 is an error.
func originOf(u *url.URL) (origin, error) {
	port, has, err := weburl.Port(u)
	if err != nil {
		return origin{}, err
	}
	if !has || port == 0 {
		port = 80
		if u.Scheme == "https" {
			port = 443
		}
	}
	return origin{u.Scheme, weburl.Hostname(u), port}, nil
}

// internalRuleBase is _internal_rule_base: the server origin whose API the
// URL addresses (server_domain, then the current request's host URL). The
// error is for an unparsable URL or base or a malformed port; it propagates
// to the caller.
func internalRuleBase(rawURL string, config *jsonx.Object, hostURL string) (*url.URL, bool, error) {
	settings := RuleFetchSettings(config)
	var parsed *url.URL
	bases := []any{settings.GetOr("server_domain", "")}
	if hostURL != "" {
		bases = append(bases, hostURL)
	}
	for _, value := range bases {
		base, isText := value.(string)
		if !jsonx.Truthy(value) || !isText {
			// A non-string server_domain is a TypeError in Python (never stored:
			// settings validation keeps it a string).
			continue
		}
		if parsed == nil {
			// Parsed only when there is a base to compare with.
			var err error
			if parsed, err = parseRuleURL(rawURL); err != nil {
				return nil, false, err
			}
		}
		o, err := parseRuleURL(base)
		if err != nil {
			return nil, false, err
		}
		left, err := originOf(parsed)
		if err != nil {
			return nil, false, err
		}
		right, err := originOf(o)
		if err != nil {
			return nil, false, err
		}
		if left != right {
			continue
		}
		prefix := strings.TrimRight(weburl.RawPath(o), "/") + "/api/"
		if strings.HasPrefix(weburl.RawPath(parsed), prefix) {
			return o, true, nil
		}
	}
	return nil, false, nil
}

// IsInternalRuleURLChecked is is_internal_rule_url: requests to ConfigFlow
// itself stay away from the download proxy. The error is Python's ValueError
// for a URL (or server base) urlsplit cannot parse or with a malformed port.
func IsInternalRuleURLChecked(url string, config *jsonx.Object, hostURL string) (bool, error) {
	_, ok, err := internalRuleBase(url, config, hostURL)
	return ok, err
}

// IsInternalRuleURL is IsInternalRuleURLChecked for URLs that already passed
// ValidateRuleURL (false when Python would raise).
func IsInternalRuleURL(url string, config *jsonx.Object, hostURL string) bool {
	ok, _ := IsInternalRuleURLChecked(url, config, hostURL)
	return ok
}

// GetRuleFetchProxy is get_rule_fetch_proxy(config_data).
func GetRuleFetchProxy(config *jsonx.Object) (string, error) {
	return NormalizeRuleFetchProxy(RuleFetchSettings(config).GetOr("rule_fetch_proxy", ""))
}

// RuleRequestProxyChecked is rule_request_options(url, config)['proxies'][scheme]:
// "" (direct) for internal URLs, otherwise the configured proxy.
func RuleRequestProxyChecked(url string, config *jsonx.Object, hostURL string) (string, error) {
	internal, err := IsInternalRuleURLChecked(url, config, hostURL)
	if err != nil {
		return "", err
	}
	if internal {
		return "", nil
	}
	return GetRuleFetchProxy(config)
}

// RuleRequestProxy is RuleRequestProxyChecked.
func RuleRequestProxy(url string, config *jsonx.Object, hostURL string) (string, error) {
	return RuleRequestProxyChecked(url, config, hostURL)
}

// GithubHosts is GITHUB_HOSTS.
var GithubHosts = []string{"github.com", "raw.githubusercontent.com", "gist.githubusercontent.com", "api.github.com"}

// IsGithubURL is is_github_url.
func IsGithubURL(url string) bool {
	parsed, err := weburl.Parse(url)
	if err != nil {
		return false
	}
	host := weburl.Hostname(parsed)
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
	internal, err := IsInternalRuleURLChecked(url, config, hostURL)
	if err != nil {
		return nil, err
	}
	if internal {
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
		base, internal, err := internalRuleBase(current, config, r.HostURL)
		if err != nil {
			return nil, err
		}
		proxy, err := RuleRequestProxy(current, config, r.HostURL)
		if err != nil {
			return nil, err
		}
		var response *Response
		if internal && c.Local != nil {
			local, err := c.Local(ctx, localhttp.Request{Method: method, URL: current,
				Prefix: strings.TrimRight(weburl.RawPath(base), "/"), Stream: r.Stream || r.MaxBytes > 0, ReadTimeout: r.ReadTimeout})
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
			if !r.Stream {
				// requests.get/head without stream=True: Session.send reads the
				// body. On the plain path a redirect first goes through
				// resolve_redirects (yield_requests=True); _RuleSession (HTTPS
				// proxy) never resolves redirects.
				httpsProxy := false
				if p, err := weburl.Parse(proxy); err == nil && p.Scheme == "https" {
					httpsProxy = true
				}
				if err := readUnstreamed(response, httpsProxy); err != nil {
					response.Close()
					return nil, err
				}
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
			location, _ := response.HeaderValue("Location")
			if location == "" {
				return "", errors.New("Redirect without Location")
			}
			target, err := joinURL(current, location)
			if err != nil {
				return "", err
			}
			targetParts, err := ValidateRuleURL(target)
			if err != nil {
				return "", err
			}
			if internal {
				left, err := originOf(parsed)
				if err != nil {
					return "", err
				}
				right, err := originOf(targetParts)
				if err != nil {
					return "", err
				}
				targetInternal := false
				if left == right {
					if targetInternal, err = IsInternalRuleURLChecked(target, config, r.HostURL); err != nil {
						return "", err
					}
				}
				if !targetInternal {
					return "", errors.New("Internal API redirect leaves its origin")
				}
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

// pyUnicodeDecodeError is the UnicodeDecodeError requests raises for a
// Location header that is not UTF-8 (get_redirect_target re-encodes the
// ISO-8859-1 header text and decodes it as UTF-8); not a RequestException.
type pyUnicodeDecodeError struct{ message string }

func (e *pyUnicodeDecodeError) Error() string       { return e.message }
func (e *pyUnicodeDecodeError) PythonClass() string { return "UnicodeDecodeError" }

// latin1Bytes reverses latin1Text (header text back to its bytes).
func latin1Bytes(text string) []byte {
	out := make([]byte, 0, len(text))
	for _, r := range text {
		out = append(out, byte(r))
	}
	return out
}

// readUnstreamed is what requests does with a response before returning it
// when stream is not set. Through an HTTPS proxy (_RuleSession, whose
// resolve_redirects yields nothing) the body is read and any failure raised.
// On the plain path a redirect status with a Location field goes through
// Session.resolve_redirects(yield_requests=True) first: the Location text is
// decoded as UTF-8 (UnicodeDecodeError), the body is read with
// ChunkedEncodingError / ContentDecodingError ignored, and the target is
// parsed (urlparse's ValueError). Every other
// response is read with failures raised.
func readUnstreamed(response *Response, httpsProxy bool) error {
	location, hasLocation := response.HeaderValue("Location")
	if httpsProxy || !redirectStatuses[response.StatusCode] || !hasLocation {
		_, err := response.Content()
		return err
	}
	raw := latin1Bytes(location)
	if message := pystr.UTF8DecodeError(raw); message != "" {
		return &pyUnicodeDecodeError{message}
	}
	target := string(raw)
	if target == "" {
		_, err := response.Content()
		return err
	}
	if _, err := response.Content(); err != nil {
		// ChunkedEncodingError and ContentDecodingError are caught there,
		// but a Content-Length body cut short is raised again when
		// Session.send reads the content (urllib3 enforces the length);
		// a chunked body cut short reads as empty then.
		var fetch *FetchError
		chunked := strings.EqualFold(response.Header.Get("Transfer-Encoding"), "chunked")
		swallowed := errors.As(err, &fetch) && (fetch.Class == "ContentDecodingError" ||
			(fetch.Class == "ChunkedEncodingError" && (!fetch.Transient || chunked)))
		if !swallowed {
			return err
		}
		response.content, response.consumed = nil, true
		response.Body = io.NopCloser(bytes.NewReader(nil))
	}
	if strings.HasPrefix(target, "//") {
		if from, err := weburl.Parse(response.URL); err == nil {
			target = from.Scheme + ":" + target
		}
	}
	_, err := weburl.Prepare(target, nil)
	return err
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
			int(retryDelays[attempt]/time.Second), attempt+1, weburl.SafeForLog(url))
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
			logger.Warningf("规则下载失败（%s），改用%s：%s", PythonClass(err), candidates[index+1].Label, weburl.SafeForLog(url))
			continue
		}
		if len(attempts) == 1 {
			return nil, err
		}
		// A mirror embeds the original URL in its path: redact source
		// userinfo there too, not just the outer URL's host.
		var secrets []string
		for _, source := range candidates {
			parsed, err := parseRuleURL(source.URL)
			if err != nil || parsed.User == nil {
				continue
			}
			if user := parsed.User.Username(); user != "" {
				secrets = append(secrets, user)
			}
			if password, _ := parsed.User.Password(); password != "" {
				secrets = append(secrets, password)
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
