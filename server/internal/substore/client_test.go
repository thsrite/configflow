package substore

// Ports of backend/test_subscription_fetch_requests.py,
// test_sub_store_no_flow.py, the client cases of test_sub_store_runtime.py
// and the sub_store_client cases of test_request_exception_redaction.py.
// Every upstream is a loopback server: a fake airport and a fake Sub-Store.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

const testYAML = "proxies: [{name: A, type: ss, server: example.test, port: 443, cipher: aes-128-gcm, password: p}]"
const testFlow = "upload=1; download=2; total=100"

// external is the Python fixture `isolated`: _get_base_url patched to the
// test Sub-Store, not the bundled one (no lease, no process control).
func external(t *testing.T) (*harness, *Client) {
	h := newHarness(t, harnessOptions{})
	return h, h.service.Client
}

func closedURL(t *testing.T) string {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + "/sub?token=private"
}

func fetch(t *testing.T, c *Client, subID, url string, opts FetchOptions) *SubscriptionResult {
	t.Helper()
	result, err := c.GetSubscriptionProxiesYAML(context.Background(), subID, url, "", opts)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return result
}

// test_subscription_fetch_requests.py::test_yaml_and_traffic_use_one_get_and_no_sub_store
func TestYAMLAndTrafficUseOneGetAndNoSubStore(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText(testYAML, map[string]string{"subscription-userinfo": testFlow}))
	result := fetch(t, c, "s", airport.url(), FetchOptions{})
	if result.Text != testYAML || result.Source != "direct_yaml" {
		t.Fatalf("result %q %q", result.Text, result.Source)
	}
	if jsonx.Repr(result.Traffic) != "{'upload': 1, 'download': 2, 'total': 100}" {
		t.Fatalf("traffic %s", jsonx.Repr(result.Traffic))
	}
	if airport.count() != 1 || len(h.store.eventList()) != 0 {
		t.Fatalf("airport %d, sub-store %v", airport.count(), h.store.eventList())
	}
	if airport.agents[0] != "clash.meta" {
		t.Fatalf("user agent %q", airport.agents[0])
	}
}

// test_subscription_fetch_requests.py::test_raw_body_converted_locally_without_second_airport_get
func TestRawBodyConvertedLocallyWithoutSecondAirportGet(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText("ss://encoded-node", map[string]string{"subscription-userinfo": testFlow}))
	result := fetch(t, c, "s", airport.url(), FetchOptions{})
	if result.Source != "sub_store" || result.Traffic.GetInt("total") != 100 {
		t.Fatalf("result %s %s", result.Source, jsonx.Repr(result.Traffic))
	}
	if airport.count() != 1 {
		t.Fatalf("airport hit %d times", airport.count())
	}
	payload := h.store.createdPayloads()[0]
	if payload.GetString("source") != "local" || payload.GetString("content") != "ss://encoded-node" ||
		payload.Value("noFlow") != true {
		t.Fatalf("payload %s", jsonx.Repr(payload))
	}
	if h.store.downloads[0].Get("noFlow") != "true" {
		t.Fatalf("download query %v", h.store.downloads[0])
	}
	if events := h.store.eventList(); len(events) != 3 || events[0] != "create:_cf_tmp_s_a0000001" ||
		events[1] != "download:_cf_tmp_s_a0000001" || events[2] != "delete:_cf_tmp_s_a0000001" {
		t.Fatalf("events %v", events)
	}
}

// test_subscription_fetch_requests.py::test_rendered_yaml_keeps_source_and_no_second_request
func TestRenderedYAMLKeepsSourceAndNoSecondRequest(t *testing.T) {
	_, c := external(t)
	airport := newFakeAirport(t, serveText(testYAML, map[string]string{"x-powered-by": "Sub-Store"}))
	if source := fetch(t, c, "s", airport.url(), FetchOptions{}).Source; source != "rendered_yaml" {
		t.Fatalf("source %s", source)
	}
	if airport.count() != 1 {
		t.Fatal("second request")
	}
}

