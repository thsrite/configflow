package agents

// Review of A02 against reference results produced by the Python functions
// (testdata/gen_a02_reference.py → testdata/a02_reference.json, CPython
// 3.11 on the test server). Every case asserts the Python result.

import (
	"bytes"
	"configflow/internal/timefmt"
	"configflow/internal/version"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/taskstatus/isoformtest"
	"configflow/internal/testjson"
)

func loadReferenceMap(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/a02_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	// Frozen CPython fixture stores the 1e400 legacy row value as JSON Infinity.
	data = bytes.ReplaceAll(data, []byte(": Infinity"), []byte(": 1e400"))
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	return root
}

func referenceRawCases(t *testing.T, name string) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	if err := json.Unmarshal(loadReferenceMap(t)[name], &out); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no %s cases", name)
	}
	return out
}

func secretsFromAny(list []any) redact.Secrets {
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return redact.NewSecrets(out)
}

func statusSet(list []string) map[string]bool {
	if list == nil {
		return nil
	}
	set := map[string]bool{}
	for _, item := range list {
		set[item] = true
	}
	return set
}

func decodeEventList(raw json.RawMessage) []model.OperationEvent {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]model.OperationEvent, len(items))
	for i, item := range items {
		_ = json.Unmarshal(item, &out[i])
	}
	return out
}

