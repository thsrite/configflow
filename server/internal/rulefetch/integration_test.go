package rulefetch

// Ported from backend/test_rule_fetch_proxy_integration.py: rule downloads
// over real loopback HTTP, HTTPS and proxy sockets (httptest servers; no
// external host is contacted). Not ported: the "untrusted origin behind a
// trusted HTTPS proxy" variant (httptest servers share one certificate, and
// Python reaches it only by patching urllib3's proxy context).

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var integrationBody = []byte("payload:\n  - DOMAIN,example.test\n")

type seenRequest struct {
	method, url string
	header      http.Header
}

type recorder struct {
	*httptest.Server
	mu            sync.Mutex
	seen          []seenRequest
	routes        map[string]func(w http.ResponseWriter)
	accepted      int
	handshakeFail int
	partialFail   int
	chunkFaults   []string
	requiredAuth  string
	reject        bool
	hostOverrides map[string]string
}

func (r *recorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	url := req.RequestURI
	if req.Method == http.MethodConnect {
		url = req.Host
	}
	r.seen = append(r.seen, seenRequest{req.Method, url, req.Header.Clone()})
}

func (r *recorder) seenURLs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, s := range r.seen {
		out = append(out, s.method+" "+s.url)
	}
	return out
}

// countingListener counts accepted connections and, for TLS servers, fails
// the first handshakes by reading the ClientHello and closing (a TLS EOF).
type countingListener struct {
	net.Listener
	r *recorder
}

func (l *countingListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.r.mu.Lock()
		l.r.accepted++
		fail := l.r.handshakeFail > 0
		if fail {
			l.r.handshakeFail--
		}
		l.r.mu.Unlock()
		if !fail {
			return conn, nil
		}
		header := make([]byte, 5)
		if _, err := io.ReadFull(conn, header); err == nil {
			_, _ = io.CopyN(io.Discard, conn, int64(header[3])<<8|int64(header[4]))
		}
		conn.(*net.TCPConn).CloseWrite()
		time.Sleep(20 * time.Millisecond)
		conn.Close()
	}
}

func startServer(t *testing.T, proxy, useTLS bool, configure func(*recorder)) *recorder {
	r := &recorder{routes: map[string]func(http.ResponseWriter){}, hostOverrides: map[string]string{}}
	if configure != nil {
		configure(r)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.record(req)
		if proxy {
			r.serveProxy(w, req)
			return
		}
		r.serveOrigin(w, req)
	})
	server := httptest.NewUnstartedServer(handler)
	server.Listener = &countingListener{Listener: server.Listener, r: r}
	server.Config.ErrorLog = nil
	if useTLS {
		server.StartTLS()
	} else {
		server.Start()
	}
	r.Server = server
	t.Cleanup(server.Close)
	return r
}

func (r *recorder) serveOrigin(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	route, ok := r.routes[req.RequestURI]
	var fault string
	hasFault := len(r.chunkFaults) > 0
	if hasFault {
		fault = r.chunkFaults[0]
		r.chunkFaults = r.chunkFaults[1:]
	}
	partial := r.partialFail > 0
	if partial {
		r.partialFail--
	}
	r.mu.Unlock()
	if ok {
		route(w)
		return
	}
	body := integrationBody
	if hasFault {
		conn, buf, _ := w.(http.Hijacker).Hijack()
		defer conn.Close()
		fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n%x\r\n%s\r\n", len(body), body)
		switch fault {
		case "invalid_length":
			buf.WriteString("not-a-chunk-length\r\n")
		case "eof":
		default:
			buf.WriteString("0\r\n\r\n")
		}
		buf.Flush()
		return
	}
	if partial {
		conn, buf, _ := w.(http.Hijacker).Hijack()
		fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(body))
		buf.Write(body[:len(body)-1])
		buf.Flush()
		conn.Close()
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if req.Method != http.MethodHead {
		w.Write(body)
	}
}

