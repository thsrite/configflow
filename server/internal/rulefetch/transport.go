package rulefetch

import (
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

	"github.com/klauspost/compress/zstd"
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

// PythonClass is type(err).__name__ for errors of this package; plain
// errors are ValueError.
func PythonClass(err error) string {
	var classed interface{ PythonClass() string }
	if errors.As(err, &classed) {
		return classed.PythonClass()
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
// the default), gzip/deflate/zstd decoding, no redirects, no keep-alive.
type HTTPTransport struct {
	// Dial replaces the TCP dialer (tests: host overrides); nil dials
	// with net.Dialer.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
}

type tunnelError struct{ message string }

func (e *tunnelError) Error() string { return e.message }

// Do implements Transport.
func (h HTTPTransport) Do(ctx context.Context, req TransportRequest) (*Response, error) {
	tlsConfig := &tls.Config{}
	if req.Insecure {
		tlsConfig.InsecureSkipVerify = true
	} else {
		bundle := req.CABundle
		if bundle == "" {
			bundle = os.Getenv("REQUESTS_CA_BUNDLE")
			if bundle == "" {
				bundle = os.Getenv("CURL_CA_BUNDLE")
			}
		}
		if bundle != "" {
			pool, err := loadBundle(bundle)
			if err != nil {
				return nil, &FetchError{Class: "SSLError", Message: err.Error(), Err: err}
			}
			tlsConfig.RootCAs = pool
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
	if req.Proxy != "" {
		proxyURL, err := url.Parse(req.Proxy)
		if err != nil {
			return nil, &FetchError{Class: "InvalidURL", Message: err.Error(), Err: err}
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, nil)
	if err != nil {
		return nil, &FetchError{Class: "InvalidURL", Message: err.Error(), Err: err}
	}
	httpReq.Header.Set("User-Agent", "python-requests/2.31.0")
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate")
	httpReq.Header.Set("Accept", "*/*")
	res, err := (&http.Client{Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(httpReq)
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
	_, reason, _ := strings.Cut(res.Status, " ")
	return &Response{StatusCode: res.StatusCode, Reason: reason, Header: res.Header, URL: prepareURL(req.URL), Body: body}, nil
}

// prepareURL is the part of requests' URL preparation visible in messages:
// an empty path becomes "/".
func prepareURL(raw string) string {
	parsed, err := SplitURL(raw, "")
	if err != nil || parsed.Path != "" {
		return raw
	}
	parsed.Path = "/"
	return parsed.String()
}

func loadBundle(bundle string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	info, err := os.Stat(bundle)
	if err != nil {
		return nil, fmt.Errorf("Could not find a suitable TLS CA certificate bundle, invalid path: %s", bundle)
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
	return pool, nil
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

// decodeBody applies Content-Encoding (urllib3: gzip, deflate, zstd; other
// encodings are left as they are), last encoding first.
func decodeBody(res *http.Response) (io.ReadCloser, error) {
	var encodings []string
	for _, value := range res.Header.Values("Content-Encoding") {
		for _, part := range strings.Split(value, ",") {
			if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
				encodings = append(encodings, part)
			}
		}
	}
	if len(encodings) == 0 || res.Request.Method == "HEAD" {
		return res.Body, nil
	}
	var reader io.Reader = res.Body
	for i := len(encodings) - 1; i >= 0; i-- {
		var err error
		switch encodings[i] {
		case "gzip", "x-gzip":
			reader, err = gzip.NewReader(reader)
		case "deflate":
			reader = &deflateReader{source: reader}
		case "zstd":
			var d *zstd.Decoder
			d, err = zstd.NewReader(reader)
			reader = d
		}
		if err != nil {
			return nil, decodingError(err)
		}
	}
	return &decodedBody{Reader: reader, closer: res.Body}, nil
}

type decodedBody struct {
	io.Reader
	closer io.Closer
}

func (d *decodedBody) Read(p []byte) (int, error) {
	n, err := d.Reader.Read(p)
	if err != nil && err != io.EOF && !isTransportError(err) {
		return n, decodingError(err)
	}
	return n, err
}

func (d *decodedBody) Close() error { return d.closer.Close() }

// deflateReader is urllib3's DeflateDecoder: zlib first, raw deflate if
// that fails on the first bytes.
type deflateReader struct {
	source io.Reader
	reader io.Reader
}

func (d *deflateReader) Read(p []byte) (int, error) {
	if d.reader == nil {
		head := make([]byte, 2)
		n, _ := io.ReadFull(d.source, head)
		combined := io.MultiReader(bytes.NewReader(head[:n]), d.source)
		if z, err := zlib.NewReader(io.MultiReader(bytes.NewReader(head[:n]))); err == nil {
			z.Close()
			d.reader, _ = zlib.NewReader(combined)
		} else {
			d.reader = flate.NewReader(combined)
		}
	}
	return d.reader.Read(p)
}

func decodingError(err error) error {
	return &FetchError{Class: "ContentDecodingError",
		Message: fmt.Sprintf("('Received response with content-encoding: gzip, but failed to decode it.', %v)", err), Err: err}
}

func isTransportError(err error) bool {
	var op *net.OpError
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &op) || errors.Is(err, errBodyTimeout) ||
		errors.Is(err, syscall.ECONNRESET)
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
