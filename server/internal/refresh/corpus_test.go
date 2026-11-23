package refresh

// The reference corpus from the real Python modules
// (testdata/gen.py → testdata/corpus.json; regenerate on the test server:
// rt --back server/internal/refresh/testdata/corpus.json -- bash -lc
// 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/gen.py').

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/taskstatus/isoformtest"
	"configflow/internal/testjson"
	"configflow/internal/yamlx/yamlxtest"
)

var corpusNow = time.Date(2026, 11, 1, 0, 0, 0, 250000000, time.UTC)

func sanitizeNonFiniteJSON(data []byte) []byte {
	if !bytes.Contains(data, []byte("Infinity")) && !bytes.Contains(data, []byte("NaN")) {
		return data
	}
	out := make([]byte, 0, len(data)+64)
	inStr := false
	for i := 0; i < len(data); {
		c := data[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				out = append(out, data[i+1])
				i += 2
				continue
			}
			if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			i++
			continue
		}
		if bytes.HasPrefix(data[i:], []byte("-Infinity")) {
			out = append(out, `{"__nonfinite__":"-Infinity"}`...)
			i += len("-Infinity")
			continue
		}
		if bytes.HasPrefix(data[i:], []byte("Infinity")) {
			out = append(out, `{"__nonfinite__":"Infinity"}`...)
			i += len("Infinity")
			continue
		}
		if bytes.HasPrefix(data[i:], []byte("NaN")) {
			out = append(out, `{"__nonfinite__":"NaN"}`...)
			i += len("NaN")
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

func decodeAny(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(sanitizeNonFiniteJSON(data)))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeMap(data []byte) (map[string]any, error) {
	v, err := decodeAny(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object, got %T", v)
	}
	return m, nil
}

func decodeInto(value any, target any) error {
	if value == nil {
		return nil
	}
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return err
	}
	if bytes.Contains(raw, []byte(`"__nonfinite__"`)) {
		return model.ErrJSON
	}
	return jsondecode.Unmarshal(raw, target)
}

func mapStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func mapInt(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch n := m[key].(type) {
	case json.Number:
		v, _ := n.Int64()
		return v
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

func mapObj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	o, _ := m[key].(map[string]any)
	return o
}

func mapList(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	l, _ := m[key].([]any)
	return l
}

func pyTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i != 0
		}
		if f, err := x.Float64(); err == nil {
			return f != 0
		}
		return true
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func compactJSON(v any) string {
	b, err := jsonresp.Encode(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

func loadCorpus(t *testing.T) []map[string]any {
	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeAny(data)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, row := range value.([]any) {
		rows = append(rows, row.(map[string]any))
	}
	return rows
}

// outcome is the corpus form of a result: {"output": v} or {"error": class}.
func outcome(value any, err error) map[string]any {
	if err != nil {
		return map[string]any{"error": pythonClass(err)}
	}
	return map[string]any{"output": value}
}

func expected(row map[string]any) map[string]any {
	if errVal, ok := row["error"]; ok {
		return map[string]any{"error": errVal}
	}
	return map[string]any{"output": row["output"]}
}

// pyISO is datetime.isoformat() of an aware time.
func pyISO(t time.Time) string {
	text := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		text += fmt.Sprintf(".%06d", us)
	}
	_, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	text += fmt.Sprintf("%s%02d:%02d", sign, offset/3600, offset%3600/60)
	if offset%60 != 0 {
		text += fmt.Sprintf(":%02d", offset%60)
	}
	return text
}

func buildError(spec any) error {
	object, ok := spec.(map[string]any)
	if !ok {
		return nil
	}
	class, message := mapStr(object, "class"), mapStr(object, "message")
	switch class {
	case "ValueError", "KeyError", "TypeError", "AttributeError", "RuntimeError":
		return &PyError{Class: class, Message: message}
	case "StrategyReferenceError":
		return &rules.StrategyReferenceError{Message: message}
	case "DialerReferenceError":
		return &convert.DialerReferenceError{Message: message}
	case "RemoteRuleValidationError":
		return rulefetch.NewRemoteRuleMessage(message)
	case "StaleRuleCache":
		return &StaleRuleCache{Message: message}
	case "PermissionError":
		return &fs.PathError{Op: "open", Path: "x", Err: syscall.EACCES}
	case "ProfileValidationError":
		return repo.NewValidationError(message)
	case "RuleFetchError":
		return &rulefetch.RuleFetchError{LastError: &rulefetch.FetchError{Class: "ConnectionError"}}
	case "SubscriptionFetchError":
		return &substore.SubscriptionFetchError{
			SubStoreError: buildError(object["sub_store"]),
			DirectError:   buildError(object["direct"]),
		}
	}
	return &rulefetch.FetchError{Class: class, Message: message, Status: int(mapInt(object, "status"))}
}

func nextRunAt(entry any, interval any, baseline any, now *time.Time) (*time.Time, error) {
	receipt := &taskstatus.Receipt{}
	_ = decodeInto(entry, receipt)
	text, _ := baseline.(string)
	numText := ""
	switch n := interval.(type) {
	case json.Number:
		numText = n.String()
	case int:
		numText = strconv.Itoa(n)
	case int64:
		numText = strconv.FormatInt(n, 10)
	}
	seconds, err := strconv.ParseInt(numText, 10, 64)
	if err != nil {
		_, hasLast := parseStamp(textOr(receipt.LastRunAt, ""))
		if !hasLast {
			_, hasLast = parseStamp(text)
		}
		if !hasLast {
			return NextDue(receipt, 1, optionalText(text), now)
		}
		bigVal, ok := new(big.Int).SetString(numText, 10)
		if !ok || bigVal.Sign() < 0 || textOr(receipt.LastStatus, "") != "failed" {
			return nil, ErrScheduleTime
		}
		seconds = math.MaxInt64
	}
	return NextDue(receipt, model.Int(seconds), optionalText(text), now)
}

func parseIntervalAny(value any, label string) (int64, error) {
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return 0, err
	}
	return ParseInterval(raw, label)
}

func sourceFromMap(m map[string]any) model.Subscription {
	var sub model.Subscription
	_ = decodeInto(m, &sub)
	return sub
}

func ruleFromMap(m map[string]any) model.RuleLibraryItem {
	var rule model.RuleLibraryItem
	_ = decodeInto(m, &rule)
	return rule
}

// cpythonOnlyISO reports a next_run_at row whose receipt or cache time is
// spelled outside the stored timestamp grammar (week dates, basic format,
// space separators, sub-second offsets): Python's fromisoformat accepted
// those, ConfigFlow never writes them, and they are no longer chased.
func cpythonOnlyISO(args map[string]any) bool {
	texts := []any{args["baseline"]}
	if entry, ok := args["entry"].(map[string]any); ok {
		texts = append(texts, entry["last_run_at"])
	}
	for _, value := range texts {
		if text, ok := value.(string); ok && text != "" && text[0] >= '0' && text[0] <= '9' && !isoformtest.Stored(text) {
			return true
		}
	}
	return false
}

func TestReferenceCorpus(t *testing.T) {
	// Sequential: all rows share a frozen clock through the process-wide Env.
	clock := &fakeClock{now: corpusNow}
	env := DefaultRefreshEnv()
	env.Clock = clock
	t.Cleanup(SetRefreshEnv(env))
	given := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	counts := map[string]int{}
	refs := loadNativeReferences(t)
	for i, row := range loadCorpus(t) {
		fn := mapStr(row, "fn")
		input := row["input"]
		var got map[string]any
		switch fn {
		case "validate_refresh_interval":
			v, err := parseIntervalAny(input, "")
			got = outcome(v, err)
		case "validate_refresh_interval_label":
			_, err := parseIntervalAny(input, "更新间隔")
			got = outcome(nil, err)
		case "subscription_interval":
			got = outcome(int64(SourceInterval(sourceFromMap(input.(map[string]any)))), nil)
		case "rule_interval":
			got = outcome(int64(RemoteInterval(ruleFromMap(input.(map[string]any)))), nil)
		case "next_run_at":
			args := input.(map[string]any)
			if cpythonOnlyISO(args) {
				counts["skipped: CPython-only ISO spelling"]++
				continue
			}
			at, err := nextRunAt(args["entry"], args["interval"], args["baseline"], &given)
			var value any
			if at != nil {
				value = pyISO(*at)
			}
			got = outcome(value, err)
		case "is_fresh":
			args := input.(map[string]any)
			ts, _ := args["timestamp"].(string)
			got = outcome(fresh(ts, mapInt(args, "interval")), nil)
		case "download_cache_path":
			got = outcome(DownloadCachePath(input.(string)), nil)
		case "describe_failure":
			got = outcome(FailureReason(errorFromLegacy(buildError(input))), nil)
		case "mtime_iso":
			pair := input.([]any)
			sec, _ := pair[0].(json.Number).Int64()
			nsec, _ := pair[1].(json.Number).Int64()
			got = outcome(time.Unix(mtimeSeconds(sec, nsec), 0).UTC().Format("2006-01-02T15:04:05Z"), nil)
		case "host":
			text, _ := input.(string)
			got = outcome(sourceHost(text), nil)
		case "scenario":
			checkScenario(t, clock, row, nativeReference(t, refs, i, row))
			counts[fn]++
			continue
		default:
			t.Fatalf("unknown fn %s", fn)
		}
		counts[fn]++
		want := nativeReference(t, refs, i, row)
		if fn == "host" {
			if text, ok := input.(string); ok && (text == "http://host.test:abc/" || text == "http://%41.test/") &&
				mapStr(got, "output") == "" && mapStr(want, "output") != "" {
				// urllib exposes hostname despite a bad port and preserves an
				// escaped host; net/url refuses both. Neither URL is requestable.
				counts["accepted N2 invalid host URL"]++
				continue
			}
		}
		if !testjson.Equal(got, want) {
			t.Errorf("row %d %s(%s): got %s, want %s", i, fn, compactJSON(input), compactJSON(got), compactJSON(want))
		}
	}
	t.Logf("corpus rows: %v", counts)
}

func seedSharedMap(t *testing.T, ctx context.Context, r *repo.Repository, shared map[string]any) {
	t.Helper()
	err := r.UpdateShared(ctx, func(_ context.Context, current *model.Shared) error {
		existingRaw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		merged, err := decodeMap(existingRaw)
		if err != nil {
			return err
		}
		for k, v := range shared {
			merged[k] = v
		}
		updatedRaw, err := jsonresp.Encode(merged)
		if err != nil {
			return err
		}
		var next model.Shared
		if err := json.Unmarshal(updatedRaw, &next); err != nil {
			return err
		}
		*current = next
		return nil
	})
	if err != nil {
		t.Fatalf("seed shared: %v", err)
	}
}

func loadStatusSnapshot(t *testing.T, dir string) taskstatus.Snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, taskstatus.Filename))
	if err != nil {
		if os.IsNotExist(err) {
			return taskstatus.Snapshot{}
		}
		t.Fatal(err)
	}
	var snap taskstatus.Snapshot
	_ = json.Unmarshal(raw, &snap)
	return snap
}