// test_subscription_fetch_requests.py::test_direct_transport_failure_can_still_use_sub_store
// (a refused connection instead of requests.Timeout: both are transport
// failures).
func TestDirectTransportFailureCanStillUseSubStore(t *testing.T) {
	h, c := external(t)
	if source := fetch(t, c, "s", closedURL(t), FetchOptions{}).Source; source != "sub_store" {
		t.Fatalf("source %s", source)
	}
	if h.store.createdPayloads()[0].Has("content") {
		t.Fatal("content sent without a downloaded body")
	}
}

// test_subscription_fetch_requests.py::test_fragment_options_go_to_sub_store_without_probe
func TestFragmentOptionsGoToSubStoreWithoutProbe(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText(testYAML, nil))
	u := airport.url() + `#{"insecure":true,"userAgent":"custom"}`
	if source := fetch(t, c, "s", u, FetchOptions{}).Source; source != "sub_store" {
		t.Fatalf("source %s", source)
	}
	if airport.count() != 0 || len(h.store.downloads) != 1 || h.store.downloads[0].Get("noFlow") != "true" {
		t.Fatalf("airport %d downloads %v", airport.count(), h.store.downloads)
	}
	payload := h.store.createdPayloads()[0]
	if payload.GetString("url") != u || payload.Has("content") {
		t.Fatalf("payload %s", jsonx.Repr(payload))
	}
}

// test_subscription_fetch_requests.py::test_concurrent_and_recent_refreshes_across_ids_share_one_get
func TestConcurrentAndRecentRefreshesAcrossIDsShareOneGet(t *testing.T) {
	_, c := external(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	airport := newFakeAirport(t, func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		w.Write([]byte(testYAML))
	})
	u := airport.url()
	results := make([]*SubscriptionResult, 5)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); results[0] = fetch(t, c, "manual", u, FetchOptions{}) }()
	if !waitFor(t, entered, 5*time.Second) {
		t.Fatal("not entered")
	}
	for i := 1; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = fetch(t, c, fmt.Sprintf("agg-%d", i), u, FetchOptions{Refresh: true})
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for _, r := range results {
		if r != results[0] {
			t.Fatal("results differ")
		}
	}
	if fetch(t, c, "another-profile", u, FetchOptions{}) != results[0] {
		t.Fatal("recent result not reused")
	}
	if airport.count() != 1 {
		t.Fatalf("airport hit %d times", airport.count())
	}
}

// test_subscription_fetch_requests.py::test_reuse_expires_and_changed_url_fetches_again
func TestReuseExpiresAndChangedURLFetchesAgain(t *testing.T) {
	h, c := external(t)
	var seen []string
	var mu sync.Mutex
	airport := newFakeAirport(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RawQuery)
		mu.Unlock()
		w.Write([]byte(testYAML))
	})
	u := airport.url()
	fetch(t, c, "s", u, FetchOptions{})
	h.clock.set(299)
	fetch(t, c, "s", u, FetchOptions{})
	fetch(t, c, "s", u+"&changed=1", FetchOptions{})
	h.clock.set(300)
	fetch(t, c, "s", u, FetchOptions{})
	if strings.Join(seen, "|") != "token=private|token=private&changed=1|token=private" {
		t.Fatalf("seen %v", seen)
	}
}

// test_subscription_fetch_requests.py::test_manual_refresh_debounces_then_renews_shared_automatic_cache
func TestManualRefreshDebouncesThenRenewsSharedAutomaticCache(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText(testYAML, nil))
	u := airport.url()
	step := func(at float64, subID string, refresh bool, want int) {
		t.Helper()
		h.clock.set(at)
		fetch(t, c, subID, u, FetchOptions{Refresh: refresh})
		if airport.count() != want {
			t.Fatalf("at %v: %d downloads, want %d", at, airport.count(), want)
		}
	}
	step(0, "auto", false, 1)
	step(29, "manual", true, 1)
	step(30, "manual", true, 2)
	step(329, "auto", false, 2)
	step(330, "auto", false, 3)
}

