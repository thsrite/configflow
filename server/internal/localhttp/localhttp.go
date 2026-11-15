// Package localhttp is the port of backend/utils/local_http.py: dispatching
// the app's own HTTP callbacks (a rule URL that points at this server) straight
// into its handler instead of through the network, so that a request which
// must wait for another request to the same server cannot starve the worker
// pool.
//
// The caller establishes the same-origin boundary first. The dispatched
// request still passes the normal routing and authentication middleware (it is
// an ordinary call into the registered http.Handler); it never inherits
// credentials or any other state of the request that triggered it: the
// handler gets a fresh context carrying only the nesting depth and the URL
// prefix, and only the headers, cookies and credentials named in Request.
//
// Differences from the Python module: there are no thread-local repository
// transactions to isolate (the repository is passed explicitly in Go); the
// handler runs in its own goroutine, connected to the caller by a pipe, which
// gives the same lazy, back-pressured streaming the WSGI iterator had; and the
// nested request's User-Agent is empty, where Werkzeug's test client sent
// "Werkzeug/x.y".
package localhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/urlx"
)

// maxDepth is the nesting limit of callbacks (`depth >= 8` in Python).
const maxDepth = 8

// ErrTooManyRedirects is requests.TooManyRedirects for nested callbacks.
var ErrTooManyRedirects = errors.New("Too many nested ConfigFlow API callbacks")

// ErrReadTimeout is requests.ReadTimeout for a response that took too long.
var ErrReadTimeout = errors.New("ConfigFlow API callback exceeded its read timeout")

type holder struct{ handler http.Handler }

var application atomic.Pointer[holder]

// Register is register_application: the handler callbacks are dispatched to.
// nil unregisters.
func Register(handler http.Handler) {
	if handler == nil {
		application.Store(nil)
		return
	}
	application.Store(&holder{handler})
}

type ctxKey int

const (
	depthKey ctxKey = iota
	scriptRootKey
)

// ScriptRoot is Flask's request.script_root for a dispatched request: the URL
// prefix (such as "/cf") the app is served under, "" for normal requests.
func ScriptRoot(ctx context.Context) string {
	root, _ := ctx.Value(scriptRootKey).(string)
	return root
}

// Depth is how many dispatches deep the context is (0 for a normal request).
// Pass the incoming request's context to Request so the limit works.
func Depth(ctx context.Context) int {
	depth, _ := ctx.Value(depthKey).(int)
	return depth
}

// Param is one query parameter, kept in order.
type Param struct{ Key, Value string }

// Request is the keyword arguments of request_local.
type Request struct {
	Method string
	URL    string
	// Prefix is the URL prefix of the app ("" or "/cf"); only paths below
	// Prefix + "/api/" are dispatched locally.
	Prefix  string
	Headers http.Header
	Params  []Param
	// Body forms, at most one: Data (raw bytes), Form (urlencoded) or JSON.
	Data []byte
	Form []Param
	JSON any
	// HasJSON distinguishes `json=None` from no json argument.
	HasJSON bool
	Cookies []Param
	// BasicAuth is requests' auth=(user, password).
	BasicAuth *[2]string
	// Stream keeps the body lazy; otherwise it is read before returning.
	Stream bool
	// ReadTimeout bounds the wait for the headers and for each chunk; 0 means none.
	ReadTimeout time.Duration
}

// Response is the requests-compatible result.
type Response struct {
	StatusCode int
	Reason     string
	Header     http.Header
	URL        string
	// Body is the response body; Close it. For a non-stream request it is
	// already in memory.
	Body io.ReadCloser
}

// Content reads the whole body and closes it.
func (r *Response) Content() ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

// Do is request_local: it returns (nil, nil) when no app is registered or the
// URL is not below Prefix + "/api/" after normalisation, which tells the
// caller to use the network.
func Do(ctx context.Context, r Request) (*Response, error) {
	app := application.Load()
	if app == nil {
		return nil, nil
	}
	depth := Depth(ctx)
	if depth >= maxDepth {
		return nil, ErrTooManyRedirects
	}
	method := strings.ToUpper(r.Method)
	prepared, err := prepare(r, method)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(prepared.path, r.Prefix+"/api/") {
		// Requests normalises dot segments while preparing a URL: recheck the
		// API boundary after that normalisation rather than dispatch outside it.
		return nil, nil
	}
	path := prepared.path[len(r.Prefix):]
	target := path
	if prepared.query != "" {
		target += "?" + prepared.query
	}

	inner, cancel := context.WithCancel(context.Background())
	inner = context.WithValue(inner, depthKey, depth+1)
	inner = context.WithValue(inner, scriptRootKey, r.Prefix)

	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		decodedPath = path
	}
	req := &http.Request{
		Method:        method,
		URL:           &url.URL{Path: decodedPath, RawPath: path, RawQuery: prepared.query},
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        prepared.header,
		Body:          http.NoBody,
		Host:          prepared.host,
		RemoteAddr:    "127.0.0.1:0",
		RequestURI:    target,
		ContentLength: int64(len(prepared.body)),
	}
	if prepared.scheme == "https" {
		req.URL.Scheme = "https"
	}
	if len(prepared.body) > 0 {
		req.Body = io.NopCloser(bytes.NewReader(prepared.body))
	}
	req = req.WithContext(inner)

	started := time.Now()
	pr, pw := io.Pipe()
	rw := &responseWriter{header: http.Header{}, ready: make(chan struct{}), pw: pw, head: method == "HEAD"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				rw.fail(fmt.Errorf("callback handler panicked: %v", recovered))
			}
		}()
		app.handler.ServeHTTP(rw, req)
		rw.finish()
	}()

	<-rw.ready
	body := &responseBody{reader: pr, cancel: cancel, done: done, timeout: r.ReadTimeout}
	if r.ReadTimeout > 0 && time.Since(started) > r.ReadTimeout {
		body.Close()
		return nil, ErrReadTimeout
	}
	response := &Response{
		StatusCode: rw.status,
		Reason:     http.StatusText(rw.status),
		Header:     rw.snapshot,
		URL:        prepared.url,
		Body:       body,
	}
	if !r.Stream {
		content, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, err
		}
		response.Body = io.NopCloser(bytes.NewReader(content))
	}
	return response, nil
}

