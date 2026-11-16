package routersplit

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"configflow/internal/gateway/gwbase"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Replay of every router_split call the Python suite made
// (testdata/r02_record.py → testdata/r02_recorded.jsonl.gz). Collaborator
// answers (profile_sessions, get_profile_agent_bindings, get_profile) are
// served from the record in call order; Go must ask for the same ones.

// RecordedRow is one recorded call (shared with internal/ippresence's
// replay through LoadRecorded in export_test.go).
type recordedRow struct {
	Test string
	Fn   string
	Args *jsonx.Object
	Out  any
	Err  *jsonx.Object
	Env  []any
	Raw  *jsonx.Object
}

func loadRecorded(t testing.TB, path string) []recordedRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	var rows []recordedRow
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		value, err := jsonx.Parse(line)
		if err != nil {
			t.Fatal(err)
		}
		entry := decodeRecorded(value).(*jsonx.Object)
		row := recordedRow{Test: entry.GetString("test"), Fn: entry.GetString("fn"), Args: entry.GetObject("args"),
			Out: entry.Value("out"), Err: entry.GetObject("err"), Env: entry.GetList("env"), Raw: entry}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

type repoMarker struct{}

// decodeRecorded turns the recorder's markers back into values: tuples into
// lists, the repository argument into repoMarker.
func decodeRecorded(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value.Len() == 1 {
			if items, ok := value.Get("__tuple__"); ok {
				return decodeRecorded(items)
			}
			if _, ok := value.Get("__repo__"); ok {
				return repoMarker{}
			}
		}
		out := jsonx.NewObject()
		for k, item := range value.All() {
			out.Set(k, decodeRecorded(item))
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = decodeRecorded(item)
		}
		return out
	}
	return v
}

type replayError struct{ class, message string }

func (e *replayError) Error() string       { return e.message }
func (e *replayError) PythonClass() string { return e.class }

func encode(v any) string {
	b, err := jsonx.Compact(v)
	if err != nil {
		return fmt.Sprintf("<%v: %#v>", err, v)
	}
	return string(b)
}

// compareError checks a Go error against the recorded exception.
func compareError(want *jsonx.Object, got error) string {
	if got == nil {
		return "Python raised " + encode(want) + ", Go returned no error"
	}
	class := "?"
	var classed interface{ PythonClass() string }
	if errors.As(got, &classed) {
		class = classed.PythonClass()
	}
	var problems []string
	if class != want.GetString("class") {
		problems = append(problems, fmt.Sprintf("class python %s go %s", want.GetString("class"), class))
	}
	if got.Error() != want.GetString("msg") {
		problems = append(problems, fmt.Sprintf("message python %q go %q", want.GetString("msg"), got.Error()))
	}
	var gateway *gwbase.LinuxGatewayError
	if errors.As(got, &gateway) != (want.Value("gateway") == true) {
		problems = append(problems, "isinstance LinuxGatewayError differs")
	}
	if code, ok := want.Get("code"); ok {
		var split *RouterSplitError
		if !errors.As(got, &split) || split.Code != code || !jsonx.Equal(jsonx.Int(int64(split.Status)), want.Value("status")) {
			problems = append(problems, "RouterSplitError code/status differ")
		}
	}
	return strings.Join(problems, "; ")
}

// replayEnv serves the recorded collaborator answers in order.
type replayEnv struct {
	calls    []any
	next     int
	problems []string
}

func (r *replayEnv) take(fn, profileID string) (any, error) {
	if r.next >= len(r.calls) {
		r.problems = append(r.problems, fmt.Sprintf("Go called %s(%q), Python made no further collaborator call", fn, profileID))
		return nil, &replayError{"ReplayError", "unexpected call"}
	}
	entry := r.calls[r.next].(*jsonx.Object)
	r.next++
	if entry.GetString("fn") != fn || !jsonx.Equal(entry.GetList("args"), []any{profileID}) {
		r.problems = append(r.problems, fmt.Sprintf("collaborator call %d: python %s%s go %s(%q)", r.next,
			entry.GetString("fn"), encode(entry.GetList("args")), fn, profileID))
	}
	if err := entry.GetObject("err"); err != nil {
		return nil, &replayError{err.GetString("class"), err.GetString("msg")}
	}
	return entry.Value("out"), nil
}

func (r *replayEnv) install() (restore func()) {
	savedBindings, savedProfile, savedSessions := agentBindings, getProfile, sessionsOf
	agentBindings = func(_ context.Context, _ *repo.Repository, profileID string) (*jsonx.Object, error) {
		out, err := r.take("get_profile_agent_bindings", profileID)
		if err != nil {
			return nil, err
		}
		o, _ := out.(*jsonx.Object)
		return o, nil
	}
	getProfile = func(_ context.Context, _ *repo.Repository, profileID string) (*jsonx.Object, error) {
		out, err := r.take("get_profile", profileID)
		if err != nil {
			return nil, err
		}
		o, _ := out.(*jsonx.Object)
		return o, nil
	}
	sessionsOf = func(_ context.Context, _ *repo.Repository, profileID string) ([]*jsonx.Object, error) {
		out, err := r.take("profile_sessions", profileID)
		if err != nil {
			return nil, err
		}
		var sessions []*jsonx.Object
		for _, item := range out.([]any) {
			sessions = append(sessions, item.(*jsonx.Object))
		}
		return sessions, nil
	}
	return func() { agentBindings, getProfile, sessionsOf = savedBindings, savedProfile, savedSessions }
}

