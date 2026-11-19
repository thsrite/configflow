package substore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Reuse windows and bounds of sub_store_client.py. Shared by manual
// refresh, provider delivery and aggregation within this process:
// automatic delivery tolerates five minutes, explicit refresh keeps a short
// debounce.
const (
	FetchReuseSeconds      = 300
	ManualReuseSeconds     = 30
	BackgroundReuseSeconds = 900
	BlockedReuseSeconds    = 60
	FetchCacheLimit        = 128
	NodeConversionLimit    = 512
)

var clientLog = logx.Get("backend.utils.sub_store_client")

// FetchResult is the text and metadata obtained from one upstream response.
type FetchResult struct {
	Text      string
	Source    string
	Traffic   *Traffic
	FetchedAt string
}

func newFetchResult(text, source string, traffic *Traffic, fetchedAt string, clock Clock) *FetchResult {
	if fetchedAt == "" {
		fetchedAt = isoFormat(clock.Now())
	}
	return &FetchResult{Text: text, Source: source, Traffic: traffic, FetchedAt: fetchedAt}
}

// ErrBodyConversion identifies a fetched body that could not be converted.
var ErrBodyConversion = errors.New(msgBodyConversion)

// SubscriptionFetchError retains both conversion and direct-fetch failures.
type SubscriptionFetchError struct {
	SubStoreError error
	DirectError   error
}

func (e *SubscriptionFetchError) Error() string {
	return "subscription fetch failed via Sub-Store and direct fallback"
}
func (e *SubscriptionFetchError) Unwrap() error { return e.DirectError }

// FetchOptions are the keyword arguments of get_subscription_proxies_yaml.
type FetchOptions struct {
	Refresh      bool // explicit refresh: reuse for 30 s only
	PreferCached bool // background work: reuse for 15 minutes
}

// flight shares one in-progress fetch and its immutable result.
type flight struct {
	done   chan struct{}
	result *FetchResult
	err    error
}

func newFlight() *flight { return &flight{done: make(chan struct{})} }

func (f *flight) finish(result *FetchResult, err error) {
	f.result, f.err = result, err
	close(f.done)
}

func (f *flight) isDone() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

func (f *flight) succeeded() bool { return f.isDone() && f.err == nil }

type fetchKey struct{ url, target, base string }

type fetchEntry struct {
	flight  *flight
	expires float64
}

type failureEntry struct {
	count int
	last  float64
}

type convKey struct{ version, target, proxy string }

// Client is the Sub-Store client of sub_store_client.py: subscription
// downloads (direct first, Sub-Store as the fallback through temporary
// subscriptions), request coalescing with reuse windows and failure
// cooldowns, and node conversions remembered per bundled Sub-Store version.
type Client struct {
	env     *Env
	runtime *Runtime

	mu         sync.Mutex
	results    *omap[fetchKey, fetchEntry]
	failures   *omap[fetchKey, failureEntry]
	cacheLimit int

	convMu          sync.Mutex
	conversions     *omap[convKey, *yamlx.Node]
	conversionLimit int

	// Replaceable steps (tests): _get_subscription_proxies_yaml and
	// _convert_proxy_string.
	fetch   func(ctx context.Context, subID, url, target string) (*FetchResult, error)
	convert func(ctx context.Context, proxyString, target string) (*yamlx.Node, error)
}

// NewClient returns a client on env using runtime for leases.
func NewClient(env *Env, runtime *Runtime) *Client {
	return &Client{env: env, runtime: runtime, results: newOmap[fetchKey, fetchEntry](),
		failures: newOmap[fetchKey, failureEntry](), cacheLimit: FetchCacheLimit,
		conversions: newOmap[convKey, *yamlx.Node](), conversionLimit: NodeConversionLimit}
}

var _ convert.NodeConverter = (*Client)(nil)

// isYAMLResponse is _is_yaml_response: text that looks like proxies YAML,
// not an HTML page.
func isYAMLResponse(text string) bool {
	stripped := strings.TrimSpace(text)
	if strings.HasPrefix(stripped, "<!") || strings.HasPrefix(stripped, "<html") ||
		strings.Contains(runePrefix(stripped, 500), "<head>") {
		return false
	}
	if strings.Contains(runePrefix(stripped, 200), "proxies:") || strings.HasPrefix(stripped, "proxies:") {
		return true
	}
	n, err := yamlx.Parse([]byte(stripped))
	return err == nil && yamlx.Get(n, "proxies") != nil
}

