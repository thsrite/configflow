package substore

// Regression tests of review C13 fixes that are not covered by the Python
// reference file: requests' environment proxies (no loopback bypass),
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
	previous := environ
	environ = func() []string { return entries }
	t.Cleanup(func() { environ = previous })
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

// TestReviewLoopbackSubStoreGoesThroughEnvironmentProxy: requests (trust_env)
// sends a request for the Sub-Store on 127.0.0.1 to HTTP_PROXY unless
// NO_PROXY names it; Go's ProxyFromEnvironment bypassed loopback hosts.
func TestReviewLoopbackSubStoreGoesThroughEnvironmentProxy(t *testing.T) {
	direct := newFakeSubStore(t)
	direct.setVersion("2.40.0")
	proxy := newRecordingProxy(t)
	proxyURL, _ := url.Parse(proxy.srv.URL)
	target := direct.srv.URL + "/api/utils/env"
	env := &Env{HTTP: RequestsClient{}}

	withEnviron(t, "HTTP_PROXY=http://user:p%40ss@"+proxyURL.Host)
	if got := env.FetchRunningVersion(context.Background(), direct.srv.URL, 5); got != "9.9.9" {
		t.Fatalf("version %q: not sent through the proxy", got)
	}
	want := "GET " + target + " auth=Basic dXNlcjpwQHNz" // user:p@ss
	if seen := proxy.requests(); len(seen) != 1 || seen[0] != want {
		t.Fatalf("proxy saw %q, want %q", seen, want)
	}

	withEnviron(t, "HTTP_PROXY=http://"+proxyURL.Host, "NO_PROXY=127.0.0.1")
	if got := env.FetchRunningVersion(context.Background(), direct.srv.URL, 5); got != "2.40.0" {
		t.Fatalf("version %q: NO_PROXY ignored", got)
	}
	withEnviron(t, "http_proxy=http://"+proxyURL.Host, "HTTP_PROXY=http://127.0.0.1:1")
	if got := env.FetchRunningVersion(context.Background(), direct.srv.URL, 5); got != "9.9.9" {
		t.Fatalf("version %q: lowercase http_proxy must win", got)
	}

	// proxies= (the rule download proxy for GitHub) wins over the
	// environment and carries its credentials.
	withEnviron(t, "HTTP_PROXY=http://127.0.0.1:1")
	resp, err := RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: target, Timeout: Seconds(5),
		Proxy: "http://u:pw@" + proxyURL.Host})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("explicit proxy: %v", err)
	}
	if seen := proxy.requests(); seen[len(seen)-1] != "GET "+target+" auth=Basic dTpwdw==" {
		t.Fatalf("explicit proxy saw %q", seen[len(seen)-1])
	}

	withEnviron(t, "HTTP_PROXY=socks5://127.0.0.1:1")
	_, err = RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: target, Timeout: Seconds(5)})
	if pythonClass(err) != "InvalidSchema" {
		t.Fatalf("socks proxy without PySocks: %v (%s)", err, pythonClass(err))
	}
	withEnviron(t, "HTTP_PROXY=http://")
	_, err = RequestsClient{}.Do(context.Background(), &Request{Method: "GET", URL: target, Timeout: Seconds(5)})
	if pythonClass(err) != "InvalidProxyURL" {
		t.Fatalf("proxy without host: %v (%s)", err, pythonClass(err))
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
