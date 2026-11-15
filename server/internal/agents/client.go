package agents

// The HTTP client of every request the panel sends to an Agent
// (http://<host>:<port>/api/... with the Agent's Bearer token). Python used
// requests directly; here every call goes through Client, so tests use a fake
// Agent and later packages (#24–#31) reuse the same client.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
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
// them with that codec. Unobservable with ConfigFlow Agents (A01 review):
// the Go Agent answers application/json without charset (routes/utils.go
// sendJSON) or text/plain; charset=utf-8 (http.Error), and the Mihomo and
// OxiDNS answers it relays are UTF-8. Only a foreign server at an Agent's
// address could declare another charset, and then only the text of a
// failure diagnostic differs. The standard library has no such codecs.
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

// resolveHook, when set (tests only), replaces name resolution for hosts that
// are not numeric: it returns the address to dial or an error.
var resolveHook func(host string) (string, error)

// Do sends the request. The URL is treated as requests 2.31 prepares it:
// percent-escapes of unreserved characters are decoded (requote_uri), a
// user:password in the authority becomes Basic authentication replacing the
// Authorization header, and a host in any numeric form glibc's getaddrinfo
// accepts (inet_aton: "127.1", "0x7f.0.0.1", "2130706433", octal parts) is
// dialled as that address instead of being looked up as a name.
func (HTTPClient) Do(ctx context.Context, request Request) (*Response, error) {
	dialer := &net.Dialer{Timeout: request.Timeout.Connect}
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if host, port, err := net.SplitHostPort(address); err == nil && net.ParseIP(host) == nil {
				if ip, ok := inetAtonExact(host); ok {
					address = net.JoinHostPort(ip, port)
				} else if resolveHook != nil {
					resolved, err := resolveHook(host)
					if err != nil {
						return nil, err
					}
					address = net.JoinHostPort(resolved, port)
				}
			}
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
	req, err := http.NewRequestWithContext(ctx, request.Method, unquoteUnreserved(request.URL), body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	if label := invalidIDNALabel(req.URL.Hostname()); label != "" {
		return nil, &TransportError{Err: errors.New("Failed to parse: Name '" + label + "' is not a valid IDNA label")}
	}
	for key, values := range request.Header {
		req.Header[key] = values
	}
	if user := req.URL.User; user != nil {
		// requests: get_auth_from_url → HTTPBasicAuth (latin-1), only when a
		// password is present (unquote(None) fails and leaves no auth).
		if password, ok := user.Password(); ok {
			credentials, ok := latin1(user.Username() + ":" + password)
			if !ok {
				return nil, errors.New("'latin-1' codec can't encode characters in the URL credentials")
			}
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(credentials))
		}
		req.URL.User = nil
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

// unquoteUnreserved is requests.utils.unquote_unreserved as requote_uri uses
// it: %XX escapes of unreserved characters (letters, digits, "-._~") are
// decoded; an escape with non-hex alphanumerics leaves the URL as it is
// (requote_uri's InvalidURL fallback).
func unquoteUnreserved(uri string) string {
	if !strings.Contains(uri, "%") {
		return uri
	}
	parts := strings.Split(uri, "%")
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 2 || !isASCIIAlnum(part[0]) || !isASCIIAlnum(part[1]) {
			parts[i] = "%" + part
			continue
		}
		value, err := strconv.ParseUint(part[:2], 16, 8)
		if err != nil {
			return uri
		}
		if c := byte(value); isASCIIAlnum(c) || c == '-' || c == '.' || c == '_' || c == '~' {
			parts[i] = string(c) + part[2:]
		} else {
			parts[i] = "%" + part
		}
	}
	return strings.Join(parts, "")
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func latin1(s string) ([]byte, bool) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return nil, false
		}
		out = append(out, byte(r))
	}
	return out, true
}

// inetAtonExact is glibc's __inet_aton_exact, the numeric IPv4 forms
// getaddrinfo accepts: one to four parts, each decimal, octal (leading 0) or
// hex (0x), the last one filling the remaining bytes; nothing may follow.
func inetAtonExact(host string) (string, bool) {
	var parts []uint64
	i := 0
	for {
		if i >= len(host) || host[i] < '0' || host[i] > '9' {
			return "", false
		}
		base, value, digit := uint64(10), uint64(0), false
		if host[i] == '0' {
			i++
			if i < len(host) && (host[i] == 'x' || host[i] == 'X') {
				base = 16 // "0x" alone has no digit
				i++
			} else {
				base, digit = 8, true
			}
		}
		for ; i < len(host); i++ {
			c := host[i]
			var d uint64
			switch {
			case c >= '0' && c <= '9':
				if base == 8 && c >= '8' {
					return "", false
				}
				d = uint64(c - '0')
			case base == 16 && c >= 'a' && c <= 'f':
				d = uint64(c-'a') + 10
			case base == 16 && c >= 'A' && c <= 'F':
				d = uint64(c-'A') + 10
			default:
				goto partDone
			}
			value = value*base + d
			if value > 0xffffffff {
				return "", false
			}
			digit = true
		}
	partDone:
		if !digit {
			return "", false
		}
		if i < len(host) && host[i] == '.' {
			if len(parts) == 3 || value > 0xff {
				return "", false
			}
			parts = append(parts, value)
			i++
			continue
		}
		if i != len(host) {
			return "", false
		}
		limits := []uint64{0xffffffff, 0xffffff, 0xffff, 0xff}
		if value > limits[len(parts)] {
			return "", false
		}
		address := value
		for n, part := range parts {
			address |= part << (24 - 8*n)
		}
		return net.IPv4(byte(address>>24), byte(address>>16), byte(address>>8), byte(address)).String(), true
	}
}

// invalidIDNALabel returns the first non-ASCII label of host that urllib3's
// strict IDNA 2008 encoding (idna.encode(name.lower(), strict=True,
// std3_rules=True)) refuses because it holds a compatibility character
// (full-width and half-width forms, Roman numerals, enclosed alphanumerics,
// no-break and ideographic spaces), which Go's UTS #46 lookup would instead
// map to ASCII ("１２７.0.0.1" → "127.0.0.1"). Other IDNA 2008 rules are not
// checked (no golang.org/x/text): such hosts fail name resolution anyway.
func invalidIDNALabel(host string) string {
	for _, label := range strings.Split(host, ".") {
		for _, r := range label {
			if r >= 0xff01 && r <= 0xffef || r >= 0x2160 && r <= 0x2188 || r >= 0x2460 && r <= 0x24ff ||
				r == 0xa0 || r == 0x3000 {
				return label
			}
		}
	}
	return ""
}