func preview(text string) string {
	return strings.ReplaceAll(runePrefix(text, 200), "\n", "\\n")
}

func (c *Client) get(ctx context.Context, url string, header http.Header, params []Param, timeout float64) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := c.env.HTTP.Do(ctx, &Request{Method: "GET", URL: url, Header: header, Params: params, Timeout: Seconds(timeout)})
	if err != nil {
		return nil, err
	}
	if err := resp.RaiseForStatus(); err != nil {
		return nil, err
	}
	return resp, nil
}

func clashHeader() http.Header { return http.Header{"User-Agent": {"clash.meta"}} }

// fetchDirectSubscriptionYAML is _fetch_direct_subscription_yaml.
func (c *Client) fetchDirectSubscriptionYAML(ctx context.Context, url string) (string, error) {
	clientLog.Info("Direct subscription fetch started")
	resp, err := c.get(ctx, url, clashHeader(), nil, 30)
	if err != nil {
		return "", err
	}
	text := resp.Text()
	if !isYAMLResponse(text) {
		return "", fmt.Errorf(msgDirectYAML, preview(text))
	}
	clientLog.Info("订阅 URL 直接返回有效 YAML，跳过 Sub-Store 转换")
	return text, nil
}

// createSubscription is _create_subscription: a temporary Sub-Store
// subscription; #noFlow skips Sub-Store's traffic request. content (when
// given) is converted instead of the upstream URL.
func (c *Client) createSubscription(ctx context.Context, base, subID, url string, content *string) bool {
	if ctx.Err() != nil {
		return false
	}
	if !strings.Contains(url, "#") {
		url += "#noFlow"
	}
	payload, err := jsonresp.Encode(struct {
		Name    string  `json:"name"`
		URL     string  `json:"url"`
		NoFlow  bool    `json:"noFlow"`
		Source  string  `json:"source,omitempty"`
		Content *string `json:"content,omitempty"`
	}{subID, url, true, func() string {
		if content != nil {
			return "local"
		}
		return ""
	}(), content})
	if err != nil {
		return false
	}
	clientLog.Info("Sub-Store 创建临时订阅 '" + subID + "'")
	resp, err := c.env.HTTP.Do(ctx, &Request{Method: "POST", URL: base + "/api/subs", JSON: payload, Timeout: Seconds(10)})
	if err != nil {
		clientLog.Warning("Sub-Store 创建订阅失败: " + safeExceptionDetails(err))
		return false
	}
	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		return true
	}
	clientLog.Warning(fmt.Sprintf("Sub-Store 创建订阅 '%s' 返回状态码: %d", subID, resp.StatusCode))
	return false
}

// deleteSubscription is _delete_subscription (used and deleted at once).
func (c *Client) deleteSubscription(ctx context.Context, base, subID string) {
	if ctx.Err() != nil {
		return
	}
	resp, err := c.env.HTTP.Do(ctx, &Request{Method: "DELETE", URL: base + "/api/sub/" + QuotePath(subID), Timeout: Seconds(10)})
	if err != nil {
		clientLog.Debug("Sub-Store 删除订阅失败（可忽略）: " + safeExceptionDetails(err))
		return
	}
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		clientLog.Info("Sub-Store 已删除临时订阅 '" + subID + "'")
	} else {
		clientLog.Debug(fmt.Sprintf("Sub-Store 删除订阅 '%s' 返回状态码: %d", subID, resp.StatusCode))
	}
}