// test_subscription_fetch_requests.py::test_conversion_failure_does_not_redownload_body
func TestConversionFailureDoesNotRedownloadBody(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText("ss://node", nil))
	h.store.download = func(string, string, url.Values, *jsonx.Object) (int, string, http.Header) {
		return 200, "<html>failure</html>", nil
	}
	_, err := c.GetSubscriptionProxiesYAML(context.Background(), "s", airport.url(), "", FetchOptions{})
	var fetchErr *SubscriptionFetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("err %v", err)
	}
	if airport.count() != 1 {
		t.Fatalf("airport hit %d times", airport.count())
	}
	if fetchErr.DirectError.Error() != "已拉取的订阅正文无法转换为目标 YAML" || pythonClass(fetchErr.DirectError) != "ValueError" {
		t.Fatalf("direct error %v", fetchErr.DirectError)
	}
	if !strings.HasPrefix(fetchErr.SubStoreError.Error(), "Sub-Store 返回了非 YAML 内容（可能是 HTML 页面），请检查 Sub-Store URL 配置是否正确。响应预览: <html>failure</html>") {
		t.Fatalf("sub-store error %v", fetchErr.SubStoreError)
	}
}

// test_subscription_fetch_requests.py::test_completed_cache_entries_are_bounded
func TestCompletedCacheEntriesAreBounded(t *testing.T) {
	_, c := external(t)
	c.cacheLimit = 2
	airport := newFakeAirport(t, serveText(testYAML, nil))
	for i := 0; i < 10; i++ {
		fetch(t, c, "s", fmt.Sprintf("%s&i=%d", airport.url(), i), FetchOptions{})
	}
	if c.results.len() != 2 {
		t.Fatalf("%d entries", c.results.len())
	}
}

// test_subscription_fetch_requests.py::test_background_reuses_recent_body_but_expires_at_fifteen_minutes
func TestBackgroundReusesRecentBodyButExpiresAtFifteenMinutes(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText(testYAML, nil))
	u := airport.url()
	fetch(t, c, "auto", u, FetchOptions{})
	for _, moment := range []float64{300, 899} {
		h.clock.set(moment)
		fetch(t, c, "background", u, FetchOptions{PreferCached: true})
	}
	if airport.count() != 1 {
		t.Fatalf("%d downloads", airport.count())
	}
	h.clock.set(900)
	fetch(t, c, "background", u, FetchOptions{PreferCached: true})
	if airport.count() != 2 {
		t.Fatalf("%d downloads", airport.count())
	}
}

// test_subscription_fetch_requests.py::test_failure_backoff_caps_and_success_resets_it
func TestFailureBackoffCapsAndSuccessResetsIt(t *testing.T) {
	h, c := external(t)
	var seen []float64
	failing := true
	c.fetch = func(context.Context, string, string, string) (*SubscriptionResult, error) {
		seen = append(seen, h.clock.Monotonic())
		if failing {
			return nil, &SubscriptionFetchError{nil, &RequestError{Class: "Timeout", Message: ""}}
		}
		return NewSubscriptionResult(testYAML, "direct_yaml", nil, "", h.clock), nil
	}
	const u = "https://airport.test/sub?token=private"
	ctx := context.Background()
	for _, cooldown := range []float64{3, 6, 12, 24, 48, 96, 192, 300, 300} {
		if _, err := c.GetSubscriptionProxiesYAML(ctx, "s", u, "", FetchOptions{}); err == nil {
			t.Fatal("no error")
		}
		count := len(seen)
		h.clock.set(h.clock.Monotonic() + cooldown - 0.1)
		if _, err := c.GetSubscriptionProxiesYAML(ctx, "s", u, "", FetchOptions{Refresh: true}); err == nil {
			t.Fatal("no error during the cooldown")
		}
		if len(seen) != count {
			t.Fatalf("retried during the %v s cooldown", cooldown)
		}
		h.clock.set(h.clock.Monotonic() + 0.1)
	}
	failing = false
	fetch(t, c, "s", u, FetchOptions{})
	if c.failures.len() != 0 {
		t.Fatal("failures kept after a success")
	}
	failing = true
	h.clock.set(h.clock.Monotonic() + 300)
	if _, err := c.GetSubscriptionProxiesYAML(ctx, "s", u, "", FetchOptions{}); err == nil {
		t.Fatal("no error")
	}
	entry, _ := c.results.get(c.results.keys()[0])
	if entry.expires != h.clock.Monotonic()+3 {
		t.Fatalf("expires %v, now %v", entry.expires, h.clock.Monotonic())
	}
}

