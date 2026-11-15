package rulefetch

// Ported from backend/test_rule_fetch_retries.py and
// test_rule_fetch_fallback.py with a fake Transport: the Python tests
// monkeypatch requests.get with exceptions built from urllib3/ssl classes;
// here the fake returns FetchError values with the class and retry decision
// HTTPTransport derives from the corresponding Go network errors (that
// mapping is exercised over real sockets in integration_test.go).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

const (
	retrySource = "https://raw.githubusercontent.com/owner/repo/main/rules.list"
	retryMirror = "https://mirror.test/"
)

type fakeCall struct {
	req TransportRequest
}

type fakeTransport struct {
	calls []fakeCall
	reply func(n int, req TransportRequest) (*Response, error)
}

func (f *fakeTransport) Do(_ context.Context, req TransportRequest) (*Response, error) {
	f.calls = append(f.calls, fakeCall{req})
	return f.reply(len(f.calls), req)
}

func (f *fakeTransport) urls() []string {
	out := []string{}
	for _, c := range f.calls {
		out = append(out, c.req.URL)
	}
	return out
}

type trackedBody struct {
	io.Reader
	closed *[]string
	url    string
}

func (b *trackedBody) Close() error {
	*b.closed = append(*b.closed, b.url)
	return nil
}

func reply(status int, content string) *Response {
	return &Response{StatusCode: status, Reason: http.StatusText(status), Header: http.Header{}, URL: retrySource,
		Body: io.NopCloser(strings.NewReader(content))}
}

func newClient(transport Transport, sleeps *[]time.Duration) *Client {
	return &Client{Transport: transport, Sleep: func(d time.Duration) { *sleeps = append(*sleeps, d) }}
}

func sysConfig(kv ...any) *jsonx.Object { return jsonx.Obj("system_config", jsonx.Obj(kv...)) }

func tlsEOF() error {
	return &FetchError{Class: "SSLError", Message: "[SSL: UNEXPECTED_EOF_WHILE_READING] EOF occurred in violation of protocol", Transient: true}
}

func seconds(values ...int) []time.Duration {
	out := []time.Duration{}
	for _, v := range values {
		out = append(out, time.Duration(v)*time.Second)
	}
	return out
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n") && len(a) == len(b)
}

func repeat(s string, n int) []string {
	out := []string{}
	for i := 0; i < n; i++ {
		out = append(out, s)
	}
	return out
}

// test_tls_eof_recovers_on_third_attempt_with_same_options
func TestTLSEOFRecoversOnThirdAttemptWithSameOptions(t *testing.T) {
	for _, method := range []string{"get", "head"} {
		for _, proxy := range []string{"", "http://proxy.test:7890"} {
			fake := &fakeTransport{reply: func(n int, _ TransportRequest) (*Response, error) {
				if n < 3 {
					return nil, tlsEOF()
				}
				return reply(200, "complete rule file"), nil
			}}
			var sleeps []time.Duration
			config := sysConfig("rule_fetch_proxy", proxy, "github_proxy_domain", retryMirror)
			response, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource,
				Request{Method: method, Config: config, ConnectTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if content, _ := response.Content(); string(content) != "complete rule file" {
				t.Fatal(string(content))
			}
			if len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != 2*time.Second || !equalStrings(fake.urls(), repeat(retrySource, 3)) {
				t.Fatal(sleeps, fake.urls())
			}
			for _, c := range fake.calls {
				if c.req.Insecure || c.req.ConnectTimeout != 10*time.Second || c.req.ReadTimeout != 60*time.Second ||
					c.req.Proxy != proxy || c.req.Method != strings.ToUpper(method) {
					t.Fatalf("%+v", c.req)
				}
			}
		}
	}
}

// test_disconnect_retries_are_bounded_and_preserve_final_error
func TestDisconnectRetriesAreBoundedAndPreserveFinalError(t *testing.T) {
	for _, class := range []string{"ConnectionError", "ChunkedEncodingError", "ProxyError", "SSLError"} {
		failure := &FetchError{Class: class, Message: "interrupted", Transient: true}
		fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) { return nil, failure }}
		var sleeps []time.Duration
		_, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource, Request{Config: sysConfig()})
		if err != error(failure) || !equalStrings(fake.urls(), repeat(retrySource, 3)) || len(sleeps) != 2 {
			t.Fatal(class, err, fake.urls(), sleeps)
		}
	}
}

