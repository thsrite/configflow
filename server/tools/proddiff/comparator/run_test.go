package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProcess struct {
	mu     sync.Mutex
	killed bool
}

func (p *fakeProcess) Exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

func (p *fakeProcess) Wait() {}

func (p *fakeProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killed = true
}

// mainFixture drives run.main with process and network boundaries replaced;
// every file belongs to its temporary tree.
type mainFixture struct {
	t                 *testing.T
	root, parity      string
	r                 *run
	out               bytes.Buffer
	launches          [][]string
	manifests         []string
	candidateManifest string
	answer            func(port int, method, path string) reply
	cases             []testCase
	generatorFound    map[[3]string]bool
	workerArgs        []any
}

func newMainFixture(t *testing.T) *mainFixture {
	f := &mainFixture{t: t, root: t.TempDir()}
	f.parity = filepath.Join(f.root, "parity")
	for _, dir := range []string{f.parity, filepath.Join(f.root, "snapshot"), filepath.Join(f.root, "runs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.write("proddiff-normalize.json", "[]")
	f.write("proddiff-readonly-post.json", "{}")
	f.write("proddiff-coverage.txt", "")
	f.write("proddiff-generator-coverage.json", "{}")
	f.write("proddiff-baseline-go.txt", "# header\n")
	r := newRun(f.root)
	r.parity, r.private, r.stdout = f.parity, filepath.Join(f.root, "runs"), &f.out
	r.cmp = testComparator(t)
	r.getenv = func(name string) string {
		return map[string]string{"PRODDIFF_BASE_BINARY": "/synthetic/base-configflow", "PRODDIFF_GENERATOR": "/synthetic/generator",
			"PRODDIFF_BASE_GENERATOR": "/synthetic/base-generator"}[name]
	}
	r.assertNoEgress = func() error { return nil }
	r.readOnly = func(string) (bool, error) { return true, nil }
	r.digest = func(string) ([]byte, error) { return []byte("immutable"), nil }
	r.lockRuns = func(string) (func(), error) { return func() {}, nil }
	r.loadAvg = func() string { return "0.00 0.00 0.00 1/1 1" }
	r.sleep = func(time.Duration) {}
	r.copySnapshot = func(string, string) error { return nil }
	r.document = func(string) (*Map, error) { return docFrom(t, `{"profiles":{"default":{}}}`), nil }
	r.selfCheck = func(reply, map[string]reply) ([]string, error) { return nil, nil }
	r.enumerate = func([][]string, map[string]bool, *Map, string) ([]testCase, error) { return f.cases, nil }
	r.workers = func(snapshot, copiesParent, report, candidate string, reference []string, doc *Map, implemented map[routeKey]bool) (generatorResults, error) {
		f.workerArgs = []any{candidate, reference, implemented}
		found := map[[3]string]bool{}
		for k := range f.generatorFound {
			found[k] = true
		}
		return generatorResults{found: found, coverage: newMap(), samples: map[string]reply{}}, nil
	}
	var mu sync.Mutex
	r.launch = func(command, env []string, logPath string) (process, error) {
		mu.Lock()
		defer mu.Unlock()
		f.launches = append(f.launches, command)
		manifest := ""
		for _, item := range env {
			if strings.HasPrefix(item, "PRODDIFF_ROUTES=") {
				manifest = strings.TrimPrefix(item, "PRODDIFF_ROUTES=")
			}
		}
		f.manifests = append(f.manifests, manifest)
		routes := f.candidateManifest
		if command[0] == "/synthetic/base-configflow" {
			routes = f.baseManifest()
		}
		for name, text := range map[string]string{"": routes, ".ready": "", ".loops": "", ".startups": ""} {
			if err := os.WriteFile(manifest+name, []byte(text), 0o600); err != nil {
				return nil, err
			}
		}
		return &fakeProcess{}, nil
	}
	r.send = func(port int, method, path, token string, body *Map, timeout time.Duration) (reply, *transportError, error) {
		if method == "POST" && path == "/api/auth/login" {
			return jrep(t, `{"token":"synthetic-login"}`), nil, nil
		}
		if path == "/api/auth/status" {
			return jrep(t, `{"auth_enabled":true}`), nil, nil
		}
		return f.answer(port, method, path), nil, nil
	}
	f.r = r
	return f
}

func (f *mainFixture) baseManifest() string {
	var lines []string
	for _, line := range strings.Split(f.candidateManifest, "\n") {
		if line != "" && !strings.Contains(line, "candidate-only") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func (f *mainFixture) write(name, text string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.parity, name), []byte(text), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *mainFixture) run(args ...string) (int, *Map) {
	f.t.Helper()
	code, err := f.r.main(append([]string{filepath.Join(f.root, "snapshot"), "/synthetic/candidate-configflow"}, args...))
	if err != nil {
		f.t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(f.root, "runs", "run-*", "summary.json"))
	if len(matches) != 1 {
		f.t.Fatalf("summaries %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		f.t.Fatal(err)
	}
	summary, err := loadJSON(data)
	if err != nil {
		f.t.Fatal(err)
	}
	return code, summary.(*Map)
}

func TestMainComparesTwoGoProcessesAndCatchesInjectedChange(t *testing.T) {
	for _, candidateValue := range []int{1, 2} {
		f := newMainFixture(t)
		f.write("routes.tsv", "GET\t/api/synthetic\nGET\t/api/candidate-only\n")
		f.write("proddiff-coverage.txt", "GET /api/synthetic\n")
		f.candidateManifest = "GET\t/api/synthetic\nGET\t/api/candidate-only\n"
		f.cases = []testCase{{Method: "GET", Route: "/api/synthetic", Path: "/api/synthetic"},
			{Method: "GET", Route: "/api/candidate-only", Path: "/api/candidate-only"}}
		f.answer = func(port int, method, path string) reply {
			if port == 15001 {
				return jrep(t, `{"value":1}`)
			}
			return jrep(t, `{"value":`+string(rune('0'+candidateValue))+`}`)
		}
		code, summary := f.run()
		if candidateValue == 2 {
			if code != 1 || counter(summary, "new") != 1 {
				t.Fatalf("injected change: code %d new %d", code, counter(summary, "new"))
			}
			if !strings.Contains(f.out.String(), "NEW\tGET /api/synthetic\tbody.value\tvalue") {
				t.Fatal(f.out.String())
			}
			continue
		}
		if code != 0 {
			t.Fatalf("code %d: %s", code, f.out.String())
		}
		if f.launches[0][0] != "/synthetic/base-configflow" || !strings.HasSuffix(f.manifests[0], "/base-routes.tsv") ||
			f.launches[1][0] != "/synthetic/candidate-configflow" || !strings.HasSuffix(f.manifests[1], "/go-routes.tsv") {
			t.Fatal(f.launches, f.manifests)
		}
		if f.workerArgs[0] != "/synthetic/generator" || f.workerArgs[1].([]string)[0] != "/synthetic/base-generator" {
			t.Fatal(f.workerArgs)
		}
		if implemented := f.workerArgs[2].(map[routeKey]bool); len(implemented) != 1 || !implemented[routeKey{"GET", "/api/synthetic"}] {
			t.Fatal(implemented)
		}
		if !pyEqual(getOr(summary, "mode", nil), "go") || counter(summary, "stale") != 0 || counter(summary, "candidate_only_routes") != 1 ||
			counter(summary, "coverage_regressions") != 0 || summary.Has("python_threads_started") {
			t.Fatal(dumpJSON(summary, dumpOptions{indent: -1}))
		}
		responses, _ := filepath.Glob(filepath.Join(f.root, "runs", "run-*", "responses.jsonl"))
		data, _ := os.ReadFile(responses[0])
		if !strings.Contains(string(data), `"base": [200`) {
			t.Fatal(string(data))
		}
		copies, _ := filepath.Glob(filepath.Join(f.root, "runs", "run-*", "copies-*"))
		if len(copies) != 0 {
			t.Fatal("copies left behind", copies)
		}
	}
}

func TestMainRejectsStaleButAcceptsMatchingAndRecordedBaselines(t *testing.T) {
	difference := [3]string{"GET /synthetic", "body.value", "value"}
	obsolete := [3]string{"GET /synthetic", "body.old", "missing-in-go"}
	tests := []struct {
		name     string
		baseline [][3]string
		record   bool
		want     int
	}{
		{"matching", [][3]string{difference}, false, 0},
		{"stale-only", [][3]string{difference, obsolete}, false, 1},
		{"new-only", nil, false, 1},
		{"explicit-record", [][3]string{obsolete}, true, 0},
		{"agent-identities", nil, false, 0},
		{"agent-content-mutation", nil, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMainFixture(t)
			agentCase := strings.HasPrefix(tt.name, "agent-")
			var routes []string
			if agentCase {
				routes = sortedStrings(agentComparisonRoutes)
			}
			manifest := ""
			for _, route := range routes {
				manifest += "GET\t" + route + "\n"
			}
			f.write("routes.tsv", manifest)
			f.candidateManifest = manifest
			var baseline strings.Builder
			for _, entry := range tt.baseline {
				baseline.WriteString(strings.Join(entry[:], "\t") + "\tSynthetic accepted difference\n")
			}
			f.write("proddiff-baseline-go.txt", baseline.String())
			if !agentCase {
				f.generatorFound = map[[3]string]bool{difference: true}
			}
			for i := 0; i < 2; i++ {
				for _, route := range routes {
					f.cases = append(f.cases, testCase{Method: "GET", Route: route, Path: route})
				}
			}
			f.answer = func(port int, method, path string) reply {
				suffix, version := "2Z", "b"
				if port == 15001 {
					suffix, version = "1Z", "a"
				}
				value := map[string]any{"status": "changed", "checked_at": "2026-01-01T00:00:00.00000" + suffix,
					"version": strings.Repeat(version, 16), "files": []any{}}
				if path == "/api/agents/<agent_id>/push-diff" {
					if tt.name == "agent-content-mutation" && port == 15002 {
						value["files"] = []any{map[string]any{"path": "synthetic.yaml"}}
					}
					return jrep(t, value)
				}
				public := map[string]any{"content_sync": map[string]any{"status": value["status"], "checked_at": value["checked_at"]}}
				if path == "/api/agents" {
					return jrep(t, []any{public})
				}
				return jrep(t, public)
			}
			var args []string
			if tt.record {
				args = append(args, "--record-baseline")
			}
			code, summary := f.run(args...)
			if code != tt.want {
				t.Fatalf("code %d: %s", code, f.out.String())
			}
			if counter(summary, "stale") != boolInt(tt.name == "stale-only") ||
				counter(summary, "new") != boolInt(tt.name == "new-only" || tt.name == "agent-content-mutation") {
				t.Fatal(dumpJSON(summary, dumpOptions{indent: -1}))
			}
			if tt.record {
				text, _ := os.ReadFile(filepath.Join(f.parity, "proddiff-baseline-go.txt"))
				if strings.Contains(string(text), "body.old") || !strings.Contains(string(text), "body.value") {
					t.Fatal(string(text))
				}
			}
		})
	}
}

func TestMainFailsTheFrameworkOnABaselineWithoutReason(t *testing.T) {
	f := newMainFixture(t)
	f.write("routes.tsv", "")
	f.write("proddiff-baseline-go.txt", "GET /x\tbody\tvalue\topen\n")
	f.answer = func(int, string, string) reply { return jrep(t, "{}") }
	code, err := f.r.main([]string{filepath.Join(f.root, "snapshot"), "/synthetic/candidate-configflow"})
	if code != 2 || err == nil {
		t.Fatalf("code %d err %v", code, err)
	}
}