// checkScenario replays a repository scenario: the files Python started
// from, then each call; outputs are compared structurally.
func checkScenario(t *testing.T, clock *fakeClock, row map[string]any, native map[string]any) {
	name := mapStr(row, "input")
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	files := mapObj(row, "files")
	if text, ok := files["health"].(string); ok {
		_, _ = r.WriteSharedText(ctx, HealthFile, []byte(text))
	}
	if shared := mapObj(files, "shared"); shared != nil {
		seedSharedMap(t, ctx, r, shared)
		_ = os.WriteFile(filepath.Join(r.DataDir(), taskstatus.Filename), []byte(mapStr(files, "statuses")), 0o644)
		for id, value := range mapObj(files, "caches") {
			data, err := jsonresp.Encode(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.WriteSharedText(ctx, "subscribes/"+id+".json", data); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range mapList(files, "rule_files") {
			file := item.(map[string]any)
			path, _ := r.SharedPath("rules/" + mapStr(file, "name"))
			_, _ = r.WriteSharedText(ctx, "rules/"+mapStr(file, "name"), []byte(mapStr(file, "content")))
			ns := mapInt(file, "mtime_ns")
			stamp := time.Unix(0, ns)
			_ = os.Chtimes(path, stamp, stamp)
		}
	}
	nativeOutputs := mapList(native, "output")
	for callIndex, item := range mapList(row, "output") {
		call := item.(map[string]any)
		label := mapStr(call, "call")
		var got map[string]any
		switch {
		case label == "rows":
			statuses := loadStatusSnapshot(t, r.DataDir())
			list := []any{}
			var failure error
			for _, build := range []func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error){SubscriptionTasks, RuleTasks} {
				part, err := build(repo.WithScope(ctx), statuses)
				if err != nil {
					failure = err
					break
				}
				for _, row := range part {
					data, err := jsonresp.Encode(row)
					if err != nil {
						t.Fatal(err)
					}
					v, err := decodeAny(data)
					if err != nil {
						t.Fatal(err)
					}
					list = append(list, v)
				}
			}
			got = outcome(list, failure)
		case label == "due":
			given := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			jobs, err := due(repo.WithScope(ctx), &given)
			keys := []any{}
			for _, j := range jobs {
				keys = append(keys, j.key())
			}
			got = outcome(keys, err)
		case strings.HasPrefix(label, "forget "):
			err := ForgetHealth(ctx, strings.TrimPrefix(label, "forget "))
			got = outcome(readHealth(t, r), err)
		default:
			parsed, _ := decodeAny([]byte(label))
			args := parsed.([]any)
			kwargs := args[1].(map[string]any)
			var count model.Int
			_ = decodeInto(kwargs["count"], &count)
			var traffic *Traffic
			if tMap := mapObj(kwargs, "traffic"); tMap != nil {
				traffic = &Traffic{}
				_ = decodeInto(tMap, traffic)
			}
			_, err := RecordHealth(ctx, args[0].(string), FetchEventInput{
				OK:        pyTruthy(kwargs["ok"]),
				Count:     count,
				FromCache: pyTruthy(kwargs["from_cache"]),
				Traffic:   traffic,
			})
			var value any
			if err == nil {
				value = readHealth(t, r)
			}
			got = outcome(value, err)
		}
		want := expected(nativeOutputs[callIndex].(map[string]any))
		if label == "rows" {
			want = storedSwitchTruth(t, want, got, mapObj(files, "shared"))
		}
		want = healthDroppedTraffic(want).(map[string]any)
		if !yamlxtest.EqualJSON(got, want) {
			t.Errorf("scenario %s / %s: %s\n  got  %s\n  want %s", name, label, testjson.Diff(want, got), compactJSON(got), compactJSON(want))
		}
	}
}

// healthDroppedTraffic removes, from expected health output, a subscription's
// traffic member that is not an object. Accepted O18 T1d difference: the
// recorded entry is rewritten from its typed form, where traffic of another
// type reads as absent (Python kept the stored value).
func healthDroppedTraffic(want any) any {
	root, ok := want.(map[string]any)
	if !ok {
		return want
	}
	output, ok := root["output"].(map[string]any)
	if !ok {
		return want
	}
	for _, entry := range output {
		if fields, ok := entry.(map[string]any); ok {
			if traffic, present := fields["traffic"]; present {
				if _, object := traffic.(map[string]any); !object {
					delete(fields, "traffic")
				}
			}
		}
	}
	return want
}

// storedSwitchTruth handles, in the expected task rows, a subscription or
// rule source stored with a non-boolean enabled member. Accepted O18 T1c
// difference: plain records read such a switch by flag truthiness (enabled: 0
// is off, so the task is disabled and has no next run) where Python tested
// "is not False" (on). For those rows the Go row is checked to carry the
// truth value and then stands in for Python's; every other row is compared
// unchanged.
func storedSwitchTruth(t *testing.T, want, got map[string]any, shared map[string]any) map[string]any {
	t.Helper()
	rows, ok := want["output"].([]any)
	gotRows, gotOK := got["output"].([]any)
	if !ok || !gotOK || len(rows) != len(gotRows) {
		return want
	}
	stored := map[string]any{}
	for category, list := range map[string]string{"subscription": "subscriptions", "rule_source": "rule_library"} {
		for _, item := range mapList(shared, list) {
			record, _ := item.(map[string]any)
			if value, present := record["enabled"]; present && value != nil {
				if _, isBool := value.(bool); !isBool {
					stored[category+":"+mapStr(record, "id")] = value
				}
			}
		}
	}
	if len(stored) == 0 {
		return want
	}
	out := make([]any, len(rows))
	for i, item := range rows {
		row, _ := item.(map[string]any)
		gotRow, _ := gotRows[i].(map[string]any)
		value, dirty := stored[mapStr(row, "key")]
		if !dirty || mapStr(gotRow, "key") != mapStr(row, "key") {
			out[i] = item
			continue
		}
		if enabled, _ := gotRow["enabled"].(bool); enabled != pyTruthy(value) {
			t.Errorf("task %s: enabled %v, want the stored switch's truth value", mapStr(row, "key"), gotRow["enabled"])
		}
		out[i] = gotRow
	}
	patched := maps.Clone(want)
	patched["output"] = out
	return patched
}

func readHealth(t *testing.T, r *repo.Repository) any {
	path, _ := r.SharedPath(HealthFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeAny(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// validate_refresh_interval has a private copy in internal/repo
// (compat_health.go; repo cannot import this package). Both must agree on
// repo's golden rows from Python.
func TestValidateRefreshIntervalMatchesTheRepoCopyGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../repo/testdata/compat_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, `"validate_refresh_interval"`) {
			continue
		}
		line = strings.TrimSuffix(strings.TrimSpace(line), ",")
		row, err := decodeMap([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		_, got := parseIntervalAny(row["input"], "")
		if raised := row["raised"] == true; raised != (got != nil) || (raised && got.Error() != mapStr(row, "error")) {
			t.Errorf("%s: %v", compactJSON(row["input"]), got)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no golden rows")
	}
}
