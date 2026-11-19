package agents

// Review of A02 against reference results produced by the Python functions
// (testdata/gen_a02_reference.py → testdata/a02_reference.json, CPython
// 3.11 on the test server). Every case asserts the Python result.

import (
	"database/sql"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

func loadReference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/a02_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func referenceCases(t *testing.T, name string) []*jsonx.Object {
	t.Helper()
	var out []*jsonx.Object
	for _, item := range loadReference(t).GetList(name) {
		out = append(out, item.(*jsonx.Object))
	}
	if len(out) == 0 {
		t.Fatalf("no %s cases", name)
	}
	return out
}

func compact(t *testing.T, value any) string {
	t.Helper()
	data, err := jsonx.Compact(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func statusSet(value any) map[string]bool {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	set := map[string]bool{}
	for _, item := range list {
		set[item.(string)] = true
	}
	return set
}

func intOf(value any) int {
	n, _ := jsonx.AsInt(value)
	return int(n)
}

func TestReferenceFailureText(t *testing.T) {
	for i, c := range referenceCases(t, "failure_text") {
		got := FailureText(c.Value("value"), c.GetList("secrets"), intOf(c.Value("limit")))
		if want := c.Value("want"); got != want {
			t.Errorf("case %d failure_text(%.80q, %v, %v)\n got %.300q\nwant %.300q", i, jsonx.Str(c.Value("value")),
				c.Value("secrets"), c.Value("limit"), got, want)
		}
	}
}

func TestReferenceEventMessage(t *testing.T) {
	for i, c := range referenceCases(t, "event_message") {
		if got, want := EventMessage(c.Value("value"), c.GetList("secrets")), c.Value("want"); got != want {
			t.Errorf("case %d event_message(%q)\n got %q\nwant %q", i, c.Value("value"), got, want)
		}
	}
}

func TestReferenceEventTime(t *testing.T) {
	for _, c := range referenceCases(t, "event_time") {
		if c.Value("want") == "NOW" {
			continue
		}
		got, ok := EventTime(c.Value("value"))
		want, wantOK := c.Value("want").(string)
		if ok != wantOK || got != want {
			t.Errorf("event_time(%q) = %q, %v; want %v", c.Value("value"), got, ok, c.Value("want"))
		}
	}
}

func TestReferenceEventLists(t *testing.T) {
	secrets := []any{"abc"}
	for i, c := range referenceCases(t, "clean_events") {
		got := CleanEvents(c.Value("value"), secrets, statusSet(c.Value("statuses")))
		if compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("clean_events %d\n got %s\nwant %s", i, compact(t, got), compact(t, c.Value("want")))
		}
	}
	for i, c := range referenceCases(t, "merge_events") {
		got := MergeEvents(c.Value("events"), c.Value("incoming"), secrets, statusSet(c.Value("statuses")))
		if compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("merge_events %d\n got %s\nwant %s", i, compact(t, got), compact(t, c.Value("want")))
		}
	}
	for i, c := range referenceCases(t, "append_event") {
		got := AppendEvent(c.Value("events"), c.GetString("status"), c.Value("message"), c.GetString("at"), secrets)
		if compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("append_event %d\n got %s\nwant %s", i, compact(t, got), compact(t, c.Value("want")))
		}
	}
}

func TestReferenceDNSReportFields(t *testing.T) {
	for i, c := range referenceCases(t, "dns_report_fields") {
		got := DNSReportFields(c.GetObject("payload"), []any{"abc"})
		if compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("dns_report_fields %d\n got %s\nwant %s", i, compact(t, got), compact(t, c.Value("want")))
		}
	}
}

func TestReferenceCompareVersions(t *testing.T) {
	for i, c := range referenceCases(t, "compare_versions") {
		a, b := c.GetString("a"), c.GetString("b")
		if got, want := CompareVersions(a, b), nativeVersionReferenceWant(t, i, c); got != want {
			t.Errorf("compare_versions(%.40q, %q) = %d, want %d", a, b, got, want)
		}
	}
}