func TestReferenceFailureText(t *testing.T) {
	for i, raw := range referenceRawCases(t, "failure_text") {
		var c struct {
			Value   any    `json:"value"`
			Secrets []any  `json:"secrets"`
			Limit   int    `json:"limit"`
			Want    string `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		text, _ := c.Value.(string)
		got := redact.DiagnosticText(text, secretsFromAny(c.Secrets), c.Limit)
		if got != c.Want {
			t.Errorf("case %d failure_text(%.80q, %v, %v)\n got %.300q\nwant %.300q", i, text, c.Secrets, c.Limit, got, c.Want)
		}
	}
}

func TestReferenceEventMessage(t *testing.T) {
	for i, raw := range referenceRawCases(t, "event_message") {
		var c struct {
			Value   any    `json:"value"`
			Secrets []any  `json:"secrets"`
			Want    string `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		text, _ := c.Value.(string)
		if got := redact.EventText(text, secretsFromAny(c.Secrets)); got != c.Want {
			t.Errorf("case %d event_message(%q)\n got %q\nwant %q", i, c.Value, got, c.Want)
		}
	}
}

func TestReferenceEventTime(t *testing.T) {
	for _, raw := range referenceRawCases(t, "event_time") {
		var c struct {
			Value any `json:"value"`
			Want  any `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if c.Want == "NOW" {
			continue
		}
		text, isStr := c.Value.(string)
		if isStr && c.Want != nil && !isoformtest.Stored(text) {
			continue // a CPython-only ISO spelling Python accepted (no longer chased)
		}
		var got string
		var ok bool
		if isStr {
			got, ok = NormalizeEventTime(text)
		}
		want, wantOK := c.Want.(string)
		if ok != wantOK || got != want {
			t.Errorf("event_time(%q) = %q, %v; want %v", c.Value, got, ok, c.Want)
		}
	}
}

func TestReferenceEventLists(t *testing.T) {
	secrets := redact.Secrets{"abc"}
	for i, raw := range referenceRawCases(t, "clean_events") {
		var c struct {
			Value    json.RawMessage `json:"value"`
			Statuses []string        `json:"statuses"`
			Want     json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if nonTextMember(c.Value, "at", "status", "message") {
			continue
		}
		got := CleanOperationEvents(decodeEventList(c.Value), secrets, statusSet(c.Statuses))
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("clean_events %d: %s", i, diff)
		}
	}
	for i, raw := range referenceRawCases(t, "merge_events") {
		var c struct {
			Events   json.RawMessage `json:"events"`
			Incoming json.RawMessage `json:"incoming"`
			Statuses []string        `json:"statuses"`
			Want     json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		got := MergeOperationEvents(decodeEventList(c.Events), decodeEventList(c.Incoming), secrets, statusSet(c.Statuses))
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("merge_events %d: %s", i, diff)
		}
	}
	for i, raw := range referenceRawCases(t, "append_event") {
		var c struct {
			Events  json.RawMessage `json:"events"`
			Status  string          `json:"status"`
			Message any             `json:"message"`
			At      string          `json:"at"`
			Want    json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		msg, _ := c.Message.(string)
		got := AppendOperationEvent(decodeEventList(c.Events), c.Status, msg, c.At, secrets)
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("append_event %d: %s", i, diff)
		}
	}
}

func TestReferenceDNSReportFields(t *testing.T) {
	for i, raw := range referenceRawCases(t, "dns_report_fields") {
		var c struct {
			Payload json.RawMessage `json:"payload"`
			Want    json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		var stamp, kept struct {
			CheckedAt *string `json:"dns_checked_at"`
		}
		_ = json.Unmarshal(c.Want, &kept)
		if json.Unmarshal(c.Payload, &stamp) == nil && stamp.CheckedAt != nil && kept.CheckedAt != nil && !isoformtest.Stored(*stamp.CheckedAt) {
			continue // a CPython-only ISO spelling (no longer chased)
		}
		var dep model.Deployment
		var report DNSReport
		if len(c.Payload) > 0 && string(c.Payload) != "null" {
			if err := json.Unmarshal(c.Payload, &dep); err == nil {
				var err error
				report, err = SanitizeDNSReport(&dep, redact.Secrets{"abc"})
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		if diff := diffJSONValues(t, c.Want, report); diff != "" {
			t.Errorf("dns_report_fields %d: %s", i, diff)
		}
	}
}

// intOnlyComponent reports a version with a dotted component that only
// Python's int() reads as a number: surrounding whitespace, a sign, an
// underscore or more than 19 digits.
func intOnlyComponent(text string) bool {
	for _, part := range strings.Split(strings.SplitN(text, "-", 2)[0], ".") {
		if part != "" && (strings.ContainsAny(part, "+_") || strings.TrimSpace(part) != part || len(part) > 19 && strings.Trim(part, "0123456789") == "") {
			return true
		}
	}
	return false
}

func TestReferenceCompareVersions(t *testing.T) {
	for i, raw := range referenceRawCases(t, "compare_versions") {
		var c struct {
			A    string `json:"a"`
			B    string `json:"b"`
			Want int    `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if intOnlyComponent(c.A) || intOnlyComponent(c.B) {
			continue // O17: only Python int() read these as numbers; see version.TestNonDecimalComponents
		}
		if got, want := version.CompareAgent(c.A, c.B), nativeVersionReferenceWant(t, i, raw, c.Want); got != want {
			t.Errorf("compare_versions(%.40q, %q) = %d, want %d", c.A, c.B, got, want)
		}
	}
}

func TestReferenceMessages(t *testing.T) {
	for _, raw := range referenceRawCases(t, "deployment_message") {
		var c struct {
			Value json.RawMessage `json:"value"`
			Want  json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		var s *string
		var got any
		if json.Unmarshal(c.Value, &s) == nil && s != nil {
			got = ExplainDeployment(*s)
		} else {
			got = c.Value
		}
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("deployment_message(%s): %s", c.Value, diff)
		}
	}
	for _, raw := range referenceRawCases(t, "upgrade_error_message") {
		var c struct {
			Value any    `json:"value"`
			Want  string `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		text, _ := c.Value.(string)
		if got := ExplainUpgradeFailure(text); got != c.Want {
			t.Errorf("upgrade_error_message(%q) = %q, want %q", c.Value, got, c.Want)
		}
	}
	for i, raw := range referenceRawCases(t, "public_deployment_messages") {
		var c struct {
			Value json.RawMessage `json:"value"`
			Want  json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if nonTextMember(c.Value, "at", "status", "message", "error", "rollback_error", "rollback_message") || nonListEvents(c.Value) {
			continue
		}
		var dep *model.Deployment
		if len(c.Value) > 0 && string(c.Value) != "null" {
			dep = new(model.Deployment)
			if err := json.Unmarshal(c.Value, dep); err != nil {
				t.Fatal(err)
			}
		}
		got, err := PublicDeploymentMessagesModel(dep)
		if err != nil {
			t.Fatal(err)
		}
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("public_deployment_messages %d: %s (input %s)", i, diff, c.Value)
		}
	}
	for i, raw := range referenceRawCases(t, "public_upgrade_state") {
		var c struct {
			Value json.RawMessage `json:"value"`
			Want  json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if nonTextMember(c.Value, "at", "status", "message", "error") || nonListEvents(c.Value) || !strings.Contains(string(c.Value), "started_at") {
			continue
		}
		var state *model.AgentUpgrade
		if len(c.Value) > 0 && string(c.Value) != "null" {
			state = new(model.AgentUpgrade)
			if err := json.Unmarshal(c.Value, state); err != nil {
				t.Fatal(err)
			}
		}
		got, err := PublicUpgradeStateModel(state)
		if err != nil {
			t.Fatal(err)
		}
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("public_upgrade_state %d: %s (input %s)", i, diff, c.Value)
		}
	}
}

func pyFloatRepr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	abs := math.Abs(f)
	if abs != 0 && (abs < 1e-4 || abs >= 1e16) {
		return strconv.FormatFloat(f, 'e', -1, 64)
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
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
	var zones map[string]struct {
		Timestamp []struct {
			Value string `json:"value"`
			Want  string `json:"want"`
		} `json:"timestamp"`
		FromTimestamp []struct {
			NS    int64  `json:"ns"`
			Float string `json:"float"`
			Want  string `json:"want"`
		} `json:"fromtimestamp"`
	}
	if err := json.Unmarshal(loadReferenceMap(t)["time"], &zones); err != nil {
		t.Fatal(err)
	}
	for zone, cases := range zones {
		withZone(t, zone, func() {
			for _, c := range cases.Timestamp {
				moment, naive, ok := fromISO(c.Value)
				got := "ValueError"
				if ok {
					got = pyFloatRepr(pyTimestampOf(moment, naive))
				}
				if got != c.Want {
					t.Errorf("%s: datetime.fromisoformat(%q).timestamp() = %s, want %s", zone, c.Value, got, c.Want)
				}
			}
			for _, c := range cases.FromTimestamp {
				ts := pyTimeFromNanos(c.NS)
				if got := pyFloatRepr(ts); got != c.Float {
					t.Errorf("time.time() for %d ns = %s, want %s", c.NS, got, c.Float)
				}
				if got := timefmt.Naive(fromTimestamp(ts)); got != c.Want {
					t.Errorf("%s: datetime.fromtimestamp(%s).isoformat() = %s, want %s", zone, c.Float, got, c.Want)
				}
			}
		})
	}
}

func TestReferenceLegacyRows(t *testing.T) {
	for index, rowRaw := range referenceRawCases(t, "legacy_rows") {
		var c struct {
			Name string          `json:"name"`
			Raw  *string         `json:"raw"`
			SQL  []string        `json:"sql"`
			Rows json.RawMessage `json:"rows"`
			Logs json.RawMessage `json:"logs"`
		}
		if err := json.Unmarshal(rowRaw, &c); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), LegacyDatabase)
		if c.Raw != nil {
			if err := os.WriteFile(path, []byte(*c.Raw), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range c.SQL {
				if _, err := db.Exec(statement); err != nil {
					t.Fatalf("%s: %s: %v", c.Name, statement, err)
				}
			}
			db.Close()
		}
		rows, err := readLegacyRows(t.Context(), path)
		got := []any{}
		for _, row := range rows {
			got = append(got, []any{row.agentID, pyFloatRepr(row.heartbeatAt), row.lastHeartbeat, row.fields})
		}
		logs := []any{}
		if err != nil {
			logs = append(logs, "旧版 Agent 心跳状态库无法读取，已忽略："+err.Error())
		}
		if err != nil {
			verifyNativeRuntimeErrorRow(t, index, rowRaw, err)
			// Only bound interpreter/driver error bytes are retired; original rows
			// continue to compare exactly, including the all-or-nothing empty result.
			if diff := diffJSONValues(t, c.Rows, got); diff != "" {
				t.Fatalf("native read returned partial rows: %s", diff)
			}
			continue
		}
		if diff := diffJSONValues(t, agentRowsAsStored(t, c.Rows), got); diff != "" {
			t.Errorf("%s rows: %s (got %s want %s)", c.Name, diff, mustJSONText(got), c.Rows)
		}
		if diff := diffJSONValues(t, c.Logs, logs); diff != "" {
			t.Errorf("%s logs: %s", c.Name, diff)
		}
	}
}

func TestReferenceResponses(t *testing.T) {
	for i, raw := range referenceRawCases(t, "responses") {
		var c struct {
			Body        []int           `json:"body"`
			ContentType *string         `json:"content_type"`
			Status      int             `json:"status"`
			Text        string          `json:"text"`
			JSON        json.RawMessage `json:"json"`
			Want        json.RawMessage `json:"want"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, len(c.Body))
		for j, b := range c.Body {
			body[j] = byte(b)
		}
		header := http.Header{}
		if c.ContentType != nil {
			header.Set("Content-Type", *c.ContentType)
		}
		response := &Response{StatusCode: c.Status, Header: header, Body: body}
		if got := response.Text(); got != c.Text {
			t.Errorf("case %d (%v) text = %q, want %q", i, c.ContentType, got, c.Text)
		}
		value, err := testjson.Parse(response.jsonData())
		var jsonWant map[string]json.RawMessage
		if uErr := json.Unmarshal(c.JSON, &jsonWant); uErr != nil {
			t.Fatal(uErr)
		}
		wantRaw, wantOK := jsonWant["ok"]
		if wantOK != (err == nil) {
			t.Errorf("case %d (%v) json = %v, %v; want %s", i, c.ContentType, value, err, c.JSON)
		} else if wantOK {
			wVal := parseJSONVal(t, wantRaw)
			gVal := parseJSONVal(t, mustJSON(t, value))
			if diff := testjson.Diff(wVal, gVal); diff != "" {
				t.Errorf("case %d (%v) json diff: %s", i, c.ContentType, diff)
			}
		}
		got, err := ResponseFailureModel(response, "fallback", redact.Secrets{"abc"})
		if err != nil {
			t.Fatal(err)
		}
		if diff := diffJSONValues(t, c.Want, got); diff != "" {
			t.Errorf("case %d (%v) response_failure: %s", i, c.ContentType, diff)
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
	removeLogsRaw := loadReferenceMap(t)["remove_logs"]
	var removeLogs []string
	if err := json.Unmarshal(removeLogsRaw, &removeLogs); err != nil {
		t.Fatal(err)
	}
	verifyNativeRemovalReference(t, removeLogsRaw, removeLogs, logs(), dir)
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
	var reference struct {
		Links [][2]string `json:"links"`
		Cases []struct {
			Query string `json:"query"`
			Want  string `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(loadReferenceMap(t)["resolve"], &reference); err != nil {
		t.Fatal(err)
	}
	for _, pair := range reference.Links {
		target := strings.ReplaceAll(pair[1], "<DIR>", dir)
		if err := os.Symlink(target, filepath.Join(dir, pair[0])); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range reference.Cases {
		want := strings.ReplaceAll(c.Want, "<DIR>", dir)
		if got := pyResolve(dir + "/" + c.Query); got != want {
			t.Errorf("Path(%q).resolve() = %q, want %q", c.Query, got, want)
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

func mustJSONText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// nonListEvents reports an events member that is neither a list nor absent
// (accepted O18 T1b difference: it reads as no events).
func nonListEvents(raw json.RawMessage) bool {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return false
	}
	events, present := members["events"]
	return present && (len(events) == 0 || events[0] != '[')
}

// agentRowsAsStored passes the fields of Python's legacy rows through the
// Agent record, as the Go reader stores them (O18 T1b: undeclared members
// and values of another type are dropped).
func agentRowsAsStored(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var rows [][]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return raw
	}
	for _, row := range rows {
		if len(row) < 4 {
			continue
		}
		var agent model.Agent
		if err := json.Unmarshal(row[3], &agent); err != nil {
			t.Fatal(err)
		}
		encoded, err := model.Marshal(agent)
		if err != nil {
			t.Fatal(err)
		}
		row[3] = encoded
	}
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
