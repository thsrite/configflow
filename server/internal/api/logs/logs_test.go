package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// These tests drive the handlers through a minimal pipeline (the package
// cannot import httpxtest, which wires it).

type logClient struct {
	t    *testing.T
	app  *httpx.App
	repo *repo.Repository
	path string
}

func newLogClient(t *testing.T) *logClient {
	dir := t.TempDir()
	repository, err := repo.Open(filepath.Join(dir, "repository"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.log")
	previous := LogFilePath
	LogFilePath = func() string { return path }
	t.Cleanup(func() { LogFilePath = previous; openFile = defaultOpen })
	app := httpx.New()
	Register(app.Router)
	return &logClient{t, app, repository, path}
}

var defaultOpen = openFile

func (c *logClient) get(path string) (int, *jsonx.Object) {
	c.t.Helper()
	ctx := repo.WithRepository(context.Background(), c.repo)
	res, err := c.app.Invoke(ctx, httpx.InvokeRequest{Method: "GET", Path: path[:strings.IndexByte(path+"?", '?')],
		Query: queryOf(path)})
	if err != nil {
		c.t.Fatal(err)
	}
	value, err := jsonx.Parse(res.Body)
	if err != nil {
		c.t.Fatalf("%s: %s", err, res.Body)
	}
	return res.Status, value.(*jsonx.Object)
}

func queryOf(path string) [][2]string {
	_, raw, _ := strings.Cut(path, "?")
	return httpx.ParseQuery(raw)
}

func strs(values ...string) []any { return jsonx.ListOf(values) }

// test_logs_tail.py::test_tail_precedes_filters_and_nonpositive_limits_return_all
func TestTailPrecedesFiltersAndNonpositiveLimitsReturnAll(t *testing.T) {
	all := strs("ERROR: old needle", "INFO: two", "WARNING: three", "INFO: needle four", "ERROR: final")
	cases := []struct {
		query string
		want  []any
	}{
		{"lines=2", strs("INFO: needle four", "ERROR: final")},
		{"lines=2&search=NEEDLE", strs("INFO: needle four")},
		{"lines=2&level=warning", strs()},
		{"lines=3&level=warning", strs("WARNING: three")},
		{"lines=2&search=needle&level=error", strs()},
		{"lines=0", all},
		{"lines=-2", all},
	}
	for _, c := range cases {
		client := newLogClient(t)
		os.WriteFile(client.path, []byte("ERROR: old needle\nINFO: two\nWARNING: three\nINFO: needle four\nERROR: final"), 0o644)
		status, data := client.get("/api/logs/tail?" + c.query)
		want := jsonx.Obj("success", true, "logs", c.want, "total_lines", 5, "filtered_lines", len(c.want), "log_file", client.path)
		if status != 200 || !jsonx.Equal(data, want) {
			t.Errorf("%s: %d %s", c.query, status, jsonx.Repr(data))
		}
	}
}

// test_logs_tail.py::test_tail_default_invalid_limit_and_maximum
func TestTailDefaultInvalidLimitAndMaximum(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 10003; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	for query, count := range map[string]int{"": 100, "lines=invalid": 100, "lines=10001": 10000} {
		client := newLogClient(t)
		os.WriteFile(client.path, []byte(b.String()), 0o644)
		_, data := client.get("/api/logs/tail?" + query)
		if data.GetInt("total_lines") != 10003 || data.GetInt("filtered_lines") != int64(count) {
			t.Fatalf("%s: %d %d", query, data.GetInt("total_lines"), data.GetInt("filtered_lines"))
		}
		logs := data.GetList("logs")
		if logs[0] != fmt.Sprintf("line %d", 10003-count) || logs[len(logs)-1] != "line 10002" {
			t.Fatalf("%s: %v %v", query, logs[0], logs[len(logs)-1])
		}
	}
}

// test_logs_tail.py::test_tail_counts_blank_lines_ignores_invalid_utf8_and_normalizes_newlines
func TestTailCountsBlankLinesIgnoresInvalidUTF8AndNormalizesNewlines(t *testing.T) {
	client := newLogClient(t)
	os.WriteFile(client.path, []byte("first\r\n\nINFO: \xffvalid\rfinal"), 0o644)
	_, data := client.get("/api/logs/tail?lines=3")
	if data.GetInt("total_lines") != 4 || !jsonx.Equal(data.Value("logs"), strs("", "INFO: valid", "final")) {
		t.Fatal(jsonx.Repr(data))
	}
}

type countingFile struct {
	lines, read int
	closed      bool
	buffer      []byte
}

func (f *countingFile) Read(p []byte) (int, error) {
	for len(f.buffer) == 0 {
		if f.read == f.lines {
			return 0, io.EOF
		}
		f.buffer = []byte(fmt.Sprintf("INFO: generated %d\n", f.read))
		f.read++
	}
	n := copy(p, f.buffer)
	f.buffer = f.buffer[n:]
	return n, nil
}

func (f *countingFile) Close() error { f.closed = true; return nil }

// test_logs_tail.py::test_tail_reads_full_count_without_reading_file_into_a_list
// (the bounded tail is a ring of `lines` entries while the file streams).
func TestTailReadsFullCountWithoutReadingFileIntoAList(t *testing.T) {
	client := newLogClient(t)
	os.WriteFile(client.path, nil, 0o644)
	var opened []*countingFile
	openFile = func(path string) (io.ReadCloser, error) {
		f := &countingFile{lines: 50000}
		opened = append(opened, f)
		return f, nil
	}
	_, data := client.get("/api/logs/tail?lines=60")
	want := []any{}
	for i := 49940; i < 50000; i++ {
		want = append(want, fmt.Sprintf("INFO: generated %d", i))
	}
	if data.Value("success") != true || data.GetInt("total_lines") != 50000 || !jsonx.Equal(data.Value("logs"), want) {
		t.Fatal(data.GetInt("total_lines"))
	}
	if len(opened) != 1 || !opened[0].closed || opened[0].read != 50000 {
		t.Fatal("one streamed read, closed")
	}
}

// test_logs_tail.py::test_tail_missing_or_empty_file
func TestTailMissingOrEmptyFile(t *testing.T) {
	for _, present := range []bool{false, true} {
		client := newLogClient(t)
		if present {
			os.WriteFile(client.path, nil, 0o644)
		}
		_, data := client.get("/api/logs/tail?lines=60")
		if data.Value("success") != true || len(data.GetList("logs")) != 0 || data.GetInt("total_lines") != 0 {
			t.Fatal(jsonx.Repr(data))
		}
		if present && data.GetInt("filtered_lines") != 0 {
			t.Fatal("filtered")
		}
		if !present && data.GetString("message") != "日志文件不存在: "+client.path {
			t.Fatal(data.GetString("message"))
		}
	}
}

// test_logs_tail.py::test_tail_read_error_and_invalid_level_keep_existing_error_response
func TestTailReadErrorAndInvalidLevelKeepExistingErrorResponse(t *testing.T) {
	client := newLogClient(t)
	os.WriteFile(client.path, []byte("INFO: one\n"), 0o644)
	status, data := client.get("/api/logs/tail?level=%5B")
	if status != 500 || data.Value("success") != false {
		t.Fatal(status, jsonx.Repr(data))
	}
	openFile = func(string) (io.ReadCloser, error) { return nil, errors.New("synthetic read failure") }
	status, data = client.get("/api/logs/tail")
	if status != 500 || !jsonx.Equal(data, jsonx.Obj("success", false, "error", "synthetic read failure", "logs", []any{}, "total_lines", 0)) {
		t.Fatal(status, jsonx.Repr(data))
	}
}

// test_logs_tail.py::test_tail_response_still_redacts_repository_credentials
func TestTailResponseStillRedactsRepositoryCredentials(t *testing.T) {
	client := newLogClient(t)
	secret := "log-tail-private-capability"
	_, err := client.repo.UpdateSystemTransaction(context.Background(), func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetObject("system_config").Set("rule_proxy_token", secret)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(client.path, []byte("INFO: safe\nERROR: url=https://invalid/"+secret+"\n"), 0o644)
	_, data := client.get("/api/logs/tail?lines=2")
	if !jsonx.Equal(data.Value("logs"), strs("INFO: safe", "[REDACTED]")) {
		t.Fatal(jsonx.Repr(data))
	}
	content, _ := os.ReadFile(client.path)
	if !strings.Contains(string(content), secret) {
		t.Fatal("the file itself is untouched")
	}
}

func TestInfoAndClear(t *testing.T) {
	client := newLogClient(t)
	status, data := client.get("/api/logs/info")
	if status != 200 || !jsonx.Equal(data, jsonx.Obj("success", true, "exists", false, "path", client.path)) {
		t.Fatal(jsonx.Repr(data))
	}
	os.WriteFile(client.path, []byte(strings.Repeat("x", 1536*1024)), 0o644)
	_, data = client.get("/api/logs/info")
	if data.GetInt("size") != 1536*1024 || data.Value("size_mb").(jsonx.Number).String() != "1.5" || data.GetString("modified") == "" {
		t.Fatal(jsonx.Repr(data))
	}
	ctx := repo.WithRepository(context.Background(), client.repo)
	res, _ := client.app.Invoke(ctx, httpx.InvokeRequest{Method: "POST", Path: "/api/logs/clear"})
	cleared, _ := jsonx.Parse(res.Body)
	if !jsonx.Equal(cleared, jsonx.Obj("message", "日志已清空", "success", true)) || !strings.HasPrefix(string(res.Body), `{"message":"日志`) {
		t.Fatal(string(res.Body))
	}
	if content, _ := os.ReadFile(client.path); len(content) != 0 {
		t.Fatal("not cleared")
	}
}

func TestPathStr(t *testing.T) {
	for in, want := range map[string]string{"/data/app.log": "/data/app.log", "./app.log": "app.log",
		"data//app.log": "data/app.log", "//x/y": "//x/y", "///x": "/x", ".": "."} {
		if got := pathStr(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