// test_persistent_errors_and_timeouts_do_not_retry,
// test_certificate_error_takes_precedence_over_disconnect_in_exception_chain,
// test_read_timeout_is_not_retried_even_with_disconnect_context
func TestPersistentErrorsAndTimeoutsDoNotRetry(t *testing.T) {
	for _, class := range []string{"SSLError", "ProxyError", "ConnectTimeout", "ReadTimeout", "ConnectionError", "ContentDecodingError", "ChunkedEncodingError"} {
		failure := &FetchError{Class: class, Message: "persistent"}
		fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) { return nil, failure }}
		var sleeps []time.Duration
		_, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource, Request{Config: sysConfig()})
		if PythonClass(err) != class || len(fake.calls) != 1 || len(sleeps) != 0 {
			t.Fatal(class, err, len(fake.calls), sleeps)
		}
	}
}

// test_exhaust_source_before_mirror_and_bound_each_candidate
func TestExhaustSourceBeforeMirror(t *testing.T) {
	for _, mirrorFails := range []bool{false, true} {
		fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
			if req.URL == retrySource || mirrorFails {
				return nil, tlsEOF()
			}
			return reply(200, "complete rule file"), nil
		}}
		var sleeps []time.Duration
		config := sysConfig("github_proxy_domain", retryMirror)
		response, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource, Request{Config: config})
		want := append(repeat(retrySource, 3), repeat(retryMirror+retrySource, 1)...)
		if mirrorFails {
			var fetchErr *RuleFetchError
			if !errors.As(err, &fetchErr) || len(fetchErr.Attempts) != 2 || fetchErr.Attempts[0].Label != "原始地址" || fetchErr.Attempts[1].Label != "GitHub 代理地址" {
				t.Fatal(err)
			}
			want = append(repeat(retrySource, 3), repeat(retryMirror+retrySource, 3)...)
		} else if content, _ := response.Content(); string(content) != "complete rule file" {
			t.Fatal(err)
		}
		if !equalStrings(fake.urls(), want) || len(sleeps) != map[bool]int{false: 2, true: 4}[mirrorFails] {
			t.Fatal(fake.urls(), sleeps)
		}
	}
}

// test_http_failures_go_to_mirror_without_backoff
func TestHTTPFailuresGoToMirrorWithoutBackoff(t *testing.T) {
	for _, status := range []int{403, 404, 429, 503} {
		var closed []string
		fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
			code := 200
			if req.URL == retrySource {
				code = status
			}
			r := reply(code, "x")
			r.Body = &trackedBody{Reader: strings.NewReader("x"), closed: &closed, url: req.URL}
			return r, nil
		}}
		var sleeps []time.Duration
		response, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource, Request{Config: sysConfig("github_proxy_domain", retryMirror)})
		if err != nil || response.StatusCode != 200 || !equalStrings(fake.urls(), []string{retrySource, retryMirror + retrySource}) || len(sleeps) != 0 {
			t.Fatal(status, err, fake.urls())
		}
		if len(closed) == 0 || closed[0] != retrySource {
			t.Fatal(closed)
		}
	}
}

// test_single_url_without_bounded_body_preserves_http_error_response
func TestSingleURLPreservesHTTPErrorResponse(t *testing.T) {
	fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) { return reply(503, ""), nil }}
	var sleeps []time.Duration
	response, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource, Request{Config: sysConfig()})
	if err != nil || response.StatusCode != 503 || len(sleeps) != 0 {
		t.Fatal(err)
	}
}

type brokenBody struct {
	sent   bool
	events *[]string
}

