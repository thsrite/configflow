package routers_test

// R05 boundary corpus: the real Python iKuai driver (testdata/r05_reference.py,
// run with ~/cf-test/.venv/bin/python) and the Go driver work against the same
// loopback iKuai fake (routerfake.Ikuai). Compared per case: the outcome
// (results, undo records, error class/message/undo, rollback results), the
// complete request sequence (paths and parsed bodies, login included) and the
// final device tables. Cases are in ikuai_cases_test.go.

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

type ikuaiCase struct {
	name   string
	action string
	state  routerfake.IkuaiState
	input  map[string]any // op, ops, func, param, prefix, ids, write_action, param2, classes, timeout, values, tags, items, batch, always_rollback, path, password
	// fixed names a Python bug the Go driver fixes; check then asserts both sides.
	fixed string
	check func(t *testing.T, python, golang ikuaiOutcome, pythonState, goState routerfake.IkuaiState)
}

type ikuaiError struct {
	Class   string `json:"class"`
	Message string `json:"message"`
	Undo    any    `json:"undo"`
}

type ikuaiOutcome struct {
	Result   any              `json:"result"`
	Error    *ikuaiError      `json:"error"`
	Records  []map[string]any `json:"records"`
	Rollback []map[string]any `json:"rollback"`
}

func TestIkuaiBoundaryCorpus(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(home, "cf-test", ".venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("R05 Python boundary reference needs ~/cf-test/.venv/bin/python")
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(working, "..", "..", ".."))
	helper := filepath.Join(working, "testdata", "r05_reference.py")
	fake := routerfake.NewIkuai(routerfake.IkuaiState{})
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	cases := ikuaiCases()
	fixed := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake.Reset(tc.state)
			pythonOutcome := runPythonIkuai(t, python, repoRoot, helper, server.URL, tc)
			pythonState := fake.Snapshot()
			fake.Reset(tc.state)
			goOutcome := runGoIkuai(t, tc, server.URL)
			goState := fake.Snapshot()
			if tc.fixed != "" {
				fixed++
				tc.check(t, pythonOutcome, goOutcome, pythonState, goState)
				return
			}
			compareIkuai(t, "outcome", normalizeOutcome(pythonOutcome), normalizeOutcome(goOutcome))
			compareIkuai(t, "requests", pythonState.Calls, goState.Calls)
			compareIkuai(t, "tables", pythonState.Tables, goState.Tables)
			compareIkuai(t, "stale", pythonState.Stale, goState.Stale)
			if tc.check != nil {
				tc.check(t, pythonOutcome, goOutcome, pythonState, goState)
			}
		})
	}
	t.Logf("%d cases, %d intended differences (Python bugs fixed in Go)", len(cases), fixed)
}

func compareIkuai(t *testing.T, label string, python, golang any) {
	t.Helper()
	left, right := jsonComparable(t, python), jsonComparable(t, golang)
	if !reflect.DeepEqual(left, right) {
		t.Errorf("%s differ: %s", label, jsonDiff(left, right))
	}
}

// normalizeOutcome folds crashes of either side (Python KeyError/ValueError,
// Go plain errors) into one class: only Chinese RouterError messages are
// compared byte for byte.
func normalizeOutcome(o ikuaiOutcome) ikuaiOutcome {
	fold := func(e *ikuaiError) *ikuaiError {
		if e == nil {
			return nil
		}
		switch e.Class {
		case "RouterError", "_Rejected", "RouterRollbackError":
			return e
		}
		return &ikuaiError{Class: "crash"}
	}
	o.Error = fold(o.Error)
	for _, item := range o.Rollback {
		for _, key := range []string{"error", "batch_error"} {
			if raw, ok := item[key]; ok {
				data, _ := json.Marshal(raw)
				var e ikuaiError
				_ = json.Unmarshal(data, &e)
				item[key] = fold(&e)
			}
		}
	}
	return o
}

func runPythonIkuai(t *testing.T, python, repoRoot, helper, endpoint string, tc ikuaiCase) ikuaiOutcome {
	t.Helper()
	input := map[string]any{"action": tc.action, "state": tc.state}
	for key, value := range tc.input {
		input[key] = value
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, helper)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R05_ENDPOINT="+endpoint)
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python R05 reference failed: %v\n%s", err, stderr.String())
	}
	var outcome ikuaiOutcome
	if err := json.Unmarshal(output, &outcome); err != nil {
		t.Fatalf("decode Python R05 result: %v\n%s", err, output)
	}
	return outcome
}

