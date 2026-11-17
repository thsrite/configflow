package httpx_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

// test_http_server.py::test_proxy_headers_auth_mcp_upload_and_file_responses
// (the observable parts: forwarded headers reach the handler unchanged,
// large uploads, send_file with HEAD and ranges).
func TestFileResponsesHeadRangeAndUploads(t *testing.T) {
	body := bytes.Repeat([]byte("configflow-transfer\x00"), 65536)
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/static/download", "test.download", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Data: body, AsAttachment: true, DownloadName: "fixture.bin",
				MimeType: "application/octet-stream"})
		})
		r.POST("/static/upload", "test.upload", func(c *httpx.Ctx) (*httpx.Response, error) {
			data, err := c.Data()
			if err != nil {
				return nil, err
			}
			return httpx.JSON(jsonx.Obj("size", len(data))), nil
		})
		r.GET("/static/headers", "test.headers", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(jsonx.Obj("host", c.HeaderDefault("X-Forwarded-Host", ""),
				"scheme", c.HeaderDefault("X-Forwarded-Proto", ""), "forwarded", c.HeaderDefault("X-Forwarded-For", ""))), nil
		})
	})})
	base := srv.URL()
	get := func(method, path string, headers map[string]string, payload []byte) *http.Response {
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(payload))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := get("GET", "/static/download", nil, nil)
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(data, body) ||
		resp.Header.Get("Content-Disposition") != "attachment; filename=fixture.bin" ||
		resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatal(resp.StatusCode, resp.Header)
	}
	resp = get("HEAD", "/static/download", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength != int64(len(body)) {
		t.Fatal("HEAD", resp.StatusCode, resp.ContentLength)
	}
	resp = get("GET", "/static/download", map[string]string{"Range": "bytes=100-199"}, nil)
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || !bytes.Equal(data, body[100:200]) ||
		resp.Header.Get("Content-Range") != "bytes 100-199/"+itoa(len(body)) {
		t.Fatal("range", resp.StatusCode, resp.Header)
	}
	resp = get("POST", "/static/upload", map[string]string{"Content-Type": "application/octet-stream"}, body)
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(data) != `{"size":`+itoa(len(body))+`}` {
		t.Fatal(string(data))
	}
	resp = get("GET", "/static/headers", map[string]string{"X-Forwarded-Host": "config.example:8443",
		"X-Forwarded-Proto": "https", "X-Forwarded-For": "198.51.100.10, 192.0.2.1"}, nil)
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(data) != `{"forwarded":"198.51.100.10, 192.0.2.1","host":"config.example:8443","scheme":"https"}` {
		t.Fatal(string(data))
	}
}

func itoa(n int) string { return jsonx.Int(int64(n)).String() }

func TestFileOnDiskIsConditional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.bin")
	os.WriteFile(path, []byte("\x7fELF-binary"), 0o644)
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/static/agent", "test.agent", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Path: path, AsAttachment: true, DownloadName: "configflow-agent-amd64",
				MimeType: "application/octet-stream"})
		})
	})})
	res := srv.Get("/static/agent").WantStatus(200)
	etag := res.Header.Get("ETag")
	if etag == "" || res.Header.Get("Last-Modified") == "" || res.Text() != "\x7fELF-binary" {
		t.Fatal(res.Header)
	}
	srv.Get("/static/agent", httpxtest.Header("If-None-Match", etag)).WantStatus(304)
	srv.Get("/static/agent", httpxtest.Header("If-Modified-Since", res.Header.Get("Last-Modified"))).WantStatus(304)
}

func edgeFixture(t *testing.T) (*httpx.Edge, string) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "assets"), 0o755)
	os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>spa</html>"), 0o644)
	os.WriteFile(filepath.Join(root, "frontend-version.json"), []byte(`{"v":1}`), 0o644)
	os.WriteFile(filepath.Join(root, "assets", "index-AbCdEf12.js"), []byte(strings.Repeat("console.log(1);", 10)), 0o644)
	os.WriteFile(filepath.Join(root, "assets", "plain.txt"), []byte("plain"), 0o644)
	os.WriteFile(filepath.Join(root, "sw.js"), []byte("self"), 0o644)
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte("precompressed-css"))
	w.Close()
	os.WriteFile(filepath.Join(root, "assets", "style-12345678.css"), []byte("original-css"), 0o644)
	os.WriteFile(filepath.Join(root, "assets", "style-12345678.css.gz"), gz.Bytes(), 0o644)
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		echoRoutes(r)
	})})
	edge := &httpx.Edge{API: srv.Handler(), Root: root}
	edge.SetListenerToken("tok123")
	return edge, root
}

