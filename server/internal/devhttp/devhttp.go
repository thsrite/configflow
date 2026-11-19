// Package devhttp is the HTTP client for the panel's network devices (the
// RouterOS REST and iKuai drivers). It keeps what the Python drivers'
// requests.Session (trust_env=False) did on the wire and in user messages:
//
//   - no proxy (the environment is ignored) and redirects are not followed;
//   - TLS verification on or off per device (routers use self-signed
//     certificates); HTTP/1.1 with keep-alive;
//   - optional Basic auth and an optional cookie jar (iKuai's session);
//   - JSON bodies with non-ASCII escaped as \uXXXX, like json.dumps;
//   - timeout as requests meant it: the connection must be set up within the
//     timeout and every wait for data from the device may take that long
//     again (there is no total limit);
//   - each failure carries the requests exception class name the drivers put
//     into their Chinese messages ("无法连接 RouterOS：ReadTimeout"). The
//     mapping was measured with requests 2.31 / urllib3 2.8
//     (testdata/requests_classes.py).
package devhttp

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"
	"unicode/utf8"
)

// Failure classes (requests exception class names).
const (
	ConnectionError      = "ConnectionError"
	ConnectTimeout       = "ConnectTimeout"
	ReadTimeout          = "ReadTimeout"
	SSLError             = "SSLError"
	ChunkedEncodingError = "ChunkedEncodingError"
	InvalidURL           = "InvalidURL"
)

// Error is a request that got no complete response.
type Error struct {
	Class string // one of the classes above
	Err   error
}

func (e *Error) Error() string { return e.Class + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Timeout: the device did not answer in time (requests' Timeout family).
func (e *Error) Timeout() bool { return e.Class == ConnectTimeout || e.Class == ReadTimeout }

// ClassOf is the failure class of err (ConnectionError when it is no *Error).
func ClassOf(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Class
	}
	return ConnectionError
}

// Config describes one device.
type Config struct {
	VerifyTLS bool
	// BasicAuth sends Username/Password on every request (also when empty).
	BasicAuth          bool
	Username, Password string
	// Cookies keeps cookies the device sets (a login session).
	Cookies bool
}

// Client talks to one device. It is safe for concurrent use.
type Client struct {
	cfg       Config
	auth      string
	transport *http.Transport
	jar       http.CookieJar // nil without Config.Cookies
}

// New returns a client for one device.
func New(cfg Config) *Client {
	c := &Client{cfg: cfg}
	if cfg.BasicAuth {
		c.auth = "Basic " + base64.StdEncoding.EncodeToString(credentials(cfg.Username+":"+cfg.Password))
	}
	c.transport = &http.Transport{
		DialContext:         c.dial,
		DialTLSContext:      c.dialTLS,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	if cfg.Cookies {
		c.jar, _ = cookiejar.New(nil)
	}
	return c
}

// credentials: Latin-1 when every character fits (what requests sent), else
// UTF-8 (requests raised UnicodeEncodeError there and the call crashed).
func credentials(text string) []byte {
	out := make([]byte, 0, len(text))
	for _, r := range text {
		if r > 0xFF {
			return []byte(text)
		}
		out = append(out, byte(r))
	}
	return out
}

// Close drops idle connections.
func (c *Client) Close() { c.transport.CloseIdleConnections() }

// Response is a complete device answer of any status.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body (numbers as json.Number); an error means it is not JSON.
func (r *Response) JSON(v any) error { return DecodeJSON(r.Body, v) }

// DecodeJSON decodes exactly one JSON value, keeping numbers as json.Number.
func DecodeJSON(data []byte, v any) error {
	return jsondecode.Decode(data, v)
}

// EncodeJSON is the request body for v: compact JSON with every non-ASCII
// character escaped (\uXXXX, surrogate pairs above U+FFFF) and no HTML
// escaping, which is what json.dumps sent.
func EncodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	raw := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	out := make([]byte, 0, len(raw))
	for len(raw) > 0 {
		r, size := utf8.DecodeRune(raw)
		switch {
		case r < utf8.RuneSelf:
			out = append(out, raw[0])
		case r > 0xFFFF:
			r -= 0x10000
			out = fmt.Appendf(out, `\u%04x\u%04x`, 0xD800+(r>>10), 0xDC00+(r&0x3FF))
		default:
			out = fmt.Appendf(out, `\u%04x`, r)
		}
		raw = raw[size:]
	}
	return out, nil
}

type timeoutKey struct{}

func timeoutOf(ctx context.Context) time.Duration {
	if t, ok := ctx.Value(timeoutKey{}).(time.Duration); ok {
		return t
	}
	return 0
}

func (c *Client) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: timeoutOf(ctx)}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		class := ConnectionError
		if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
			class = ConnectTimeout
		}
		return nil, &Error{Class: class, Err: err}
	}
	return conn, nil
}

