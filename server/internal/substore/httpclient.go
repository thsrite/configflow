package substore

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"configflow/internal/lifecycle"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"

	"configflow/internal/jsonx"
	"configflow/internal/weburl"
)

func isASCIIText(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Timeout is a requests timeout: Connect for establishing the connection,
// Read for every socket read (and write) afterwards.
type Timeout struct {
	Connect time.Duration
	Read    time.Duration
}

// Seconds is a single-number requests timeout (both phases).
func Seconds(n float64) Timeout {
	d := time.Duration(n * float64(time.Second))
	return Timeout{Connect: d, Read: d}
}

// Param is one query parameter of requests' params= (in order).
type Param struct{ Key, Value string }

// Request is one requests.get/post/delete call.
type Request struct {
	Method string
	URL    string
	Params []Param
	Header http.Header
	// JSON is the json= body (encoded like requests: json.dumps, ASCII);
	// nil = no body.
	JSON    any
	Timeout Timeout
	// Proxy is proxies={'http': p, 'https': p}; "" = the environment's
	// proxies (requests' trust_env).
	Proxy string
}

// Response is a requests.Response whose body was read (stream=False).
type Response struct {
	StatusCode int
	Reason     string
	Header     http.Header
	URL        string
	Body       []byte
}

// HeaderValue is resp.headers.get(name, ”): repeated fields joined with
// ", ", decoded as ISO-8859-1 like http.client.
func (r *Response) HeaderValue(name string) string {
	return latin1(strings.Join(r.Header.Values(name), ", "))
}

// RaiseForStatus is response.raise_for_status().
func (r *Response) RaiseForStatus() error {
	kind := ""
	switch {
	case r.StatusCode >= 400 && r.StatusCode < 500:
		kind = "Client Error"
	case r.StatusCode >= 500 && r.StatusCode < 600:
		kind = "Server Error"
	default:
		return nil
	}
	return &RequestError{Class: "HTTPError", Status: r.StatusCode,
		Message: fmt.Sprintf("%d %s: %s for url: %s", r.StatusCode, kind, r.Reason, r.URL)}
}

// encoding is requests.utils.get_encoding_from_headers.
func (r *Response) encoding() (string, bool) {
	contentType := r.HeaderValue("Content-Type")
	if contentType == "" {
		return "", false
	}
	parts := strings.Split(contentType, ";")
	mediaType := strings.TrimSpace(parts[0])
	// _parse_content_type_header: a dict of the parameters, so the last
	// charset wins.
	charset, hasCharset := "", false
	for _, param := range parts[1:] {
		param = pyStrip(param)
		if param == "" {
			continue
		}
		key, value, found := strings.Cut(param, "=")
		if !found {
			continue // value True: not a charset text
		}
		key = pyLower(strings.Trim(key, `"' `))
		if key == "charset" {
			charset, hasCharset = strings.Trim(strings.Trim(value, `"' `), `'"`), true
		}
	}
	if hasCharset {
		return charset, true
	}
	if strings.Contains(mediaType, "text") {
		return "ISO-8859-1", true
	}
	if strings.Contains(mediaType, "application/json") {
		return "utf-8", true
	}
	return "", false
}

// codecFamily maps a charset name to the codecs decoded here.
func codecFamily(name string) string {
	n := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_"), " ", "_")
	switch n {
	case "iso_8859_1", "iso8859_1", "8859", "cp819", "latin", "latin1", "latin_1", "l1", "iso_ir_100", "csisolatin1":
		return "latin-1"
	case "ascii", "us_ascii", "646", "us":
		return "ascii"
	case "utf_16", "utf16":
		return "utf-16"
	case "utf_16_le", "utf_16le":
		return "utf-16-le"
	case "utf_16_be", "utf_16be":
		return "utf-16-be"
	case "utf_8_sig":
		return "utf-8-sig"
	}
	return "utf-8"
}

func decodeWith(b []byte, codec string) string {
	switch codec {
	case "latin-1":
		return latin1(string(b))
	case "ascii":
		var out strings.Builder
		for _, c := range b {
			if c < 0x80 {
				out.WriteByte(c)
			} else {
				out.WriteRune(0xfffd)
			}
		}
		return out.String()
	case "utf-8-sig":
		return decodeUTF8Replace(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))
	case "utf-16", "utf-16-le", "utf-16-be":
		big := codec == "utf-16-be"
		if codec == "utf-16" {
			switch {
			case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
				b = b[2:]
			case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
				b, big = b[2:], true
			}
		}
		units := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if big {
				units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				units = append(units, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		text := string(utf16.Decode(units))
		if len(b)%2 == 1 {
			text += "�"
		}
		return text
	}
	return decodeUTF8Replace(b)
}

// Text is response.text: the body decoded with the charset of the headers
// (ISO-8859-1 for a text type without one, UTF-8 for application/json),
// undecodable bytes replaced. Without any charset requests guesses with
// charset_normalizer; here a BOM decides, otherwise UTF-8. Charsets outside
// the UTF-8, UTF-16, Latin-1 and ASCII families decode as UTF-8 (the
// standard library has no other codecs).
func (r *Response) Text() string {
	if len(r.Body) == 0 {
		return ""
	}
	if name, ok := r.encoding(); ok {
		return decodeWith(r.Body, codecFamily(name))
	}
	switch {
	case bytes.HasPrefix(r.Body, []byte{0xef, 0xbb, 0xbf}):
		return decodeWith(r.Body, "utf-8-sig")
	case bytes.HasPrefix(r.Body, []byte{0xff, 0xfe}), bytes.HasPrefix(r.Body, []byte{0xfe, 0xff}):
		return decodeWith(r.Body, "utf-16")
	}
	return decodeUTF8Replace(r.Body)
}

// JSON is response.json() (requests 2.31): without a charset in the headers
// a body longer than 3 bytes is decoded with the guessed UTF codec (a UTF-8
// BOM skipped); otherwise Text() is parsed. Failures are
// requests.exceptions.JSONDecodeError.
func (r *Response) JSON() (any, error) {
	text := ""
	if _, ok := r.encoding(); !ok && len(r.Body) > 3 {
		switch {
		case bytes.HasPrefix(r.Body, []byte{0xef, 0xbb, 0xbf}):
			text = decodeWith(r.Body, "utf-8-sig")
		case bytes.HasPrefix(r.Body, []byte{0xff, 0xfe}), bytes.HasPrefix(r.Body, []byte{0xfe, 0xff}):
			text = decodeWith(r.Body, "utf-16")
		default:
			text = r.Text()
		}
	} else {
		text = r.Text()
	}
	value, err := jsonx.ParseString(text)
	if err != nil {
		return nil, &RequestError{Class: "JSONDecodeError", Message: err.Error(), Err: err}
	}
	return value, nil
}

// RequestError is a requests.RequestException (Class is its class name:
// ConnectionError, ConnectTimeout, ReadTimeout, SSLError, ProxyError,
// ChunkedEncodingError, HTTPError, InvalidURL, MissingSchema,
// InvalidSchema, TooManyRedirects, JSONDecodeError …). Message texts are
// Go's; only the class and the status are visible (safe_exception_details).
type RequestError struct {
	Class   string
	Message string
	Status  int
	Err     error
}

func (e *RequestError) Error() string       { return e.Message }
func (e *RequestError) Unwrap() error       { return e.Err }
func (e *RequestError) PythonClass() string { return e.Class }
func (e *RequestError) HTTPStatus() int     { return e.Status }

// StatusOf is getattr(getattr(exc, 'response', None), 'status_code', None)
// (0 = none).
func StatusOf(err error) int {
	var request *RequestError
	if errors.As(err, &request) {
		return request.Status
	}
	return 0
}

// HTTPClient performs requests; tests point the production client at
// loopback servers or inject fakes.
type HTTPClient interface {
	Do(ctx context.Context, req *Request) (*Response, error)
}

// RequestsClient is the production HTTPClient, shaped after the requests
// library the Python panel used: default headers (python-requests User-Agent
// unless given, gzip/deflate), up to 30 redirects, the proxy from the
// environment (weburl.FromEnvironment, per hop) unless an explicit proxy is
// given, per-read timeouts, the URL normalised by weburl.Prepare.
type RequestsClient struct{}

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

type tunnelError struct{ status string }

func (e *tunnelError) Error() string { return "Tunnel connection failed: " + e.status }

// PrepareURL is the URL requested for raw plus params (weburl.Prepare). A
// URL with a colon that does not start with "http" is left as it is, so
// checkURL reports it as having no connection adapter; an unparsable URL is
// returned unchanged with the error.
func PrepareURL(raw string, params []Param) (string, error) {
	if strings.Contains(raw, ":") && !strings.HasPrefix(asciiLower(raw), "http") {
		return raw, nil
	}
	pairs := make([][2]string, len(params))
	for i, p := range params {
		pairs[i] = [2]string{p.Key, p.Value}
	}
	u, err := weburl.Prepare(raw, pairs)
	if err != nil {
		return raw, err
	}
	return u.String(), nil
}

// QuotePath percent-encodes s as one path segment.
func QuotePath(s string) string { return weburl.Quote(s) }

// checkURL raises what requests raises before sending: a URL with a colon
// that does not start with "http" has no connection adapter
// (InvalidSchema), no scheme is MissingSchema, no host InvalidURL, another
// scheme InvalidSchema.
func checkURL(raw string) error {
	noAdapter := &RequestError{Class: "InvalidSchema", Message: fmt.Sprintf("No connection adapters were found for %s", jsonx.Repr(raw))}
	if strings.Contains(raw, ":") && !strings.HasPrefix(pyLower(raw), "http") {
		return noAdapter
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return &RequestError{Class: "InvalidURL", Message: err.Error(), Err: err}
	}
	if parsed.Scheme == "" {
		return &RequestError{Class: "MissingSchema", Message: fmt.Sprintf("Invalid URL %s: No scheme supplied. Perhaps you meant https://%s?", jsonx.Repr(raw), raw)}
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return &RequestError{Class: "InvalidURL", Message: fmt.Sprintf("Invalid URL %s: No host supplied", jsonx.Repr(raw))}
	}
	if host := parsed.Hostname(); isASCIIText(host) && (strings.HasPrefix(host, "*") || strings.HasPrefix(host, ".")) {
		return &RequestError{Class: "InvalidURL", Message: "URL has an invalid label."}
	}
	if scheme := asciiLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return noAdapter
	}
	return nil
}

// Do implements HTTPClient.
func (RequestsClient) Do(ctx context.Context, request *Request) (*Response, error) {
	// Python's worker thread finishes a request even when the browser that
	// asked has gone: a cancelled request context must not abort a fetch
	// whose result (or failure cooldown) other callers share.
	ctx = lifecycle.Detach(ctx)
	prepared, prepareErr := PrepareURL(strings.TrimLeftFunc(request.URL, pyIsSpace), request.Params)
	if err := checkURL(prepared); err != nil {
		return nil, err
	}
	if prepareErr != nil {
		return nil, &RequestError{Class: "InvalidURL", Message: prepareErr.Error(), Err: prepareErr}
	}
	target, _ := url.Parse(prepared)
	// The proxy is chosen for the first URL before anything is sent.
	if _, err := chooseProxy(request.Proxy, target); err != nil {
		return nil, err
	}
	var body io.Reader
	var payload []byte
	if request.JSON != nil {
		encoded, err := jsonx.Default(request.JSON)
		if err != nil {
			return nil, &RequestError{Class: "InvalidJSONError", Message: err.Error(), Err: err}
		}
		payload = encoded
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, prepared, body)
	if err != nil {
		return nil, &RequestError{Class: "InvalidURL", Message: err.Error(), Err: err}
	}
	req.Header.Set("User-Agent", "python-requests/2.31.0")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range request.Header {
		req.Header[http.CanonicalHeaderKey(key)] = values
	}
	viaProxy, connected := false, false
	dialer := &net.Dialer{Timeout: request.Timeout.Connect}
	transport := &http.Transport{
		// Every redirect hop gets the proxy of its own URL; Go sends the
		// proxy URL's credentials as Proxy-Authorization.
		Proxy: func(r *http.Request) (*url.URL, error) {
			proxy, err := chooseProxy(request.Proxy, r.URL)
			viaProxy = proxy != nil
			return proxy, err
		},
		DisableKeepAlives:  true,
		DisableCompression: false,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			connected = true
			return &deadlineConn{Conn: conn, timeout: request.Timeout.Read}, nil
		},
		TLSHandshakeTimeout: request.Timeout.Connect,
		OnProxyConnectResponse: func(_ context.Context, _ *url.URL, _ *http.Request, res *http.Response) error {
			if res.StatusCode != 200 {
				return &tunnelError{res.Status}
			}
			return nil
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) > 30 {
			// requests raises with response= the 31st redirect response.
			status := 0
			if next.Response != nil {
				status = next.Response.StatusCode
			}
			return &RequestError{Class: "TooManyRedirects", Message: "Exceeded 30 redirects.", Status: status}
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, classifyRequestError(err, viaProxy, connected)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, classifyBodyError(err)
	}
	if data, err = decodeContent(data, resp.Header.Values("Content-Encoding")); err != nil {
		return nil, err
	}
	_, reason, _ := strings.Cut(resp.Status, " ")
	return &Response{StatusCode: resp.StatusCode, Reason: reason, Header: resp.Header,
		URL: resp.Request.URL.String(), Body: data}, nil
}

// chooseProxy is the proxy for target: the explicit one when given (it wins
// over the environment), else the environment's. nil = direct.
func chooseProxy(explicit string, target *url.URL) (*url.URL, error) {
	var proxy *url.URL
	var err error
	if explicit != "" {
		proxy, err = weburl.ParseProxy(explicit)
	} else {
		proxy, err = weburl.FromEnvironment(target)
	}
	if err != nil {
		return nil, &RequestError{Class: "InvalidProxyURL", Message: err.Error(), Err: err}
	}
	return proxy, nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout() || errors.Is(err, context.DeadlineExceeded)
}

// classifyRequestError maps a failure before the response headers to the
// requests exception class.
func classifyRequestError(err error, viaProxy, connected bool) error {
	var request *RequestError
	if errors.As(err, &request) {
		return request
	}
	var tunnel *tunnelError
	var op *net.OpError
	var certErr *x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &tunnel):
		return &RequestError{Class: "ProxyError", Message: err.Error(), Err: err}
	case errors.As(err, &op) && op.Op == "proxyconnect":
		return &RequestError{Class: "ProxyError", Message: err.Error(), Err: err}
	case errors.As(err, &certErr), errors.As(err, &hostErr), errors.As(err, &invalid):
		return &RequestError{Class: "SSLError", Message: err.Error(), Err: err}
	case isTimeout(err):
		if !connected {
			return &RequestError{Class: "ConnectTimeout", Message: err.Error(), Err: err}
		}
		return &RequestError{Class: "ReadTimeout", Message: err.Error(), Err: err}
	case strings.Contains(err.Error(), "tls:"):
		return &RequestError{Class: "SSLError", Message: err.Error(), Err: err}
	}
	if viaProxy && !connected {
		return &RequestError{Class: "ProxyError", Message: err.Error(), Err: err}
	}
	return &RequestError{Class: "ConnectionError", Message: err.Error(), Err: err}
}

