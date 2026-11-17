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

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
)

// frozenClock is the recorder's frozen time: datetime.now() fixed for the
// whole call and a constant time.monotonic().
type frozenClock struct{ now time.Time }

func (c frozenClock) Now() time.Time      { return c.now }
func (c frozenClock) Monotonic() float64  { return 1000 }
func (c frozenClock) Sleep(time.Duration) {}

func loadRecorded(t *testing.T, name string) []*jsonx.Object {
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
	var rows []*jsonx.Object
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*jsonx.Object))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// recordedResult is the recorder's {"__result__": [text, source, traffic,
// fetched_at]} form of a SubscriptionResult.
func recordedResult(r *substore.SubscriptionResult) any {
	if r == nil {
		return nil
	}
	var traffic any
	if r.Traffic != nil {
		traffic = r.Traffic
	}
	return jsonx.Obj("__result__", []any{r.Text, r.Source, traffic, fetchedAtValue(r)})
}

func resultFromRecord(value any) *substore.SubscriptionResult {
	var list []any
	if o, ok := value.(*jsonx.Object); ok {
		list, _ = o.Value("__result__").([]any)
	} else {
		list, _ = value.([]any)
	}
	if len(list) < 2 {
		return nil
	}
	r := &substore.SubscriptionResult{}
	r.Text, _ = list[0].(string)
	r.Source, _ = list[1].(string)
	if len(list) > 2 {
		r.Traffic, _ = list[2].(*jsonx.Object)
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
	return jsonx.Obj("__datetime__", pyISO(*t))
}

// recordedClassOf is type(err).__name__ for the replay comparison.
func recordedClassOf(err error) string {
	return pythonClass(err)
}

// seedFiles writes the files the call read (null: absent).
func seedFiles(t *testing.T, ctx context.Context, r *repo.Repository, files *jsonx.Object) {
	t.Helper()
	for label, value := range files.All() {
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

func readFiles(t *testing.T, r *repo.Repository, files *jsonx.Object) *jsonx.Object {
	t.Helper()
	out := jsonx.NewObject()
	for _, label := range files.Keys() {
		path := filepath.Join(r.DataDir(), label)
		if label != taskstatus.Filename {
			path, _ = r.SharedPath(label)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			out.Set(label, nil)
			continue
		}
		out.Set(label, string(data))
	}
	return out
}

// replayConverter answers convert_proxy_string with the recorded results.
type replayConverter struct{ answers map[string]*jsonx.Object }

func (c replayConverter) ConvertProxyString(_ context.Context, proxy string) *jsonx.Object {
	return c.answers[proxy]
}

type replayCall struct {
	row *jsonx.Object
	ctx context.Context
	r   *repo.Repository
}

func (c *replayCall) arg(name string) any { return c.row.GetObject("args").Value(name) }

func (c *replayCall) obj(name string) *jsonx.Object {
	o, _ := c.arg(name).(*jsonx.Object)
	return o
}

func (c *replayCall) str(name string) string {
	s, _ := c.arg(name).(string)
	return s
}

// replay runs one recorded call; it returns the recorded form of the output.
func (c *replayCall) replay() (any, error) {
	ctx := c.ctx
	switch c.row.GetString("fn") {
	case "validate_refresh_interval":
		label, _ := c.arg("label").(string)
		_, err := ValidateRefreshInterval(c.arg("value"), label)
		if err != nil {
			return nil, err
		}
		return c.arg("value"), nil
	case "source_cache_metadata":
		return SourceCacheMetadata(c.obj("sub"), resultFromRecord(c.arg("result")))
	case "_is_fresh":
		interval, _ := c.arg("interval").(jsonx.Number).Int64()
		return isFresh(c.arg("timestamp"), interval), nil
	case "_download_cache_path":
		return DownloadCachePath(c.str("url")), nil
	case "remember_source_download":
		r, err := RememberSourceDownload(ctx, c.obj("sub"), resultFromRecord(c.arg("result")))
		return recordedResult(r), err
	case "download_subscription_nodes":
		nodes, result, cache, err := DownloadSubscriptionNodes(ctx, c.obj("sub"))
		if err != nil {
			return nil, err
		}
		return []any{nodes, recordedResult(result), cache}, nil
	case "refresh_subscription":
		trigger, _ := c.arg("trigger").(string)
		nodes, result, cache, err := RefreshSubscription(ctx, c.obj("sub"), trigger)
		if err != nil {
			return nil, err
		}
		return []any{nodes, recordedResult(result), cache}, nil
	case "fetch_subscription_source":
		answers := c.row.GetList("fetches")
		calls := 0
		r, err := FetchSubscriptionSource(ctx, c.obj("sub"), func(_ context.Context, subID, url string) (*substore.SubscriptionResult, error) {
			if calls >= len(answers) {
				return nil, errors.New("unrecorded fetch")
			}
			answer := answers[calls].(*jsonx.Object)
			calls++
			if spec := answer.Value("err"); spec != nil {
				return nil, buildError(spec)
			}
			return resultFromRecord(answer.Value("ok")), nil
		})
		return recordedResult(r), err
	case "parse_userinfo":
		info, err := ParseUserinfo(c.str("header"))
		if info == nil {
			return nil, err
		}
		return info, err
	case "load_health":
		return LoadHealth(ctx)
	case "record_fetch":
		traffic, _ := c.arg("traffic").(*jsonx.Object)
		return RecordFetch(ctx, c.str("sub_id"), FetchRecord{OK: jsonx.Truthy(c.arg("ok")), Count: c.arg("count"),
			FromCache: jsonx.Truthy(c.arg("from_cache")), Traffic: traffic})
	case "forget":
		return nil, Forget(ctx, c.str("sub_id"))
	case "describe_failure":
		return DescribeFailure(buildError(c.arg("error"))), nil
	case "rule_interval":
		return RuleInterval(c.obj("rule")), nil
	case "subscription_interval":
		return SubscriptionInterval(c.obj("sub")), nil
	case "next_run_at":
		interval, _ := c.arg("interval").(jsonx.Number)
		var now *time.Time
		if o, ok := c.arg("now").(*jsonx.Object); ok {
			parsed, ok := parseTime(o.GetString("__datetime__"))
			if !ok {
				return nil, errors.New("bad recorded now")
			}
			now = &parsed
		}
		at, err := NextRunAt(c.arg("entry"), interval, c.arg("baseline"), now)
		return recordedDatetime(at), err
	case "subscription_cache_time":
		return SubscriptionCacheTime(ctx, c.obj("sub"))
	case "is_library_source":
		return IsLibrarySource(ctx, c.obj("rule"))
	case "find_resource":
		return FindResource(ctx, c.str("kind"), c.str("resource_id"))
	case "_history":
		return historyOf(c.arg("entry"))
	case "_host":
		return host(c.arg("url")), nil
	}
	return nil, errors.New("unknown function " + c.row.GetString("fn"))
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

func replayRecordedRows(t *testing.T, rows []*jsonx.Object, template string) {
	t.Helper()
	counts := map[string]int{}
	failures := 0
	for i, row := range rows {
		fn := row.GetString("fn")
		if patchedCollaborator(fn, row.GetList("patched")) {
			// The test replaced a collaborator of this call (e.g.
			// save_subscription_nodes with a lambda): not Python's behaviour.
			counts["skipped: monkeypatched collaborator"]++
			continue
		}
		now, ok := taskstatus.FromISOFormat(row.GetString("now"))
		if !ok {
			t.Fatalf("row %d: now %q", i, row.GetString("now"))
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
		if before := row.GetObject("before"); before != nil {
			seedFiles(t, ctx, r, before)
		}
		if shared := row.GetObject("shared"); shared != nil {
			if _, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
				for key, value := range shared.All() {
					current.Set(key, jsonx.DeepCopy(value))
				}
				return nil, nil
			}); err != nil {
				t.Fatalf("row %d: seed shared: %v", i, err)
			}
		}
		uuids := row.GetList("uuids")
		downloads := row.GetList("downloads")
		conversions := map[string]*jsonx.Object{}
		for _, item := range row.GetList("conversions") {
			call := item.(*jsonx.Object)
			proxy, _ := call.GetList("args")[0].(string)
			answer, _ := call.Value("ok").(*jsonx.Object)
			conversions[proxy] = answer
		}
		nextUUID, nextDownload := 0, 0
		env := &Env{
			Clock: frozenClock{now: now},
			RandomHex: func() string {
				if nextUUID >= len(uuids) {
					return "ffffffff"
				}
				nextUUID++
				return uuids[nextUUID-1].(string)
			},
			FetchSubscription: func(_ context.Context, subID, url string, opts substore.FetchOptions) (*substore.SubscriptionResult, error) {
				if nextDownload >= len(downloads) {
					return nil, errors.New("unrecorded download")
				}
				answer := downloads[nextDownload].(*jsonx.Object)
				nextDownload++
				if spec := answer.Value("err"); spec != nil {
					return nil, buildError(spec)
				}
				return resultFromRecord(answer.Value("ok")), nil
			},
			Converter: replayConverter{answers: conversions},
		}
		call := &replayCall{row: row, ctx: ctx, r: r}
		var value any
		func() {
			defer SetEnv(env)()
			value, err = call.replay()
		}()
		var got, want *jsonx.Object
		if err != nil {
			got = jsonx.Obj("err", jsonx.Obj("class", recordedClassOf(err), "text", err.Error()))
		} else {
			got = jsonx.Obj("ok", value)
		}
		if row.Has("err") {
			want = jsonx.Obj("err", row.Value("err"))
		} else {
			want = jsonx.Obj("ok", row.Value("ok"))
		}
		if fn == "forget" && err == nil {
			got.Set("ok", nil)
		}
		if after := row.GetObject("after"); after != nil {
			got.Set("after", readFiles(t, r, after))
			want.Set("after", after)
		}
		gotText, _ := jsonx.Compact(got)
		wantText, _ := jsonx.Compact(want)
		if !sourceWriterOutcomeEqual(fn, want, got) {
			failures++
			if failures <= 30 {
				t.Errorf("row %d %s (%s):\n  got  %s\n  want %s", i, fn, row.GetString("test"), clip(string(gotText)), clip(string(wantText)))
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