func (r *recorder) serveProxy(w http.ResponseWriter, req *http.Request) {
	if r.requiredAuth != "" && req.Header.Get("Proxy-Authorization") != r.requiredAuth {
		w.Header().Set("Proxy-Authenticate", `Basic realm="rules"`)
		w.WriteHeader(407)
		return
	}
	if r.reject {
		w.WriteHeader(502)
		w.Write([]byte("Proxy unavailable"))
		return
	}
	target := func(hostport string) string {
		host, port, _ := net.SplitHostPort(hostport)
		if override, ok := r.hostOverrides[host]; ok {
			host = override
		}
		return net.JoinHostPort(host, port)
	}
	if req.Method == http.MethodConnect {
		upstream, err := net.DialTimeout("tcp", target(req.Host), 3*time.Second)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		conn, buf, _ := w.(http.Hijacker).Hijack()
		buf.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		buf.Flush()
		go func() { io.Copy(upstream, buf); upstream.(*net.TCPConn).CloseWrite() }()
		io.Copy(conn, upstream)
		conn.Close()
		upstream.Close()
		return
	}
	out, _ := http.NewRequest(req.Method, req.RequestURI, nil)
	for k, v := range req.Header {
		if k != "Proxy-Authorization" && k != "Proxy-Connection" {
			out.Header[k] = v
		}
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target(addr))
	}, DisableCompression: true}
	res, err := transport.RoundTrip(out)
	if err != nil {
		w.WriteHeader(502)
		return
	}
	defer res.Body.Close()
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)
}

// caBundle writes httptest's TLS certificate (shared by every httptest TLS
// server) to a PEM file.
func caBundle(t *testing.T, _ *recorder) string {
	tlsServer := httptest.NewTLSServer(http.NotFoundHandler())
	defer tlsServer.Close()
	path := filepath.Join(t.TempDir(), "cert.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolatedEnv(t *testing.T) {
	for _, name := range []string{"http_proxy", "https_proxy", "all_proxy", "no_proxy", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		t.Setenv(name, "")
		t.Setenv(strings.ToUpper(name), "")
	}
}

func liveClient(sleeps *[]time.Duration) *Client {
	return &Client{Transport: HTTPTransport{}, Sleep: func(d time.Duration) { *sleeps = append(*sleeps, d) }}
}

func proxyConfig(proxy, serverDomain string) DownloadRequest {
	return DownloadRequest{Config: sysConfig("rule_fetch_proxy", proxy, "server_domain", serverDomain),
		ConnectTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second}
}

func hostOf(s *recorder) string { return strings.SplitN(s.URL, "://", 2)[1] }

// test_get_and_head_use_configured_http_or_https_proxy
func TestGetAndHeadUseConfiguredProxy(t *testing.T) {
	isolatedEnv(t)
	for _, method := range []string{"get", "head"} {
		for _, proxyTLS := range []bool{false, true} {
			for _, originTLS := range []bool{false, true} {
				origin := startServer(t, false, originTLS, nil)
				proxy := startServer(t, true, proxyTLS, nil)
				url := origin.URL + "/rules.yaml"
				req := proxyConfig(proxy.URL, "")
				req.Method, req.CABundle = method, caBundle(t, origin)
				var sleeps []time.Duration
				response, err := liveClient(&sleeps).Download(context.Background(), url, req)
				if err != nil {
					t.Fatalf("%s proxyTLS=%v originTLS=%v: %v", method, proxyTLS, originTLS, err)
				}
				content, _ := response.Content()
				want := string(integrationBody)
				if method == "head" {
					want = ""
				}
				if response.StatusCode != 200 || string(content) != want {
					t.Fatal(response.StatusCode, string(content))
				}
				if seen := origin.seenURLs(); len(seen) != 1 || seen[0] != strings.ToUpper(method)+" /rules.yaml" {
					t.Fatal(seen)
				}
				wantProxy := strings.ToUpper(method) + " " + url
				if originTLS {
					wantProxy = "CONNECT " + hostOf(origin)
				}
				if seen := proxy.seenURLs(); len(seen) != 1 || seen[0] != wantProxy {
					t.Fatal(seen, wantProxy)
				}
			}
		}
	}
}

// test_untrusted_https_proxy_certificate_is_rejected
func TestUntrustedHTTPSProxyCertificateIsRejected(t *testing.T) {
	isolatedEnv(t)
	for _, originTLS := range []bool{false, true} {
		origin, proxy := startServer(t, false, originTLS, nil), startServer(t, true, true, nil)
		var sleeps []time.Duration
		_, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", proxyConfig(proxy.URL, ""))
		if class := errorClass(err); class != "ProxyError" && class != "SSLError" {
			t.Fatal(err, class)
		}
		if len(origin.seen) != 0 || len(proxy.seen) != 0 || proxy.accepted != 1 || len(sleeps) != 0 {
			t.Fatal(origin.seen, proxy.seen, proxy.accepted, sleeps)
		}
	}
}

// test_https_proxy_uses_environment_certificate_bundle
func TestHTTPSProxyUsesEnvironmentCertificateBundle(t *testing.T) {
	for _, variable := range []string{"REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		isolatedEnv(t)
		origin, proxy := startServer(t, false, false, nil), startServer(t, true, true, nil)
		t.Setenv(variable, caBundle(t, proxy))
		var sleeps []time.Duration
		response, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", proxyConfig(proxy.URL, ""))
		if err != nil {
			t.Fatal(err)
		}
		if content, _ := response.Content(); string(content) != string(integrationBody) || len(origin.seen) != 1 || len(proxy.seen) != 1 {
			t.Fatal(string(content))
		}
	}
}

// test_authenticated_proxy_does_not_pass_credentials_to_origin
func TestAuthenticatedProxyDoesNotPassCredentialsToOrigin(t *testing.T) {
	isolatedEnv(t)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("rule-user:test-secret"))
	origin := startServer(t, false, false, nil)
	proxy := startServer(t, true, false, func(r *recorder) { r.requiredAuth = expected })
	authURL := strings.Replace(proxy.URL, "://", "://rule-user:test-secret@", 1)
	var sleeps []time.Duration
	response, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", proxyConfig(authURL, ""))
	if err != nil || response.StatusCode != 200 {
		t.Fatal(err)
	}
	if proxy.seen[0].header.Get("Proxy-Authorization") != expected || origin.seen[0].header.Get("Proxy-Authorization") != "" {
		t.Fatal(proxy.seen[0].header, origin.seen[0].header)
	}
}