type notExpressible string

func (e notExpressible) Error() string { return string(e) }

func profileArg(v any) (string, error) {
	switch value := v.(type) {
	case string:
		return value, nil
	case nil:
		return "", nil
	}
	return "", notExpressible("profile_id is not a string")
}

func boolOut(v bool, err error) (any, error) { return v, err }

// runRow calls the Go function of a recorded row.
func runRow(row recordedRow) (any, error) {
	a := row.Args
	ctx := context.Background()
	switch row.Fn {
	case "read_marker":
		state, marker := ReadMarker(a.Value("config"))
		if marker == nil {
			return []any{state, nil}, nil
		}
		return []any{state, marker}, nil
	case "is_full_tun":
		return IsFullTun(a.Value("config")), nil
	case "route_rule_set_ids":
		return RouteRuleSetIDs(a.Value("config")), nil
	case "tun_filters":
		return jsonx.ListOf(TunFilters(a.Value("tun"))), nil
	case "full_tun_document":
		return FullTunDocument(a.Value("document"))
	case "full_tun_base_yaml":
		return FullTunBaseYAML(a.Value("text"))
	case "is_full_tun_document":
		return IsFullTunDocument(a.Value("doc")), nil
	case "check_generation":
		return nil, CheckGeneration(a.Value("config"))
	case "require_full_tun_generated":
		return nil, RequireFullTunGenerated(a.Value("main"))
	case "legacy_context":
		profileID, err := profileArg(a.Value("profile_id"))
		if err != nil {
			return nil, err
		}
		return LegacyContext(ctx, nil, profileID)
	case "needs_context":
		return boolOut(NeedsContext(a.Value("config")))
	case "base_is_full_tun":
		return boolOut(BaseIsFullTun(a.Value("config")))
	case "legacy_source":
		return LegacySource(a.Value("config"), a.Value("context"))
	case "publish_legacy_source":
		profileID, err := profileArg(a.Value("profile_id"))
		if err != nil {
			return nil, err
		}
		return PublishLegacySource(ctx, nil, profileID, a.Value("config"), a.Value("agent_id"))
	case "legacy_gateway_agent_ids":
		return LegacyGatewayAgentIDs(a.Value("config"), a.Value("context"))
	case "gateway_was_used":
		return boolOut(GatewayWasUsed(a.Value("config"), a.Value("agent"), a.Value("source")))
	case "mode_from_context":
		return ModeFromContext(a.Value("config"), a.Value("context"))
	case "router_split_state":
		profileID, err := profileArg(a.Value("profile_id"))
		if err != nil {
			return nil, err
		}
		return RouterSplitState(ctx, nil, profileID, a.Value("config"), a.Value("context"))
	}
	return nil, notExpressible("unknown function " + row.Fn)
}

func TestRecordedPythonTests(t *testing.T) { replay(t, "testdata/r02_recorded.jsonl.gz") }

// TestCorpus replays the random corpus generated with real Python
// (testdata/gen_r02.py): malformed markers, gateway settings, base YAML
// (anchors, non-string keys, timestamps, invalid YAML, non-strings), router
// topologies, contexts, sessions and agent bindings.
func TestCorpus(t *testing.T) { replay(t, "testdata/r02_corpus.jsonl.gz") }

func replay(t *testing.T, path string) {
	rows := loadRecorded(t, path)
	identical, notExpr := map[string]int{}, map[string]int{}
	differ := 0
	tests, files := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if row.Fn == "classical_ip_lines" {
			continue // replayed in internal/ippresence
		}
		tests[row.Test] = true
		files[strings.SplitN(row.Test, "::", 2)[0]] = true
		env := &replayEnv{calls: row.Env}
		restore := env.install()
		out, err := runRow(row)
		restore()
		var ne notExpressible
		if errors.As(err, &ne) {
			notExpr[row.Fn]++
			continue
		}
		var problems []string
		if row.Err != nil {
			if p := compareError(row.Err, err); p != "" {
				problems = append(problems, p)
			}
		} else if err != nil {
			problems = append(problems, "Go error "+err.Error()+", Python returned "+encode(row.Out))
		} else if encode(out) != encode(row.Out) {
			problems = append(problems, "result\n  python "+encode(row.Out)+"\n  go     "+encode(out))
		}
		if env.next != len(env.calls) {
			problems = append(problems, fmt.Sprintf("Go made %d of Python's %d collaborator calls", env.next, len(env.calls)))
		}
		problems = append(problems, env.problems...)
		if len(problems) > 0 {
			differ++
			if differ <= 20 {
				t.Errorf("%s %s(%s):\n  %s", row.Test, row.Fn, encode(row.Args), strings.Join(problems, "\n  "))
			}
			continue
		}
		identical[row.Fn]++
	}
	total, notTotal := 0, 0
	for _, n := range notExpr {
		notTotal += n
	}
	var names []string
	for name, n := range identical {
		total += n
		names = append(names, fmt.Sprintf("%s %d", name, n))
	}
	sort.Strings(names)
	t.Logf("%s router_split: %d identical, %d differ, %d not expressible %v; from %d tests in %d files; identical per function: %s",
		path, total, differ, notTotal, notExpr, len(tests), len(files), strings.Join(names, ", "))
}
