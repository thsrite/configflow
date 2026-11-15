package refresh

// The reference corpus from the real Python modules
// (testdata/gen.py → testdata/corpus.json; regenerate on the test server:
// rt --back server/internal/refresh/testdata/corpus.json -- bash -lc
// 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/gen.py').

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
)

var corpusNow = time.Date(2026, 11, 1, 0, 0, 0, 250000000, time.UTC)

func loadCorpus(t *testing.T) []*jsonx.Object {
	data, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var rows []*jsonx.Object
	for _, row := range value.([]any) {
		rows = append(rows, row.(*jsonx.Object))
	}
	return rows
}

// outcome is the corpus form of a result: {"output": v} or {"error": class}.
func outcome(value any, err error) *jsonx.Object {
	if err != nil {
		return jsonx.Obj("error", pythonClass(err))
	}
	return jsonx.Obj("output", value)
}

func expected(row *jsonx.Object) *jsonx.Object {
	if row.Has("error") {
		return jsonx.Obj("error", row.Value("error"))
	}
	return jsonx.Obj("output", row.Value("output"))
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
	object, ok := spec.(*jsonx.Object)
	if !ok {
		return nil
	}
	class, message := object.GetString("class"), object.GetString("message")
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
		return &substore.SubscriptionFetchError{SubStoreError: buildError(object.Value("sub_store")),
			DirectError: buildError(object.Value("direct"))}
	}
	return &rulefetch.FetchError{Class: class, Message: message, Status: int(object.GetInt("status"))}
}

func TestReferenceCorpus(t *testing.T) {
	clock := &fakeClock{now: corpusNow}
	t.Cleanup(SetEnv(&Env{Clock: clock}))
	given := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	counts := map[string]int{}
	for i, row := range loadCorpus(t) {
		fn := row.GetString("fn")
		input := row.Value("input")
		var got *jsonx.Object
		switch fn {
		case "validate_refresh_interval":
			v, err := ValidateRefreshInterval(input, "")
			got = outcome(jsonx.Int(v), err)
		case "validate_refresh_interval_label":
			_, err := ValidateRefreshInterval(input, "更新间隔")
			got = outcome(nil, err)
		case "subscription_interval":
			got = outcome(SubscriptionInterval(input.(*jsonx.Object)), nil)
		case "rule_interval":
			got = outcome(RuleInterval(input.(*jsonx.Object)), nil)
		case "next_run_at":
			args := input.(*jsonx.Object)
			interval, _ := args.GetNumber("interval")
			at, err := NextRunAt(args.Value("entry"), interval, args.Value("baseline"), &given)
			var value any
			if at != nil {
				value = pyISO(*at)
			}
			got = outcome(value, err)
		case "is_fresh":
			args := input.(*jsonx.Object)
			got = outcome(isFresh(args.Value("timestamp"), args.GetInt("interval")), nil)
		case "download_cache_path":
			got = outcome(DownloadCachePath(input.(string)), nil)
		case "describe_failure":
			got = outcome(DescribeFailure(buildError(input)), nil)
		case "mtime_iso":
			pair := input.([]any)
			sec, _ := pair[0].(jsonx.Number).Int64()
			nsec, _ := pair[1].(jsonx.Number).Int64()
			got = outcome(time.Unix(mtimeSeconds(sec, nsec), 0).UTC().Format("2006-01-02T15:04:05Z"), nil)
		case "host":
			got = outcome(host(input), nil)
		case "scenario":
			checkScenario(t, clock, row)
			counts[fn]++
			continue
		default:
			t.Fatalf("unknown fn %s", fn)
		}
		counts[fn]++
		want := expected(row)
		if fn == "next_run_at" && strings.HasSuffix(want.GetString("output"), ":15.250000") {
			// Accepted: sub-second UTC offsets are cut to seconds
			// (time.FixedZone); the instant must still match.
			a, _ := taskstatus.ParseTime(got.GetString("output"))
			b, _ := taskstatus.ParseTime(want.GetString("output"))
			if !a.Equal(b) {
				t.Errorf("row %d: instant %v, want %v", i, a, b)
			}
			counts["accepted sub-second offset"]++
			continue
		}
		if !jsonx.Equal(got, want) {
			t.Errorf("row %d %s(%s): got %s, want %s", i, fn, jsonx.Repr(input), jsonx.Repr(got), jsonx.Repr(want))
		}
	}
	t.Logf("corpus rows: %v", counts)
}

