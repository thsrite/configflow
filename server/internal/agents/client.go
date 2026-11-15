package agents

// The HTTP client of every request the panel sends to an Agent
// (http://<host>:<port>/api/... with the Agent's Bearer token). Python used
// requests directly; here every call goes through Client, so tests use a fake
// Agent and later packages (#24–#31) reuse the same client.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"configflow/internal/jsonx"
)

// Timeout is requests' (connect, read) timeout pair. Read applies to every
// socket read (and write), not to the whole exchange, as in requests.
type Timeout struct {
	Connect time.Duration
	Read    time.Duration
}

// Seconds is a single-number requests timeout (both phases).
func Seconds(n float64) Timeout {
	d := time.Duration(n * float64(time.Second))
	return Timeout{Connect: d, Read: d}
}

// Pair is a (connect, read) requests timeout in seconds.
func Pair(connect, read float64) Timeout {
	return Timeout{Connect: time.Duration(connect * float64(time.Second)), Read: time.Duration(read * float64(time.Second))}
}

// Request is one call to an Agent.
type Request struct {
	Method  string // GET, POST, PUT, DELETE
	URL     string
	Header  http.Header
	Body    []byte
	Timeout Timeout
}

// Response is the Agent's answer.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// JSON is response.json() as requests 2.31 does it: without a charset from
// the headers, a body longer than 3 bytes is decoded with the guessed UTF
// codec (a UTF-8 BOM is skipped there); otherwise Text() is parsed, so an
// application/json body keeps its BOM (and fails) and invalid bytes become
// U+FFFD. The error text is json.loads' message.
func (r *Response) JSON() (any, error) {
	if _, ok := r.encoding(); !ok && len(r.Body) > 3 {
		if codec := guessJSONUTF(r.Body); codec != "" {
			if text, ok := pyDecode(r.Body, codec, true); ok {
				return jsonx.ParseString(text)
			}
		}
	}
	return jsonx.ParseString(r.Text())
}

// Text is response.text: the body decoded with the charset of the headers
// (ISO-8859-1 for a text type without one, utf-8 for application/json),
// invalid bytes replaced. An unknown codec decodes as UTF-8, as requests
// falls back to on LookupError. Without any charset requests guesses with
// charset_normalizer; here a BOM or the NUL pattern of UTF-16/32 decides,
// otherwise UTF-8 (apparentCodec).
//
// Not reproduced: codecs other than the UTF-8, Latin-1, ASCII, UTF-16 and
// UTF-32 families (e.g. charset=gbk) decode as UTF-8 here; Python decodes
// them with that codec.
func (r *Response) Text() string {
	if len(r.Body) == 0 {
		return ""
	}
	codec := apparentCodec(r.Body)
	if name, ok := r.encoding(); ok {
		codec = "utf-8"
		if known := pyCodec(name); known != "" {
			codec = known
		}
	}
	text, _ := pyDecode(r.Body, codec, false)
	return text
}

// Client sends requests to Agents. Errors are transport failures (the
// requests.RequestException family); an HTTP error status is a Response.
type Client interface {
	Do(ctx context.Context, request Request) (*Response, error)
}

// TransportError is requests.RequestException: the request may or may not
// have reached the Agent. Error() is the underlying Go error text, which
// differs from requests' exception texts (see the package documentation).
type TransportError struct {
	Err error
}

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// IsTransportError reports whether err is a TransportError (Python:
// isinstance(err, requests.RequestException)).
func IsTransportError(err error) bool {
	var transport *TransportError
	return errors.As(err, &transport)
}

// HTTPClient is the production Client: one connection per request, proxies
// from the environment like requests (trust_env), per-read timeouts.
type HTTPClient struct{}

type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c *deadlineConn) Read(p []byte) (int, error) {
	if c.timeout > 0 {
		c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Read(p)
}

func (c *deadlineConn) Write(p []byte) (int, error) {
	if c.timeout > 0 {
		c.Conn.SetWriteDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Write(p)
}

// Do sends the request.
func (HTTPClient) Do(ctx context.Context, request Request) (*Response, error) {
	dialer := &net.Dialer{Timeout: request.Timeout.Connect}
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &deadlineConn{Conn: conn, timeout: request.Timeout.Read}, nil
		},
	}
	defer transport.CloseIdleConnections()
	var body io.Reader
	if request.Body != nil {
		body = bytes.NewReader(request.Body)
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, request.URL, body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	for key, values := range request.Header {
		req.Header[key] = values
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

var (
	clientMu      sync.RWMutex
	defaultClient Client = HTTPClient{}
)

// SetDefaultClient replaces the client of every Manager that has none of its
// own (tests: a fake Agent) and returns a function restoring the previous one.
func SetDefaultClient(client Client) (restore func()) {
	clientMu.Lock()
	previous := defaultClient
	defaultClient = client
	clientMu.Unlock()
	return func() {
		clientMu.Lock()
		defaultClient = previous
		clientMu.Unlock()
	}
}

// DefaultClient is the client Managers use unless one was set on them.
func DefaultClient() Client {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return defaultClient
}