// Fetch is get_subscription_proxies_yaml(sub_id, url,
// target, refresh=, prefer_cached=): concurrent requests for the same URL /
// format / Sub-Store share one download; a success is reused for 5 minutes
// (30 s for an explicit refresh, 15 minutes for background work), a failure
// blocks retries for a growing cooldown (3 s doubling to 5 minutes; at
// least 60 s when the upstream answered 403/429). The shared result must
// not be modified.
func (c *Client) Fetch(ctx context.Context, subID, url, target string, opts FetchOptions) (*FetchResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if target == "" {
		target = "ClashMeta"
	}
	base, err := c.env.BaseURL()
	if err != nil {
		return nil, err
	}
	key := fetchKey{url, target, base}
	inf := math.Inf(1)
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	now := c.env.Clock.Monotonic()
	for _, old := range c.failures.keys() {
		if entry, _ := c.failures.get(old); now-entry.last >= 3600 {
			c.failures.delete(old)
		}
	}
	for _, old := range c.results.keys() {
		entry, _ := c.results.get(old)
		retention := 0.0
		if entry.flight.succeeded() {
			retention = BackgroundReuseSeconds - FetchReuseSeconds
		}
		if entry.flight.isDone() && entry.expires+retention <= now {
			c.results.delete(old)
		}
	}
	if entry, ok := c.results.get(key); ok {
		reuse := float64(FetchReuseSeconds)
		if opts.Refresh {
			reuse = ManualReuseSeconds
		} else if opts.PreferCached {
			reuse = BackgroundReuseSeconds
		}
		// Never bypass an in-flight request or a failure's cooldown.
		if entry.flight.succeeded() && now >= entry.expires-FetchReuseSeconds+reuse {
			c.results.delete(key)
		}
	}
	owner := !c.results.has(key)
	var f *flight
	tracked := false
	if owner {
		// Evict only completed entries; never evict a flight with waiters.
		var completed []fetchKey
		for _, k := range c.results.keys() {
			if entry, _ := c.results.get(k); entry.flight.isDone() {
				completed = append(completed, k)
			}
		}
		for c.results.len() >= c.cacheLimit && len(completed) > 0 {
			c.results.delete(completed[0])
			completed = completed[1:]
		}
		f = newFlight()
		tracked = c.results.len() < c.cacheLimit
		if tracked {
			c.results.set(key, fetchEntry{f, inf})
		}
	} else {
		entry, _ := c.results.get(key)
		f = entry.flight
	}
	c.mu.Unlock()
	if !owner {
		clientLog.Info("复用订阅近期结果或等待正在进行的拉取")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return f.result, f.err
	}
	var result *FetchResult
	completed := false
	defer func() {
		if !completed {
			// A panic: waiters see an error instead of hanging.
			c.failed(key, f, tracked, fmt.Errorf("subscription fetch panicked"))
		}
	}()
	if c.fetch != nil {
		result, err = c.fetch(ctx, subID, url, target)
	} else {
		result, err = c.fetchSubscription(ctx, subID, url, target)
	}
	completed = true
	if canceled := ctx.Err(); canceled != nil {
		err = canceled
	}
	if err != nil {
		c.failed(key, f, tracked, err)
		return nil, err
	}
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.results.delete(key)
		f.finish(nil, err)
		c.mu.Unlock()
		return nil, err
	}
	f.finish(result, nil)
	c.failures.delete(key)
	if tracked {
		c.results.set(key, fetchEntry{f, c.env.Clock.Monotonic() + FetchReuseSeconds})
	}
	c.mu.Unlock()
	return result, nil
}

func (c *Client) failed(key fetchKey, f *flight, tracked bool, err error) {
	f.finish(nil, err)
	c.mu.Lock()
	defer c.mu.Unlock()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		c.results.delete(key)
		return
	}
	if !tracked {
		return
	}
	previous, known := c.failures.get(key)
	failures := previous.count + 1
	if failures > 8 {
		failures = 8
	}
	if !known && c.failures.len() >= c.cacheLimit {
		c.failures.delete(c.failures.keys()[0])
	}
	c.failures.set(key, failureEntry{failures, c.env.Clock.Monotonic()})
	status := 0
	if fetchErr, ok := err.(*SubscriptionFetchError); ok && fetchErr.DirectError != nil {
		if s, ok := fetchErr.DirectError.(statusCarrier); ok {
			status = s.HTTPStatus()
		}
	}
	cooldown := math.Min(300, 3*math.Pow(2, float64(failures-1)))
	if status == 403 || status == 429 {
		cooldown = math.Max(BlockedReuseSeconds, cooldown)
	}
	c.results.set(key, fetchEntry{f, c.env.Clock.Monotonic() + cooldown})
}

func blocked(err error) bool {
	s, ok := err.(statusCarrier)
	return ok && (s.HTTPStatus() == 403 || s.HTTPStatus() == 429)
}

