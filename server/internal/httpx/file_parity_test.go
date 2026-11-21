package httpx_test

import (
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

// TestFileRangeHeaders: file responses are served by http.ServeContent
// (O18 C15): Accept-Ranges on every served answer, a single range as 206,
// an unusable range as a plain-text 416. Other methods (POST download
// routes) get the whole file and no range handling.
func TestFileRangeHeaders(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.Handle([]string{"GET", "POST"}, "/api/file", "test.file", func(*httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Data: []byte("synthetic-attachment"), AsAttachment: true, DownloadName: "fixture.bin"})
		})
	})})
	tests := []struct {
		method, rangeHeader string
		status              int
		contentRange, body  string
	}{
		{"GET", "", 200, "", "synthetic-attachment"},
		{"HEAD", "", 200, "", ""},
		{"POST", "", 200, "", "synthetic-attachment"},
		{"GET", "bytes=0-3", 206, "bytes 0-3/20", "synt"},
		{"HEAD", "bytes=0-3", 206, "bytes 0-3/20", ""},
		{"GET", "bytes=30-", 416, "bytes */20", "invalid range: failed to overlap\n"},
		{"POST", "bytes=0-3", 200, "", "synthetic-attachment"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.rangeHeader, func(t *testing.T) {
			var options []httpxtest.Option
			if tt.rangeHeader != "" {
				options = append(options, httpxtest.Header("Range", tt.rangeHeader))
			}
			response := srv.Do(tt.method, "/api/file", options...)
			acceptRanges := map[bool]string{true: "bytes", false: ""}[tt.status != 416 && tt.method != "POST"]
			if response.Status != tt.status || response.Header.Get("Accept-Ranges") != acceptRanges ||
				response.Header.Get("Content-Range") != tt.contentRange || string(response.Body) != tt.body {
				t.Fatalf("got %d %q %q %q", response.Status, response.Header.Get("Accept-Ranges"), response.Header.Get("Content-Range"), response.Body)
			}
		})
	}
}

func TestGeneratedFileETagStabilityAndInvalidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.yaml")
	if err := os.WriteFile(path, []byte("synthetic: first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/api/file", "test.file", func(*httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Path: path, DownloadName: "generated.yaml", AsAttachment: true})
		})
	})})
	first, again := srv.Get("/api/file"), srv.Get("/api/file")
	first.WantStatus(200)
	again.WantStatus(200)
	etag := first.Header.Get("ETag")
	if etag == "" || again.Header.Get("ETag") != etag {
		t.Fatal("unchanged file lost its stable ETag")
	}
	srv.Get("/api/file", httpxtest.Header("If-None-Match", etag)).WantStatus(304)
	if err := os.WriteFile(path, []byte("synthetic: second-content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := srv.Get("/api/file")
	changed.WantStatus(200)
	if changed.Header.Get("ETag") == etag {
		t.Fatal("changed file retained its ETag")
	}
	srv.Get("/api/file", httpxtest.Header("If-None-Match", etag)).WantStatus(200)
	srv.Get("/api/file", httpxtest.Header("If-None-Match", changed.Header.Get("ETag"))).WantStatus(304)
}

// TestFilePOSTIgnoresPreconditions: a file answered to POST (generated
// downloads) is not subject to If-None-Match/If-Match, which ServeContent
// would turn into 412 for a non-GET method.
func TestFilePOSTIgnoresPreconditions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.yaml")
	if err := os.WriteFile(path, []byte("synthetic: post\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/file", "test.file", func(*httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Path: path, DownloadName: "generated.yaml", AsAttachment: true})
		})
	})})
	for _, header := range [][2]string{{"If-None-Match", "*"}, {"If-Match", `"other"`}, {"Range", "bytes=0-3"}} {
		response := srv.Do("POST", "/api/file", httpxtest.Header(header[0], header[1]))
		if response.Status != 200 || string(response.Body) != "synthetic: post\n" {
			t.Fatalf("%s: got %d %q", header[0], response.Status, response.Body)
		}
	}
}