// classifyBodyError maps a failure while reading the body: a read timeout
// is requests' ConnectionError there, anything else ChunkedEncodingError.
func classifyBodyError(err error) error {
	if isTimeout(err) {
		return &RequestError{Class: "ConnectionError", Message: err.Error(), Err: err}
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "chunk") {
		return &RequestError{Class: "ChunkedEncodingError", Message: err.Error(), Err: err}
	}
	return &RequestError{Class: "ContentDecodingError", Message: err.Error(), Err: err}
}

// decodeContent applies Content-Encoding like urllib3 (gzip and deflate;
// other encodings are left as they are), last encoding first.
func decodeContent(data []byte, header []string) ([]byte, error) {
	var encodings []string
	for _, value := range header {
		for _, part := range strings.Split(value, ",") {
			if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
				encodings = append(encodings, part)
			}
		}
	}
	for i := len(encodings) - 1; i >= 0; i-- {
		var reader io.Reader
		switch encodings[i] {
		case "gzip", "x-gzip":
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, &RequestError{Class: "ContentDecodingError", Message: err.Error(), Err: err}
			}
			reader = gz
		case "deflate":
			if z, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
				reader = z
			} else {
				reader = flate.NewReader(bytes.NewReader(data))
			}
		default:
			continue
		}
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return nil, &RequestError{Class: "ContentDecodingError", Message: err.Error(), Err: err}
		}
		data = decoded
	}
	return data, nil
}