// fetchSubscription is _get_subscription_proxies_yaml: the target format
// text and its real source. A plain http(s) URL is downloaded once (body
// and traffic); when it is not already the target YAML, Sub-Store converts
// the downloaded body. URLs with a fragment (Sub-Store options) go to
// Sub-Store first and are downloaded directly only when it fails.
func (c *Client) fetchSubscription(ctx context.Context, subID, url, target string) (*FetchResult, error) {
	var rawText *string
	var traffic *Traffic
	var directErr error
	lowered := strings.ToLower(url)
	directAttempted := !strings.Contains(url, "#") &&
		(strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://"))
	if directAttempted {
		result, err := func() (*FetchResult, error) {
			clientLog.Info("拉取订阅正文与流量信息（单次请求）")
			resp, err := c.get(ctx, url, clashHeader(), nil, 30)
			if err != nil {
				return nil, err
			}
			text := resp.Text()
			rawText = &text
			parsed, err := ReadUserinfo(resp.HeaderValue("subscription-userinfo"))
			if err != nil {
				return nil, err
			}
			traffic = parsed
			if isYAMLResponse(text) && target == "ClashMeta" {
				source := "direct_yaml"
				if strings.Contains(strings.ToLower(resp.HeaderValue("x-powered-by")), "sub-store") {
					source = "rendered_yaml"
				}
				return newFetchResult(text, source, traffic, "", c.env.Clock), nil
			}
			return nil, nil
		}()
		if result != nil {
			return result, nil
		}
		if err != nil {
			if canceled := ctx.Err(); canceled != nil {
				return nil, canceled
			}
			directErr = err
			if blocked(err) {
				// A blocked/rate-limited upstream must not be retried through
				// Sub-Store immediately; that would compound the restriction.
				clientLog.Warning("订阅上游拒绝或限流，暂缓拉取: " + safeExceptionDetails(err))
				return nil, &SubscriptionFetchError{nil, err}
			}
			clientLog.Warning("直接拉取订阅失败，尝试 Sub-Store: " + safeExceptionDetails(err))
		}
	}

	base, err := c.env.BaseURL()
	if err != nil {
		return nil, err
	}
	clientLog.Info("Sub-Store base URL: " + weburl.SafeForLog(base))
	// A prefix and a random suffix: no clash with the user's subscriptions
	// nor with concurrent requests for the same subscription.
	tempName := "_cf_tmp_" + subID + "_" + c.env.RandomHex()
	var result *FetchResult
	subStoreErr := c.runtime.Use(ctx, func(ctx context.Context) error {
		c.createSubscription(ctx, base, tempName, url, rawText)
		defer c.deleteSubscription(ctx, base, tempName)
		downloadURL := base + "/download/" + QuotePath(tempName) + "/" + target
		clientLog.Info("Sub-Store 下载: " + weburl.SafeForLog(downloadURL))
		resp, err := c.get(ctx, downloadURL, nil, []Param{{"noFlow", "true"}}, 30)
		if err != nil {
			return err
		}
		text := resp.Text()
		if !isYAMLResponse(text) {
			return fmt.Errorf(msgConvertedYAML, preview(text))
		}
		if traffic == nil {
			parsed, err := ReadUserinfo(resp.HeaderValue("subscription-userinfo"))
			if err != nil {
				return err
			}
			traffic = parsed
		}
		result = newFetchResult(text, "sub_store", traffic, "", c.env.Clock)
		return nil
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if subStoreErr == nil {
		return result, nil
	}
	clientLog.Warning("Sub-Store 获取订阅失败: " + safeExceptionDetails(subStoreErr))

	// Do not re-download a URL already attempted above, even on conversion failure.
	if directAttempted {
		if directErr == nil {
			directErr = ErrBodyConversion
		}
		return nil, &SubscriptionFetchError{subStoreErr, directErr}
	}
	text, err := c.fetchDirectSubscriptionYAML(ctx, url)
	if err != nil {
		clientLog.Warning("直接拉取订阅失败: " + safeExceptionDetails(err))
		return nil, &SubscriptionFetchError{subStoreErr, err}
	}
	return newFetchResult(text, "direct_url_fallback", nil, "", c.env.Clock), nil
}

// nodeConversionVersion is _node_conversion_version: the bundled
// Sub-Store's version, "" (None) where conversions are not kept.
func (c *Client) nodeConversionVersion() string {
	if !c.env.IsBuiltinAvailable() {
		return ""
	}
	_, version, err := c.env.ActiveBundle()
	if err != nil {
		return ""
	}
	return version
}

// ConvertProxy converts an opaque node through Sub-Store to a caller-owned YAML graph.
func (c *Client) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	return c.ConvertTarget(ctx, text, "ClashMeta")
}

// ConvertTarget caches successful independent node graphs by bundle version and target.
func (c *Client) ConvertTarget(ctx context.Context, text, target string) (*yamlx.Node, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	version := c.nodeConversionVersion()
	key := convKey{version, target, text}
	if version != "" {
		c.convMu.Lock()
		if err := ctx.Err(); err != nil {
			c.convMu.Unlock()
			return nil, err
		}
		kept, ok := c.conversions.get(key)
		if ok {
			c.conversions.moveToEnd(key)
			c.convMu.Unlock()
			return yamlx.CopyGraph(kept), nil
		}
		c.convMu.Unlock()
	}
	var proxy *yamlx.Node
	var err error
	if c.convert != nil {
		proxy, err = c.convert(ctx, text, target)
	} else {
		proxy, err = c.convertProxy(ctx, text, target)
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	if proxy != nil && version != "" && c.nodeConversionVersion() == version {
		c.convMu.Lock()
		defer c.convMu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.conversions.set(key, yamlx.CopyGraph(proxy))
		c.conversions.moveToEnd(key)
		for c.conversions.len() > c.conversionLimit {
			c.conversions.delete(c.conversions.keys()[0])
		}
	}
	return proxy, nil
}

// convertProxy requests one temporary conversion. The log lines carry only the
// scheme and the length, never the node string.
func (c *Client) convertProxy(ctx context.Context, proxyString, target string) (*yamlx.Node, error) {
	base, err := c.env.BaseURL()
	if err != nil {
		// Invalid local configuration fails before creating a temporary subscription.
		clientLog.Warning("节点转换失败: " + err.Error())
		return nil, err
	}
	proxyType := proxyTypeOf(proxyString)
	inputLength := utf8.RuneCountInString(proxyString)
	clientLog.Info(fmt.Sprintf("节点转换开始: proxy_type=%s input_length=%d", proxyType, inputLength))

	tempName := "_cf_tmp_node_convert_" + c.env.RandomHex()
	var converted *yamlx.Node
	err = c.runtime.Use(ctx, func(ctx context.Context) error {
		c.createSubscription(ctx, base, tempName, "https://example.com", nil)
		defer c.deleteSubscription(ctx, base, tempName)
		// The content parameter carries the node instead of the subscription URL.
		downloadURL := base + "/download/" + QuotePath(tempName) + "/" + target
		resp, err := c.get(ctx, downloadURL, nil, []Param{{"content", proxyString}}, 15)
		if err != nil {
			return err
		}
		text := resp.Text()
		if !isYAMLResponse(text) {
			clientLog.Warning(fmt.Sprintf("节点转换返回无效内容: proxy_type=%s input_length=%d", proxyType, inputLength))
			return nil
		}
		proxies, err := ParseProxyNodes(text)
		if err != nil {
			return err
		}
		if len(proxies) > 0 {
			clientLog.Info(fmt.Sprintf("节点转换成功: proxy_type=%s input_length=%d", proxyType, inputLength))
			converted = proxies[0]
			return nil
		}
		clientLog.Warning(fmt.Sprintf("节点转换结果为空: proxy_type=%s input_length=%d", proxyType, inputLength))
		return nil
	})
	if err != nil {
		clientLog.Warning(fmt.Sprintf("节点转换失败: proxy_type=%s input_length=%d", proxyType, inputLength))
		return nil, err
	}
	return converted, nil
}

// proxyTypeOf is the scheme _convert_proxy_string logs (at most 32
// characters, "unknown" unless alphanumeric apart from "+", "-", ".").
func proxyTypeOf(proxyString string) string {
	if scheme, _, found := strings.Cut(proxyString, "://"); found {
		candidate := cases.Lower(language.Und).String(scheme)
		stripped := strings.NewReplacer("+", "", "-", "", ".", "").Replace(candidate)
		if strings.IndexFunc(stripped, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) < 0 && stripped != "" {
			return runePrefix(candidate, 32)
		}
	}
	return "unknown"
}
