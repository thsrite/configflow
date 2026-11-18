package rulefetch

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"configflow/internal/pystr"
	"configflow/internal/weburl"
)

// FetchError is a requests.RequestException raised by a rule download:
// Class is the requests class name (ConnectionError, SSLError, ProxyError,
// ConnectTimeout, ReadTimeout, ChunkedEncodingError, ContentDecodingError,
// HTTPError, TooManyRedirects). Transient marks what
// rule_fetch._is_transient_disconnect retries: interrupted connections
// (reset, abort, broken pipe, a server or proxy closing before or during
// the response, TLS EOF, a body cut short), never certificate failures,
// timeouts, refused connections, DNS errors, HTTP statuses, decoding errors
// or invalid chunk lengths. Message texts are Go's (requests renders
// urllib3 objects there); the class and the retry decision follow Python.
type FetchError struct {
	Class     string
	Message   string
	Transient bool
	Status    int // HTTPError: the response status
	Err       error
}

func (e *FetchError) Error() string { return e.Message }
func (e *FetchError) Unwrap() error { return e.Err }

// PythonClass is type(exc).__name__.
func (e *FetchError) PythonClass() string { return e.Class }

// HTTPStatus is exc.response.status_code for an HTTPError (0 otherwise).
func (e *FetchError) HTTPStatus() int { return e.Status }

// IsTimeout is isinstance(err, requests.Timeout).
func IsTimeout(err error) bool {
	var f *FetchError
	return errors.As(err, &f) && (f.Class == "ConnectTimeout" || f.Class == "ReadTimeout")
}

// IsRequestException is isinstance(err, requests.RequestException)
// (FetchError or RuleFetchError).
func IsRequestException(err error) bool {
	var f *FetchError
	var r *RuleFetchError
	return errors.As(err, &f) || errors.As(err, &r)
}

// PythonClass is type(err).__name__ for the errors of this package and the
// repository errors it passes on; plain errors are ValueError.
func PythonClass(err error) string {
	if class := pythonClassOf(err); class != "" {
		return class
	}
	return "ValueError"
}

// TransportRequest is one HTTP request without redirect handling.
type TransportRequest struct {
	Method string // "GET" or "HEAD"
	URL    string
	// Proxy is the forward proxy URL ("" = direct; environment proxies are
	// never used).
	Proxy          string
	ConnectTimeout time.Duration // 0 = none
	ReadTimeout    time.Duration // 0 = none
	Insecure       bool          // verify=False
	CABundle       string        // verify=<path>; "" = default trust
}

// Response is the requests.Response of a rule download.
type Response struct {
	StatusCode int
	Reason     string
	Header     http.Header
	URL        string
	Body       io.ReadCloser // decoded body
	content    []byte
	consumed   bool
}

// Content is response.content: the whole decoded body (read once).
func (r *Response) Content() ([]byte, error) {
	if r.consumed {
		return r.content, nil
	}
	data, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, classifyBodyError(err)
	}
	r.content, r.consumed = data, true
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data, nil
}

// Close is response.close().
func (r *Response) Close() {
	if r.Body != nil {
		r.Body.Close()
	}
}

// HeaderValue is response.headers.get(name): repeated fields joined with
// ", " (urllib3's HTTPHeaderDict); ok is false when the field is absent.
func (r *Response) HeaderValue(name string) (string, bool) {
	values := r.Header.Values(name)
	if len(values) == 0 {
		return "", false
	}
	return strings.Join(values, ", "), true
}

// Read reads the decoded body (iter_content), classifying failures.
func (r *Response) Read(p []byte) (int, error) {
	n, err := r.Body.Read(p)
	if err != nil && err != io.EOF {
		return n, classifyBodyError(err)
	}
	return n, err
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
	return &FetchError{Class: "HTTPError", Status: r.StatusCode,
		Message: fmt.Sprintf("%d %s: %s for url: %s", r.StatusCode, kind, r.Reason, r.URL)}
}

// Transport performs single requests; tests inject fakes.
type Transport interface {
	Do(ctx context.Context, req TransportRequest) (*Response, error)
}

// HTTPTransport is the network transport: requests' default headers, the
// configured proxy only, TLS verification for the proxy and the origin with
// the same trust (REQUESTS_CA_BUNDLE / CURL_CA_BUNDLE apply when verify is
// the default), gzip/deflate decoding as urllib3 does, no redirects, no keep-alive.
type HTTPTransport struct {
	// Dial replaces the TCP dialer (tests: host overrides); nil dials
	// with net.Dialer.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
}

type tunnelError struct{ message string }

