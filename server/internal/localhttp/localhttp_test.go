package localhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
)

// Ported from backend/test_local_http.py. The Python tests that go through
// rule_fetch (redirect policy, host comparison with server_domain) belong to
// the remote rule package; what is tested here is local_http itself against a
// plain http.Handler in place of the Flask app.

type fixture struct {
	mux     *http.ServeMux
	closed  chan struct{}
	chunks  atomic.Int32
	calls   atomic.Int32
	depthAt []int
	mu      sync.Mutex
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{mux: http.NewServeMux(), closed: make(chan struct{}, 4)}
	f.mux.HandleFunc("/api/inspect", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path, "raw": r.RequestURI, "query": r.URL.Query().Get("q"), "cookie": r.Header.Get("Cookie"),
			"authorization": r.Header.Get("Authorization"), "x": r.Header.Get("X-Test"), "host": r.Host,
			"script_root": ScriptRoot(r.Context()), "outer": r.Context().Value("outer"), "remote": r.RemoteAddr,
			"method": r.Method, "content_type": r.Header.Get("Content-Type"), "length": r.Header.Get("Content-Length"),
		})
	})
	f.mux.HandleFunc("/api/fixture-rule", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "26")
		_, _ = io.WriteString(w, "DOMAIN-SUFFIX,example.com\n")
	})
	f.mux.HandleFunc("/api/stream", func(w http.ResponseWriter, r *http.Request) {
		defer func() { f.closed <- struct{}{} }()
		for i := 0; i < 100; i++ {
			f.chunks.Store(int32(i + 1))
			if _, err := w.Write([]byte(strings.Repeat("x", 8192))); err != nil {
				return
			}
		}
	})
	f.mux.HandleFunc("/api/context-stream", func(w http.ResponseWriter, r *http.Request) {
		defer func() { f.closed <- struct{}{} }()
		_, _ = io.WriteString(w, "one")
		if r.URL.Query().Get("fail") == "1" {
			panic("stream failure")
		}
		_, _ = io.WriteString(w, "two")
	})
	f.mux.HandleFunc("/api/slow", func(w http.ResponseWriter, r *http.Request) {
		defer func() { f.closed <- struct{}{} }()
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	})
	f.mux.HandleFunc("/api/recurse", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.depthAt = append(f.depthAt, Depth(r.Context()))
		f.mu.Unlock()
		response, err := Do(r.Context(), Request{Method: "get", URL: "http://config.test/api/recurse"})
		switch {
		case errors.Is(err, ErrTooManyRedirects):
			_, _ = io.WriteString(w, "limit")
		case err != nil:
			_, _ = io.WriteString(w, "error: "+err.Error())
		default:
			content, _ := response.Content()
			_, _ = w.Write(content)
		}
	})
	Register(f.mux, testSpawner{})
	t.Cleanup(func() { Register(nil, nil) })
	return f
}

func decode(t *testing.T, response *Response) map[string]any {
	t.Helper()
	content, err := response.Content()
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(content, &out); err != nil {
		t.Fatalf("%s: %v", content, err)
	}
	return out
}