func goErrorOf(err error) *ikuaiError {
	e := &ikuaiError{Class: "crash", Message: err.Error()}
	if routerErr, ok := errors.AsType[*routers.RouterError](err); ok {
		switch routerErr.Kind {
		case routers.KindRejected:
			e.Class = "_Rejected"
		case routers.KindRollback:
			e.Class = "RouterRollbackError"
		default:
			e.Class = "RouterError"
		}
		if undo := device.UndoOf(err); undo != nil {
			_ = json.Unmarshal(undo, &e.Undo)
		}
	}
	return e
}

func decoded(raw json.RawMessage) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

func groupStateView(s *routers.GroupState) any {
	if s == nil {
		return nil
	}
	groups := []any{}
	for _, g := range s.Groups {
		groups = append(groups, map[string]any{"id": g.ID, "gid": g.GID, "name": g.Name, "comment": g.Comment,
			"values": g.Values, "entries": g.Entries, "refs": g.Refs, "role": g.Role, "index": g.Index,
			"users": g.Users, "dup": g.Dup, "adopted": g.Adopted})
	}
	rules := []any{}
	for _, r := range s.Rules {
		rules = append(rules, map[string]any{"key": r.Key, "func": r.Func, "id": r.ID, "comment": r.Comment,
			"enabled": r.Enabled, "action": r.Action, "custom": r.Custom, "objects": r.Objects, "src_objects": r.SrcObjects})
	}
	return map[string]any{"groups": groups, "rules": rules}
}

// classHTTP fails every /Action/call with one devhttp failure class.
type classHTTP struct{ class string }

func (c classHTTP) Do(_ context.Context, _, url string, _ any, _ time.Duration) (*devhttp.Response, error) {
	if strings.HasSuffix(url, "/Action/login") {
		return &devhttp.Response{Status: 200, Body: []byte(`{"Result":10000,"code":0}`)}, nil
	}
	return nil, &devhttp.Error{Class: c.class, Err: errors.New("synthetic-secret-detail")}
}

func (classHTTP) Close() {}

func stringOf(v any) string { s, _ := v.(string); return s }

func runGoIkuai(t *testing.T, tc ikuaiCase, endpoint string) ikuaiOutcome {
	t.Helper()
	ctx := context.Background()
	password := routerfake.IkuaiPassword
	if p, ok := tc.input["password"].(string); ok {
		password = p
	}
	driver := routers.NewIkuai(endpoint+stringOf(tc.input["path"]), routerfake.IkuaiUser, password, false)
	defer driver.Close()
	outcome := ikuaiOutcome{Records: []map[string]any{}, Rollback: []map[string]any{}}
	encode := func(v any) json.RawMessage { data, _ := json.Marshal(v); return data }
	var result any
	var err error
	switch tc.action {
	case "probe":
		result, err = driver.Probe(ctx)
	case "read_state":
		var state *routers.RouterState
		if state, err = driver.ReadState(ctx); err == nil {
			result = map[string]any{"state": state, "groups": groupStateView(state.IkuaiGroups)}
		}
	case "read_dhcp_dns":
		result, err = driver.ReadDHCPDNS(ctx)
	case "apply":
		var undo json.RawMessage
		if undo, err = driver.Apply(ctx, encode(tc.input["op"])); err == nil {
			result = decoded(undo)
		}
	case "roundtrip":
		var undo, restored json.RawMessage
		if undo, err = driver.Apply(ctx, encode(tc.input["op"])); err == nil {
			if restored, err = driver.Apply(ctx, undo); err == nil {
				result = map[string]any{"undo": decoded(undo), "restored": decoded(restored)}
			}
		}
	case "release":
		outcome = goRelease(ctx, driver, tc.input)
		return outcome
	case "call":
		var param map[string]any
		_ = json.Unmarshal(encode(tc.input["param"]), &param)
		result, err = driver.Call(ctx, stringOf(tc.input["func"]), param)
	case "list_all":
		ids, ok := tc.input["ids"].(bool)
		result, err = driver.ListAll(ctx, stringOf(tc.input["func"]), stringOf(tc.input["prefix"]), ids || !ok, "")
	case "write":
		result, err = driver.Write(ctx, stringOf(tc.input["func"]), stringOf(tc.input["write_action"]), tc.input["param"], 0)
	case "write_twice":
		var first, second any
		if first, err = driver.Write(ctx, stringOf(tc.input["func"]), stringOf(tc.input["write_action"]), tc.input["param"], 0); err == nil {
			second, err = driver.Write(ctx, stringOf(tc.input["func"]), stringOf(tc.input["write_action"]), tc.input["param2"], 0)
			result = []any{first, second}
		}
	case "transport":
		messages := map[string]any{}
		timeout, _ := tc.input["timeout"].(float64)
		for _, class := range tc.input["classes"].([]string) {
			local := routers.NewIkuaiWithHTTP("http://192.0.2.1", routerfake.IkuaiUser, routerfake.IkuaiPassword, classHTTP{class})
			_, writeErr := local.Write(ctx, "nat_rule", "edit", map[string]any{"id": 1}, timeout)
			messages[class] = goErrorOf(writeErr)
		}
		result = messages
	case "values":
		var values []any
		for _, pair := range tc.input["values"].([][2]string) {
			values = append(values, ikuai.DecodeIkuaiValue(pair[1], pair[0]))
		}
		var tags []string
		for _, pair := range tc.input["tags"].([][2]string) {
			tags = append(tags, ikuai.IkuaiTagname(pair[0], pair[1]))
		}
		result = map[string]any{"decoded": values, "tagnames": tags}
	case "oversized":
		var ops []json.RawMessage
		for _, op := range tc.input["ops"].([]map[string]any) {
			ops = append(ops, encode(op))
		}
		result = ikuai.IkuaiOversizedRequests(ops)
	case "request_bytes":
		var sizes []int
		for _, item := range tc.input["items"].([]map[string]any) {
			sizes = append(sizes, ikuai.IkuaiRequestBytes(stringOf(item["func"]), stringOf(item["action"]), item["param"]))
		}
		result = sizes
	default:
		t.Fatalf("unknown action %s", tc.action)
	}
	if err != nil {
		outcome.Error = goErrorOf(err)
		return outcome
	}
	outcome.Result = result
	return outcome
}

