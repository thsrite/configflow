package difftest

// Flask↔Go differential of GET /api/agents/download/<filename> (A10):
// status, the send_file headers (type, attachment name, ETag, Last-Modified,
// length, cache control) and the bytes, for regular files, an empty file, a
// directory, an unreadable file, a symlink, invalid names, HEAD, and a missing
// static directory. Range and conditional requests are served by
// http.ServeContent (O18 C15, maintainer ruling 2026-11-22: no Werkzeug
// make_conditional emulation); those rows assert the standard net/http result
// (status, Content-Range, body length) instead of Flask's.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Download ./internal/api/agents/difftest/'

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
)

var downloadHeaders = []string{"Content-Type", "Content-Disposition", "Cache-Control", "ETag", "Last-Modified",
	"Content-Length", "Accept-Ranges", "Content-Range", "X-Content-Type-Options"}

type downloadRequest struct {
	name    string
	method  string
	path    string
	headers map[string]string
	// httpx names a known deviation of the shared httpx send_file code from
	// Werkzeug (reported to the coordinator, not this route's logic).
	httpx string
	// std and stdAfter are the http.ServeContent results asserted instead of
	// comparing with Flask, before and after the files change; nil compares.
	std, stdAfter *stdDownload
}

// stdDownload is the expected standard answer to a Range/conditional request.
type stdDownload struct {
	status       int
	contentRange string
	size         int // body length; -1 is not checked
}

func checkStd(want *stdDownload, gor response) []string {
	var diffs []string
	if gor.status != want.status {
		diffs = append(diffs, fmt.Sprintf("status %d, want %d", gor.status, want.status))
	}
	if got := gor.header.Get("Content-Range"); got != want.contentRange {
		diffs = append(diffs, fmt.Sprintf("Content-Range %q, want %q", got, want.contentRange))
	}
	if want.size >= 0 && len(gor.body) != want.size {
		diffs = append(diffs, fmt.Sprintf("body %d bytes, want %d", len(gor.body), want.size))
	}
	return diffs
}