func (b *brokenBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "partial content that must not appear in the final file"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (b *brokenBody) Close() error {
	*b.events = append(*b.events, "close")
	return nil
}

// test_partial_response_is_closed_before_backoff_and_bytes_are_discarded
func TestPartialResponseIsClosedBeforeBackoff(t *testing.T) {
	var events []string
	fake := &fakeTransport{reply: func(n int, _ TransportRequest) (*Response, error) {
		events = append(events, "fetch")
		r := reply(200, "complete rule file")
		if n < 3 {
			r.Body = &brokenBody{events: &events}
		}
		return r, nil
	}}
	client := &Client{Transport: fake, Sleep: func(d time.Duration) { events = append(events, d.String()) }}
	response, err := client.RequestRule(context.Background(), retrySource, Request{Config: sysConfig(), Stream: true, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := response.Content(); string(content) != "complete rule file" {
		t.Fatal(string(content))
	}
	if strings.Join(events, ",") != "fetch,close,1s,fetch,close,2s,fetch" {
		t.Fatal(events)
	}
}

// test_oversize_body_closes_without_retry_or_mirror
func TestOversizeBodyClosesWithoutRetryOrMirror(t *testing.T) {
	var closed []string
	fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
		r := reply(200, "oversize")
		r.Body = &trackedBody{Reader: strings.NewReader("oversize"), closed: &closed, url: req.URL}
		return r, nil
	}}
	var sleeps []time.Duration
	_, err := newClient(fake, &sleeps).RequestRule(context.Background(), retrySource,
		Request{Config: sysConfig("github_proxy_domain", retryMirror), Stream: true, MaxBytes: 1})
	if !errors.Is(err, ErrSizeLimit) || !strings.Contains(err.Error(), "size limit") || !equalStrings(fake.urls(), []string{retrySource}) ||
		!equalStrings(closed, []string{retrySource}) || len(sleeps) != 0 {
		t.Fatal(err, fake.urls(), closed)
	}
}

const (
	fallbackSource = "https://github.com/owner/arbitrary-repo/releases/download/v2/custom.bin"
	fallbackMirror = "https://gh-proxy.com/"
)

// test_arbitrary_github_assets_try_original_then_mirror
func TestArbitraryGithubAssetsTryOriginalThenMirror(t *testing.T) {
	for _, proxy := range []string{"", "http://127.0.0.1:7890"} {
		for _, prefixed := range []bool{false, true} {
			for _, failure := range []any{404, 503, &FetchError{Class: "ConnectionError", Message: "EOF"}} {
				var closed []string
				fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
					if req.URL == fallbackSource {
						if err, ok := failure.(error); ok {
							return nil, err
						}
						r := reply(failure.(int), "")
						r.Body = &trackedBody{Reader: strings.NewReader(""), closed: &closed, url: req.URL}
						return r, nil
					}
					return reply(200, "complete file"), nil
				}}
				url := fallbackSource
				if prefixed {
					url = fallbackMirror + fallbackSource
				}
				var sleeps []time.Duration
				config := sysConfig("github_proxy_domain", fallbackMirror, "rule_fetch_proxy", proxy)
				response, err := newClient(fake, &sleeps).RequestRule(context.Background(), url, Request{Config: config})
				if err != nil {
					t.Fatal(err)
				}
				if content, _ := response.Content(); string(content) != "complete file" {
					t.Fatal(string(content))
				}
				if !equalStrings(fake.urls(), []string{fallbackSource, fallbackMirror + fallbackSource}) {
					t.Fatal(fake.urls())
				}
				for _, c := range fake.calls {
					if c.req.Proxy != proxy {
						t.Fatal(c.req.Proxy)
					}
				}
				_, isErr := failure.(error)
				if isErr != (len(closed) == 0) {
					t.Fatal(closed)
				}
			}
		}
	}
}

// test_successful_original_does_not_request_mirror
func TestSuccessfulOriginalDoesNotRequestMirror(t *testing.T) {
	fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) { return reply(200, "x"), nil }}
	var sleeps []time.Duration
	if _, err := newClient(fake, &sleeps).RequestRule(context.Background(), fallbackSource, Request{Config: sysConfig("github_proxy_domain", fallbackMirror)}); err != nil ||
		!equalStrings(fake.urls(), []string{fallbackSource}) {
		t.Fatal(err, fake.urls())
	}
}

