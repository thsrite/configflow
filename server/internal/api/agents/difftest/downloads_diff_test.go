package difftest

// Flask↔Go differential of GET /api/agents/download/<filename> (A10):
// status, the send_file headers (type, attachment name, ETag, Last-Modified,
// length, ranges, cache control) and the bytes, for regular files, an empty
// file, a directory, an unreadable file, a symlink, invalid names,
// conditional and range requests, HEAD, and a missing static directory.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Download ./internal/api/agents/difftest/'

import (
	"encoding/json"
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
	"configflow/internal/jsonx"
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
	// L1 D7 fixes Flask's catch-all 500 for unsatisfiable ranges. Accept only
	// the identical JSON error (or HEAD body), corrected status and size header.
	if py.status == 500 && gor.status == 416 && strings.Contains(py.header.Get("Content-Type"), "application/json") {
		var a, b struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if py.method == "HEAD" && len(py.body) == 0 && len(gor.body) == 0 ||
			json.Unmarshal(py.body, &a) == nil && json.Unmarshal(gor.body, &b) == nil && a == b && !a.Success && a.Message == "416 Requested Range Not Satisfiable: The server cannot provide the requested range." {
			if !strings.HasPrefix(gor.header.Get("Content-Range"), "bytes */") || gor.header.Get("Content-Disposition") != "" {
				return []string{"invalid L1 D7 range error headers"}
			}
			py.status = 416
			py.header = py.header.Clone()
			py.header.Set("Content-Range", gor.header.Get("Content-Range"))
		}
	}
	var diffs []string
	pv, perr := jsonx.Parse(py.body)
	gv, gerr := jsonx.Parse(gor.body)
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
		if !jsonx.Equal(pv, gv) {
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
			diffs := compareDownload(py, gor)
			if gor.status == 416 && gor.header.Get("Content-Range") != "bytes */"+strconv.Itoa(len(binary)) {
				diffs = append(diffs, "L1 D7 wrong binary size in Content-Range")
			}
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
			if py.status == 304 && gor.status == 304 {
				var rest []string
				for _, d := range diffs {
					if !strings.HasPrefix(d, "header Content-Disposition ") && !strings.HasPrefix(d, "header Last-Modified ") {
						rest = append(rest, d)
					}
				}
				if len(rest) == 0 && len(diffs) > 0 {
					known++
					t.Logf("known httpx difference %s %s (304 keeps Content-Disposition, drops Last-Modified in Werkzeug):\n  %s", label, r.name, strings.Join(diffs, "\n  "))
					return
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
		get("range head", "configflow-agent-linux-amd64", "Range", "bytes=0-9"),
		get("range tail", "configflow-agent-linux-amd64", "Range", "bytes=-5"),
		get("range open", "configflow-agent-linux-amd64", "Range", "bytes=69990-"),
		get("range unsatisfiable", "configflow-agent-linux-amd64", "Range", "bytes=80000-90000"),
		get("range multi", "configflow-agent-linux-amd64", "Range", "bytes=0-1,5-6"),
		get("range malformed", "configflow-agent-linux-amd64", "Range", "bytes=x-y"),
		get("if-modified-since later", "configflow-agent-linux-amd64", "If-Modified-Since", "Sun, 01 Nov 2026 12:30:16 GMT"),
		get("if-modified-since earlier", "configflow-agent-linux-amd64", "If-Modified-Since", "Sun, 01 Nov 2026 12:30:14 GMT"),
		get("if-none-match star", "configflow-agent-linux-amd64", "If-None-Match", "*"),
		get("if-none-match other", "configflow-agent-linux-amd64", "If-None-Match", `"other"`),
		get("if-none-match etag", "configflow-agent-linux-amd64", "If-None-Match", "{etag}"),
		get("if-none-match weak etag", "configflow-agent-linux-amd64", "If-None-Match", "W/{etag}"),
		get("if-range etag", "configflow-agent-linux-amd64", "Range", "bytes=0-3", "If-Range", "{etag}"),
		get("if-range stale", "configflow-agent-linux-amd64", "Range", "bytes=0-3", "If-Range", `"stale"`),
		func() downloadRequest {
			r := get("range with matching if-none-match", "configflow-agent-linux-amd64", "Range", "bytes=0-3", "If-None-Match", "{etag}")
			r.httpx = "Werkzeug processes Range before the 304 check"
			return r
		}(),
		func() downloadRequest {
			r := get("if-range weak etag", "configflow-agent-linux-amd64", "Range", "bytes=0-3", "If-Range", "W/{etag}")
			r.httpx = "Werkzeug's If-Range compares the unquoted tag"
			return r
		}(),
		func() downloadRequest {
			r := get("if-match other", "configflow-agent-linux-amd64", "If-Match", `"other"`)
			r.httpx = "Werkzeug answers 412 for a failed If-Match"
			return r
		}(),
		get("range bad units", "configflow-agent-linux-amd64", "Range", "items=0-3"),
		get("range reversed", "configflow-agent-linux-amd64", "Range", "bytes=9-3"),
		get("range empty", "configflow-agent-linux-amd64", "Range", ""),
		func() downloadRequest {
			r := get("range spaces", "configflow-agent-linux-amd64", "Range", " bytes = 1 - 2 ")
			r.httpx = "Werkzeug strips spaces around the unit and bounds"
			return r
		}(),
		func() downloadRequest {
			r := get("range on empty file", "configflow-agent-linux-arm64", "Range", "bytes=5-9")
			r.httpx = "Werkzeug ignores Range for an empty file"
			return r
		}(),
		get("range with if-range date", "configflow-agent-linux-amd64", "Range", "bytes=80000-", "If-Range", "Sun, 01 Nov 2026 12:30:15 GMT"),
		get("range with old if-range date", "configflow-agent-linux-amd64", "Range", "bytes=80000-", "If-Range", "Sun, 01 Nov 2026 12:30:14 GMT"),
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
		// A10 review: conditional headers Werkzeug's make_conditional turns
		// into exceptions (→ the view's 500) or accepts where Go did not.
		get("review if-modified-since overflow", "configflow-agent-linux-amd64", "If-Modified-Since", "01 Nov 99999999999 12:30:15 GMT"),
		get("review if-modified-since c long overflow", "configflow-agent-linux-amd64", "If-Modified-Since", "01 Nov 99999999999999999999 12:30:15 GMT"),
		get("review if-modified-since offset overflow", "configflow-agent-linux-amd64", "If-Modified-Since", "01 Nov 2026 12:30:15 +3000000000000"),
		get("review if-range overflow", "configflow-agent-linux-amd64", "Range", "bytes=0-3", "If-Range", "01 Nov 99999999999 12:30:15 GMT"),
		get("review if-range date without weekday", "configflow-agent-linux-amd64", "Range", "bytes=80000-", "If-Range", "01 Nov 2026 12:30:15 +0000"),
		get("review range begin beyond int64", "configflow-agent-linux-amd64", "Range", "bytes=99999999999999999999-"),
		func() downloadRequest {
			r := get("review range end beyond int64", "configflow-agent-linux-amd64", "Range", "bytes=0-99999999999999999999")
			r.httpx = "httpx range parsing (int64) differs from Werkzeug for huge bounds"
			return r
		}(),
		func() downloadRequest {
			r := get("review range latin-1 nbsp", "configflow-agent-linux-amd64", "Range", "bytes=0-9\xa0")
			r.httpx = "httpx does not strip Latin-1 white space in Range"
			return r
		}(),
		func() downloadRequest {
			r := get("review if-range date without weekday satisfiable", "configflow-agent-linux-amd64", "Range", "bytes=0-3", "If-Range", "01 Nov 2026 12:30:15 +0000")
			r.httpx = "httpx parses dates with http.ParseTime, Werkzeug with email.utils"
			return r
		}(),
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
