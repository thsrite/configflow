package substore

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"configflow/internal/jsonx"
	"configflow/internal/urlx"
)

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
	for _, param := range parts[1:] {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}
		key, value, _ := strings.Cut(param, "=")
		key = strings.ToLower(strings.Trim(strings.TrimSpace(key), `'" `))
		if key == "charset" {
			return strings.Trim(strings.TrimSpace(value), `'" `), true
		}
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

// RequestsClient is the production HTTPClient, shaped after requests 2.31:
// default headers (python-requests User-Agent unless given, gzip/deflate),
// up to 30 redirects, proxies from the environment unless an explicit
// proxy is given, per-read timeouts, requote_uri on the URL.
//
// Not reproduced: urllib3's URL normalisation beyond requote_uri (host
// lowercasing, IDNA), and Go never sends loopback requests through an
// environment proxy where requests would (unless NO_PROXY lists them).
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

// PrepareURL is requests' URL preparation (PreparedRequest.prepare_url
// with urllib3 2.x parse_url): scheme and host lowercased, dot segments
// removed, invalid characters of path/query/fragment percent-encoded and
// existing escapes upper-cased, an empty path made "/", params appended to
// the query (urlencode), then requote_uri. A URL with a colon that does not
// start with "http" is left as it is (requests skips preparation there).
// IDNA encoding of non-ASCII hosts is not reproduced.
func PrepareURL(raw string, params []Param) (string, error) {
	if strings.Contains(raw, ":") && !strings.HasPrefix(asciiLower(raw), "http") {
		return raw, nil
	}
	if !schemeRE.MatchString(raw) {
		raw = "//" + raw
	}
	m := uriRE.FindStringSubmatchIndex(raw)
	group := func(i int) (string, bool) {
		if m[2*i] < 0 {
			return "", false
		}
		return raw[m[2*i]:m[2*i+1]], true
	}
	scheme, _ := group(1)
	authority, hasAuthority := group(2)
	path, _ := group(3)
	query, _ := group(4)
	fragment, _ := group(5)
	normalize := scheme == "" || asciiLower(scheme) == "http" || asciiLower(scheme) == "https"
	scheme = asciiLower(scheme)
	netloc := ""
	if hasAuthority && authority != "" {
		auth, hostPort := "", authority
		if i := strings.LastIndex(authority, "@"); i >= 0 {
			auth, hostPort = authority[:i], authority[i+1:]
		}
		host, port := hostPort, ""
		if strings.HasPrefix(hostPort, "[") {
			if i := strings.Index(hostPort, "]"); i >= 0 {
				host, port = hostPort[:i+1], strings.TrimPrefix(hostPort[i+1:], ":")
			}
		} else if i := strings.LastIndex(hostPort, ":"); i >= 0 {
			host, port = hostPort[:i], hostPort[i+1:]
		}
		if auth != "" && normalize {
			auth = encodeInvalidChars(auth, userinfoChars)
		}
		if normalize {
			host = asciiLower(host)
		}
		if auth != "" {
			netloc = auth + "@"
		}
		netloc += host
		// urllib3 parses the port as an int; requests drops port 0.
		if n, err := strconv.Atoi(port); err == nil && n > 0 {
			netloc += ":" + strconv.Itoa(n)
		} else if err != nil && port != "" {
			netloc += ":" + port
		}
	}
	if normalize && path != "" {
		path = encodeInvalidChars(removeDotSegments(path), pathChars)
	}
	if normalize && query != "" {
		query = encodeInvalidChars(query, pathChars+"?")
	}
	if normalize && fragment != "" {
		fragment = encodeInvalidChars(fragment, pathChars+"?")
	}
	if path == "" {
		path = "/"
	}
	if len(params) > 0 {
		encoded := make([]string, len(params))
		for i, p := range params {
			encoded[i] = urlx.QuotePlus(p.Key) + "=" + urlx.QuotePlus(p.Value)
		}
		if query != "" {
			query += "&" + strings.Join(encoded, "&")
		} else {
			query = strings.Join(encoded, "&")
		}
	}
	out := ""
	if scheme != "" {
		out = scheme + ":"
	}
	if netloc != "" || scheme == "http" || scheme == "https" {
		out += "//" + netloc
	}
	out += path
	if query != "" {
		out += "?" + query
	}
	if fragment != "" {
		out += "#" + fragment
	}
	return requoteURI(out), nil
}

var (
	schemeRE  = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+-]*:|/)`)
	uriRE     = regexp.MustCompile(`(?s)^(?:([a-zA-Z][a-zA-Z0-9+.-]*):)?(?://([^\\/?#]*))?([^?#]*)(?:\?([^#]*))?(?:#(.*))?$`)
	percentRE = regexp.MustCompile(`%[a-fA-F0-9]{2}`)
)