type preparedRequest struct {
	scheme, host, path, query, url string
	header                         http.Header
	body                           []byte
}

// prepare is requests.Request(...).prepare() for the parts that matter here.
func prepare(r Request, method string) (*preparedRequest, error) {
	params := make([][2]string, len(r.Params))
	for i, p := range r.Params {
		params[i] = [2]string{p.Key, p.Value}
	}
	// requests.Request(...).prepare().url, then urlsplit of it (request_local).
	full, err := urlx.RequestsPrepareURL(strings.TrimLeft(r.URL, " \t\n\r\v\f"), params)
	if err != nil {
		return nil, fmt.Errorf("Invalid URL %s: %w", jsonx.Repr(r.URL), err)
	}
	split, err := urlx.Split(full)
	if err != nil {
		return nil, fmt.Errorf("Invalid URL %s: %w", jsonx.Repr(r.URL), err)
	}
	host := split.Netloc
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	if split.Scheme == "" {
		return nil, fmt.Errorf("Invalid URL %s: No scheme supplied. Perhaps you meant https://%s?", jsonx.Repr(r.URL), r.URL)
	}
	if host == "" {
		return nil, fmt.Errorf("Invalid URL %s: No host supplied", jsonx.Repr(r.URL))
	}
	path, query := split.Path, split.Query
	parsed := struct{ Scheme, Host string }{split.Scheme, host}
	prepared := &preparedRequest{scheme: parsed.Scheme, host: parsed.Host, path: path, query: query, header: http.Header{}}
	for key, values := range r.Headers {
		for _, v := range values {
			prepared.header.Add(key, v)
		}
	}
	switch {
	case r.HasJSON:
		body, err := jsonx.Default(r.JSON)
		if err != nil {
			return nil, err
		}
		prepared.body = body
		if prepared.header.Get("Content-Type") == "" {
			prepared.header.Set("Content-Type", "application/json")
		}
	case len(r.Form) > 0:
		prepared.body = []byte(encodeParams(r.Form))
		if prepared.header.Get("Content-Type") == "" {
			prepared.header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	case r.Data != nil:
		prepared.body = r.Data
	}
	if len(prepared.body) > 0 || method == "POST" || method == "PUT" || method == "PATCH" {
		prepared.header.Set("Content-Length", strconv.Itoa(len(prepared.body)))
	}
	if len(r.Cookies) > 0 {
		pairs := make([]string, len(r.Cookies))
		for i, c := range r.Cookies {
			pairs[i] = c.Key + "=" + c.Value
		}
		prepared.header.Set("Cookie", strings.Join(pairs, "; "))
	}
	if r.BasicAuth != nil {
		prepared.header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(r.BasicAuth[0]+":"+r.BasicAuth[1])))
	}
	prepared.url = full
	return prepared, nil
}

func encodeParams(params []Param) string {
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = urlx.QuotePlus(p.Key) + "=" + urlx.QuotePlus(p.Value)
	}
	return strings.Join(parts, "&")
}

// responseWriter hands the handler's output to the caller: the status and
// headers as soon as the handler commits them, the body through a pipe.
type responseWriter struct {
	header   http.Header
	snapshot http.Header
	status   int
	ready    chan struct{}
	once     sync.Once
	pw       *io.PipeWriter
	head     bool
}

func (w *responseWriter) Header() http.Header { return w.header }

func (w *responseWriter) WriteHeader(code int) {
	w.once.Do(func() {
		w.status = code
		w.snapshot = w.header.Clone()
		close(w.ready)
	})
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if w.head {
		return len(p), nil
	}
	return w.pw.Write(p)
}

func (w *responseWriter) Flush() {}

func (w *responseWriter) finish() {
	w.WriteHeader(http.StatusOK)
	w.pw.Close()
}

func (w *responseWriter) fail(err error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.pw.CloseWithError(err)
}

// responseBody is the caller's end of the pipe.
type responseBody struct {
	reader  *io.PipeReader
	cancel  context.CancelFunc
	done    chan struct{}
	timeout time.Duration
	once    sync.Once
}

func (b *responseBody) Read(p []byte) (int, error) {
	started := time.Now()
	n, err := b.reader.Read(p)
	if b.timeout > 0 && time.Since(started) > b.timeout {
		b.Close()
		return n, ErrReadTimeout
	}
	return n, err
}

// Close releases the request: the handler's context is cancelled and a write
// it is blocked in fails, so a lazy stream stops where it is.
func (b *responseBody) Close() error {
	b.once.Do(func() {
		b.cancel()
		b.reader.CloseWithError(io.ErrClosedPipe)
	})
	return nil
}

// Wait blocks until the handler returned (tests use it to see that a closed
// stream really stopped). It is not part of the requests-compatible surface.
func (b *responseBody) Wait() { <-b.done }