func (e *tunnelError) Error() string { return e.message }

// pyOSError is an error raised outside requests' exception hierarchy (a
// missing CA bundle, unencodable proxy credentials): not a
// RequestException, so neither retried nor answered by the GitHub mirror.
type pyOSError struct{ class, message string }

func (e *pyOSError) Error() string       { return e.message }
func (e *pyOSError) PythonClass() string { return e.class }

// verifyBundle is the verify value requests ends up with: verify=<path>, else
// REQUESTS_CA_BUNDLE or CURL_CA_BUNDLE (merge_environment_settings), "" for
// the default trust.
func verifyBundle(req TransportRequest) string {
	if req.CABundle != "" {
		return req.CABundle
	}
	if bundle := os.Getenv("REQUESTS_CA_BUNDLE"); bundle != "" {
		return bundle
	}
	return os.Getenv("CURL_CA_BUNDLE")
}

// latin1Text is how http.client decodes header bytes (ISO-8859-1).
func latin1Text(raw string) string {
	runes := make([]rune, len(raw))
	for i := 0; i < len(raw); i++ {
		runes[i] = rune(raw[i])
	}
	return string(runes)
}

// Do implements Transport: one request with requests' default headers, the
// URL normalised by weburl.Prepare, the configured proxy (if any) for every
// scheme, header values decoded as ISO-8859-1.
func (h HTTPTransport) Do(ctx context.Context, req TransportRequest) (*Response, error) {
	preparedURL, err := weburl.Prepare(req.URL, nil)
	if err != nil {
		return nil, &FetchError{Class: "InvalidURL", Message: err.Error(), Err: err}
	}
	prepared := preparedURL.String()
	var chosen *url.URL
	if req.Proxy != "" {
		if chosen, err = weburl.ParseProxy(req.Proxy); err != nil {
			return nil, &FetchError{Class: "InvalidProxyURL", Message: err.Error(), Err: err}
		}
	}
	tlsConfig := &tls.Config{}
	if req.Insecure {
		tlsConfig.InsecureSkipVerify = true
	} else if bundle := verifyBundle(req); bundle != "" {
		_, statErr := os.Stat(bundle)
		switch {
		case statErr != nil && chosen != nil && chosen.Scheme == "https":
			// _request_with_https_proxy: ssl.create_default_context(cafile=...)
			return nil, &pyOSError{class: "FileNotFoundError", message: "[Errno 2] No such file or directory"}
		case statErr != nil && strings.HasPrefix(strings.ToLower(prepared), "https"):
			// HTTPAdapter.cert_verify (https URLs only).
			return nil, &pyOSError{class: "OSError",
				message: "Could not find a suitable TLS CA certificate bundle, invalid path: " + bundle}
		case statErr == nil:
			tlsConfig.RootCAs = loadBundle(bundle)
		}
	}
	dialer := &net.Dialer{Timeout: req.ConnectTimeout}
	transport := &http.Transport{
		DialContext: dialer.DialContext,

		TLSClientConfig:    tlsConfig,
		DisableKeepAlives:  true,
		DisableCompression: true,
		ForceAttemptHTTP2:  false,
		Proxy:              func(*http.Request) (*url.URL, error) { return nil, nil },
		OnProxyConnectResponse: func(_ context.Context, _ *url.URL, _ *http.Request, res *http.Response) error {
			if res.StatusCode != 200 {
				return &tunnelError{"Tunnel connection failed: " + res.Status}
			}
			return nil
		},
		TLSHandshakeTimeout:   req.ConnectTimeout,
		ResponseHeaderTimeout: req.ReadTimeout,
	}
	if h.Dial != nil {
		dial := h.Dial
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if req.ConnectTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, req.ConnectTimeout)
				defer cancel()
			}
			return dial(ctx, network, address)
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, prepared, nil)
	if err != nil {
		return nil, &FetchError{Class: "InvalidURL", Message: err.Error(), Err: err}
	}
	httpReq.Header.Set("User-Agent", "python-requests/2.31.0")
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate")
	httpReq.Header.Set("Accept", "*/*")
	if chosen != nil {
		// Go sends the proxy URL's credentials as Proxy-Authorization (with
		// CONNECT for https targets, with the request for http targets).
		transport.Proxy = http.ProxyURL(chosen)
	}
	if user, password, ok := urlBasicAuth(preparedURL); ok {
		// PreparedRequest.prepare_auth: credentials of the URL, latin-1.
		header, err := basicAuthHeader(user, password)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", header)
	}
	// RoundTrip, not http.Client: the client parses a redirect's Location
	// with Go's URL rules before any redirect policy runs, and requests
	// leaves that to the caller (request_rule).
	res, err := transport.RoundTrip(httpReq)
	if err != nil {
		return nil, classifyRequestError(err, req.Proxy != "")
	}
	body, err := decodeBody(res)
	if err != nil {
		res.Body.Close()
		return nil, err
	}
	if req.ReadTimeout > 0 {
		body = &timeoutBody{ReadCloser: body, timeout: req.ReadTimeout}
	}
	header := make(http.Header, len(res.Header)+1)
	if len(res.TransferEncoding) > 0 {
		// net/http moves the field out of Header; requests keeps it.
		header["Transfer-Encoding"] = []string{strings.Join(res.TransferEncoding, ", ")}
	}
	for key, values := range res.Header {
		decoded := make([]string, len(values))
		for i, value := range values {
			decoded[i] = latin1Text(value)
		}
		header[key] = decoded
	}
	_, reason, _ := strings.Cut(res.Status, " ")
	return &Response{StatusCode: res.StatusCode, Reason: latin1Text(reason), Header: header, URL: prepared, Body: boundDecodedBody(body)}, nil
}