func TestInternalRequestDoesNotInheritAuthHeadersOrContext(t *testing.T) {
	newFixture(t)
	outer := context.WithValue(context.Background(), "outer", "private")
	response, err := Do(outer, Request{Method: "get", URL: "http://config.test/api/inspect"})
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, response)
	if got["cookie"] != "" || got["authorization"] != "" || got["x"] != "" || got["outer"] != nil || got["script_root"] != "" || got["host"] != "config.test" {
		t.Errorf("state leaked into the callback: %v", got)
	}
	response, err = Do(outer, Request{Method: "GET", URL: "http://config.test/api/inspect", Headers: http.Header{"Authorization": {"Bearer t"}, "X-Test": {"1"}},
		Cookies: []Param{{"a", "1"}, {"b", "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	got = decode(t, response)
	if got["authorization"] != "Bearer t" || got["x"] != "1" || got["cookie"] != "a=1; b=2" || got["remote"] != "127.0.0.1:0" {
		t.Errorf("explicit headers must pass: %v", got)
	}
}

func TestPrefixParamsExplicitAuthAndHeadArePreserved(t *testing.T) {
	newFixture(t)
	response, err := Do(context.Background(), Request{Method: "get", URL: "https://config.test/cf/api/inspect", Prefix: "/cf",
		Params: []Param{{"q", "中文 & value"}}, BasicAuth: &[2]string{"u", "p"}})
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, response)
	if got["script_root"] != "/cf" || got["query"] != "中文 & value" || got["path"] != "/api/inspect" || got["authorization"] != "Basic dTpw" {
		t.Errorf("got %v", got)
	}
	if got["raw"] != "/api/inspect?q=%E4%B8%AD%E6%96%87+%26+value" {
		t.Errorf("request uri %v", got["raw"])
	}
	head, err := Do(context.Background(), Request{Method: "head", URL: "http://config.test/api/fixture-rule"})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := head.Content()
	if head.StatusCode != 200 || len(content) != 0 || head.Header.Get("Content-Length") != "26" {
		t.Errorf("HEAD: %d %q %v", head.StatusCode, content, head.Header)
	}
}

func TestBodiesUseNativeJSONEncoding(t *testing.T) {
	newFixture(t)
	response, err := Do(context.Background(), Request{Method: "post", URL: "http://config.test/api/inspect", HasJSON: true, JSON: jsonx.Obj("name", "例子")})
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, response)
	// Native UTF-8 JSON body: 17 bytes.
	if got["content_type"] != "application/json" || got["length"] != "17" || got["method"] != "POST" {
		t.Errorf("json body: %v", got)
	}
	response, _ = Do(context.Background(), Request{Method: "post", URL: "http://config.test/api/inspect", Form: []Param{{"a", "b c"}}})
	got = decode(t, response)
	if got["content_type"] != "application/x-www-form-urlencoded" || got["length"] != "5" {
		t.Errorf("form body: %v", got)
	}
}

func TestStreamIsLazyAndCloseReleasesTheRequest(t *testing.T) {
	f := newFixture(t)
	response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/stream", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if f.chunks.Load() != 1 {
		t.Fatalf("the handler produced %d chunks before anything was read", f.chunks.Load())
	}
	buf := make([]byte, 1024)
	if _, err := io.ReadFull(response.Body, buf); err != nil || string(buf) != strings.Repeat("x", 1024) {
		t.Fatalf("first read: %v", err)
	}
	response.Body.Close()
	select {
	case <-f.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler still running after Close")
	}
	if f.chunks.Load() > 2 {
		t.Errorf("the stream went on after Close: %d chunks", f.chunks.Load())
	}
}

func TestStreamCompletionAndFailure(t *testing.T) {
	f := newFixture(t)
	response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/context-stream", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	content, err := response.Content()
	if err != nil || string(content) != "onetwo" {
		t.Errorf("%q %v", content, err)
	}
	<-f.closed
	response, err = Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/context-stream?fail=1", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := response.Content(); err == nil || !strings.Contains(err.Error(), "stream failure") {
		t.Errorf("the failure must reach the reader: %v", err)
	}
	<-f.closed
}

func TestRegisteredAppHandlesCallbacksFromOtherGoroutines(t *testing.T) {
	newFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/inspect"})
			if err != nil || response.StatusCode != 200 {
				t.Errorf("%v", err)
			}
		}()
	}
	wg.Wait()
}

func TestExternalNonAPIAndNormalizedEscapeUseTheNetwork(t *testing.T) {
	newFixture(t)
	for _, url := range []string{"http://config.test/elsewhere", "http://config.test/api/../elsewhere", "http://config.test/api", "http://config.test/api/../elsewhere"} {
		response, err := Do(context.Background(), Request{Method: "get", URL: url})
		if err != nil || response != nil {
			t.Errorf("%s: %v %v", url, response, err)
		}
	}
	if response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/a/../inspect"}); err != nil || response == nil {
		t.Errorf("a normalised path inside /api/ is local: %v %v", response, err)
	}
	if response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/%61pi/inspect"}); err != nil || response == nil {
		t.Errorf("an escaped unreserved API path is local after server path decoding: %v %v", response, err)
	}
	if _, err := Do(context.Background(), Request{Method: "get", URL: "not a url"}); err == nil {
		t.Error("an invalid URL must fail like requests does")
	}
}

