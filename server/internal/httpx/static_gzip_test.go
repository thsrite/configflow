package httpx

import (
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticGzipMatchesFreshWriter(t *testing.T) {
	file := filepath.Join(t.TempDir(), "small.js")
	edge := &Edge{}
	for _, body := range [][]byte{[]byte("example.com"), bytes.Repeat([]byte("a"), 997), {}} {
		if err := os.WriteFile(file, body, 0600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		edge.serveCompressed(rec, httptest.NewRequest("GET", "/small.js", nil), file, info)
		var want bytes.Buffer
		gz, _ := gzip.NewWriterLevel(&want, 6)
		if _, err := gz.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || !bytes.Equal(rec.Body.Bytes(), want.Bytes()) {
			t.Fatalf("gzip response differs for %d bytes", len(body))
		}
	}
}

func BenchmarkServeSmallGzip(b *testing.B) {
	file := filepath.Join(b.TempDir(), "small.js")
	if err := os.WriteFile(file, bytes.Repeat([]byte("a"), 997), 0600); err != nil {
		b.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		b.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/small.js", nil)
	edge := &Edge{}
	b.ReportAllocs()
	for b.Loop() {
		edge.serveCompressed(httptest.NewRecorder(), req, file, info)
	}
}