// test_incomplete_stream_is_retried_without_retaining_partial_bytes
func TestIncompleteStreamGoesToMirrorWithoutPartialBytes(t *testing.T) {
	var events []string
	fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
		r := reply(200, "complete file")
		if req.URL == fallbackSource {
			r.Body = &brokenBody{events: &events}
			// a ChunkedEncodingError("incomplete body") without a disconnect
			// cause is not retried: the mirror is tried next.
			r.Body = &persistentBroken{brokenBody{events: &events}}
		}
		return r, nil
	}}
	var sleeps []time.Duration
	response, err := newClient(fake, &sleeps).RequestRule(context.Background(), fallbackSource,
		Request{Config: sysConfig("github_proxy_domain", fallbackMirror), Stream: true, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := response.Content(); !bytes.Equal(content, []byte("complete file")) {
		t.Fatal(string(content))
	}
	if !equalStrings(fake.urls(), []string{fallbackSource, fallbackMirror + fallbackSource}) || strings.Join(events, ",") != "close" {
		t.Fatal(fake.urls(), events)
	}
}

type persistentBroken struct{ brokenBody }

func (b *persistentBroken) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "partial file"), nil
	}
	return 0, &FetchError{Class: "ChunkedEncodingError", Message: "incomplete body"}
}

// test_both_failures_are_reported_without_credentials
func TestBothFailuresAreReportedWithoutCredentials(t *testing.T) {
	fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) {
		return nil, &FetchError{Class: "ProxyError", Message: "failed via http://user:private@proxy.test:7890"}
	}}
	var sleeps []time.Duration
	_, err := newClient(fake, &sleeps).RequestRule(context.Background(), fallbackSource, Request{Config: sysConfig("github_proxy_domain", fallbackMirror)})
	var fetchErr *RuleFetchError
	if !errors.As(err, &fetchErr) {
		t.Fatal(err)
	}
	text := err.Error()
	if !strings.Contains(text, "原始地址") || !strings.Contains(text, "GitHub 代理地址") || strings.Contains(text, "private") || strings.Contains(text, "user:") {
		t.Fatal(text)
	}
}

// test_non_github_source_never_uses_mirror
func TestNonGithubSourceNeverUsesMirror(t *testing.T) {
	fake := &fakeTransport{reply: func(int, TransportRequest) (*Response, error) {
		return nil, &FetchError{Class: "ConnectionError", Message: "unavailable"}
	}}
	var sleeps []time.Duration
	_, err := newClient(fake, &sleeps).RequestRule(context.Background(), "https://custom.test/data", Request{Config: sysConfig("github_proxy_domain", fallbackMirror)})
	if PythonClass(err) != "ConnectionError" || !equalStrings(fake.urls(), []string{"https://custom.test/data"}) {
		t.Fatal(err, fake.urls())
	}
}

// test_mirror_error_redacts_userinfo_nested_in_path
func TestMirrorErrorRedactsUserinfoNestedInPath(t *testing.T) {
	source := "https://private-user:private-password@github.com/owner/repo/raw/main/rules"
	fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
		return nil, &FetchError{Class: "ConnectionError", Message: "failed to download " + req.URL}
	}}
	var sleeps []time.Duration
	_, err := newClient(fake, &sleeps).RequestRule(context.Background(), source, Request{Config: sysConfig("github_proxy_domain", fallbackMirror)})
	var fetchErr *RuleFetchError
	if !errors.As(err, &fetchErr) || strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "private-password") {
		t.Fatal(err)
	}
}