// test_clearing_proxy_uses_direct_connection_despite_environment,
// test_empty_proxy_without_environment_connects_directly
func TestClearingProxyUsesDirectConnectionDespiteEnvironment(t *testing.T) {
	isolatedEnv(t)
	origin := startServer(t, false, false, nil)
	configured, environment := startServer(t, true, false, nil), startServer(t, true, false, nil)
	t.Setenv("HTTP_PROXY", environment.URL)
	var sleeps []time.Duration
	client := liveClient(&sleeps)
	if r, err := client.Download(context.Background(), origin.URL+"/first", proxyConfig(configured.URL, "")); err != nil || r.StatusCode != 200 {
		t.Fatal(err)
	}
	if r, err := client.Download(context.Background(), origin.URL+"/second", proxyConfig("", "")); err != nil || r.StatusCode != 200 {
		t.Fatal(err)
	}
	if !equalStrings(configured.seenURLs(), []string{"GET " + origin.URL + "/first"}) || len(environment.seen) != 0 ||
		!equalStrings(origin.seenURLs(), []string{"GET /first", "GET /second"}) {
		t.Fatal(configured.seenURLs(), environment.seenURLs(), origin.seenURLs())
	}
}

// test_github_fallback_uses_same_transport_for_both_real_requests
func TestGithubFallbackUsesSameTransport(t *testing.T) {
	isolatedEnv(t)
	for _, useProxy := range []bool{false, true} {
		origin := startServer(t, false, false, func(r *recorder) {
			r.routes["/rules/custom.yaml"] = func(w http.ResponseWriter) { w.WriteHeader(503); w.Write([]byte("unavailable")) }
		})
		mirror := startServer(t, false, false, nil)
		proxy := startServer(t, true, false, func(r *recorder) { r.hostOverrides["raw.githubusercontent.com"] = "127.0.0.1" })
		_, port, _ := net.SplitHostPort(hostOf(origin))
		source := "http://raw.githubusercontent.com:" + port + "/rules/custom.yaml"
		req := proxyConfig("", "")
		if useProxy {
			req = proxyConfig(proxy.URL, "")
		}
		req.Config.Settings.GitHubProxyDomain = mirror.URL
		req.Stream, req.MaxBytes = true, 1024
		var sleeps []time.Duration
		client := &Client{Transport: HTTPTransport{Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			addr = strings.Replace(addr, "raw.githubusercontent.com", "127.0.0.1", 1)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}}, Sleep: func(d time.Duration) { sleeps = append(sleeps, d) }}
		response, err := client.Download(context.Background(), source, req)
		if err != nil {
			t.Fatal(err)
		}
		if content, _ := response.Content(); string(content) != string(integrationBody) {
			t.Fatal(string(content))
		}
		if !equalStrings(origin.seenURLs(), []string{"GET /rules/custom.yaml"}) || !equalStrings(mirror.seenURLs(), []string{"GET /" + source}) {
			t.Fatal(origin.seenURLs(), mirror.seenURLs())
		}
		wantProxy := []string{}
		if useProxy {
			wantProxy = []string{"GET " + source, "GET " + mirror.URL + "/" + source}
		}
		if !equalStrings(proxy.seenURLs(), wantProxy) {
			t.Fatal(proxy.seenURLs())
		}
	}
}

