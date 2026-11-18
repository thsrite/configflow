package httpx_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

func TestFileRangeHeadersMatchFlask(t *testing.T) {
	type result struct {
		Status       int    `json:"status"`
		AcceptRanges string `json:"accept_ranges"`
		ContentRange string `json:"content_range"`
		Body         string `json:"body"`
	}
	home, _ := os.UserHomeDir()
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `from flask import Flask,send_file
from io import BytesIO
import json
app=Flask(__name__)
@app.route('/file',methods=['GET','POST'])
def file(): return send_file(BytesIO(b'synthetic-attachment'),as_attachment=True,download_name='fixture.bin')
client=app.test_client()
result=[]
for method,headers in [('GET',{}),('HEAD',{}),('POST',{}),('GET',{'Range':'bytes=0-3'}),('POST',{'Range':'bytes=0-3'})]:
 r=client.open('/file',method=method,headers=headers)
 result.append({'status':r.status_code,'accept_ranges':r.headers.get('Accept-Ranges',''),'content_range':r.headers.get('Content-Range',''),'body':r.get_data(as_text=True)})
print(json.dumps(result))`)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Flask file reference: %v %s", err, raw)
	}
	var want []result
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.Handle([]string{"GET", "POST"}, "/api/file", "test.file", func(*httpx.Ctx) (*httpx.Response, error) {
			return httpx.File(httpx.FileOptions{Data: []byte("synthetic-attachment"), AsAttachment: true, DownloadName: "fixture.bin"})
		})
	})})
	for i, test := range []struct {
		method      string
		rangeHeader bool
	}{{"GET", false}, {"HEAD", false}, {"POST", false}, {"GET", true}, {"POST", true}} {
		var options []httpxtest.Option
		if test.rangeHeader {
			options = append(options, httpxtest.Header("Range", "bytes=0-3"))
		}
		response := srv.Do(test.method, "/api/file", options...)
		got := result{response.Status, response.Header.Get("Accept-Ranges"), response.Header.Get("Content-Range"), string(response.Body)}
		if got != want[i] {
			t.Fatalf("%s range=%v: got=%+v want=%+v", test.method, test.rangeHeader, got, want[i])
		}
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