// dialTLS: requests counted the handshake as reading (a stalled handshake is
// a ReadTimeout); any other TLS failure is an SSLError.
func (c *Client) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := c.dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(addr)
	tc := tls.Client(conn, &tls.Config{ServerName: host, InsecureSkipVerify: !c.cfg.VerifyTLS, //nolint:gosec // per-router setting
		NextProtos: []string{"http/1.1"}})
	if t := timeoutOf(ctx); t > 0 {
		_ = conn.SetDeadline(time.Now().Add(t))
	}
	err = tc.HandshakeContext(ctx)
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		conn.Close()
		class := SSLError
		if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
			class = ReadTimeout
		}
		return nil, &Error{Class: class, Err: err}
	}
	return tc, nil
}

var errReadTimeout = errors.New("devhttp: the device stopped answering")

// watchdog cancels the request when the device sends nothing for timeout.
type watchdog struct {
	mu      sync.Mutex
	timer   *time.Timer
	timeout time.Duration
	fire    func()
}

func (w *watchdog) kick() {
	if w.timeout <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer == nil {
		w.timer = time.AfterFunc(w.timeout, w.fire)
	} else {
		w.timer.Reset(w.timeout)
	}
}

func (w *watchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
}

// Do sends one request. body nil sends none; anything else is sent as JSON.
// A response of any status is returned whole; a failure is an *Error.
func (c *Client) Do(ctx context.Context, method, rawURL string, body any, timeout time.Duration) (*Response, error) {
	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		if err == nil {
			err = errors.New("devhttp: not an http(s) URL")
		}
		return nil, &Error{Class: InvalidURL, Err: err}
	}
	var payload []byte
	if body != nil {
		if payload, err = EncodeJSON(body); err != nil {
			return nil, &Error{Class: InvalidURL, Err: err} // unreachable for decoded values
		}
	}
	ctx, cancel := context.WithCancelCause(context.WithValue(ctx, timeoutKey{}, timeout))
	defer cancel(nil)
	dog := &watchdog{timeout: timeout, fire: func() { cancel(errReadTimeout) }}
	defer dog.stop()
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn:      func(httptrace.GotConnInfo) { dog.kick() },
		WroteRequest: func(httptrace.WroteRequestInfo) { dog.kick() },
	})
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, &Error{Class: InvalidURL, Err: err}
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "ConfigFlow")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	} else if user := target.User; user != nil {
		// Credentials in the management address (a login-protected reverse
		// proxy in front of iKuai) are sent as Basic auth, as requests and
		// http.Client do; explicit device credentials take precedence.
		password, _ := user.Password()
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(credentials(user.Username()+":"+password)))
	}
	// The transport is used directly: redirects are never followed, and
	// http.Client would fail on a 3xx whose Location does not parse before
	// handing the response back (the drivers explain such redirects).
	if c.jar != nil {
		for _, cookie := range c.jar.Cookies(req.URL) {
			req.AddCookie(cookie)
		}
	}
	resp, err := c.transport.RoundTrip(req)
	if err == nil && c.jar != nil {
		if cookies := resp.Cookies(); len(cookies) > 0 {
			c.jar.SetCookies(req.URL, cookies)
		}
	}
	if err != nil {
		if context.Cause(ctx) == errReadTimeout {
			return nil, &Error{Class: ReadTimeout, Err: errReadTimeout}
		}
		if e, ok := errors.AsType[*Error](err); ok {
			return nil, e
		}
		return nil, &Error{Class: ConnectionError, Err: err}
	}
	defer resp.Body.Close()
	dog.kick()
	var data bytes.Buffer
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			data.Write(buf[:n])
			dog.kick()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// requests: a stalled body is a ConnectionError, a broken one a
			// ChunkedEncodingError.
			if context.Cause(ctx) == errReadTimeout {
				return nil, &Error{Class: ConnectionError, Err: errReadTimeout}
			}
			return nil, &Error{Class: ChunkedEncodingError, Err: err}
		}
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: data.Bytes()}, nil
}