// test_subscription_fetch_requests.py::test_failed_bursts_share_error_but_retry_after_three_seconds
func TestFailedBurstsShareErrorButRetryAfterThreeSeconds(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, func(w http.ResponseWriter, r *http.Request) { hijack(w) })
	h.store.download = func(string, string, url.Values, *jsonx.Object) (int, string, http.Header) { return -1, "", nil }
	gets := func() int { return airport.count() + len(h.store.downloads) }
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := c.GetSubscriptionProxiesYAML(ctx, "s", airport.url(), "", FetchOptions{}); err == nil {
			t.Fatal("no error")
		}
	}
	if gets() != 2 { // direct GET + Sub-Store attempt, not five repetitions
		t.Fatalf("%d GETs", gets())
	}
	h.clock.set(4)
	if _, err := c.GetSubscriptionProxiesYAML(ctx, "s", airport.url(), "", FetchOptions{}); err == nil {
		t.Fatal("no error")
	}
	if gets() != 4 {
		t.Fatalf("%d GETs", gets())
	}
}

// test_subscription_fetch_requests.py::test_blocked_upstream_is_not_retried_via_sub_store
func TestBlockedUpstreamIsNotRetriedViaSubStore(t *testing.T) {
	for _, status := range []int{403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h, c := external(t)
			airport := newFakeAirport(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
			ctx := context.Background()
			for _, moment := range []float64{0, 5, 30, 59} {
				h.clock.set(moment)
				_, err := c.GetSubscriptionProxiesYAML(ctx, "s", airport.url(), "", FetchOptions{Refresh: true})
				var fetchErr *SubscriptionFetchError
				if !errors.As(err, &fetchErr) || fetchErr.SubStoreError != nil || StatusOf(fetchErr.DirectError) != status {
					t.Fatalf("err %#v", err)
				}
			}
			if airport.count() != 1 || len(h.store.eventList()) != 0 {
				t.Fatalf("airport %d sub-store %v", airport.count(), h.store.eventList())
			}
			h.clock.set(61)
			c.GetSubscriptionProxiesYAML(ctx, "s", airport.url(), "", FetchOptions{})
			if airport.count() != 2 {
				t.Fatalf("airport %d", airport.count())
			}
		})
	}
}

// test_sub_store_no_flow.py::test_subscription_temp_sub_disables_flow_info
func TestSubscriptionTempSubDisablesFlowInfo(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText("ss://example", nil))
	fetch(t, c, "sub_1", airport.srv.URL+"/c/?token=abc", FetchOptions{})
	if got := h.store.createdPayloads()[0].GetString("url"); got != airport.srv.URL+"/c/?token=abc#noFlow" {
		t.Fatalf("url %q", got)
	}
}

// test_sub_store_no_flow.py::test_node_convert_temp_sub_disables_flow_info
func TestNodeConvertTempSubDisablesFlowInfo(t *testing.T) {
	h, c := external(t)
	c.ConvertProxyString(context.Background(), "ss://YWVzLTEyOC1nY206cA@1.1.1.1:1#a")
	payloads := h.store.createdPayloads()
	if len(payloads) != 1 || payloads[0].GetString("url") != "https://example.com#noFlow" {
		t.Fatalf("payloads %s", jsonx.Repr(payloads))
	}
}

// test_sub_store_no_flow.py::test_url_with_existing_fragment_is_left_unchanged
func TestURLWithExistingFragmentIsLeftUnchanged(t *testing.T) {
	h, c := external(t)
	u := `http://airport.test/sub#{"insecure":true}`
	fetch(t, c, "sub_1", u, FetchOptions{})
	if got := h.store.createdPayloads()[0].GetString("url"); got != u {
		t.Fatalf("url %q", got)
	}
}

