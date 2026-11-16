package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/localhttp"
	"configflow/internal/urlx"
)

// Ctx is one request: Flask's request object plus flask.g.
type Ctx struct {
	// Request is the underlying request. Prefer the helpers: they follow
	// Werkzeug's semantics.
	Request *http.Request
	// Endpoint is request.endpoint ("" when no rule matched).
	Endpoint string
	// Blueprint is request.blueprint ("" when no rule matched).
	Blueprint string
	// ProfileID is g.configflow_profile_id: the profile the request is bound
	// to ("" for global requests). It is echoed as X-ConfigFlow-Profile.
	ProfileID string

	app    *App
	ctx    context.Context
	route  *Route
	params map[string]string
	path   string

	body     []byte
	bodyRead bool
	bodyErr  error

	jsonDone  bool
	jsonValue any
	jsonErr   error

	values map[string]any
	query  [][2]string
}

// Context is the request context: it carries the repository Scope
// (repo.WithScope) and is cancelled when the client goes away. Pass it to
// every repo call.
func (c *Ctx) Context() context.Context { return c.ctx }

// SetContext replaces the request context (to add values for later hooks).
func (c *Ctx) SetContext(ctx context.Context) { c.ctx = ctx }

// App returns the application serving the request.
func (c *Ctx) App() *App { return c.app }

// Method is request.method.
func (c *Ctx) Method() string { return c.Request.Method }

// Path is request.path (percent-decoded).
func (c *Ctx) Path() string { return c.path }

// Param is a view argument (<name> in the rule); "" when absent.
func (c *Ctx) Param(name string) string { return c.params[name] }

// LookupParam is a view argument and whether the rule has it.
func (c *Ctx) LookupParam(name string) (string, bool) {
	v, ok := c.params[name]
	return v, ok
}

// Params returns request.view_args.
func (c *Ctx) Params() map[string]string { return c.params }

// Arg is request.args.get(name): the first value, ok false when absent.
func (c *Ctx) Arg(name string) (string, bool) {
	for _, pair := range c.queryPairs() {
		if pair[0] == name {
			return pair[1], true
		}
	}
	return "", false
}

// ArgList is request.args.getlist(name).
func (c *Ctx) ArgList(name string) []string {
	var out []string
	for _, pair := range c.queryPairs() {
		if pair[0] == name {
			out = append(out, pair[1])
		}
	}
	return out
}

// queryPairs is Werkzeug's query parsing: "&"-separated pairs, "+" is a
// space, percent escapes decoded (the bytes of invalid UTF-8 re-quoted as
// %XX, errors="werkzeug.url_quote"), empty pairs skipped, a pair without "="
// has an empty value.
func (c *Ctx) queryPairs() [][2]string {
	if c.query == nil {
		c.query = ParseQuery(c.Request.URL.RawQuery)
	}
	return c.query
}

// ParseQuery splits a raw query string the way Werkzeug does.
func ParseQuery(raw string) [][2]string {
	pairs := [][2]string{}
	for _, piece := range strings.Split(raw, "&") {
		if piece == "" {
			continue
		}
		key, value, _ := strings.Cut(piece, "=")
		pairs = append(pairs, [2]string{urlx.UnquotePlusURLQuote(key), urlx.UnquotePlusURLQuote(value)})
	}
	return pairs
}

// ArgDefault is request.args.get(name, default).
func (c *Ctx) ArgDefault(name, def string) string {
	if v, ok := c.Arg(name); ok {
		return v
	}
	return def
}

// ArgInt is request.args.get(name, def, type=int): the default when the
// argument is missing or int() rejects it. Values beyond int64 saturate.
func (c *Ctx) ArgInt(name string, def int64) int64 {
	v, ok := c.Arg(name)
	if !ok {
		return def
	}
	n, ok := PyParseInt(v)
	if !ok {
		return def
	}
	return saturate(n)
}

func saturate(n *big.Int) int64 {
	if n.IsInt64() {
		return n.Int64()
	}
	if n.Sign() < 0 {
		return -1 << 63
	}
	return 1<<63 - 1
}

// Args returns the query pairs in order.
func (c *Ctx) Args() [][2]string { return c.queryPairs() }

// HasArgs is bool(request.args).
func (c *Ctx) HasArgs() bool { return len(c.queryPairs()) > 0 }

// Header is request.headers.get(name); ok is false when absent. Repeated
// headers are joined with ", " as the WSGI server does.
func (c *Ctx) Header(name string) (string, bool) {
	return headerValue(c.Request.Header, name)
}

func headerValue(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", false
	}
	return strings.Join(values, ", "), true
}

// HeaderDefault is request.headers.get(name, default).
func (c *Ctx) HeaderDefault(name, def string) string {
	if v, ok := c.Header(name); ok {
		return v
	}
	return def
}