func TestReferenceMessages(t *testing.T) {
	for _, c := range referenceCases(t, "deployment_message") {
		if got := DeploymentMessage(c.Value("value")); !jsonx.Equal(got, c.Value("want")) {
			t.Errorf("deployment_message(%q) = %q, want %q", c.Value("value"), got, c.Value("want"))
		}
	}
	for _, c := range referenceCases(t, "upgrade_error_message") {
		if got := UpgradeErrorMessage(c.Value("value")); got != c.Value("want") {
			t.Errorf("upgrade_error_message(%q) = %q, want %q", c.Value("value"), got, c.Value("want"))
		}
	}
	for i, c := range referenceCases(t, "public_deployment_messages") {
		if got := PublicDeploymentMessages(c.GetObject("value")); compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("public_deployment_messages %d\n got %s\nwant %s", i, compact(t, got), compact(t, c.Value("want")))
		}
	}
	for i, c := range referenceCases(t, "public_upgrade_state") {
		if got := PublicUpgradeState(c.GetObject("value")); compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("public_upgrade_state %d\n got %s\nwant %s", i, compact(t, got), compact(t, c.Value("want")))
		}
	}
}

func pyFloatRepr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	return jsonx.Float(f).String()
}

// withZone runs fn with the local zone of the Python-time helpers set to
// name (Python read TZ at start-up).
func withZone(t *testing.T, name string, fn func()) {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no zone %s: %v", name, err)
	}
	previous := localZone
	localZone = location
	defer func() { localZone = previous }()
	fn()
}

func TestReferenceTimestampsAndLocalTimes(t *testing.T) {
	zones := loadReference(t).GetObject("time")
	for zone, value := range zones.All() {
		cases := value.(*jsonx.Object)
		withZone(t, zone, func() {
			for _, item := range cases.GetList("timestamp") {
				c := item.(*jsonx.Object)
				moment, naive, ok := fromISO(c.GetString("value"))
				got := "ValueError"
				if ok {
					got = pyFloatRepr(pyTimestampOf(moment, naive))
				}
				if want := c.GetString("want"); got != want {
					t.Errorf("%s: datetime.fromisoformat(%q).timestamp() = %s, want %s", zone, c.GetString("value"), got, want)
				}
			}
			for _, item := range cases.GetList("fromtimestamp") {
				c := item.(*jsonx.Object)
				ns, _ := jsonx.AsInt(c.Value("ns"))
				ts := pyTimeFromNanos(ns)
				if got := pyFloatRepr(ts); got != c.GetString("float") {
					t.Errorf("time.time() for %d ns = %s, want %s", ns, got, c.GetString("float"))
				}
				if got := isoformat(fromTimestamp(ts), false); got != c.GetString("want") {
					t.Errorf("%s: datetime.fromtimestamp(%s).isoformat() = %s, want %s", zone, c.GetString("float"), got, c.GetString("want"))
				}
			}
		})
	}
}

func TestReferenceLegacyRows(t *testing.T) {
	for _, c := range referenceCases(t, "legacy_rows") {
		path := filepath.Join(t.TempDir(), LegacyDatabase)
		if raw, ok := c.Value("raw").(string); ok {
			if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range c.GetList("sql") {
				if _, err := db.Exec(statement.(string)); err != nil {
					t.Fatalf("%s: %s: %v", c.GetString("name"), statement, err)
				}
			}
			db.Close()
		}
		rows, err := readLegacyRows(path)
		var got []any
		for _, row := range rows {
			got = append(got, []any{row.agentID, pyFloatRepr(row.heartbeatAt), row.lastHeartbeat, row.fields})
		}
		if got == nil {
			got = []any{}
		}
		logs := []any{}
		if err != nil {
			logs = append(logs, "旧版 Agent 心跳状态库无法读取，已忽略："+err.Error())
		}
		if compact(t, got) != compact(t, c.Value("rows")) || compact(t, logs) != compact(t, c.Value("logs")) {
			t.Errorf("%s:\n got %s %s\nwant %s %s", c.GetString("name"), compact(t, got), compact(t, logs),
				compact(t, c.Value("rows")), compact(t, c.Value("logs")))
		}
	}
}