func sendDownload(t *testing.T, base string, r downloadRequest) response {
	req, err := http.NewRequest(r.method, base+r.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return response{r.method, resp.StatusCode, resp.Header, data}
}

func compareDownload(py, gor response) []string {
	py.header = httpxtest.O9ExpectedHeaders(py.header, gor.header)
	var diffs []string
	pv, perr := parsePythonJSON(py.body)
	gv, gerr := parsePythonJSON(gor.body)
	if nativeP, nativeG, ok := nativeFileDiagnostic(py, gor); ok {
		pv, gv = nativeP, nativeG
	}
	jsonType := strings.HasPrefix(py.header.Get("Content-Type"), "application/json")
	jsonBody := jsonType && perr == nil && gerr == nil
	jsonHead := jsonType && len(py.body) == 0 && len(gor.body) == 0
	migrated := httpxtest.MigratedAPIError(py.method, py.status, gor.status, py.header, gor.header, py.body, gor.body)
	if py.status != gor.status {
		diffs = append(diffs, fmt.Sprintf("status python %d go %d", py.status, gor.status))
	}
	for _, name := range downloadHeaders {
		if ((jsonBody || jsonHead) && name == "Content-Length") || (migrated && (name == "Content-Type" || name == "Content-Length")) {
			continue
		}
		if a, b := py.header.Get(name), gor.header.Get(name); a != b {
			diffs = append(diffs, fmt.Sprintf("header %s python %q go %q", name, a, b))
		}
	}
	if migrated {
		// Step B #3: exact JSON error envelope checked above.
	} else if jsonBody {
		if !jsonEqual(pv, gv) {
			diffs = append(diffs, fmt.Sprintf("json python %s go %s", py.body, gor.body))
		}
	} else if string(py.body) != string(gor.body) {
		diffs = append(diffs, fmt.Sprintf("body (%d vs %d bytes) python %.200q go %.200q", len(py.body), len(gor.body), py.body, gor.body))
	}
	return diffs
}

// etagFloatOnly reports whether both ETags are "<mtime>-<size>-<adler32>"
// with the same size and checksum and mtimes within a microsecond.
func etagFloatOnly(py, gor response) bool {
	a, b := strings.Trim(py.header.Get("ETag"), `"`), strings.Trim(gor.header.Get("ETag"), `"`)
	if a == b {
		return false
	}
	ta, sa, okA := strings.Cut(a, "-")
	tb, sb, okB := strings.Cut(b, "-")
	if !okA || !okB || sa != sb {
		return false
	}
	fa, errA := strconv.ParseFloat(ta, 64)
	fb, errB := strconv.ParseFloat(tb, 64)
	return errA == nil && errB == nil && math.Abs(fa-fb) < 1e-6
}

func TestDownloadDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	static := filepath.Join(base, "static")
	os.MkdirAll(static, 0o755)
	binary := make([]byte, 70000)
	rand.New(rand.NewSource(20261116)).Read(binary)
	os.WriteFile(filepath.Join(static, "configflow-agent-linux-amd64"), binary, 0o755)
	os.WriteFile(filepath.Join(static, "configflow-agent-linux-arm64"), nil, 0o644)
	os.MkdirAll(filepath.Join(static, "configflow-agent-linux-armv7"), 0o755)
	os.WriteFile(filepath.Join(static, "configflow-agent-linux-386"), []byte("x"), 0o644)
	modTime := time.Date(2026, 11, 1, 12, 30, 15, 123456789, time.UTC)
	os.Chtimes(filepath.Join(static, "configflow-agent-linux-amd64"), modTime, modTime)

	same, failed, known := 0, 0, 0
	phase := func(label, staticDir string, requests []downloadRequest, between func()) {
		seedDir, pyDir, goDir := filepath.Join(base, label, "seed"), filepath.Join(base, label, "python"), filepath.Join(base, label, "go")
		for _, dir := range []string{seedDir, pyDir, goDir} {
			os.MkdirAll(dir, 0o755)
		}
		seed(t, seedDir)
		copyDir(t, seedDir, pyDir)
		copyDir(t, seedDir, goDir)
		t.Setenv("AGENTS_STATIC_DIR", staticDir)
		pyBase, stopPython := startPython(t, pyDir)
		defer stopPython()
		goBase, stopGo := startGo(t, goDir)
		defer stopGo()
		etag := ""
		after := false
		run := func(r downloadRequest) {
			headers := map[string]string{}
			for k, v := range r.headers {
				headers[k] = strings.ReplaceAll(v, "{etag}", etag)
			}
			r.headers = headers
			py, gor := sendDownload(t, pyBase, r), sendDownload(t, goBase, r)
			if r.name == "amd64" && py.status == 200 {
				etag = py.header.Get("ETag")
			}
			if want := map[bool]*stdDownload{false: r.std, true: r.stdAfter}[after]; want != nil {
				if diffs := checkStd(want, gor); len(diffs) > 0 {
					failed++
					t.Errorf("DIFF %s %s (net/http standard):\n  %s", label, r.name, strings.Join(diffs, "\n  "))
				} else {
					same++
					t.Logf("standard %s %-40.40s → %d %d bytes", label, r.name, gor.status, len(gor.body))
				}
				return
			}
			diffs := compareDownload(py, gor)
			if etagFloatOnly(py, gor) {
				// Werkzeug's ETag uses CPython's st_mtime (sec + nsec*1e-9 as a
				// double); httpx formats a differently rounded float.
				var rest []string
				for _, d := range diffs {
					if !strings.HasPrefix(d, "header ETag ") {
						rest = append(rest, d)
					}
				}
				if len(rest) == 0 {
					known++
					t.Logf("known httpx difference %s %s (ETag mtime float): %v", label, r.name, diffs)
					return
				}
				if r.httpx == "" {
					diffs = rest
				}
			}
			if len(diffs) > 0 && r.httpx != "" {
				known++
				t.Logf("known httpx difference %s %s (%s):\n  %s", label, r.name, r.httpx, strings.Join(diffs, "\n  "))
				return
			}
			if len(diffs) > 0 {
				failed++
				t.Errorf("DIFF %s %s:\n  %s", label, r.name, strings.Join(diffs, "\n  "))
			} else {
				same++
				t.Logf("same %s %-40.40s → %d %d bytes", label, r.name, py.status, len(py.body))
			}
		}
		for _, r := range requests {
			run(r)
		}
		if between != nil {
			between()
			after = true
			for _, r := range requests {
				run(r)
			}
		}
	}
	get := func(name, filename string, headers ...string) downloadRequest {
		r := downloadRequest{name: name, method: "GET", path: "/api/agents/download/" + filename, headers: map[string]string{}}
		for i := 0; i+1 < len(headers); i += 2 {
			r.headers[headers[i]] = headers[i+1]
		}
		return r
	}
	const amd64 = "configflow-agent-linux-amd64"
	std := func(r downloadRequest, status int, contentRange string, size int) downloadRequest {
		r.std = &stdDownload{status, contentRange, size}
		return r
	}
	requests := []downloadRequest{
		get("amd64", "configflow-agent-linux-amd64"),
		get("arm64 empty", "configflow-agent-linux-arm64"),
		get("armv7 directory", "configflow-agent-linux-armv7"),
		get("386 not allowed", "configflow-agent-linux-386"),
		get("sha256 not allowed", "configflow-agent-linux-amd64.sha256"),
		get("upper case", "CONFIGFLOW-AGENT-LINUX-AMD64"),
		get("encoded slash", "..%2Fconfigflow-agent-linux-amd64"),
		get("nul", "configflow-agent-linux-amd64%00"),
		get("trailing slash", "configflow-agent-linux-amd64/"),
		std(get("range head", amd64, "Range", "bytes=0-9"), 206, "bytes 0-9/70000", 10),
		std(get("range tail", amd64, "Range", "bytes=-5"), 206, "bytes 69995-69999/70000", 5),
		std(get("range open", amd64, "Range", "bytes=69990-"), 206, "bytes 69990-69999/70000", 10),
		std(get("range unsatisfiable", amd64, "Range", "bytes=80000-90000"), 416, "bytes */70000", -1),
		std(get("range multi", amd64, "Range", "bytes=0-1,5-6"), 206, "", -1), // multipart/byteranges
		std(get("range malformed", amd64, "Range", "bytes=x-y"), 416, "", -1),
		std(get("if-modified-since later", amd64, "If-Modified-Since", "Sun, 01 Nov 2026 12:30:16 GMT"), 304, "", 0),
		std(get("if-modified-since earlier", amd64, "If-Modified-Since", "Sun, 01 Nov 2026 12:30:14 GMT"), 200, "", 70000),
		std(get("if-none-match star", amd64, "If-None-Match", "*"), 304, "", 0),
		std(get("if-none-match other", amd64, "If-None-Match", `"other"`), 200, "", 70000),
		std(get("if-none-match etag", amd64, "If-None-Match", "{etag}"), 304, "", 0),
		std(get("if-none-match weak etag", amd64, "If-None-Match", "W/{etag}"), 304, "", 0),
		std(get("if-range etag", amd64, "Range", "bytes=0-3", "If-Range", "{etag}"), 206, "bytes 0-3/70000", 4),
		std(get("if-range stale", amd64, "Range", "bytes=0-3", "If-Range", `"stale"`), 200, "", 70000),
		std(get("range with matching if-none-match", amd64, "Range", "bytes=0-3", "If-None-Match", "{etag}"), 304, "", 0),
		std(get("if-range weak etag", amd64, "Range", "bytes=0-3", "If-Range", "W/{etag}"), 200, "", 70000),
		std(get("if-match other", amd64, "If-Match", `"other"`), 412, "", 0),
		std(get("range bad units", amd64, "Range", "items=0-3"), 416, "", -1),
		std(get("range reversed", amd64, "Range", "bytes=9-3"), 416, "", -1),
		std(get("range empty", amd64, "Range", ""), 200, "", 70000),
		std(get("range spaces", amd64, "Range", " bytes = 1 - 2 "), 416, "", -1),
		func() downloadRequest {
			// Empty file: ServeContent ignores Range; after the change it is
			// a 1-byte file, so bytes=5-9 cannot overlap.
			r := std(get("range on empty file", "configflow-agent-linux-arm64", "Range", "bytes=5-9"), 200, "", 0)
			r.stdAfter = &stdDownload{416, "bytes */1", -1}
			return r
		}(),
		std(get("range with if-range date", amd64, "Range", "bytes=80000-", "If-Range", "Sun, 01 Nov 2026 12:30:15 GMT"), 416, "bytes */70000", -1),
		std(get("range with old if-range date", amd64, "Range", "bytes=80000-", "If-Range", "Sun, 01 Nov 2026 12:30:14 GMT"), 200, "", 70000),
		{name: "head amd64", method: "HEAD", path: "/api/agents/download/configflow-agent-linux-amd64"},
		{name: "post", method: "POST", path: "/api/agents/download/configflow-agent-linux-amd64"},
		// A10 review: traversal and encoding attempts (Werkzeug decodes the
		// path before routing; <filename> never matches "/").
		get("review dotdot", ".."),
		get("review dot", "."),
		get("review encoded dotdot", "%2e%2e"),
		get("review encoded dotdot slash", "%2e%2e%2fconfigflow-agent-linux-amd64"),
		get("review dotdot slash", "../configflow-agent-linux-amd64"),
		get("review double encoded slash", "..%252Fconfigflow-agent-linux-amd64"),
		get("review backslash", "..%5Cconfigflow-agent-linux-amd64"),
		get("review encoded allowed name", "%63onfigflow-agent-linux-amd%364"),
		get("review encoded dash", "configflow%2Dagent%2dlinux-arm64"),
		get("review absolute", "%2Fetc%2Fpasswd"),
		get("review empty", ""),
		get("review query", "configflow-agent-linux-amd64?x=1&filename=configflow-agent-linux-386"),
		get("review semicolon", "configflow-agent-linux-amd64;x"),
		get("review unicode", "configflow-agent-linux-%E4%BE%8B"),
		get("review invalid utf8", "configflow-agent-linux-%FF"),
		get("review space", "configflow-agent-linux-amd64%20"),
		get("review long", strings.Repeat("a", 4000)),
		{name: "review no trailing segment", method: "GET", path: "/api/agents/download"},
		{name: "review options", method: "OPTIONS", path: "/api/agents/download/configflow-agent-linux-amd64"},
		// A10 review: absurd dates are not HTTP dates, so the conditions are
		// ignored (Werkzeug raised → Flask 500); huge or non-ASCII bounds
		// are invalid ranges (416).
		std(get("review if-modified-since overflow", amd64, "If-Modified-Since", "01 Nov 99999999999 12:30:15 GMT"), 200, "", 70000),
		std(get("review if-modified-since c long overflow", amd64, "If-Modified-Since", "01 Nov 99999999999999999999 12:30:15 GMT"), 200, "", 70000),
		std(get("review if-modified-since offset overflow", amd64, "If-Modified-Since", "01 Nov 2026 12:30:15 +3000000000000"), 200, "", 70000),
		std(get("review if-range overflow", amd64, "Range", "bytes=0-3", "If-Range", "01 Nov 99999999999 12:30:15 GMT"), 200, "", 70000),
		std(get("review if-range date without weekday", amd64, "Range", "bytes=80000-", "If-Range", "01 Nov 2026 12:30:15 +0000"), 200, "", 70000),
		std(get("review range begin beyond int64", amd64, "Range", "bytes=99999999999999999999-"), 416, "", -1),
		std(get("review range end beyond int64", amd64, "Range", "bytes=0-99999999999999999999"), 416, "", -1),
		std(get("review range latin-1 nbsp", amd64, "Range", "bytes=0-9\xa0"), 416, "", -1),
		std(get("review if-range date without weekday satisfiable", amd64, "Range", "bytes=0-3", "If-Range", "01 Nov 2026 12:30:15 +0000"), 200, "", 70000),
	}
	// "{etag}" is the file's ETag from Flask's first answer (Werkzeug:
	// mtime-size-adler32(path)); both sides must honour it.
	phase("present", static, requests, func() {
		os.Chmod(filepath.Join(static, "configflow-agent-linux-amd64"), 0o000)
		os.Symlink(filepath.Join(static, "configflow-agent-linux-386"), filepath.Join(static, "configflow-agent-linux-arm64.tmp"))
		os.Remove(filepath.Join(static, "configflow-agent-linux-arm64"))
		os.Rename(filepath.Join(static, "configflow-agent-linux-arm64.tmp"), filepath.Join(static, "configflow-agent-linux-arm64"))
		os.RemoveAll(filepath.Join(static, "configflow-agent-linux-armv7"))
		os.Symlink(filepath.Join(base, "dangling"), filepath.Join(static, "configflow-agent-linux-armv7"))
	})
	os.Chmod(filepath.Join(static, "configflow-agent-linux-amd64"), 0o644)
	phase("missing directory", filepath.Join(base, "does-not-exist"), requests[:3], nil)
	phase("empty variable", "", requests[:3], nil)
	t.Logf("download route: %d comparisons equal, %d differ, %d known httpx differences (reported)", same, failed, known)
}

