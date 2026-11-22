package metrics

// Port of backend/test_agent_metrics_history.py (memory window, append-only
// saves, compaction, migration, deletion, the 定时任务 row and the flush on
// graceful shutdown).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/testjson"
)

var start = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type testClock struct{ current time.Time }

func (c *testClock) advance(d time.Duration) { c.current = c.current.Add(d) }

// useClock is the clock fixture: datetime.now() of metrics_history.
func useClock(t *testing.T) *testClock {
	t.Helper()
	c := &testClock{current: start}
	previous := now
	now = func() time.Time { return c.current }
	t.Cleanup(func() { now = previous })
	return c
}

func sample(i int) map[string]any {
	return map[string]any{
		"cpu":          map[string]any{"usage_percent": i},
		"memory":       map[string]any{"used_percent": 40 + i},
		"disk":         map[string]any{"used_percent": 50},
		"network":      map[string]any{"bytes_sent": 1000 * i, "bytes_recv": 2000 * i, "speed_sent": i, "speed_recv": 2 * i},
		"collected_at": "ignored",
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func add(t *testing.T, h *History, c *testClock, count int, agent string, step time.Duration) {
	t.Helper()
	for i := 0; i < count; i++ {
		c.advance(step)
		if ok, err := h.AddSample(context.Background(), agent, mustMarshal(t, sample(i))); !ok || err != nil {
			t.Fatalf("add_metrics %v %v", ok, err)
		}
	}
}

func get(t *testing.T, h *History, agent string, hours any) []json.RawMessage {
	t.Helper()
	var points []json.RawMessage
	var err error
	switch v := hours.(type) {
	case nil:
		points, err = h.Samples(agent, nil)
	case int:
		points, err = h.Samples(agent, bigOf(int64(v)))
	}
	if err != nil {
		t.Fatal(err)
	}
	return points
}

func cpus(points []json.RawMessage) []string {
	var out []string
	for _, p := range points {
		v, _ := testjson.Path(p, "cpu", "usage_percent")
		out = append(out, fmt.Sprint(v))
	}
	return out
}

func jsonObjectKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("expected object: %s (%v)", raw, err)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return splitLinesKeepEnds(string(data))
}

// splitLinesKeepEnds is str.splitlines() for the files written here.
func splitLinesKeepEnds(text string) []string {
	var out []string
	for _, line := range splitUniversal(text) {
		out = append(out, strings.TrimSuffix(line, "\n"))
	}
	return out
}

func parsedLines(t *testing.T, path string) []any {
	t.Helper()
	var out []any
	for _, line := range readLines(t, path) {
		value, err := testjson.Parse([]byte(line))
		if err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, value)
	}
	return out
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

func wantEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if diff := testjson.Diff(want, got); diff != "" {
		t.Fatalf("%s: %s", label, diff)
	}
}

func wantFlush(t *testing.T, h *History, agents, appended, compacted int) {
	t.Helper()
	got, err := h.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if want := (FlushResult{agents, appended, compacted}); got != want {
		t.Fatalf("flush %+v, want %+v", got, want)
	}
}

func TestMemoryKeeps24HoursAndCapsPointsPerAgent(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := NewHistory(dir, 5)
	add(t, h, c, 8, "a1", time.Second)
	kept := get(t, h, "a1", nil)
	if got := strings.Join(cpus(kept), ","); got != "3,4,5,6,7" {
		t.Fatal(got)
	}
	if h.PendingCount() != 5 {
		t.Fatal(h.PendingCount())
	}
	if got := strings.Join(jsonObjectKeys(t, kept[0]), ","); got != "timestamp,cpu,memory,disk,network" {
		t.Fatal(got)
	}
	if got := names(t, dir); len(got) != 0 { // heartbeats never touch the disk
		t.Fatal(got)
	}

	h = NewHistory(filepath.Join(dir, "window"), MaxPointsPerAgent)
	add(t, h, c, 3, "a1", 10*time.Hour)
	if len(get(t, h, "a1", 24)) != 3 || len(get(t, h, "a1", 1)) != 1 {
		t.Fatal("window")
	}
	c.advance(15 * time.Hour)
	if got := strings.Join(cpus(get(t, h, "a1", nil)), ","); got != "2" {
		t.Fatal(got)
	}
	if h.PendingCount() != 1 {
		t.Fatal(h.PendingCount())
	}
}

