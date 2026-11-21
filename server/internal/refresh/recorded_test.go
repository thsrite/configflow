package refresh

// Record & replay (PORTING rule 4): every call the whole Python test suite
// made to C15's public functions, recorded by testdata/c15_record.py with
// the files they read and wrote, the collaborators' answers and a frozen
// clock, replayed here with 0 differences. Regenerate on the test server:
//
//	rt -- bash -lc 'rm -rf /tmp/c15rec && C15_RECORD_DIR=/tmp/c15rec PYTHONPATH=.:server/internal/refresh/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 --dist=worksteal -p c15_record -p no:cacheprovider'
//	rt --back server/internal/refresh/testdata/c15_recorded.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/c15_record.py merge /tmp/c15rec'

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

// frozenClock is the recorder's frozen time: datetime.now() fixed for the
// whole call and a constant time.monotonic().
type frozenClock struct{ now time.Time }

func (c frozenClock) Now() time.Time      { return c.now }
func (c frozenClock) Monotonic() float64  { return 1000 }
func (c frozenClock) Sleep(time.Duration) {}

func loadRecorded(t *testing.T, name string) []map[string]any {
	t.Helper()
	handle, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	reader, err := gzip.NewReader(handle)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		row, err := decodeMap(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// recordedResult is the recorder's {"__result__": [text, source, traffic,
// fetched_at]} form of a SourceResult.
func recordedResult(r *SourceResult) any {
	if r == nil {
		return nil
	}
	var traffic any
	if r.Traffic != nil {
		if data, err := jsonresp.Encode(r.Traffic); err == nil {
			traffic, _ = decodeAny(data)
		}
	}
	var fetchedAt any
	if r.FetchedAt != "" {
		fetchedAt = r.FetchedAt
	}
	return map[string]any{"__result__": []any{r.Text, r.Source, traffic, fetchedAt}}
}

func resultFromRecord(value any) *SourceResult {
	var list []any
	if o, ok := value.(map[string]any); ok {
		list, _ = o["__result__"].([]any)
	} else {
		list, _ = value.([]any)
	}
	if len(list) < 2 {
		return nil
	}
	r := &SourceResult{}
	r.Text, _ = list[0].(string)
	r.Source, _ = list[1].(string)
	if len(list) > 2 && list[2] != nil {
		r.Traffic = &Traffic{}
		_ = decodeInto(list[2], r.Traffic)
	}
	if len(list) > 3 {
		r.FetchedAt, _ = list[3].(string)
	}
	return r
}

func recordedDatetime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return map[string]any{"__datetime__": pyISO(*t)}
}

// recordedClassOf is type(err).__name__ for the replay comparison.
func recordedClassOf(err error) string {
	var subscription *substore.SubscriptionFetchError
	if errors.As(err, &subscription) {
		return "SubscriptionFetchError"
	}
	return pythonClass(err)
}

// seedFiles writes the files the call read (null: absent).
func seedFiles(t *testing.T, ctx context.Context, r *repo.Repository, files map[string]any) {
	t.Helper()
	for label, value := range files {
		text, ok := value.(string)
		if !ok {
			continue
		}
		path := filepath.Join(r.DataDir(), label)
		if label != taskstatus.Filename {
			var err error
			if path, err = r.SharedPath(label); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFiles(t *testing.T, r *repo.Repository, files map[string]any) map[string]any {
	t.Helper()
	out := map[string]any{}
	for label := range files {
		path := filepath.Join(r.DataDir(), label)
		if label != taskstatus.Filename {
			path, _ = r.SharedPath(label)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			out[label] = nil
			continue
		}
		out[label] = string(data)
	}
	return out
}

// replayNodeConverter answers convert_proxy_string with the recorded results.
type replayNodeConverter struct{ answers map[string]map[string]any }

func (c replayNodeConverter) ConvertProxy(_ context.Context, proxy string) (*yamlx.Node, error) {
	answer := c.answers[proxy]
	if answer == nil {
		return nil, nil
	}
	raw, err := jsonresp.Encode(answer)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(raw)
}

type replayCall struct {
	row map[string]any
	ctx context.Context
	r   *repo.Repository
}

func (c *replayCall) arg(name string) any { return mapObj(c.row, "args")[name] }

func (c *replayCall) obj(name string) map[string]any {
	o, _ := c.arg(name).(map[string]any)
	return o
}

func (c *replayCall) str(name string) string {
	s, _ := c.arg(name).(string)
	return s
}

func encodeToAny(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return decodeAny(data)
}

// replay runs one recorded call; it returns the recorded form of the output.
func (c *replayCall) replay() (any, error) {
	ctx := c.ctx
	switch mapStr(c.row, "fn") {
	case "validate_refresh_interval":
		label, _ := c.arg("label").(string)
		_, err := parseIntervalAny(c.arg("value"), label)
		if err != nil {
			return nil, err
		}
		return c.arg("value"), nil
	case "source_cache_metadata":
		meta, err := CacheMetadata(context.Background(), sourceFromMap(c.obj("sub")), resultFromRecord(c.arg("result")))
		if err != nil {
			return nil, err
		}
		return encodeToAny(meta)
	case "_is_fresh":
		ts, _ := c.arg("timestamp").(string)
		return fresh(ts, mapInt(mapObj(c.row, "args"), "interval")), nil
	case "_download_cache_path":
		return DownloadCachePath(c.str("url")), nil
	case "remember_source_download":
		r, err := RememberDownload(ctx, sourceFromMap(c.obj("sub")), resultFromRecord(c.arg("result")))
		return recordedResult(r), err
	case "download_subscription_nodes":
		nodes, result, cache, err := DownloadNodes(ctx, sourceFromMap(c.obj("sub")))
		if err != nil {
			return nil, err
		}
		nodesAny, _ := encodeToAny(nodes)
		cacheAny, _ := encodeToAny(cache)
		return []any{nodesAny, recordedResult(result), cacheAny}, nil
	case "refresh_subscription":
		trigger, _ := c.arg("trigger").(string)
		nodes, result, cache, err := RefreshSource(ctx, sourceFromMap(c.obj("sub")), trigger)
		if err != nil {
			return nil, err
		}
		nodesAny, _ := encodeToAny(nodes)
		cacheAny, _ := encodeToAny(cache)
		return []any{nodesAny, recordedResult(result), cacheAny}, nil
	case "fetch_subscription_source":
		answers := mapList(c.row, "fetches")
		calls := 0
		r, err := FetchSource(ctx, sourceFromMap(c.obj("sub")), func(_ context.Context, subID, url string) (*SourceResult, error) {
			if calls >= len(answers) {
				return nil, errors.New("unrecorded fetch")
			}
			answer := answers[calls].(map[string]any)
			calls++
			if spec := answer["err"]; spec != nil {
				return nil, errorFromLegacy(buildError(spec))
			}
			return resultFromRecord(answer["ok"]), nil
		})
		return recordedResult(r), err
	case "parse_userinfo":
		info, err := parseTraffic(c.str("header"))
		if info == nil || err != nil {
			return nil, err
		}
		return encodeToAny(info)
	case "load_health":
		data, err := ReadHealth(detached(ctx))
		if err != nil {
			return nil, err
		}
		return encodeToAny(data)
	case "record_fetch":
		var count model.Int
		_ = decodeInto(c.arg("count"), &count)
		var traffic *Traffic
		if tObj := c.obj("traffic"); tObj != nil {
			traffic = &Traffic{}
			_ = decodeInto(tObj, traffic)
		}
		res, err := RecordHealth(ctx, c.str("sub_id"), FetchEventInput{
			OK:        pyTruthy(c.arg("ok")),
			Count:     count,
			FromCache: pyTruthy(c.arg("from_cache")),
			Traffic:   traffic,
		})
		if err != nil {
			return nil, err
		}
		return encodeToAny(res)
	case "forget":
		return nil, ForgetHealth(ctx, c.str("sub_id"))
	case "describe_failure":
		return FailureReason(errorFromLegacy(buildError(c.arg("error")))), nil
	case "rule_interval":
		return int64(RemoteInterval(ruleFromMap(c.obj("rule")))), nil
	case "subscription_interval":
		return int64(SourceInterval(sourceFromMap(c.obj("sub")))), nil
	case "next_run_at":
		var now *time.Time
		if o, ok := c.arg("now").(map[string]any); ok {
			parsed, ok := parseStamp(mapStr(o, "__datetime__"))
			if !ok {
				return nil, errors.New("bad recorded now")
			}
			now = &parsed
		}
		at, err := nextRunAt(c.arg("entry"), c.arg("interval"), c.arg("baseline"), now)
		return recordedDatetime(at), err
	case "subscription_cache_time":
		at, err := SourceCacheAt(ctx, sourceFromMap(c.obj("sub")))
		if at == nil {
			return nil, err
		}
		return *at, err
	case "is_library_source":
		return StoredRule(ctx, ruleFromMap(c.obj("rule")))
	case "find_resource":
		resource, err := LookupResource(ctx, c.str("kind"), c.str("resource_id"))
		if err != nil {
			return nil, err
		}
		if resource.Subscription != nil {
			return encodeToAny(resource.Subscription)
		}
		return encodeToAny(resource.Rule)
	case "_history":
		var receipt taskstatus.Receipt
		_ = decodeInto(c.arg("entry"), &receipt)
		res, err := tasks.ProjectHistory(&receipt)
		if err != nil {
			return nil, err
		}
		return encodeToAny(res)
	case "_host":
		text, _ := c.arg("url").(string)
		return sourceHost(text), nil
	}
	return nil, errors.New("unknown function " + mapStr(c.row, "fn"))
}

func TestRecordedPythonSuite(t *testing.T) {
	rows := loadRecorded(t, "testdata/c15_recorded.jsonl.gz")
	// Build the unchanged production defaults once. Each stateful row copies
	// the closed SQLite database and directory layout, then opens its own
	// repository normally; no writable files or Repository objects are shared.
	template := t.TempDir()
	if _, err := repo.Open(template, repo.Options{}); err != nil {
		t.Fatal(err)
	}
	// Sequential: the replay changes the process-wide Env. Keep the original
	// test tree and all rows; reuse only the immutable default database image.
	replayRecordedRows(t, rows, template)
}

func replayRecordedRows(t *testing.T, rows []map[string]any, template string) {
	t.Helper()
	counts := map[string]int{}
	failures := 0
	for i, row := range rows {
		fn := mapStr(row, "fn")
		if patchedCollaborator(fn, mapList(row, "patched")) {
			// The test replaced a collaborator of this call (e.g.
			// save_subscription_nodes with a lambda): not Python's behaviour.
			counts["skipped: monkeypatched collaborator"]++
			continue
		}
		now, _, ok := taskstatus.ParseISO(mapStr(row, "now"))
		if !ok {
			t.Fatalf("row %d: now %q", i, mapStr(row, "now"))
		}
		var r *repo.Repository
		var err error
		ctx := context.Background()
		// Pure/clock-only calls never consult the repository. Keep every stateful
		// row's database and before/after files private to that row.
		switch fn {
		case "validate_refresh_interval", "source_cache_metadata", "_is_fresh", "_download_cache_path",
			"parse_userinfo", "describe_failure", "rule_interval", "subscription_interval", "next_run_at", "_history", "_host":
		default:
			dir := t.TempDir()
			if err := os.CopyFS(dir, os.DirFS(template)); err != nil {
				t.Fatal(err)
			}
			r, err = repo.Open(dir, repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx = repo.WithScope(repo.WithRepository(ctx, r))
		}
		if before := mapObj(row, "before"); before != nil {
			seedFiles(t, ctx, r, before)
		}
		if shared := mapObj(row, "shared"); shared != nil {
			seedSharedMap(t, ctx, r, shared)
		}
		uuids := mapList(row, "uuids")
		downloads := mapList(row, "downloads")
		conversions := map[string]map[string]any{}
		for _, item := range mapList(row, "conversions") {
			call := item.(map[string]any)
			callArgs := mapList(call, "args")
			if len(callArgs) == 0 {
				continue
			}
			proxy, _ := callArgs[0].(string)
			answer, _ := call["ok"].(map[string]any)
			conversions[proxy] = answer
		}
		nextUUID, nextDownload := 0, 0
		env := DefaultRefreshEnv()
		env.Clock = frozenClock{now: now}
		env.RandomHex = func() string {
			if nextUUID >= len(uuids) {
				return "ffffffff"
			}
			nextUUID++
			return uuids[nextUUID-1].(string)
		}
		env.Sources.Fetch = func(_ context.Context, subID, url string, opts substore.FetchOptions) (*SourceResult, error) {
			if nextDownload >= len(downloads) {
				return nil, errors.New("unrecorded download")
			}
			answer := downloads[nextDownload].(map[string]any)
			nextDownload++
			if spec := answer["err"]; spec != nil {
				return nil, errorFromLegacy(buildError(spec))
			}
			return resultFromRecord(answer["ok"]), nil
		}
		env.Sources.Converter = replayNodeConverter{answers: conversions}
		call := &replayCall{row: row, ctx: ctx, r: r}
		var value any
		func() {
			defer SetRefreshEnv(env)()
			value, err = call.replay()
		}()
		var got, want map[string]any
		if err != nil {
			got = map[string]any{"err": map[string]any{"class": recordedClassOf(err), "text": err.Error()}}
		} else {
			got = map[string]any{"ok": value}
		}
		if errVal, hasErr := row["err"]; hasErr {
			want = map[string]any{"err": errVal}
		} else {
			want = map[string]any{"ok": row["ok"]}
		}
		if fn == "forget" && err == nil {
			got["ok"] = nil
		}
		if after := mapObj(row, "after"); after != nil {
			got["after"] = readFiles(t, r, after)
			want["after"] = after
		}
		if !sourceWriterOutcomeEqual(fn, want, got) {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s):\n  got  %s\n  want %s", i, fn, mapStr(row, "test"), clip(compactJSON(got)), clip(compactJSON(want)))
			}
		}
		counts[fn]++
	}
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
	t.Logf("replayed %d calls: %v", len(rows), counts)
}

func clip(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return strings.TrimSpace(s)
}

// collaborators are the functions each recorded call uses that a test may
// have replaced (c15_record.py COLLABORATORS); a call is skipped only when
// one of its own was.
var collaborators = map[string][]string{
	"fetch_subscription_source":   {"subscription_cache.load_subscription_cache", "subscription_refresh.remember_source_download", "config.get_repository"},
	"remember_source_download":    {"config.get_repository"},
	"download_subscription_nodes": {"subscription_cache.save_subscription_nodes", "sub_store_client.parse_proxies_from_yaml", "sub_store_client.proxies_to_nodes", "subscription_refresh.remember_source_download", "subscription_hosts.extract_subscription_hosts", "subscription_dialers.extract_subscription_dialers", "config.get_repository"},
	"refresh_subscription":        {"*"},
	"record_fetch":                {"subscription_health.load_health", "config.get_repository"},
	"load_health":                 {"config.get_repository"},
	"forget":                      {"subscription_health.load_health", "config.get_repository"},
	"subscription_cache_time":     {"subscription_cache.load_subscription_cache"},
	"is_library_source":           {"config.get_repository"},
	"find_resource":               {"config.get_repository"},
}

func patchedCollaborator(fn string, patched []any) bool {
	for _, item := range patched {
		label := strings.TrimPrefix(item.(string), "backend.")
		label = label[strings.Index(label, ".")+1:]
		for _, name := range collaborators[fn] {
			if name == "*" || name == label {
				return true
			}
		}
	}
	return false
}
