package devhttp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve runs handler for every connection of a loopback listener.
func serve(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()
	return ln.Addr().String()
}

func readRequest(conn net.Conn) {
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil || line == "\r\n" {
			return
		}
	}
}

func hang(conn net.Conn) { time.Sleep(2 * time.Second); conn.Close() }
func reply(text string) func(net.Conn) {
	return func(conn net.Conn) { readRequest(conn); conn.Write([]byte(text)); conn.Close() }
}

// TestFailureClasses: the requests class of each failure, as measured on
// requests 2.31 / urllib3 2.8 by testdata/requests_classes.py.
func TestFailureClasses(t *testing.T) {
	refused := func() string {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := ln.Addr().String()
		ln.Close()
		return addr
	}()
	stalled := func(conn net.Conn) {
		readRequest(conn)
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n[1,2"))
		time.Sleep(2 * time.Second)
		conn.Close()
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer tlsServer.Close()
	tlsAddr := strings.TrimPrefix(tlsServer.URL, "https://")
	cases := []struct {
		name, url, class string
		verify           bool
	}{
		{"refused", "http://" + refused + "/rest", ConnectionError, false},
		{"read_timeout", "http://" + serve(t, hang) + "/rest", ReadTimeout, false},
		{"closed_after_request", "http://" + serve(t, func(c net.Conn) { readRequest(c); c.Close() }) + "/rest", ConnectionError, false},
		{"short_body", "http://" + serve(t, reply("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n[1,2")) + "/rest", ChunkedEncodingError, false},
		{"stalled_body", "http://" + serve(t, stalled) + "/rest", ConnectionError, false},
		{"garbage", "http://" + serve(t, reply("NOT HTTP AT ALL\r\n\r\n")) + "/rest", ConnectionError, false},
		{"tls_handshake_timeout", "https://" + serve(t, hang) + "/rest", ReadTimeout, false},
		{"tls_to_plain_http", "https://" + serve(t, func(c net.Conn) {
			c.Read(make([]byte, 4096))
			c.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n"))
			c.Close()
		}) + "/rest", SSLError, false},
		{"self_signed_verify", "https://" + tlsAddr + "/rest", SSLError, true},
		{"self_signed_no_verify", "https://" + tlsAddr + "/rest", "", false},
		{"bad_host", "http://router.invalid/rest", ConnectionError, false},
		{"not_a_url", "ftp://x/", InvalidURL, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Config{VerifyTLS: tc.verify})
			defer c.Close()
			resp, err := c.Do(context.Background(), "GET", tc.url, nil, 500*time.Millisecond)
			if tc.class == "" {
				if err != nil || resp.Status != 200 {
					t.Fatalf("want 200, got %v %v", resp, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want %s, got status %d", tc.class, resp.Status)
			}
			if got := ClassOf(err); got != tc.class {
				t.Fatalf("want %s, got %s (%v)", tc.class, got, err)
			}
		})
	}
}

// The read timeout restarts with every piece of data (no total limit).
func TestTimeoutIsPerRead(t *testing.T) {
	addr := serve(t, func(conn net.Conn) {
		readRequest(conn)
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\n"))
		for _, b := range []byte("[1 ]") {
			time.Sleep(200 * time.Millisecond)
			conn.Write([]byte{b})
		}
		conn.Close()
	})
	resp, err := New(Config{}).Do(context.Background(), "GET", "http://"+addr+"/", nil, 400*time.Millisecond)
	if err != nil || string(resp.Body) != "[1 ]" {
		t.Fatalf("%v %v", resp, err)
	}
}

func TestRequestShape(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must(io.ReadAll(r.Body))
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	c := New(Config{BasicAuth: true, Username: "adé", Password: "密码"})
	resp, err := c.Do(context.Background(), "PUT", srv.URL+"/rest/ip/route", map[string]any{"comment": "中文😀<&>"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != http.StatusFound {
		t.Fatalf("redirect followed: %d", resp.Status)
	}
	if want := `{"comment":"\u4e2d\u6587\ud83d\ude00<&>"}`; string(body) != want {
		t.Fatalf("body %s, want %s", body, want)
	}
	if got.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", got.Header.Get("Content-Type"))
	}
	// Not Latin-1 as a whole ("密码"): UTF-8.
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("adé:密码")); got.Header.Get("Authorization") != want {
		t.Fatalf("auth %q", got.Header.Get("Authorization"))
	}
	c = New(Config{BasicAuth: true, Username: "adé", Password: "pw"})
	if _, err := c.Do(context.Background(), "GET", srv.URL+"/", nil, time.Second); err != nil {
		t.Fatal(err)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("ad\xe9:pw")); got.Header.Get("Authorization") != want {
		t.Fatalf("latin-1 auth %q", got.Header.Get("Authorization"))
	}
}

func TestDecodeJSON(t *testing.T) {
	var v any
	if err := DecodeJSON([]byte(` {"a": 6} `), &v); err != nil || v.(map[string]any)["a"] != json.Number("6") {
		t.Fatalf("%v %v", v, err)
	}
	for _, bad := range []string{"", "<html>", "[1] [2]", "{"} {
		if DecodeJSON([]byte(bad), &v) == nil {
			t.Errorf("%q decoded", bad)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