func TestRedirectPolicy(t *testing.T) {
	// relative redirects follow the hop limit; Location is required; the
	// scheme is revalidated (test_relative_redirect_preserves_head_and_honors_hop_limit,
	// test_redirect_revalidates_destination_scheme_before_request).
	routes := map[string]*Response{}
	redirect := func(location string) *Response {
		r := reply(302, "")
		if location != "" {
			r.Header.Set("Location", location)
		}
		return r
	}
	fake := &fakeTransport{reply: func(_ int, req TransportRequest) (*Response, error) {
		if r, ok := routes[req.URL]; ok {
			return r, nil
		}
		return reply(200, "final"), nil
	}}
	var sleeps []time.Duration
	client := newClient(fake, &sleeps)
	routes["http://o.test/rules"] = redirect("/next")
	routes["http://o.test/next"] = redirect("/final")
	one := 1
	if _, err := client.RequestRule(context.Background(), "http://o.test/rules", Request{Method: "head", Config: sysConfig(), MaxRedirects: &one}); err == nil || err.Error() != "Too many redirects" {
		t.Fatal(err)
	}
	if !equalStrings(fake.urls(), []string{"http://o.test/rules", "http://o.test/next"}) || fake.calls[1].req.Method != "HEAD" {
		t.Fatal(fake.urls())
	}
	fake.calls = nil
	if r, err := client.RequestRule(context.Background(), "http://o.test/rules", Request{Config: sysConfig()}); err != nil || r.StatusCode != 200 {
		t.Fatal(err)
	}
	fake.calls = nil
	routes["http://o.test/file"] = redirect("file:///etc/hosts")
	if _, err := client.RequestRule(context.Background(), "http://o.test/file", Request{Config: sysConfig()}); !errors.Is(err, ErrInvalidURL) || len(fake.calls) != 1 {
		t.Fatal(err)
	}
	routes["http://o.test/none"] = redirect("")
	if _, err := client.RequestRule(context.Background(), "http://o.test/none", Request{Config: sysConfig()}); err == nil || err.Error() != "Redirect without Location" {
		t.Fatal(err)
	}
	// test_self_api_redirect_cannot_leak_token_to_external_origin
	fake.calls = nil
	routes["http://cf.test/api/rules?token=t"] = redirect("http://other.test/rules?token=t")
	config := sysConfig("server_domain", "http://cf.test", "rule_fetch_proxy", "http://proxy.test:1")
	if _, err := client.RequestRule(context.Background(), "http://cf.test/api/rules?token=t", Request{Config: config}); err == nil ||
		err.Error() != "Internal API redirect leaves its origin" || len(fake.calls) != 1 || fake.calls[0].req.Proxy != "" {
		t.Fatal(err, fake.calls)
	}
	// test_external_redirect_to_self_api_recomputes_proxy
	fake.calls = nil
	routes["http://cf.test/rules"] = redirect("http://cf.test/api/rules")
	if _, err := client.RequestRule(context.Background(), "http://cf.test/rules", Request{Config: config}); err != nil ||
		fake.calls[0].req.Proxy != "http://proxy.test:1" || fake.calls[1].req.Proxy != "" {
		t.Fatal(err, fake.calls)
	}
	// test_self_api_bypasses_configured_and_environment_proxy (prefix form)
	fake.calls = nil
	prefixed := sysConfig("server_domain", "http://cf.test/configflow", "rule_fetch_proxy", "http://proxy.test:1")
	if _, err := client.RequestRule(context.Background(), "http://cf.test/configflow/api/rules?token=t", Request{Config: prefixed}); err != nil || fake.calls[0].req.Proxy != "" {
		t.Fatal(err)
	}
	if !IsInternalRuleURL("http://cf.test:80/configflow/api/x", prefixed, "") || IsInternalRuleURL("http://cf.test/api/x", prefixed, "") ||
		!IsInternalRuleURL("http://h.test/api/x", sysConfig(), "http://h.test/") {
		t.Fatal("internal")
	}
}

func TestNormalizeAndOptions(t *testing.T) {
	if p, err := NormalizeRuleFetchProxy(" http://u:p@proxy.test:7890/ "); err != nil || p != "http://u:p@proxy.test:7890" {
		t.Fatal(p, err)
	}
	if _, err := NormalizeRuleFetchProxy("http://proxy.test/path"); err != ErrInvalidProxy {
		t.Fatal(err)
	}
	if _, err := NormalizeRuleFetchProxy(5); err != ErrProxyNotString {
		t.Fatal(err)
	}
	if !IsGithubURL("https://raw.githubusercontent.com/a") || IsGithubURL("https://github.com.evil/a") {
		t.Fatal("github")
	}
}