func TestSlowLocalResponseReportsTimeoutAndReleasesTheStream(t *testing.T) {
	f := newFixture(t)
	_, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/slow", ReadTimeout: time.Millisecond})
	if !errors.Is(err, ErrReadTimeout) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-f.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the slow handler was not released")
	}
}

func TestNestedCallbacksStopAtEightLevels(t *testing.T) {
	f := newFixture(t)
	response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/recurse"})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := response.Content()
	if string(content) != "limit" {
		t.Errorf("got %q", content)
	}
	if len(f.depthAt) != 8 || f.depthAt[0] != 1 || f.depthAt[7] != 8 {
		t.Errorf("depths %v", f.depthAt)
	}
}

func TestWithoutRegisteredAppNothingIsDispatched(t *testing.T) {
	Register(nil, nil)
	if response, err := Do(context.Background(), Request{Method: "get", URL: "http://config.test/api/rules"}); response != nil || err != nil {
		t.Errorf("%v %v", response, err)
	}
}

// Expected values are Go's escaped request paths; unreserved escapes remain
// escaped. Local dispatch separately checks the path after server decoding.
func TestNormalizePathPreservesGoEscapes(t *testing.T) {
	cases := map[string]string{
		"/%61pi/x":        "/%61pi/x",
		"/api/%2e%2e/e":   "/api/%2e%2e/e",
		"/api/a/../b":     "/api/b",
		"/api/%e4%be%8b":  "/api/%e4%be%8b",
		"/api/%7euser":    "/api/%7euser",
		"/api/a b":        "/api/a%20b",
		"/api/a%2Fb":      "/api/a%2Fb",
		"/api/a/b/../../": "/api/",
		"/api/..":         "/",
		"/api/.":          "/api/",
		"/api/./b":        "/api/b",
		"/api/x//y":       "/api/x//y",
		"/../a":           "/a",
		"/api/%2E/b":      "/api/%2E/b",
		"/api/a;b":        "/api/a;b",
		"/api/a|b":        "/api/a%7Cb",
		"/api/%zz":        "/api/%25zz",
		"/api/%":          "/api/%25",
	}
	for in, want := range cases {
		if u, err := weburl.Prepare("http://config.test"+in, nil); err != nil || u.String() != "http://config.test"+want {
			got := ""
			if u != nil {
				got = u.String()
			}
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// Regression (review C02): a "%" that is not an escape is dispatched as %25
// (Go's url.Parse refused it), the query is made valid, the response URL
// keeps the fragment. Escapes stay as written (review N2).
func TestPreparedURLLikeRequests(t *testing.T) {
	newFixture(t)
	for url, want := range map[string]string{
		"http://config.test/api/a%41%zz":   "http://config.test/api/a%41%25zz",
		"http://config.test/api/x?a=b c":   "http://config.test/api/x?a=b%20c",
		"http://config.test/api/inspect#f": "http://config.test/api/inspect#f",
	} {
		response, err := Do(context.Background(), Request{Method: "get", URL: url})
		if err != nil || response == nil {
			t.Errorf("%s: %v %v", url, response, err)
			continue
		}
		if response.URL != want {
			t.Errorf("%s: URL %q, want %q", url, response.URL, want)
		}
		response.Body.Close()
	}
}

// testSpawner is used only by isolated adapter fixtures; Runtime integration is
// tested externally to avoid lifecycle -> repo -> rulefetch -> localhttp cycles.
type testSpawner struct{}

func (testSpawner) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	go fn(context.WithoutCancel(ctx))
	return nil
}