func TestFlushAppendsOnlyNewPoints(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := NewHistory(dir, MaxPointsPerAgent)
	add(t, h, c, 3, "a1", 30*time.Second)
	wantFlush(t, h, 1, 3, 0)
	path := filepath.Join(dir, "a1_metrics.jsonl")
	first, _ := os.ReadFile(path)
	wantEqual(t, "file", parsedLines(t, path), get(t, h, "a1", nil))

	wantFlush(t, h, 0, 0, 0)
	if again, _ := os.ReadFile(path); string(again) != string(first) {
		t.Fatal("rewritten without new points")
	}
	add(t, h, c, 2, "a1", 30*time.Second)
	if _, err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), string(first)) {
		t.Fatal("not appended")
	}
	if len(readLines(t, path)) != 5 {
		t.Fatal(readLines(t, path))
	}
	wantEqual(t, "file", parsedLines(t, path), get(t, h, "a1", nil))
}

func TestCompactionIsSizeTriggeredAndAtomic(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := NewHistory(dir, MaxPointsPerAgent)
	add(t, h, c, 4, "a1", 6*time.Hour)
	if _, err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a1_metrics.jsonl")
	// Two points expire: 5 lines on disk for 3 retained points.
	c.advance(13 * time.Hour)
	add(t, h, c, 1, "a1", 30*time.Second)
	if len(get(t, h, "a1", nil)) != 3 {
		t.Fatal("retained")
	}
	original, _ := os.ReadFile(path)

	previous := replaceFile
	t.Cleanup(func() { replaceFile = previous })
	replaceFile = func(string, string) error {
		return &os.LinkError{Op: "rename", Err: syscall.ENOSPC}
	}
	_, err := h.Flush()
	var flushError *FlushError
	if !errors.As(err, &flushError) || !strings.Contains(err.Error(), "磁盘空间不足") {
		t.Fatalf("%v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(original) {
		t.Fatal("file changed")
	}
	if got := names(t, dir); strings.Join(got, ",") != "a1_metrics.jsonl" {
		t.Fatal(got)
	}

	var replaced []string
	replaceFile = func(source, target string) error {
		replaced = append(replaced, target)
		return os.Rename(source, target)
	}
	wantFlush(t, h, 1, 1, 1)
	if strings.Join(replaced, ",") != path {
		t.Fatal(replaced)
	}
	wantEqual(t, "file", parsedLines(t, path), get(t, h, "a1", nil))
	if got := names(t, dir); strings.Join(got, ",") != "a1_metrics.jsonl" {
		t.Fatal(got)
	}
	// Below the threshold again: the next save appends.
	add(t, h, c, 1, "a1", 30*time.Second)
	if result, _ := h.Flush(); result.Compacted != 0 {
		t.Fatal(result)
	}
	if len(readLines(t, path)) != 4 {
		t.Fatal(readLines(t, path))
	}
}

func TestFailedAppendKeepsPointsAndRewritesNextTime(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := NewHistory(dir, MaxPointsPerAgent)
	add(t, h, c, 2, "a1", 30*time.Second)
	if _, err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	add(t, h, c, 2, "a1", 30*time.Second)
	previous := appendLines
	t.Cleanup(func() { appendLines = previous })
	appendLines = func(ctx context.Context, path string, lines []string) error {
		handle, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		handle.WriteString(lines[0][:10])
		handle.Close()
		return &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
	}
	if _, err := h.Flush(); err == nil || !strings.Contains(err.Error(), "没有写入权限") {
		t.Fatal(err)
	}
	if h.PendingCount() != 2 {
		t.Fatal(h.PendingCount())
	}
	appendLines = previous
	if result, _ := h.Flush(); result.Compacted != 1 {
		t.Fatal(result)
	}
	wantEqual(t, "file", parsedLines(t, filepath.Join(dir, "a1_metrics.jsonl")), get(t, h, "a1", nil))
	if len(get(t, h, "a1", nil)) != 4 {
		t.Fatal("points")
	}
}

func pointAt(at time.Time, cpu any) map[string]any {
	return map[string]any{
		"timestamp": isoformat(at),
		"cpu":       map[string]any{"usage_percent": cpu},
		"memory":    map[string]any{},
		"disk":      map[string]any{},
		"network":   map[string]any{},
	}
}

func dumps(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadingSkipsATruncatedLastLineAndOldPoints(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a1_metrics.jsonl")
	old := pointAt(start.Add(-30*time.Hour), nil)
	old["cpu"] = map[string]any{}
	var good []any
	lines := []string{dumps(t, old)}
	for m := 6; m >= 1; m-- {
		p := pointAt(start.Add(-time.Duration(m)*time.Minute), m)
		good = append(good, p)
		lines = append(lines, dumps(t, p))
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n{\"timestamp\": \"2026-10-2"), 0o644)
	h := NewHistory(dir, MaxPointsPerAgent)
	if result, err := h.Load([]string{"a1"}); err != nil || result.Loaded != 1 {
		t.Fatal(result, err)
	}
	wantEqual(t, "loaded", get(t, h, "a1", nil), good)
	add(t, h, c, 1, "a1", 30*time.Second)
	if _, err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	// The new line starts on its own line instead of joining the broken one.
	got := readLines(t, path)
	if got[len(got)-2] != `{"timestamp": "2026-10-2` {
		t.Fatal(got)
	}
	reloaded := NewHistory(dir, MaxPointsPerAgent)
	reloaded.Load([]string{"a1"})
	wantEqual(t, "reloaded", get(t, reloaded, "a1", nil), get(t, h, "a1", nil))
	if len(get(t, reloaded, "a1", nil)) != 7 {
		t.Fatal("reloaded count")
	}
}

func TestOldJSONFilesMigrateOnceAndDeletedAgentsAreRemoved(t *testing.T) {
	useClock(t)
	dir := t.TempDir()
	var recent []any
	for _, m := range []int{3, 2, 1} {
		recent = append(recent, map[string]any{
			"timestamp": isoformat(start.Add(-time.Duration(m) * time.Minute)),
			"cpu":       map[string]any{"usage_percent": 1.5, "cores": 4},
			"memory":    map[string]any{"used_percent": 33.3},
			"disk":      map[string]any{},
			"network":   map[string]any{"bytes_sent": 10, "speed_sent": 0.5},
		})
	}
	expired := map[string]any{
		"timestamp": isoformat(start.Add(-25 * time.Hour)),
		"cpu":       map[string]any{"usage_percent": 1.5, "cores": 4},
		"memory":    map[string]any{"used_percent": 33.3},
		"disk":      map[string]any{},
		"network":   map[string]any{"bytes_sent": 10, "speed_sent": 0.5},
	}
	for _, agent := range []string{"a1", "gone"} {
		text, _ := json.MarshalIndent(map[string]any{
			"agent_id":   agent,
			"updated_at": isoformat(start),
			"metrics":    append([]any{expired}, recent...),
		}, "", "  ")
		os.WriteFile(filepath.Join(dir, agent+"_metrics.json"), text, 0o644)
	}
	os.WriteFile(filepath.Join(dir, "gone_metrics.jsonl"), []byte(dumps(t, recent[0])+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".a1_interrupted.tmp"), []byte("partial"), 0o644)

	h := NewHistory(dir, MaxPointsPerAgent)
	if result, err := h.Load([]string{"a1", "never-reported"}); err != nil || result != (LoadResult{1, 1, 2}) {
		t.Fatal(result, err)
	}
	if got := names(t, dir); strings.Join(got, ",") != "a1_metrics.jsonl" {
		t.Fatal(got)
	}
	// Same points, same shape as the old file served before.
	wantEqual(t, "migrated", get(t, h, "a1", 24), recent)
	wantEqual(t, "file", parsedLines(t, filepath.Join(dir, "a1_metrics.jsonl")), recent)
	if h.PendingCount() != 0 {
		t.Fatal(h.PendingCount())
	}

	// A migration interrupted after the new file was written keeps the new file.
	text, _ := json.Marshal(map[string]any{"metrics": []any{expired}})
	os.WriteFile(filepath.Join(dir, "a1_metrics.json"), text, 0o644)
	again := NewHistory(dir, MaxPointsPerAgent)
	if result, err := again.Load([]string{"a1"}); err != nil || result != (LoadResult{1, 0, 0}) {
		t.Fatal(result, err)
	}
	wantEqual(t, "again", get(t, again, "a1", nil), recent)
	if got := names(t, dir); strings.Join(got, ",") != "a1_metrics.jsonl" {
		t.Fatal(got)
	}
}

func TestAPIShapesMatchThePointsKeptInMemory(t *testing.T) {
	c := useClock(t)
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	add(t, h, c, 12, "a1", 30*time.Second)
	points := get(t, h, "a1", 24)
	c.advance(time.Second)
	summary, err := h.Summarize("a1", bigOf(1))
	if err != nil {
		t.Fatal(err)
	}
	if summary.DataPoints != 12 {
		t.Fatal(summary)
	}
	wantEqual(t, "cpu", summary.CPU, map[string]any{"avg": 5.5, "max": 11, "min": 0})
	stats, err := h.TrafficStatistics("a1", "hours_24")
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(11000, stats.BytesSent) || !testjson.Equal(11000, stats.BytesSentDelta) || stats.DataPoints == nil || *stats.DataPoints != 12 {
		t.Fatal(stats)
	}
	firstTS, _ := testjson.Path(points[0], "timestamp")
	lastTS, _ := testjson.Path(points[len(points)-1], "timestamp")
	if stats.StartTime == nil || *stats.StartTime != firstTS || stats.EndTime == nil || *stats.EndTime != lastTS {
		t.Fatal(stats)
	}
	trend, err := h.TrafficTrend("a1", bigOf(24), bigOf(5))
	if err != nil {
		t.Fatal(err)
	}
	if trend[len(trend)-1].Timestamp != lastTS {
		t.Fatal(trend)
	}
	// Each history point is exactly what a JSON round trip of the heartbeat gives.
	want := map[string]any{"timestamp": firstTS}
	s0 := sample(0)
	for _, key := range []string{"cpu", "memory", "disk", "network"} {
		want[key] = s0[key]
	}
	wantEqual(t, "point", points[0], want)
}

func TestPointsAreKeptAsCompactLinesAndReadBackAsFreshDicts(t *testing.T) {
	// The tracemalloc half (memory per point) measures CPython objects and
	// has no Go equivalent; points are kept as their JSON lines here too.
	c := useClock(t)
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	for i := 0; i < 2000; i++ {
		c.advance(30 * time.Second)
		item := map[string]any{
			"cpu":     map[string]any{"usage_percent": 12.5, "core_count": 4},
			"memory":  map[string]any{"total": 8123456789, "used": 4123456789, "available": 4000000000, "used_percent": 50.7},
			"disk":    map[string]any{"total": 256123456789, "used": 100123456789, "free": 156000000000, "used_percent": 39.1},
			"network": map[string]any{"bytes_sent": i, "bytes_recv": 987654321098, "speed_sent": 12345, "speed_recv": 54321},
		}
		_, _ = h.AddSample(context.Background(), "a1", mustMarshal(t, item))
	}
	h.mu.Lock()
	line := h.points["a1"][0].line
	h.mu.Unlock()
	if !strings.HasPrefix(line, `{"timestamp":"`) || strings.Contains(line, ": ") {
		t.Fatal(line)
	}
	first := get(t, h, "a1", nil)[0]
	first[0] = 'X'
	if got, _ := testjson.Path(get(t, h, "a1", nil)[0], "cpu", "usage_percent"); fmt.Sprint(got) != "12.5" {
		t.Fatal(got)
	}
}

// AddSample rejects non-object payloads and defaults missing sections to {}.
func TestAddMetricsOfANonObjectFailsLikePython(t *testing.T) {
	h := NewHistory(t.TempDir(), MaxPointsPerAgent)
	for name, raw := range map[string]string{
		"list": `[]`, "str": `"x"`, "int": `1`, "float": `1.5`, "bool": `true`, "NoneType": `null`,
	} {
		ok, err := h.AddSample(context.Background(), "a1", json.RawMessage(raw))
		if ok || err == nil {
			t.Errorf("%s: %v %v", name, ok, err)
		}
	}
	// Missing sections default to {}; present ones are kept whatever they are.
	if ok, err := h.AddSample(context.Background(), "a1", json.RawMessage(`{"cpu":null,"memory":"x"}`)); !ok || err != nil {
		t.Fatal(ok, err)
	}
	got := get(t, h, "a1", nil)[0]
	if v, _ := testjson.Path(got, "memory"); v != "x" {
		t.Fatalf("memory=%v", v)
	}
	if v, ok := testjson.Path(got, "cpu"); !ok || v != nil {
		t.Fatalf("cpu=%v ok=%v", v, ok)
	}
	disk, _ := testjson.Path(got, "disk")
	wantEqual(t, "disk", disk, map[string]any{})
}

// DescribeOSError gives the Chinese reasons for disk problems and the
// capitalized system text otherwise.
func TestDescribeOSError(t *testing.T) {
	for err, want := range map[error]string{
		&os.PathError{Err: syscall.ENOSPC}: "磁盘空间不足", &os.PathError{Err: syscall.EDQUOT}: "超出磁盘配额",
		&os.PathError{Err: syscall.EACCES}: "没有写入权限", &os.PathError{Err: syscall.EPERM}: "没有写入权限",
		&os.PathError{Err: syscall.EROFS}: "文件系统只读", &os.PathError{Err: syscall.EISDIR}: "Is a directory",
		&os.PathError{Err: syscall.ENOENT}: "No such file or directory", &os.PathError{Err: syscall.EIO}: "Input/output error",
		&os.LinkError{Err: syscall.EXDEV}: "Invalid cross-device link", &os.PathError{Err: syscall.ESTALE}: "Stale file handle",
	} {
		if got := DescribeOSError(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}

func TestDeleteAgentHistoryReportsAFileThatCannotBeRemoved(t *testing.T) {
	c := useClock(t)
	dir := t.TempDir()
	h := NewHistory(dir, MaxPointsPerAgent)
	add(t, h, c, 1, "a1", time.Second)
	h.Flush()
	if !h.DeleteAgentHistory("a1") || len(names(t, dir)) != 0 {
		t.Fatal("delete")
	}
	// A directory in place of the file: unlink fails, nothing is removed.
	os.Mkdir(filepath.Join(dir, "a2_metrics.jsonl"), 0o755)
	if h.DeleteAgentHistory("a2") {
		t.Fatal("a directory cannot be removed")
	}
	if got := names(t, dir); strings.Join(got, ",") != "a2_metrics.jsonl" {
		t.Fatal(got)
	}
}

func TestOldFileWhoseMetricsAreNotIterableEndsTheLoad(t *testing.T) {
	useClock(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a1_metrics.json"), []byte(`{"metrics": 5}`), 0o644)
	_, err := NewHistory(dir, MaxPointsPerAgent).Load([]string{"a1"})
	if err == nil || err.Error() != msgSavedMetricsArray {
		t.Fatal(err)
	}
	// Broken JSON is only ignored (and the file removed).
	os.WriteFile(filepath.Join(dir, "a1_metrics.json"), []byte(`{"metrics": [`), 0o644)
	result, err := NewHistory(dir, MaxPointsPerAgent).Load([]string{"a1"})
	if err != nil || result != (LoadResult{}) || len(names(t, dir)) != 0 {
		t.Fatal(result, err, names(t, dir))
	}
}

var _ = context.Background
var _ = agents.ErrNotPorted
