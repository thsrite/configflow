package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// These tests drive the handlers through a minimal pipeline (the package
// cannot import httpxtest, which wires it).

type logClient struct {
	t       testing.TB
	app     *httpx.App
	handler *Handler
	repo    *repo.Repository
	path    string
}

func newLogClient(t testing.TB) *logClient {
	t.Helper()
	dir := t.TempDir()
	repository, err := repo.Open(filepath.Join(dir, "repository"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repository.Close)
	path := filepath.Join(dir, "app.log")
	app := httpx.New()
	app.Repo = repository
	h := NewHandler()
	h.LogFilePath = func() (string, error) { return path, nil }
	h.Register(app.Router)
	return &logClient{t, app, h, repository, path}
}

func (c *logClient) get(path string) (int, map[string]any) {
	c.t.Helper()
	ctx := context.Background()
	res, err := c.app.Invoke(ctx, httpx.InvokeRequest{Method: "GET", Path: path[:strings.IndexByte(path+"?", '?')],
		Query: queryOf(path)})
	if err != nil {
		c.t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(res.Body))
	d.UseNumber()
	var value map[string]any
	if err := d.Decode(&value); err != nil {
		c.t.Fatalf("%s: %s", err, res.Body)
	}
	return res.Status, value
}

func jsonInt(v any) int64 {
	if n, ok := v.(json.Number); ok {
		i, _ := n.Int64()
		return i
	}
	return 0
}

func queryOf(path string) [][2]string {
	_, raw, _ := strings.Cut(path, "?")
	return httpx.ParseQuery(raw)
}

func strs(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

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
		want := map[string]any{"success": true, "logs": c.want, "total_lines": 5, "filtered_lines": len(c.want), "log_file": client.path}
		if status != 200 || !testjson.Equal(want, data) {
			t.Errorf("%s: %d %s", c.query, status, testjson.Diff(want, data))
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
		if jsonInt(data["total_lines"]) != 10003 || jsonInt(data["filtered_lines"]) != int64(count) {
			t.Fatalf("%s: %v %v", query, data["total_lines"], data["filtered_lines"])
		}
		logs, _ := data["logs"].([]any)
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
	if jsonInt(data["total_lines"]) != 4 || !testjson.Equal(strs("", "INFO: valid", "final"), data["logs"]) {
		t.Fatalf("%#v", data)
	}
}

type readMeter struct {
	bytes          int64
	opened, closed int
}

type countingFile struct {
	*os.File
	meter *readMeter
}

func (m *readMeter) open(path string) (LogFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	m.opened++
	return &countingFile{file, m}, nil
}

func (f *countingFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.meter.bytes += int64(n)
	return n, err
}

func (f *countingFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(p, off)
	f.meter.bytes += int64(n)
	return n, err
}

func (f *countingFile) Close() error {
	f.meter.closed++
	return f.File.Close()
}

func TestTailCountsOnceAndReadsOnlyTheWindowOnAppend(t *testing.T) {
	client := newLogClient(t)
	var content strings.Builder
	for i := 0; i < 50000; i++ {
		fmt.Fprintf(&content, "INFO: generated %d\n", i)
	}
	if err := os.WriteFile(client.path, []byte(content.String()), 0600); err != nil {
		t.Fatal(err)
	}
	var meter readMeter
	client.handler.OpenFile = meter.open
	_, data := client.get("/api/logs/tail?lines=60")
	want := []any{}
	for i := 49940; i < 50000; i++ {
		want = append(want, fmt.Sprintf("INFO: generated %d", i))
	}
	if data["success"] != true || jsonInt(data["total_lines"]) != 50000 || !testjson.Equal(want, data["logs"]) {
		t.Fatal(data["total_lines"])
	}
	if meter.opened != 1 || meter.closed != 1 || meter.bytes < int64(content.Len()) {
		t.Fatal("initial count", meter)
	}
	appendLog(t, client.path, []byte("INFO: appended\n"))
	meter.bytes = 0
	_, data = client.get("/api/logs/tail?lines=60")
	if meter.opened != 2 || meter.closed != 2 || meter.bytes > 256<<10 || jsonInt(data["total_lines"]) != 50001 {
		t.Fatal("incremental count", meter, data["total_lines"])
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
		logs, _ := data["logs"].([]any)
		if data["success"] != true || len(logs) != 0 || jsonInt(data["total_lines"]) != 0 {
			t.Fatalf("%#v", data)
		}
		if present && jsonInt(data["filtered_lines"]) != 0 {
			t.Fatal("filtered")
		}
		if !present && data["message"] != "日志文件不存在: "+client.path {
			t.Fatal(data["message"])
		}
	}
}

// test_logs_tail.py::test_tail_read_error_and_invalid_level_keep_existing_error_response
func TestTailReadErrorAndInvalidLevelKeepExistingErrorResponse(t *testing.T) {
	client := newLogClient(t)
	os.WriteFile(client.path, []byte("INFO: one\n"), 0o644)
	status, data := client.get("/api/logs/tail?level=%5B")
	if status != 500 || data["success"] != false {
		t.Fatalf("%d %#v", status, data)
	}
	client.handler.OpenFile = func(string) (LogFile, error) { return nil, errors.New("synthetic read failure") }
	status, data = client.get("/api/logs/tail")
	want := map[string]any{"success": false, "error": "synthetic read failure", "logs": []any{}, "total_lines": 0}
	if status != 500 || !testjson.Equal(want, data) {
		t.Fatalf("%d %s", status, testjson.Diff(want, data))
	}
}

// test_logs_tail.py::test_tail_response_still_redacts_repository_credentials
func TestTailResponseStillRedactsRepositoryCredentials(t *testing.T) {
	client := newLogClient(t)
	secret := "log-tail-private-capability"
	secretRaw, _ := json.Marshal(secret)
	err := client.repo.UpdateSystem(context.Background(), func(_ context.Context, s *model.System) error {
		return s.SystemConfig.SetJSON("rule_proxy_token", secretRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(client.path, []byte("INFO: safe\nERROR: url=https://invalid/"+secret+"\n"), 0o644)
	_, data := client.get("/api/logs/tail?lines=2")
	if !testjson.Equal(strs("INFO: safe", "[REDACTED]"), data["logs"]) {
		t.Fatalf("%#v", data)
	}
	content, _ := os.ReadFile(client.path)
	if !strings.Contains(string(content), secret) {
		t.Fatal("the file itself is untouched")
	}
}

func TestInfoAndClear(t *testing.T) {
	client := newLogClient(t)
	status, data := client.get("/api/logs/info")
	wantInfo := map[string]any{"success": true, "exists": false, "path": client.path}
	if status != 200 || !testjson.Equal(wantInfo, data) {
		t.Fatalf("%s", testjson.Diff(wantInfo, data))
	}
	os.WriteFile(client.path, []byte(strings.Repeat("x", 1536*1024)), 0o644)
	_, data = client.get("/api/logs/info")
	modified, _ := data["modified"].(string)
	if jsonInt(data["size"]) != 1536*1024 || data["size_mb"].(json.Number).String() != "1.5" || modified == "" {
		t.Fatalf("%#v", data)
	}
	ctx := context.Background()
	res, _ := client.app.Invoke(ctx, httpx.InvokeRequest{Method: "POST", Path: "/api/logs/clear"})
	wantCleared := map[string]any{"message": "日志已清空", "success": true}
	if !testjson.Equal(wantCleared, json.RawMessage(res.Body)) || !strings.HasPrefix(string(res.Body), `{"message":"日志`) {
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