// test_proxy_error_does_not_retry_directly
func TestProxyErrorDoesNotRetryDirectly(t *testing.T) {
	isolatedEnv(t)
	origin := startServer(t, false, false, nil)
	proxy := startServer(t, true, false, func(r *recorder) { r.reject = true })
	var sleeps []time.Duration
	response, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", proxyConfig(proxy.URL, ""))
	if err != nil || response.StatusCode != 502 || len(proxy.seen) != 1 || len(origin.seen) != 0 {
		t.Fatal(err)
	}
}

// test_unavailable_proxy_does_not_retry_directly
func TestUnavailableProxyDoesNotRetryDirectly(t *testing.T) {
	isolatedEnv(t)
	origin := startServer(t, false, false, nil)
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	address := listener.Addr().String()
	listener.Close()
	var sleeps []time.Duration
	req := proxyConfig("http://"+address, "")
	req.ConnectTimeout = time.Second
	_, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", req)
	if errorClass(err) != "ProxyError" || len(origin.seen) != 0 {
		t.Fatal(err, errorClass(err))
	}
}

// test_self_api_bypasses_configured_and_environment_proxy
func TestSelfAPIBypassesProxy(t *testing.T) {
	for _, prefix := range []string{"", "/configflow"} {
		isolatedEnv(t)
		origin, proxy := startServer(t, false, false, nil), startServer(t, true, false, nil)
		t.Setenv("HTTP_PROXY", proxy.URL)
		var sleeps []time.Duration
		url := origin.URL + prefix + "/api/rules?token=test-token"
		response, err := liveClient(&sleeps).Download(context.Background(), url, proxyConfig(proxy.URL, origin.URL+prefix))
		if err != nil || response.StatusCode != 200 || len(proxy.seen) != 0 || !strings.HasSuffix(origin.seen[0].url, "/api/rules?token=test-token") {
			t.Fatal(err, proxy.seen, origin.seen)
		}
	}
}

// test_proxy_resolves_source_without_local_fake_ip_preflight
func TestProxyResolvesSourceWithoutLocalPreflight(t *testing.T) {
	isolatedEnv(t)
	origin := startServer(t, false, false, nil)
	proxy := startServer(t, true, false, func(r *recorder) { r.hostOverrides["fake-ip.rules.test"] = "127.0.0.1" })
	_, port, _ := net.SplitHostPort(hostOf(origin))
	url := "http://fake-ip.rules.test:" + port + "/rules"
	var local []string
	var sleeps []time.Duration
	client := &Client{Transport: HTTPTransport{Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
		local = append(local, addr)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}, Sleep: func(d time.Duration) { sleeps = append(sleeps, d) }}
	response, err := client.Download(context.Background(), url, proxyConfig(proxy.URL, ""))
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := response.Content(); string(content) != string(integrationBody) || proxy.seen[0].url != url || len(origin.seen) != 1 {
		t.Fatal(string(content))
	}
	for _, addr := range local {
		if strings.Contains(addr, "fake-ip") {
			t.Fatal(local)
		}
	}
}