const (
	userinfoChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-~!$&'()*+,;=:"
	pathChars     = userinfoChars + "@/"
)

// encodeInvalidChars is urllib3's _encode_invalid_chars.
func encodeInvalidChars(component, allowed string) string {
	count := 0
	component = percentRE.ReplaceAllStringFunc(component, func(s string) string { count++; return strings.ToUpper(s) })
	isPercentEncoded := count == strings.Count(component, "%")
	var b strings.Builder
	for i := 0; i < len(component); i++ {
		c := component[i]
		if (isPercentEncoded && c == '%') || (c < 128 && strings.IndexByte(allowed, c) >= 0) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return b.String()
}

// removeDotSegments is urllib3's _remove_path_dot_segments.
func removeDotSegments(path string) string {
	var output []string
	for _, segment := range strings.Split(path, "/") {
		switch {
		case segment == ".":
		case segment != "..":
			output = append(output, segment)
		case len(output) > 0:
			output = output[:len(output)-1]
		}
	}
	if strings.HasPrefix(path, "/") && (len(output) == 0 || output[0] != "") {
		output = append([]string{""}, output...)
	}
	if strings.HasSuffix(path, "/.") || strings.HasSuffix(path, "/..") {
		output = append(output, "")
	}
	return strings.Join(output, "/")
}

const hexDigits = "0123456789ABCDEF"

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// requoteURI is requests.utils.requote_uri.
func requoteURI(uri string) string {
	if unquoted, ok := unquoteUnreserved(uri); ok {
		return quoteSafe(unquoted, "!#$%&'()*+,/:;=?@[]~")
	}
	return quoteSafe(uri, "!#$&'()*+,/:;=?@[]~")
}

// unquoteUnreserved is requests.utils.unquote_unreserved (false: InvalidURL).
func unquoteUnreserved(uri string) (string, bool) {
	parts := strings.Split(uri, "%")
	for i := 1; i < len(parts); i++ {
		h := parts[i]
		if len(h) >= 2 && isASCIIAlnum(h[0]) && isASCIIAlnum(h[1]) {
			if !isHex(h[0]) || !isHex(h[1]) {
				return "", false
			}
			c := unhex(h[0])<<4 | unhex(h[1])
			if isASCIIAlnum(c) || strings.IndexByte("-._~", c) >= 0 {
				parts[i] = string(rune(c)) + h[2:]
			} else {
				parts[i] = "%" + h
			}
		} else {
			parts[i] = "%" + h
		}
	}
	return strings.Join(parts, ""), true
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// quoteSafe is urllib.parse.quote(s, safe) on the UTF-8 bytes.
func quoteSafe(s, safe string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isASCIIAlnum(c) || strings.IndexByte("_.-~", c) >= 0 || strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return b.String()
}

// QuotePath is urllib.parse.quote(s, safe="").
func QuotePath(s string) string { return quoteSafe(s, "") }

// checkURL raises what requests raises before sending: a URL with a colon
// that does not start with "http" has no connection adapter
// (InvalidSchema), no scheme is MissingSchema, no host InvalidURL, another
// scheme InvalidSchema.
func checkURL(raw string) error {
	noAdapter := &RequestError{Class: "InvalidSchema", Message: fmt.Sprintf("No connection adapters were found for %s", jsonx.Repr(raw))}
	if strings.Contains(raw, ":") && !strings.HasPrefix(asciiLower(raw), "http") {
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
	if scheme := asciiLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return noAdapter
	}
	return nil
}

// Do implements HTTPClient.
func (RequestsClient) Do(ctx context.Context, request *Request) (*Response, error) {
	prepared, _ := PrepareURL(strings.TrimLeftFunc(request.URL, pyIsSpace), request.Params)
	if err := checkURL(prepared); err != nil {
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
	viaProxy := false
	proxyFunc := func(r *http.Request) (*url.URL, error) {
		var proxy *url.URL
		if request.Proxy != "" {
			parsed, err := url.Parse(request.Proxy)
			if err != nil {
				return nil, err
			}
			proxy = parsed
		} else {
			parsed, err := http.ProxyFromEnvironment(r)
			if err != nil {
				return nil, err
			}
			proxy = parsed
		}
		if proxy != nil {
			viaProxy = true
		}
		return proxy, nil
	}
	connected := false
	dialer := &net.Dialer{Timeout: request.Timeout.Connect}
	transport := &http.Transport{
		Proxy:              proxyFunc,
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
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) > 30 {
			return &RequestError{Class: "TooManyRedirects", Message: "Exceeded 30 redirects."}
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