// checkScenario replays a repository scenario: the files Python started
// from, then each call; outputs are compared with key order (Compact).
func checkScenario(t *testing.T, clock *fakeClock, row *jsonx.Object) {
	name := row.GetString("input")
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	files := row.GetObject("files")
	if text, ok := files.Value("health").(string); ok {
		r.WriteSharedText(ctx, HealthFile, []byte(text))
	}
	if shared := files.GetObject("shared"); shared != nil {
		_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
			for key, value := range shared.All() {
				current.Set(key, jsonx.DeepCopy(value))
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(r.DataDir(), taskstatus.Filename), []byte(files.GetString("statuses")), 0o644)
		for id, value := range files.GetObject("caches").All() {
			if _, err := r.WriteSharedJSON(ctx, "subscribes/"+id+".json", value); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range files.GetList("rule_files") {
			file := item.(*jsonx.Object)
			path, _ := r.SharedPath("rules/" + file.GetString("name"))
			r.WriteSharedText(ctx, "rules/"+file.GetString("name"), []byte(file.GetString("content")))
			mtime, _ := file.GetNumber("mtime_ns")
			ns, _ := mtime.Int64()
			stamp := time.Unix(0, ns)
			os.Chtimes(path, stamp, stamp)
		}
	}
	for _, item := range row.GetList("output") {
		call := item.(*jsonx.Object)
		label := call.GetString("call")
		var got *jsonx.Object
		switch {
		case label == "rows":
			statuses, err := taskstatus.New(r.DataDir()).Load()
			if err != nil {
				t.Fatal(err)
			}
			list := []any{}
			var failure error
			for _, build := range []func(context.Context, *jsonx.Object) ([]*jsonx.Object, error){SubscriptionRows, RuleRows} {
				part, err := build(repo.WithScope(ctx), statuses)
				if err != nil {
					failure = err
					break
				}
				for _, row := range part {
					list = append(list, row)
				}
			}
			got = outcome(list, failure)
		case label == "due":
			given := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			jobs, err := due(repo.WithScope(ctx), &given)
			keys := []any{}
			for _, j := range jobs {
				keys = append(keys, j.key)
			}
			got = outcome(keys, err)
		case strings.HasPrefix(label, "forget "):
			err := Forget(ctx, strings.TrimPrefix(label, "forget "))
			got = outcome(readHealth(t, r), err)
		default:
			parsed, _ := jsonx.Parse([]byte(label))
			args := parsed.([]any)
			kwargs := args[1].(*jsonx.Object)
			traffic, _ := kwargs.Value("traffic").(*jsonx.Object)
			_, err := RecordFetch(ctx, args[0].(string), FetchRecord{OK: jsonx.Truthy(kwargs.Value("ok")),
				Count: kwargs.Value("count"), FromCache: jsonx.Truthy(kwargs.Value("from_cache")), Traffic: traffic})
			var value any
			if err == nil {
				value = readHealth(t, r)
			}
			got = outcome(value, err)
		}
		want := expected(call)
		gotText, _ := jsonx.Compact(got)
		wantText, _ := jsonx.Compact(want)
		if string(gotText) != string(wantText) {
			t.Errorf("scenario %s / %s:\n  got  %s\n  want %s", name, label, gotText, wantText)
		}
	}
}

func readHealth(t *testing.T, r *repo.Repository) any {
	path, _ := r.SharedPath(HealthFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// validate_refresh_interval has a private copy in internal/repo
// (compat_health.go; repo cannot import this package). Both must agree on
// repo's golden rows from Python.
func TestValidateRefreshIntervalMatchesTheRepoCopyGolden(t *testing.T) {
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
		value, err := jsonx.Parse([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		row := value.(*jsonx.Object)
		_, got := ValidateRefreshInterval(row.Value("input"), "")
		if raised := row.Value("raised") == true; raised != (got != nil) || (raised && got.Error() != row.GetString("error")) {
			t.Errorf("%s: %v", jsonx.Repr(row.Value("input")), got)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no golden rows")
	}
}