// goRelease mirrors release._apply_stage/_rollback for the driver: undo
// records (also those a failed write carries) are kept and run in reverse in
// a rollback batch; empty records are skipped.
func goRelease(ctx context.Context, driver *routers.Ikuai, input map[string]any) ikuaiOutcome {
	outcome := ikuaiOutcome{Records: []map[string]any{}, Rollback: []map[string]any{}}
	var live []json.RawMessage
	stage := func(ctx context.Context) error {
		for _, op := range input["ops"].([]map[string]any) {
			raw, _ := json.Marshal(op)
			undo, err := driver.Apply(ctx, raw)
			if err != nil {
				if lost := device.UndoOf(err); lost != nil {
					live = append(live, lost)
					outcome.Records = append(outcome.Records, map[string]any{"undo": decoded(lost), "ok": false})
				}
				return err
			}
			live = append(live, undo)
			outcome.Records = append(outcome.Records, map[string]any{"undo": decoded(undo), "ok": true})
		}
		return nil
	}
	var err error
	if batch, ok := input["batch"].(bool); ok && !batch {
		err = stage(ctx)
	} else {
		err = driver.Batch(ctx, false, stage)
	}
	if err != nil {
		outcome.Error = goErrorOf(err)
	}
	if always, _ := input["always_rollback"].(bool); err == nil && !always {
		return outcome
	}
	rollbackErr := driver.Batch(ctx, true, func(ctx context.Context) error {
		for i := len(live) - 1; i >= 0; i-- {
			if string(live[i]) == "{}" || string(live[i]) == "null" {
				continue
			}
			result, err := driver.Apply(ctx, live[i])
			if err != nil {
				outcome.Rollback = append(outcome.Rollback, map[string]any{"error": goErrorOf(err)})
				continue
			}
			outcome.Rollback = append(outcome.Rollback, map[string]any{"ok": decoded(result)})
		}
		return nil
	})
	if rollbackErr != nil {
		outcome.Rollback = append(outcome.Rollback, map[string]any{"batch_error": goErrorOf(rollbackErr)})
	}
	return outcome
}

// wantFixed fails unless cond holds, describing an intended difference.
func wantFixed(t *testing.T, cond bool, format string, args ...any) {
	t.Helper()
	if !cond {
		t.Errorf("intended difference not observed: "+format, args...)
	}
}

func outcomeText(o ikuaiOutcome) string {
	data, _ := json.Marshal(o)
	return fmt.Sprintf("%.600s", data)
}