// RemoteAddr is request.remote_addr: the peer address without port. Behind
// the built-in web front end this is 127.0.0.1, as behind nginx; the client
// is in X-Real-IP / X-Forwarded-For.
func (c *Ctx) RemoteAddr() string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}

// Scheme is request.scheme (the WSGI url_scheme: "http" unless TLS).
func (c *Ctx) Scheme() string {
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

// werkzeugHost is werkzeug.sansio.utils._host_re (re.ASCII | re.IGNORECASE |
// re.VERBOSE), matched in full.
var werkzeugHost = regexp.MustCompile(`^(?:[a-zA-Z0-9.-]+|\[[a-fA-F0-9]*:[a-fA-F0-9.:]+\])(?::[0-9]+)?$`)

// Host is request.host, Werkzeug 3.1's get_host: the Host header without the
// scheme's default port (":80" / ":443"), or "" when its characters are not
// valid for a host.
func (c *Ctx) Host() string {
	host := c.Request.Host
	if host == "" {
		return ""
	}
	if c.Scheme() == "https" {
		host = strings.TrimSuffix(host, ":443")
	} else {
		host = strings.TrimSuffix(host, ":80")
	}
	if !werkzeugHost.MatchString(host) {
		return ""
	}
	return host
}

// currentURL is get_current_url(scheme, host) + root: uri_to_iri lower-cases
// the host name and writes the port as a number (dropped when 0). Not
// reproduced, as Python raises there (a 500): a port above 65535, an invalid
// bracketed IPv6 address, and IDNA decoding of "xn--" labels (kept as is).
func currentURL(scheme, host, root string) string {
	netloc := ""
	if host != "" {
		name, port := host, ""
		if strings.HasPrefix(host, "[") {
			end := strings.IndexByte(host, ']')
			name, port = host[1:end], strings.TrimPrefix(host[end+1:], ":")
		} else {
			name, port, _ = strings.Cut(host, ":")
		}
		netloc = strings.ToLower(name) // ASCII only (validated above)
		if strings.Contains(netloc, ":") {
			netloc = "[" + netloc + "]"
		}
		if port != "" {
			if n, err := strconv.Atoi(port); err == nil && n <= 65535 {
				if n != 0 {
					netloc += ":" + strconv.Itoa(n)
				}
			} else {
				netloc += ":" + port
			}
		}
	}
	return scheme + "://" + netloc + root + "/"
}

// HostURL is request.host_url ("http://host/").
func (c *Ctx) HostURL() string { return currentURL(c.Scheme(), c.Host(), "") }

// URLRoot is request.url_root: HostURL plus the script root of an in-process
// dispatch (localhttp).
func (c *Ctx) URLRoot() string {
	root := strings.TrimRight(localhttp.ScriptRoot(c.Request.Context()), "/")
	return currentURL(c.Scheme(), c.Host(), root)
}

// ContentLength is request.content_length (ok false when unknown).
func (c *Ctx) ContentLength() (int64, bool) {
	if c.Request.ContentLength < 0 {
		return 0, false
	}
	if c.Request.ContentLength == 0 && c.Request.Header.Get("Content-Length") == "" {
		return 0, false
	}
	return c.Request.ContentLength, true
}

// Stream is request.stream: the unread body. Do not mix with Data/JSON.
func (c *Ctx) Stream() io.Reader { return c.Request.Body }

// Data is request.get_data(): the whole body, read once and cached.
func (c *Ctx) Data() ([]byte, error) {
	if !c.bodyRead {
		c.bodyRead = true
		if c.Request.Body != nil {
			c.body, c.bodyErr = io.ReadAll(c.Request.Body)
		}
	}
	return c.body, c.bodyErr
}

// Mimetype is request.mimetype: the lower-cased Content-Type without
// parameters.
func (c *Ctx) Mimetype() string {
	mimetype, _, _ := strings.Cut(c.Request.Header.Get("Content-Type"), ";")
	return strings.ToLower(strings.TrimSpace(mimetype))
}

// IsJSON is request.is_json.
func (c *Ctx) IsJSON() bool {
	m := c.Mimetype()
	return m == "application/json" || strings.HasPrefix(m, "application/") && strings.HasSuffix(m, "+json")
}

// ErrNotJSON is the 415 of request.json for a request that is not
// application/json.
var errNotJSONDescription = "Did not attempt to load JSON data because the request Content-Type was not 'application/json'."

// JSON is request.json / request.get_json(): the decoded body (a jsonx
// value). Errors are *HTTPError: 415 when the Content-Type is not JSON, 400
// (Flask's generic Bad Request, not the decoder message) when the body does
// not decode. Return the error to send Werkzeug's page, or use err.Error()
// where Python put str(e) into a message.
func (c *Ctx) JSON() (any, error) {
	if !c.IsJSON() {
		return nil, &HTTPError{Code: 415, Description: errNotJSONDescription}
	}
	value, err := c.decodeJSON()
	if err != nil {
		return nil, BadRequest("")
	}
	return value, nil
}

// JSONSilent is request.get_json(silent=True): nil when the body is not JSON
// (wrong Content-Type or undecodable).
func (c *Ctx) JSONSilent() any {
	if !c.IsJSON() {
		return nil
	}
	value, err := c.decodeJSON()
	if err != nil {
		return nil
	}
	return value
}

// JSONForce is request.get_json(force=True, silent=True): the body decoded
// whatever the Content-Type, nil when it does not decode.
func (c *Ctx) JSONForce() any {
	value, err := c.decodeJSON()
	if err != nil {
		return nil
	}
	return value
}

// JSONObject is the common `data = request.get_json(silent=True); if not
// isinstance(data, dict)` pattern: the object or nil.
func (c *Ctx) JSONObject() *jsonx.Object {
	o, _ := c.JSONSilent().(*jsonx.Object)
	return o
}

// JSONForceError is JSONForce with the decoding error: json.loads(body) for
// handlers that parse the raw body themselves and must tell its exceptions
// apart (a SyntaxError is JSONDecodeError; an invalid encoding is
// UnicodeDecodeError; other errors are other ValueErrors).
func (c *Ctx) JSONForceError() (any, error) { return c.decodeJSON() }

func (c *Ctx) decodeJSON() (any, error) {
	if c.jsonDone {
		return c.jsonValue, c.jsonErr
	}
	c.jsonDone = true
	data, err := c.Data()
	if err == nil {
		var text []byte
		text, err = decodeJSONBytes(data)
		if err == nil {
			c.jsonValue, err = jsonx.Parse(text)
		}
	}
	c.jsonErr = err
	return c.jsonValue, c.jsonErr
}

// decodeJSONBytes is the bytes branch of json.loads: detect UTF-8 (BOM
// stripped), UTF-16 and UTF-32 and return UTF-8 text.
func decodeJSONBytes(b []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE, 0, 0}) || bytes.HasPrefix(b, []byte{0, 0, 0xFE, 0xFF}):
		return decodeUTF32(b[4:], b[0] == 0xFF)
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return decodeUTF16(b[2:], b[0] == 0xFF)
	case len(b) >= 4:
		switch {
		case b[0] == 0 && b[1] == 0 && b[2] == 0:
			return decodeUTF32(b, false)
		case b[0] == 0 && b[2] == 0:
			return decodeUTF16(b, false)
		case b[1] == 0 && b[2] == 0 && b[3] == 0:
			return decodeUTF32(b, true)
		case b[1] == 0 && b[3] == 0:
			return decodeUTF16(b, true)
		}
	case len(b) == 2:
		if b[0] == 0 {
			return decodeUTF16(b, false)
		}
		if b[1] == 0 {
			return decodeUTF16(b, true)
		}
	}
	if !utf8.Valid(b) {
		return nil, errors.New("invalid utf-8")
	}
	return b, nil
}