// test_sub_store_runtime.py::test_client_holds_lease_until_cleanup_even_on_failure:
// with the bundled Sub-Store, the shared work lock is held through the
// temporary subscription's deletion even when the download fails.
func TestClientHoldsLeaseUntilCleanupEvenOnFailure(t *testing.T) {
	for _, entry := range []string{"node", "subscription"} {
		t.Run(entry, func(t *testing.T) {
			h := newHarness(t, harnessOptions{builtin: true})
			h.store.setVersion("2.40.0")
			c, rt := h.service.Client, h.service.Runtime
			h.store.download = func(string, string, url.Values, *jsonx.Object) (int, string, http.Header) {
				return 500, "bad conversion", nil
			}
			leased := false
			h.store.onDelete = func() {
				err := rt.lock("work", true, 0, func() error { return nil })
				leased = IsRuntimeUnavailable(err)
			}
			ctx := context.Background()
			if entry == "node" {
				if c.ConvertProxyString(ctx, "ss://test") != nil {
					t.Fatal("conversion did not fail")
				}
			} else {
				airport := newFakeAirport(t, serveText("proxies: []", nil))
				result, err := c.fetchSubscription(ctx, "id", airport.srv.URL+"/#options", "ClashMeta")
				if err != nil || result.Source != "direct_url_fallback" {
					t.Fatalf("result %v err %v", result, err)
				}
			}
			events := h.store.eventList()
			if len(events) != 3 || !strings.HasPrefix(events[0], "create:") || !strings.HasPrefix(events[1], "download:") ||
				!strings.HasPrefix(events[2], "delete:") {
				t.Fatalf("events %v", events)
			}
			if !leased {
				t.Fatal("lease released before cleanup")
			}
			if err := rt.lock("work", true, 0, func() error { return nil }); err != nil {
				t.Fatalf("lease kept after cleanup: %v", err)
			}
			if stateOf(rt).Value("last_used") == nil {
				t.Fatal("idle time not recorded")
			}
		})
	}
}

// test_sub_store_runtime.py::test_direct_yaml_does_not_take_lease
func TestDirectYAMLDoesNotTakeLease(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	airport := newFakeAirport(t, serveText("proxies: []", nil))
	result, err := h.service.Client.fetchSubscription(context.Background(), "id", airport.srv.URL+"/sub", "ClashMeta")
	if err != nil || result.Source != "direct_yaml" {
		t.Fatalf("result %v err %v", result, err)
	}
	if starts, _ := h.commands.counts(); starts != 0 || h.store.envRequests != 0 || len(h.store.eventList()) != 0 {
		t.Fatal("unnecessary wakeup")
	}
}

// test_request_exception_redaction.py::test_direct_subscription_fetch_log_does_not_include_subscription_url
func TestDirectSubscriptionFetchLogDoesNotIncludeSubscriptionURL(t *testing.T) {
	logs := captureLogs(t)
	_, c := external(t)
	airport := newFakeAirport(t, serveText("proxies:\n  - {name: safe-name, type: vless, server: safe.test, port: 443}\n",
		map[string]string{"Content-Type": "text/yaml"}))
	secret := airport.srv.URL + "/sub?token=dynamic-request-secret#secret-fragment"
	text, err := c.fetchDirectSubscriptionYAML(context.Background(), secret)
	if err != nil || !strings.Contains(text, "proxies:") {
		t.Fatalf("text %q err %v", text, err)
	}
	for _, forbidden := range []string{airport.srv.Listener.Addr().String(), "dynamic-request-secret", "secret-fragment"} {
		if strings.Contains(logs.text(), forbidden) {
			t.Fatalf("log contains %q", forbidden)
		}
	}
	if !strings.Contains(logs.text(), "Direct subscription fetch started") {
		t.Fatal("start line missing")
	}
}

