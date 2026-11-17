package substore

// Regression tests of review C13 fixes that are not covered by the Python
// reference file: native environment proxies (loopback bypass since N2),
// cancelled request contexts, host label checks, subprocess text decoding,
// os.makedirs error texts, remembered falsy conversions and panics in the
// update thread.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func withEnviron(t *testing.T, entries ...string) {
	t.Helper()
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(key, "")
	}
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		t.Setenv(key, value)
	}
}

type recordingProxy struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func newRecordingProxy(t *testing.T) *recordingProxy {
	p := &recordingProxy{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.seen = append(p.seen, r.Method+" "+r.RequestURI+" auth="+r.Header.Get("Proxy-Authorization"))
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"version":"9.9.9"}}`)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *recordingProxy) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen...)
}

// TestReviewLoopbackSubStoreBypassesEnvironmentProxy covers the 2026-11-17
// native N2 decision. Explicitly configured download proxies still apply.
func TestReviewLoopbackSubStoreBypassesEnvironmentProxy(t *testing.T) {
	direct := newFakeSubStore(t)
	direct.setVersion("2.40.0")
	proxy := newRecordingProxy(t)
	env := &Env{HTTP: RequestsClient{}}
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "ALL_PROXY"} {
		withEnviron(t, key+"="+proxy.srv.URL)
		if got := env.FetchRunningVersion(context.Background(), direct.srv.URL, 5); got != "2.40.0" {
			t.Fatalf("loopback bypass: %s", got)
		}
	}
	if len(proxy.requests()) != 0 {
		t.Fatal("loopback used environment proxy")
	}
	resp, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: direct.srv.URL + "/api/utils/env", Timeout: Seconds(5), Proxy: proxy.srv.URL})
	if err != nil || resp.StatusCode != 200 || len(proxy.requests()) != 1 {
		t.Fatal("explicit proxy was bypassed")
	}
	withEnviron(t, "REQUEST_METHOD=GET", "HTTP_PROXY="+proxy.srv.URL)
	_, err = RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: direct.srv.URL, Timeout: Seconds(5)})
	if err == nil || !strings.Contains(err.Error(), "CGI") || len(proxy.requests()) != 1 {
		t.Fatal("CGI protection failed")
	}
}

// TestReviewCancelledRequestContextFinishesTheFetch: the browser going away
// must not abort a download others wait for (Python's thread finished it).
func TestReviewCancelledRequestContextFinishesTheFetch(t *testing.T) {
	withEnviron(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, "proxies: []")
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	resp, err := RequestsClient{}.Do(ctx, &Request{Method: "GET", URL: srv.URL, Timeout: Seconds(5)})
	if err != nil || resp.Text() != "proxies: []" {
		t.Fatalf("cancelled context aborted the request: %v", err)
	}
	result, err := ExecRunner{}.Run(ctx, []string{"sh", "-c", "sleep 0.2; echo done"}, 5*time.Second)
	if err != nil || result.Stdout != "done\n" {
		t.Fatalf("cancelled context killed the command: %v %q", err, result.Stdout)
	}
}

// TestReviewHostLabelIsInvalidURL: requests refuses hosts starting with "*"
// or "." before connecting.
func TestReviewHostLabelIsInvalidURL(t *testing.T) {
	withEnviron(t)
	for _, raw := range []string{"http://.example.test/sub", "http://*.example.test/sub"} {
		_, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: raw, Timeout: Seconds(1)})
		if pythonClass(err) != "InvalidURL" || err.Error() != "URL has an invalid label." {
			t.Errorf("%s: %v (%s)", raw, err, pythonClass(err))
		}
	}
}

// TestReviewCharsetLastWins: requests parses the Content-Type parameters
// into a dict, so a repeated charset takes the last value.
func TestReviewCharsetLastWins(t *testing.T) {
	resp := &Response{Header: http.Header{"Content-Type": {"text/plain; charset=latin-1; charset=utf-8"}}, Body: []byte("中")}
	if got := resp.Text(); got != "中" {
		t.Fatalf("got %q", got)
	}
}

// TestReviewCommandOutputIsStrictUTF8 is subprocess.run(text=True):
// undecodable output raises UnicodeDecodeError (not RuntimeUnavailable),
// newlines are universal.
func TestReviewCommandOutputIsStrictUTF8(t *testing.T) {
	result, err := ExecRunner{}.Run(context.Background(), []string{"sh", "-c", `printf 'a\r\nb\rc'; printf 'x\r\n' >&2; exit 3`}, 5*time.Second)
	if err != nil || result.Stdout != "a\nb\nc" || result.Stderr != "x\n" || result.ReturnCode != 3 {
		t.Fatalf("%v %#v", err, result)
	}
	_, err = ExecRunner{}.Run(context.Background(), []string{"sh", "-c", `printf 'ok\377'`}, 5*time.Second)
	if pythonClass(err) != "UnicodeDecodeError" || err.Error() != "'utf-8' codec can't decode byte 0xff in position 2: invalid start byte" {
		t.Fatalf("%v (%s)", err, pythonClass(err))
	}
	h := newHarness(t, harnessOptions{builtin: true})
	rt := h.service.Runtime
	rt.env.Commands = commandFunc(func([]string) (CommandResult, error) {
		return CommandResult{}, pyErr("UnicodeDecodeError", "bad")
	})
	if _, err := rt.runCommand(context.Background(), "status"); IsRuntimeUnavailable(err) || pythonClass(err) != "UnicodeDecodeError" {
		t.Fatalf("%v", err)
	}
}

// TestReviewMakedirsErrorTexts: os.makedirs(exist_ok=True) names the
// directory whose mkdir failed, with Python's errno text.
func TestReviewMakedirsErrorTexts(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o644)
	err := pyMakedirs(file)
	if got := osErrorText(err); got != "[Errno 17] File exists: '"+file+"'" {
		t.Errorf("file in the way: %q", got)
	}
	err = pyMakedirs(filepath.Join(file, "sub", "subscribes"))
	if got := osErrorText(err); got != "[Errno 20] Not a directory: '"+filepath.Join(file, "sub")+"'" {
		t.Errorf("parent is a file: %q", got)
	}
	if err := pyMakedirs(filepath.Join(dir, "a", "b", "c")); err != nil {
		t.Errorf("nested: %v", err)
	}
	if err := pyMakedirs(filepath.Join(dir, "a", "b", "c")); err != nil {
		t.Errorf("exists: %v", err)
	}
}

// TestReviewFalsyConversionIsRemembered: Sub-Store answering a falsy first
// proxy (0) is "no node" for every caller, and Python remembers it like any
// other answer instead of asking again.
func TestReviewFalsyConversionIsRemembered(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true, version: "2.42.3"})
	h.store.download = func(string, string, url.Values, *jsonx.Object) (int, string, http.Header) {
		return 200, "proxies: [0, {name: A}]", http.Header{}
	}
	c := h.service.Client
	for i := 0; i < 2; i++ {
		proxy := c.ConvertProxyString(context.Background(), "snell://x")
		if proxy == nil || proxy.Len() != 0 {
			t.Fatalf("round %d: %s", i, jsonx.Repr(proxy))
		}
	}
	h.store.mu.Lock()
	downloads := len(h.store.downloads)
	h.store.mu.Unlock()
	if downloads != 1 {
		t.Fatalf("Sub-Store asked %d times", downloads)
	}
}

type panickingHTTP struct{}

func (panickingHTTP) Do(ctx context.Context, req *Request) (*Response, error) {
	if strings.HasSuffix(req.URL, "/api/utils/env") {
		return RequestsClient{}.Do(ctx, req)
	}
	panic("boom")
}

// TestReviewUpdatePanicIsAFailure: an unexpected failure during the update
// becomes the recorded failure (Python's except Exception) and releases the
// update lock instead of crashing the server.
func TestReviewUpdatePanicIsAFailure(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	h.store.setVersion("2.40.0")
	d := h.service.Dependencies
	d.env.HTTP = panickingHTTP{}
	d.updateLock.Lock()
	d.runUpdate(context.Background(), jsonx.Obj("version", "2.41.0", "download_url", "https://example.test/x.js"))
	if !d.updateLock.TryLock() {
		t.Fatal("update lock kept")
	}
	state := d.GetUpdateState()
	if state.GetString("state") != "failed" || state.GetString("message") != "更新失败：exception_type=Exception" {
		t.Fatalf("state %s", jsonx.Repr(state))
	}
}

// TestReviewTooManyRedirectsCarriesStatus: requests' TooManyRedirects has
// the 31st redirect response, so safe_exception_details shows its status.
func TestReviewTooManyRedirectsCarriesStatus(t *testing.T) {
	withEnviron(t)
	hops := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, srv.URL+"/hop", http.StatusFound)
	}))
	defer srv.Close()
	_, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: srv.URL, Timeout: Seconds(5)})
	if pythonClass(err) != "TooManyRedirects" || safeExceptionDetails(err) != "exception_type=TooManyRedirects status=302" || hops != 31 {
		t.Fatalf("%v %s hops=%d", err, safeExceptionDetails(err), hops)
	}
}

// TestRedirectHopsChooseTheirOwnProxy: every redirect hop gets the
// environment proxy of its own URL (since N2; requests kept the first URL's
// proxy for a hop to a NO_PROXY host), with the proxy's credentials.
func TestRedirectHopsChooseTheirOwnProxy(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	record := func(s string) {
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	}
	var direct *httptest.Server
	direct = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("direct " + r.RequestURI + " auth=" + r.Header.Get("Proxy-Authorization"))
		if r.URL.Path == "/sub" {
			http.Redirect(w, r, "http://localhost:"+direct.URL[strings.LastIndex(direct.URL, ":")+1:]+"/next", http.StatusFound)
			return
		}
		io.WriteString(w, "direct")
	}))
	defer direct.Close()
	port := direct.URL[strings.LastIndex(direct.URL, ":")+1:]
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("proxy " + r.RequestURI + " auth=" + r.Header.Get("Proxy-Authorization"))
		if strings.HasSuffix(r.URL.Path, "/sub") {
			http.Redirect(w, r, "http://localhost:"+port+"/next", http.StatusFound)
			return
		}
		io.WriteString(w, "via-proxy")
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	cases := []struct {
		noProxy, first, text string
		want                 []string
	}{
		{"localhost", "http://agent.example:9/sub", "direct", []string{"proxy http://agent.example:9/sub auth=Basic dTpw",
			"direct /next auth="}},
		{"127.0.0.1", direct.URL + "/sub", "direct", []string{"direct /sub auth=",
			"direct /next auth="}},
	}
	for _, c := range cases {
		seen = nil
		withEnviron(t, "HTTP_PROXY=http://u:p@"+proxyURL.Host, "NO_PROXY="+c.noProxy)
		resp, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: c.first, Timeout: Seconds(5)})
		if err != nil || resp.Text() != c.text {
			t.Fatalf("%s: %v %+v", c.noProxy, err, resp)
		}
		if !jsonx.Equal(jsonx.ListOf(seen), jsonx.ListOf(c.want)) {
			t.Errorf("%s: saw %q, want %q", c.noProxy, seen, c.want)
		}
	}
}