// test_tls_handshake_eof_retries_same_source_with_backoff
func TestTLSHandshakeEOFRetriesWithBackoff(t *testing.T) {
	isolatedEnv(t)
	for _, failureAt := range []string{"origin", "https_proxy"} {
		origin := startServer(t, false, true, func(r *recorder) {
			if failureAt == "origin" {
				r.handshakeFail = 2
			}
		})
		var proxy *recorder
		req := proxyConfig("", "")
		if failureAt == "https_proxy" {
			proxy = startServer(t, true, true, func(r *recorder) { r.handshakeFail = 2 })
			req = proxyConfig(proxy.URL, "")
		}
		req.CABundle, req.Stream, req.MaxBytes = caBundle(t, origin), true, 1024
		var sleeps []time.Duration
		response, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", req)
		if err != nil {
			t.Fatal(failureAt, err)
		}
		if content, _ := response.Content(); string(content) != string(integrationBody) || len(sleeps) != 2 {
			t.Fatal(string(content), sleeps)
		}
		failing := origin
		if proxy != nil {
			failing = proxy
		}
		if failing.accepted != 3 || !equalStrings(origin.seenURLs(), []string{"GET /rules"}) {
			t.Fatal(failing.accepted, origin.seenURLs())
		}
		if proxy != nil && (!equalStrings(proxy.seenURLs(), []string{"CONNECT " + hostOf(origin)}) || origin.accepted != 1) {
			t.Fatal(proxy.seenURLs(), origin.accepted)
		}
	}
}

// test_interrupted_tls_body_retries_without_partial_content
func TestInterruptedTLSBodyRetriesWithoutPartialContent(t *testing.T) {
	isolatedEnv(t)
	for _, httpsProxy := range []bool{false, true} {
		origin := startServer(t, false, true, func(r *recorder) { r.partialFail = 2 })
		req := proxyConfig("", "")
		var proxy *recorder
		if httpsProxy {
			proxy = startServer(t, true, true, nil)
			req = proxyConfig(proxy.URL, "")
		}
		req.CABundle, req.Stream, req.MaxBytes = caBundle(t, origin), true, int64(len(integrationBody))
		var sleeps []time.Duration
		response, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", req)
		if err != nil {
			t.Fatal(err)
		}
		if content, _ := response.Content(); string(content) != string(integrationBody) || len(sleeps) != 2 ||
			!equalStrings(origin.seenURLs(), repeat("GET /rules", 3)) || origin.accepted != 3 {
			t.Fatal(string(content), sleeps, origin.seenURLs())
		}
		if proxy != nil && !equalStrings(proxy.seenURLs(), repeat("CONNECT "+hostOf(origin), 3)) {
			t.Fatal(proxy.seenURLs())
		}
	}
}

// test_untrusted_origin_certificate_is_not_retried_or_bypassed (direct)
func TestUntrustedOriginCertificateIsNotRetried(t *testing.T) {
	isolatedEnv(t)
	origin := startServer(t, false, true, nil)
	req := proxyConfig("", "")
	req.Stream, req.MaxBytes = true, 1024
	var sleeps []time.Duration
	_, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", req)
	if errorClass(err) != "SSLError" || len(origin.seen) != 0 || origin.accepted != 1 || len(sleeps) != 0 {
		t.Fatal(err, origin.accepted, sleeps)
	}
}

// test_chunked_eof_is_retried_but_invalid_length_is_not
func TestChunkedEOFRetriedInvalidLengthNot(t *testing.T) {
	isolatedEnv(t)
	for _, httpsProxy := range []bool{false, true} {
		for _, fault := range []string{"eof", "invalid_length"} {
			origin := startServer(t, false, true, func(r *recorder) {
				r.chunkFaults = []string{"invalid_length"}
				if fault == "eof" {
					r.chunkFaults = []string{"eof", "eof", ""}
				}
			})
			req := proxyConfig("", "")
			if httpsProxy {
				req = proxyConfig(startServer(t, true, true, nil).URL, "")
			}
			req.CABundle, req.Stream, req.MaxBytes = caBundle(t, origin), true, int64(len(integrationBody))
			var sleeps []time.Duration
			response, err := liveClient(&sleeps).Download(context.Background(), origin.URL+"/rules", req)
			attempts := 1
			if fault == "eof" {
				attempts = 3
				if err != nil {
					t.Fatal(err)
				}
				if content, _ := response.Content(); string(content) != string(integrationBody) || len(sleeps) != 2 {
					t.Fatal(string(content), sleeps)
				}
			} else if errorClass(err) != "ChunkedEncodingError" || !strings.Contains(err.Error(), "InvalidChunkLength") || len(sleeps) != 0 {
				t.Fatal(err)
			}
			if !equalStrings(origin.seenURLs(), repeat("GET /rules", attempts)) || origin.accepted != attempts {
				t.Fatal(origin.seenURLs(), origin.accepted)
			}
		}
	}
}

var _ = bufio.NewReader
var _ = errors.New