// test_request_exception_redaction.py::test_convert_proxy_string_logs_only_event_type_and_length
func TestConvertProxyStringLogsOnlyEventTypeAndLength(t *testing.T) {
	logs := captureLogs(t)
	h, c := external(t)
	h.store.download = func(string, string, url.Values, *jsonx.Object) (int, string, http.Header) {
		return 200, "proxies:\n  - {name: safe-name, type: vless, server: safe.test, port: 443}\n", http.Header{"Content-Type": {"text/yaml"}}
	}
	proxyURI := "vless://user-secret@example.test:443?token=converter-secret#fragment-secret"
	result := c.ConvertProxyString(context.Background(), proxyURI)
	if result.GetString("type") != "vless" {
		t.Fatalf("result %s", jsonx.Repr(result))
	}
	text := logs.text()
	for _, forbidden := range []string{"vless://", "user-secret", "example.test", "converter-secret", "fragment-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("log contains %q:\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, "proxy_type=vless") || !strings.Contains(text, fmt.Sprintf("input_length=%d", len(proxyURI))) {
		t.Fatalf("log:\n%s", text)
	}
	if h.store.downloads[0].Get("content") != proxyURI {
		t.Fatalf("content %q", h.store.downloads[0].Get("content"))
	}
}

// test_request_exception_redaction.py::test_sub_store_combined_requests_errors_keep_causes_without_leaking
func TestSubStoreCombinedRequestsErrorsKeepCausesWithoutLeaking(t *testing.T) {
	logs := captureLogs(t)
	h, c := external(t)
	// Sub-Store's download fails first (a refused connection), then the
	// direct fallback (a read timeout of a fragment URL).
	h.setenv("SUB_STORE_URL", strings.TrimSuffix(closedURL(t), "/sub?token=private"))
	slow := newFakeAirport(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(400 * time.Millisecond) })
	h.service.Env.HTTP = timeoutClient{RequestsClient{}, 100 * time.Millisecond}
	secret := slow.srv.URL + "/sub?token=dynamic-request-secret#secret-fragment"
	_, err := c.GetSubscriptionProxiesYAML(context.Background(), "sub-1", secret, "", FetchOptions{})
	var fetchErr *SubscriptionFetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("err %v", err)
	}
	if pythonClass(fetchErr.SubStoreError) != "ConnectionError" || pythonClass(fetchErr.DirectError) != "ReadTimeout" {
		t.Fatalf("causes %s / %s", pythonClass(fetchErr.SubStoreError), pythonClass(fetchErr.DirectError))
	}
	if errors.Unwrap(err) != fetchErr.DirectError {
		t.Fatal("__cause__ is not the direct error")
	}
	text := logs.text()
	if strings.Contains(err.Error(), "dynamic-request-secret") || strings.Contains(text, "dynamic-request-secret") ||
		strings.Contains(text, slow.srv.Listener.Addr().String()) {
		t.Fatalf("leak:\n%s", text)
	}
	if !strings.Contains(text, "exception_type=ConnectionError") || !strings.Contains(text, "exception_type=ReadTimeout") {
		t.Fatalf("log:\n%s", text)
	}
}

// timeoutClient shortens every request's timeouts.
type timeoutClient struct {
	inner   HTTPClient
	timeout time.Duration
}

func (c timeoutClient) Do(ctx context.Context, req *Request) (*Response, error) {
	copied := *req
	copied.Timeout = Timeout{Connect: c.timeout, Read: c.timeout}
	return c.inner.Do(ctx, &copied)
}

// Shared results are cleaned up after the retention (Python's sweep at the
// start of every call).
func TestSweepDropsExpiredResultsAndOldFailures(t *testing.T) {
	h, c := external(t)
	airport := newFakeAirport(t, serveText(testYAML, nil))
	fetch(t, c, "s", airport.url(), FetchOptions{})
	h.clock.set(899.9)
	fetch(t, c, "s", airport.url()+"&other=1", FetchOptions{})
	if c.results.len() != 2 {
		t.Fatalf("%d results", c.results.len())
	}
	h.clock.set(900)
	fetch(t, c, "s", airport.url()+"&other=1", FetchOptions{})
	if c.results.len() != 1 {
		t.Fatalf("%d results after 15 minutes", c.results.len())
	}
}