// loadBundle reads a CA file or every file of a CA directory (capath).
func loadBundle(bundle string) *x509.CertPool {
	pool := x509.NewCertPool()
	info, err := os.Stat(bundle)
	if err != nil {
		return pool
	}
	files := []string{bundle}
	if info.IsDir() {
		entries, _ := os.ReadDir(bundle)
		files = files[:0]
		for _, e := range entries {
			files = append(files, filepath.Join(bundle, e.Name()))
		}
	}
	for _, name := range files {
		if data, err := os.ReadFile(name); err == nil {
			pool.AppendCertsFromPEM(data)
		}
	}
	return pool
}

// timeoutBody applies the read timeout to each body read, like urllib3's
// socket timeout.
type timeoutBody struct {
	io.ReadCloser
	timeout time.Duration
}

func (b *timeoutBody) Read(p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := b.ReadCloser.Read(p)
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(b.timeout):
		b.ReadCloser.Close()
		r := <-done
		_ = r
		return 0, errBodyTimeout
	}
}

var errBodyTimeout = errors.New("Read timed out.")

// decodeBody applies Content-Encoding the way urllib3 2.8 (without brotli
// and zstd support, as installed) does: "gzip", "x-gzip" or "deflate" alone,
// or a comma list naming at least one of them, which then decodes every
// listed coding last first (gzip for gzip/x-gzip, deflate for anything else,
// more than five codings a DecodeError); any other value is left encoded.
func decodeBody(res *http.Response) (io.ReadCloser, error) {
	if res.Request.Method == "HEAD" {
		return res.Body, nil
	}
	values := res.Header.Values("Content-Encoding")
	if len(values) == 0 {
		return res.Body, nil
	}
	encoding := pystr.Lower(latin1Text(strings.Join(values, ", ")))
	known := map[string]bool{"gzip": true, "x-gzip": true, "deflate": true}
	var codings []string
	switch {
	case known[encoding]:
		codings = []string{encoding}
	case strings.Contains(encoding, ","):
		any := false
		for _, part := range strings.Split(encoding, ",") {
			part = pystr.Strip(part)
			codings = append(codings, part)
			any = any || known[part]
		}
		if !any {
			return res.Body, nil
		}
	default:
		return res.Body, nil
	}
	var reader io.Reader = taggedSource{res.Body}
	if len(codings) > 5 {
		reader = failingReader{fmt.Errorf("Too many content encodings in the chain: %d > 5", len(codings))}
	} else {
		for i := len(codings) - 1; i >= 0; i-- {
			if codings[i] == "gzip" || codings[i] == "x-gzip" {
				reader = &pyGzipReader{source: bufio.NewReader(reader)}
			} else {
				reader = &pyDeflateReader{source: reader}
			}
			// Bound every decoded layer, including intermediate bytes that a
			// later decoder might discard while draining trailing input.
			reader = &decodedLimitBody{source: io.NopCloser(reader), remaining: MaxDecodedBytes}
		}
	}
	return &decodedBody{Reader: reader, closer: res.Body}, nil
}

// sourceError marks a failure of the raw body (a transport error), as
// opposed to a failure of a decoder.
type sourceError struct{ err error }