// nativeFileDiagnostic compares only the native spelling of two established I/O
// failures. Every status/header and non-message body field is still compared.
func nativeFileDiagnostic(python, native response) (any, any, bool) {
	if python.status != 500 || native.status != 500 {
		return nil, nil, false
	}
	p, err := parsePythonJSON(python.body)
	if err != nil {
		return nil, nil, false
	}
	g, err := parsePythonJSON(native.body)
	if err != nil {
		return nil, nil, false
	}
	a, aok := p.(*orderedObject)
	b, bok := g.(*orderedObject)
	if !aok || !bok || a.Len() != 2 || b.Len() != 2 || a.Value("success") != false || b.Value("success") != false {
		return nil, nil, false
	}
	old, text := a.GetString("message"), b.GetString("message")
	category := ""
	if strings.HasPrefix(old, "[Errno 2] No such file or directory: ") && strings.HasSuffix(text, ": no such file or directory") {
		category = "not found"
	}
	if strings.HasPrefix(old, "[Errno 21] Is a directory: ") && strings.HasSuffix(text, ": is a directory") {
		category = "directory"
	}
	if strings.HasPrefix(old, "[Errno 13] Permission denied: ") && strings.HasSuffix(text, ": permission denied") {
		category = "permission denied"
	}
	if category == "" {
		return nil, nil, false
	}
	a.Set("message", category)
	b.Set("message", category)
	return a, b, true
}