func decodeUTF16(b []byte, little bool) ([]byte, error) {
	if len(b)%2 != 0 {
		return nil, errors.New("truncated utf-16")
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		if little {
			units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		} else {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
	}
	return []byte(string(utf16.Decode(units))), nil
}

func decodeUTF32(b []byte, little bool) ([]byte, error) {
	if len(b)%4 != 0 {
		return nil, errors.New("truncated utf-32")
	}
	var out []rune
	for i := 0; i < len(b); i += 4 {
		var r uint32
		if little {
			r = uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
		} else {
			r = uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
		}
		if r > utf8.MaxRune {
			return nil, errors.New("invalid utf-32")
		}
		out = append(out, rune(r))
	}
	return []byte(string(out)), nil
}

// Form is request.form.get(name) for urlencoded and multipart bodies.
func (c *Ctx) Form(name string) (string, bool) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return "", false
	}
	values, ok := c.Request.PostForm[name]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// Set stores a value on flask.g for later hooks of the same request.
func (c *Ctx) Set(key string, value any) {
	if c.values == nil {
		c.values = map[string]any{}
	}
	c.values[key] = value
}

// Get reads a value stored with Set.
func (c *Ctx) Get(key string) (any, bool) {
	v, ok := c.values[key]
	return v, ok
}

// IsInternalCall is backend.common.internal_call.is_internal_call(): the
// request was made in-process by Invoke (the MCP layer) after it
// authenticated the caller itself.
func (c *Ctx) IsInternalCall() bool {
	value, ok := c.Header(InternalCallHeader)
	return ok && value == InternalCallToken()
}