func (e *sourceError) Error() string { return e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

type taggedSource struct{ r io.Reader }

func (t taggedSource) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && err != io.EOF {
		err = &sourceError{err}
	}
	return n, err
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

type decodedBody struct {
	io.Reader
	closer io.Closer
}

// Read passes transport failures on unchanged (classifyBodyError decides
// their class) and turns decoder failures into ContentDecodingError.
func (d *decodedBody) Read(p []byte) (int, error) {
	n, err := d.Reader.Read(p)
	if err == nil || err == io.EOF || errors.Is(err, ErrSizeLimit) {
		return n, err
	}
	var source *sourceError
	if errors.As(err, &source) {
		return n, source.err
	}
	return n, decodingError(err)
}

func (d *decodedBody) Close() error { return d.closer.Close() }

// truncatedStream reports a decoder that ran out of input inside its stream:
// zlib.decompressobj returns what it has and flush() does not complain, so
// urllib3 ends the body there without an error.
func truncatedStream(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }

// drain reads the rest of the raw body (urllib3 keeps reading after a
// decoder swallowed its data), reporting only transport failures.
func drain(source io.Reader) error {
	_, err := io.Copy(io.Discard, source)
	return err
}

// pyGzipReader is urllib3's GzipDecoder: the first member must decode;
// further members are decoded while they are valid, and an invalid one (or
// any garbage after the first member) swallows the rest of the data. The
// output of a later member is held back until the member ends (up to 64 KiB):
// its partial output is lost with the zlib.error that swallows it.
type pyGzipReader struct {
	source  *bufio.Reader
	reader  *gzip.Reader
	members int
	pending []byte
	held    []byte
	done    bool
}

const gzipHoldLimit = 64 * 1024

// gzipHeaderError checks the header bytes zlib has seen so far (magic,
// method, reserved flags) the way inflate reports them as soon as they
// arrive; a shorter but valid prefix is not an error yet.
func gzipHeaderError(head []byte) error {
	switch {
	case len(head) > 0 && head[0] != 0x1f, len(head) > 1 && head[1] != 0x8b:
		return errors.New("Error -3 while decompressing data: incorrect header check")
	case len(head) > 2 && head[2] != 8:
		return errors.New("Error -3 while decompressing data: unknown compression method")
	case len(head) > 3 && head[3]&0xe0 != 0:
		return errors.New("Error -3 while decompressing data: unknown header flags set")
	}
	return nil
}

func (g *pyGzipReader) Read(p []byte) (int, error) {
	for {
		if len(g.pending) > 0 {
			n := copy(p, g.pending)
			g.pending = g.pending[n:]
			return n, nil
		}
		if g.done {
			return 0, io.EOF
		}
		if g.reader == nil {
			if err := g.open(); err != nil || g.done {
				return 0, err
			}
		}
		buffer := p
		if g.members > 0 {
			buffer = make([]byte, 32*1024)
		}
		n, err := g.reader.Read(buffer)
		if g.members > 0 {
			g.held = append(g.held, buffer[:n]...)
			n = 0
			if len(g.held) >= gzipHoldLimit {
				g.pending, g.held = g.held, nil
			}
		}
		switch {
		case err == nil:
			if n > 0 {
				return n, nil
			}
			continue
		case err == io.EOF:
			g.reader, g.members = nil, g.members+1
			g.pending, g.held = append(g.pending, g.held...), nil
		case isSourceError(err):
			return n, err
		case truncatedStream(err):
			g.done = true
			g.pending, g.held = append(g.pending, g.held...), nil
		case g.members > 0:
			g.done, g.held = true, nil
			if drainErr := drain(g.source); drainErr != nil {
				return n, drainErr
			}
		default:
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}
}

// open starts the next member (done when the data ends or is swallowed).
func (g *pyGzipReader) open() error {
	head, err := g.source.Peek(4)
	if err != nil && err != io.EOF {
		return err
	}
	if len(head) == 0 {
		g.done = true
		return nil
	}
	if headerErr := gzipHeaderError(head); headerErr != nil {
		g.done = true
		if g.members == 0 {
			return headerErr
		}
		return drain(g.source)
	}
	reader, err := gzip.NewReader(g.source)
	if err != nil {
		g.done = true
		switch {
		case isSourceError(err):
			return err
		case truncatedStream(err) || err == io.EOF:
			return nil
		case g.members > 0:
			return drain(g.source)
		}
		return err
	}
	reader.Multistream(false)
	g.reader = reader
	return nil
}

func isSourceError(err error) bool {
	var source *sourceError
	return errors.As(err, &source)
}

// pyDeflateReader is urllib3's DeflateDecoder: a zlib stream, or raw
// deflate when the data is not a zlib stream; a truncated stream ends the
// body, data after the end of the stream is ignored.
type pyDeflateReader struct {
	source io.Reader
	reader io.Reader
	done   bool
}

func (d *pyDeflateReader) Read(p []byte) (int, error) {
	if d.done {
		return 0, io.EOF
	}
	if d.reader == nil {
		head := make([]byte, 2)
		n, err := io.ReadFull(d.source, head)
		if err != nil && isSourceError(err) {
			return 0, err
		}
		if n == 0 {
			d.done = true
			return 0, io.EOF
		}
		if n < 2 {
			d.done = true // one byte: zlib waits for more
			return 0, io.EOF
		}
		combined := io.MultiReader(bytes.NewReader(head), d.source)
		cmf, flg := head[0], head[1]
		if cmf&0x0f == 8 && cmf>>4 <= 7 && (uint(cmf)<<8|uint(flg))%31 == 0 && flg&0x20 == 0 {
			z, err := zlib.NewReader(combined)
			if err != nil {
				return 0, err
			}
			d.reader = z
		} else {
			// Not a zlib stream (or one needing a preset dictionary):
			// zlib.error, then raw deflate.
			d.reader = flate.NewReader(combined)
		}
	}
	n, err := d.reader.Read(p)
	switch {
	case err == io.EOF:
		d.done = true
		if drainErr := drain(d.source); drainErr != nil {
			return n, drainErr
		}
	case err != nil && !isSourceError(err) && truncatedStream(err):
		d.done = true
		return n, io.EOF
	}
	return n, err
}

func decodingError(err error) error {
	return &FetchError{Class: "ContentDecodingError",
		Message: fmt.Sprintf("('Received response with content-encoding: gzip, but failed to decode it.', %v)", err), Err: err}
}

func isDisconnectErrno(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}

func isCertificateError(err error) bool {
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknown) || errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &verification)
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

// classifyRequestError maps a failure before the response headers.
func classifyRequestError(err error, viaProxy bool) error {
	message := err.Error()
	var op *net.OpError
	proxyPhase := errors.As(err, &op) && op.Op == "proxyconnect"
	var tunnel *tunnelError
	switch {
	case errors.As(err, &tunnel):
		return &FetchError{Class: "ProxyError", Message: tunnel.message, Err: err}
	case isCertificateError(err):
		class := "SSLError"
		if proxyPhase {
			class = "ProxyError"
		}
		return &FetchError{Class: class, Message: message, Err: err}
	case isTimeoutError(err):
		if strings.Contains(message, "awaiting headers") {
			return &FetchError{Class: "ReadTimeout", Message: message, Err: err}
		}
		return &FetchError{Class: "ConnectTimeout", Message: message, Err: err}
	}
	tlsPhase := strings.Contains(message, "tls: ") || strings.Contains(message, "TLS handshake")
	eof := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	class := "ConnectionError"
	switch {
	case proxyPhase:
		class = "ProxyError"
	case tlsPhase:
		class = "SSLError"
	}
	if viaProxy && errors.Is(err, syscall.ECONNREFUSED) {
		class = "ProxyError"
	}
	transient := isDisconnectErrno(err) || eof
	return &FetchError{Class: class, Message: message, Transient: transient, Err: err}
}

// classifyBodyError maps a failure while reading the body.
func classifyBodyError(err error) error {
	if errors.Is(err, ErrSizeLimit) {
		return err
	}
	var fetch *FetchError
	if errors.As(err, &fetch) {
		return err
	}
	message := err.Error()
	switch {
	case errors.Is(err, errBodyTimeout) || isTimeoutError(err):
		return &FetchError{Class: "ConnectionError", Message: message, Err: err}
	case strings.Contains(message, "chunk length") || strings.Contains(message, "chunked encoding"):
		return &FetchError{Class: "ChunkedEncodingError", Message: "InvalidChunkLength: " + message, Err: err}
	case errors.Is(err, io.ErrUnexpectedEOF) || isDisconnectErrno(err) || errors.Is(err, io.EOF):
		return &FetchError{Class: "ChunkedEncodingError", Message: "Response ended prematurely: " + message, Transient: true, Err: err}
	}
	return &FetchError{Class: "ConnectionError", Message: message, Err: err}
}

// urlBasicAuth is the user and password of the URL's userinfo; ok is false
// without a password or when both are empty.
func urlBasicAuth(u *url.URL) (string, string, bool) {
	if u.User == nil {
		return "", "", false
	}
	password, hasPassword := u.User.Password()
	if !hasPassword || u.User.Username() == "" && password == "" {
		return "", "", false
	}
	return u.User.Username(), password, true
}