func TestReferenceResponses(t *testing.T) {
	for i, c := range referenceCases(t, "responses") {
		var body []byte
		for _, b := range c.GetList("body") {
			body = append(body, byte(intOf(b)))
		}
		header := http.Header{}
		if contentType, ok := c.Value("content_type").(string); ok {
			header.Set("Content-Type", contentType)
		}
		response := &Response{StatusCode: intOf(c.Value("status")), Header: header, Body: body}
		if got := response.Text(); got != c.GetString("text") {
			t.Errorf("case %d (%v) text = %q, want %q", i, c.Value("content_type"), got, c.GetString("text"))
		}
		value, err := response.JSON()
		jsonWant := c.GetObject("json")
		if want, ok := jsonWant.Get("ok"); ok != (err == nil) || (ok && compact(t, value) != compact(t, want)) {
			t.Errorf("case %d (%v) json = %v, %v; want %s", i, c.Value("content_type"), value, err, compact(t, jsonWant))
		}
		if got := ResponseFailure(response, "fallback", []any{"abc"}); compact(t, got) != compact(t, c.Value("want")) {
			t.Errorf("case %d (%v) response_failure\n got %s\nwant %s", i, c.Value("content_type"), compact(t, got), compact(t, c.Value("want")))
		}
	}
}

func TestReferenceLegacyFileRemovalLogsPythonOSErrors(t *testing.T) {
	dir := t.TempDir()
	wal := filepath.Join(dir, "runtime.sqlite3-wal")
	if err := os.MkdirAll(filepath.Join(wal, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	shm := filepath.Join(dir, "runtime.sqlite3-shm")
	if err := os.Mkdir(shm, 0o755); err != nil {
		t.Fatal(err)
	}
	logs := captureMessages(t, "backend.agents.runtime_state")
	removeFiles([]string{filepath.Join(dir, "runtime.sqlite3"), wal, shm})
	var want []string
	for _, item := range loadReference(t).GetList("remove_logs") {
		want = append(want, strings.ReplaceAll(item.(string), "<DIR>", dir))
	}
	if got := logs(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("logs\n got %q\nwant %q", got, want)
	}
	// An empty directory is not removed (Path.unlink never calls rmdir).
	if _, err := os.Stat(shm); err != nil {
		t.Fatal("empty directory removed")
	}
}

// captureMessages records the messages logged by one logger.
func captureMessages(t *testing.T, name string) func() []string {
	dir := t.TempDir()
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: filepath.Join(dir, "app.log")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return func() []string {
		data, _ := os.ReadFile(filepath.Join(dir, "app.log"))
		var messages []string
		for _, line := range strings.Split(string(data), "\n") {
			if parts := strings.SplitN(line, " - "+name+" - ", 2); len(parts) == 2 {
				messages = append(messages, strings.SplitN(parts[1], " - ", 2)[1])
			}
		}
		return messages
	}
}

func TestReferencePathResolveLikePathlib(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	reference := loadReference(t).GetObject("resolve")
	for _, item := range reference.GetList("links") {
		pair := item.([]any)
		target := strings.ReplaceAll(pair[1].(string), "<DIR>", dir)
		if err := os.Symlink(target, filepath.Join(dir, pair[0].(string))); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range reference.GetList("cases") {
		c := item.(*jsonx.Object)
		want := strings.ReplaceAll(c.GetString("want"), "<DIR>", dir)
		if got := pyResolve(dir + "/" + c.GetString("query")); got != want {
			t.Errorf("Path(%q).resolve() = %q, want %q", c.GetString("query"), got, want)
		}
	}
	// Managers of the same directory share one state even when the path only
	// reaches it through a symlink and the rest does not exist yet.
	if RuntimeFor(filepath.Join(dir, "link", "missing")) != RuntimeFor(filepath.Join(dir, "real", "missing")) {
		t.Fatal("state not shared through a symlink")
	}
}

func TestADanglingRuntimeSqliteSymlinkIsLeftAlone(t *testing.T) {
	ResetRuntime()
	t.Cleanup(ResetRuntime)
	dir := t.TempDir()
	link := filepath.Join(dir, LegacyDatabase)
	if err := os.Symlink(filepath.Join(dir, "gone"), link); err != nil {
		t.Fatal(err)
	}
	logs := captureMessages(t, "backend.agents.runtime_state")
	newManager(t, openRepo(t, dir))
	// Path.exists() follows the link: nothing exists, nothing is removed or logged.
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("symlink removed")
	}
	if got := logs(); len(got) != 0 {
		t.Fatalf("logs %q", got)
	}
}