func echoRoutes(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/echo", "test.echo", func(c *httpx.Ctx) (*httpx.Response, error) {
		data, _ := c.Data()
		return httpx.JSON(jsonx.Obj("real_ip", c.HeaderDefault("X-Real-IP", ""), "xff", c.HeaderDefault("X-Forwarded-For", ""),
			"host", c.Host(), "fhost", c.HeaderDefault("X-Forwarded-Host", ""), "remote", c.RemoteAddr(), "size", len(data),
			"url_root", c.URLRoot())), nil
	})
	r.POST("/api/oxidns/settings", "oxidns.oxidns_settings", func(c *httpx.Ctx) (*httpx.Response, error) {
		data, _ := c.Data()
		return httpx.JSON(jsonx.Obj("size", len(data))), nil
	})
}

func edgeDo(edge *httpx.Edge, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://Config.Example:8080"+path, bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.7:5555"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	edge.ServeHTTP(w, req)
	return w
}

// The nginx.conf locations, one by one.
func TestEdgeServesTheFrontendLikeNginx(t *testing.T) {
	edge, _ := edgeFixture(t)
	check := func(path string, status int, cache, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := edgeDo(edge, "GET", path, nil, nil)
		if w.Code != status || w.Header().Get("Cache-Control") != cache || (body != "" && w.Body.String() != body) {
			t.Fatalf("%s: %d %q %q", path, w.Code, w.Header().Get("Cache-Control"), w.Body.String())
		}
		return w
	}
	check("/", 200, "no-cache", "<html>spa</html>")
	check("/agents/some/page", 200, "no-cache", "<html>spa</html>")
	check("/frontend-version.json", 200, "no-store", `{"v":1}`)
	w := check("/assets/index-AbCdEf12.js", 200, "public, max-age=31536000, immutable", "")
	if w.Header().Get("Content-Type") != "application/javascript" {
		t.Fatal(w.Header())
	}
	check("/assets/missing-AbCdEf12.js", 404, "no-store", "Static file not found\n")
	check("/assets/plain.txt", 200, "no-cache", "plain")
	check("/assets/missing.txt", 404, "no-store", "Static file not found\n")
	check("/missing.js", 404, "no-store", "Static file not found\n")
	check("/sw.js", 200, "no-cache", "self")
	check("/health", 200, "", "healthy\n")
	check("/_configflow/listener-health", 200, "no-store", "tok123\n")
	// static extensions under /api go to the API, not to the file system
	if w := edgeDo(edge, "GET", "/api/missing.js", nil, nil); w.Code != 404 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestEdgeProxyHeadersAndBodyLimits(t *testing.T) {
	edge, _ := edgeFixture(t)
	w := edgeDo(edge, "GET", "/api/echo", nil, map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "spoofed"})
	var got any
	got, _ = jsonx.Parse(w.Body.Bytes())
	want := jsonx.Obj("real_ip", "198.51.100.7", "xff", "203.0.113.1, 198.51.100.7", "host", "config.example",
		"fhost", "config.example", "remote", "127.0.0.1", "size", 0, "url_root", "http://config.example/")
	if !jsonx.Equal(got, want) {
		t.Fatal(w.Body.String())
	}
	big := bytes.Repeat([]byte("x"), httpx.DefaultBodyLimit+1)
	if w := edgeDo(edge, "POST", "/api/echo", big, nil); w.Code != 413 || !strings.Contains(w.Body.String(), "413 Request Entity Too Large") {
		t.Fatal(w.Code)
	}
	if w := edgeDo(edge, "POST", "/api/oxidns/settings", big, nil); w.Code != 200 {
		t.Fatal("oxidns settings accept up to 96 MiB", w.Code)
	}
}

func TestEdgeGzip(t *testing.T) {
	edge, _ := edgeFixture(t)
	w := edgeDo(edge, "GET", "/assets/style-12345678.css", nil, map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal(w.Header())
	}
	reader, _ := gzip.NewReader(w.Body)
	data, _ := io.ReadAll(reader)
	if string(data) != "precompressed-css" {
		t.Fatal("gzip_static must send the .gz file", string(data))
	}
	plain := edgeDo(edge, "GET", "/assets/style-12345678.css", nil, nil)
	if plain.Body.String() != "original-css" || plain.Header().Get("Content-Encoding") != "" {
		t.Fatal(plain.Header())
	}
	api := edgeDo(edge, "GET", "/api/echo", nil, map[string]string{"Accept-Encoding": "gzip"})
	if api.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("API JSON is compressed like nginx gzip_proxied any", api.Header())
	}
	reader, _ = gzip.NewReader(api.Body)
	data, _ = io.ReadAll(reader)
	if !strings.Contains(string(data), `"real_ip"`) {
		t.Fatal(string(data))
	}
}
