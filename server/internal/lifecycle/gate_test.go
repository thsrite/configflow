package lifecycle_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Each exemption is file:func (methods include their receiver). It applies only
// to joined control/fan-out work, never work abandoned when the caller returns.
var joinedGo = map[string]string{
	"internal/agents/ipv6/verification.go:Service.verificationSettings": "Every candidate capability worker waits through wg.Wait before returning; at most four capability RPCs run concurrently.",
	"cmd/configflow/service.go:run":                                     "Both HTTP Serve and shutdown workers are joined before run returns (force exit terminates the process).",
	"cmd/configflow/service.go:acquireInstance":                         "The signal watcher is joined via watched before returning, including signal races.",
	"cmd/configflow/service.go:drainServers":                            "Every Shutdown worker sends one result and all results are consumed before returning.",
	"internal/aggregations/render.go:render":                            "All bounded subscription workers join through workers.Wait before results are used.",
	"internal/health/latency.go:runLatencyTestsLocked":                  "All conversion workers join through wg.Wait before measurement or persistence.",
	"internal/refresh/scheduled.go:Service.RunRound":                    "All claimed workers join through wg.Wait, including the stopping branches.",
	"internal/rulefetch/transport.go:timeoutBody.Read":                  "Each read joins done; timeout closes the body and still consumes done before returning.",
	"tools/proddiff/comparator/run.go:comparisonResults":                "Offline test tool: the eight comparison workers join through wg.Wait before any result is used.",
	"tools/proddiff/comparator/main.go:runCommandLine":                  "Offline test tool: the signal watcher is joined via watcher before returning; on a signal it cleans up and exits the process.",
}

// File locks outside internal/repo and internal/instance, each with its owner.
var fileLocks = map[string]string{
	"tools/proddiff/comparator/run.go:lockRuns": "Offline test tool: a shared lock over private proddiff run directories; the first exclusive holder removes copies of killed runs.",
}

// These are non-periodic I/O/clock primitives or deliberately wall-clock fault
// helpers. Periodic loops and production retry scheduling must use a Clock.
var wallTime = map[string]string{
	"internal/devhttp/devhttp.go:watchdog.kick":              "Per-request inactivity watchdog, reset on incoming bytes and stopped by Do; not a periodic loop.",
	"internal/store/store.go:Store.BackupTo":                 "SQLite busy/locked backup polling uses the established 250ms retry interval under the synchronous backup API.",
	"internal/substore/env.go:systemClock.Sleep":             "Existing injected Sub-Store clock's real adapter; business readiness waits use that clock, scheduling uses Runtime Clock.",
	"internal/agents/agentfake/agentfake.go:faultResponse":   "Loopback Agent test helper deliberately delays a wire fault by wall time.",
	"internal/agents/agentfake/agentfake.go:Agent.ServeHTTP": "Loopback Agent test helper deliberately delays a lost response by wall time.",
	"internal/routers/routerfake/fake.go:Fake.ServeHTTP":     "Loopback RouterOS test helper delays wire faults/partial responses by wall time.",
	"internal/routers/routerfake/ikuai.go:Ikuai.ServeHTTP":   "Loopback iKuai test helper delays wire faults by wall time.",
}

type concurrencySite struct {
	kind, key string
	pos       token.Position
}

func concurrencySites(name string, source any) ([]concurrencySite, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		return nil, err
	}
	imports := map[string]string{}
	var found []concurrencySite
	add := func(kind, owner string, pos token.Pos) {
		found = append(found, concurrencySite{kind, name + ":" + owner, fset.Position(pos)})
	}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		alias := filepath.Base(path)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." && (path == "time" || path == "syscall" || path == "golang.org/x/sys/unix") {
			add("dot-import", "<package>", imp.Pos())
		}
		imports[alias] = path
	}
	for _, decl := range file.Decls {
		owner := "<package>"
		if fn, ok := decl.(*ast.FuncDecl); ok {
			owner = fn.Name.Name
			if fn.Recv != nil {
				recv := fn.Recv.List[0].Type
				if ptr, ok := recv.(*ast.StarExpr); ok {
					recv = ptr.X
				}
				if typ, ok := recv.(*ast.Ident); ok {
					owner = typ.Name + "." + owner
				}
			}
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			switch v := node.(type) {
			case *ast.GoStmt:
				add("go", owner, v.Pos())
			case *ast.SelectorExpr:
				// Inspect references too: assigning time.Sleep/Flock to a function value
				// must not bypass the policy. A local variable shadowing an import isn't it.
				id, ok := v.X.(*ast.Ident)
				if !ok || id.Obj != nil {
					break
				}
				path := imports[id.Name]
				if v.Sel.Name == "Flock" && (path == "syscall" || path == "golang.org/x/sys/unix") {
					add("flock", owner, v.Pos())
				}
				if path == "time" && (v.Sel.Name == "Sleep" || v.Sel.Name == "AfterFunc") {
					add("wall-time", owner, v.Pos())
				}
			}
			return true
		})
	}
	return found, nil
}

func concurrencyAllowed(site concurrencySite) bool {
	file, _, _ := strings.Cut(site.key, ":")
	switch site.kind {
	case "go":
		return filepath.ToSlash(filepath.Dir(file)) == "internal/lifecycle" || joinedGo[site.key] != ""
	case "flock":
		return filepath.ToSlash(filepath.Dir(file)) == "internal/repo" || filepath.ToSlash(filepath.Dir(file)) == "internal/instance" ||
			fileLocks[site.key] != ""
	case "wall-time":
		return wallTime[site.key] != ""
	}
	return false
}

func TestConcurrencyFacadeBoundary(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sites, err := concurrencySites(name, data)
		if err != nil {
			return err
		}
		count++
		for _, site := range sites {
			used[site.key] = true
			if !concurrencyAllowed(site) {
				t.Errorf("%s: forbidden %s in %s; use Spawn/Clock or document a joined fan-out", site.pos, site.kind, site.key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []map[string]string{joinedGo, wallTime, fileLocks} {
		for key, reason := range list {
			if strings.TrimSpace(reason) == "" || !used[key] {
				t.Errorf("empty or stale concurrency exemption: %s", key)
			}
		}
	}
	t.Logf("scanned %d non-test Go files; exemptions: go=%d wall-time=%d", count, len(joinedGo), len(wallTime))
}

// policyFailure renders the same diagnostics used by the repository gate.
func policyFailure(name, source string) ([]string, error) {
	sites, err := concurrencySites(name, source)
	if err != nil {
		return nil, err
	}
	var failures []string
	for _, site := range sites {
		if !concurrencyAllowed(site) {
			failures = append(failures, fmt.Sprintf("%s:%s", site.kind, site.key))
		}
	}
	return failures, nil
}

func TestConcurrencyGateNegativeControls(t *testing.T) {
	tests := []struct {
		name, file, source string
		bad                int
	}{
		{"bare-go", "internal/example/work.go", `package example; func work(){go worker()}`, 1},
		{"allowed-runtime", "internal/lifecycle/worker.go", `package lifecycle; func work(){go worker()}`, 0},
		{"lookalike-package", "internal/example/work.go", `package lifecycle; func work(){go worker()}`, 1},
		{"aliased-ops", "internal/example/work.go", `package example; import(t "time"; s "syscall"); func work(){t.Sleep(1);t.AfterFunc(1,func(){});s.Flock(1,2)}`, 3},
		{"function-values", "internal/example/work.go", `package example; import(t "time"; u "golang.org/x/sys/unix"); var sleep=t.Sleep; var after=t.AfterFunc; var flock=u.Flock`, 3},
		{"dot-import", "internal/example/work.go", `package example; import . "time"; func work(){Sleep(1)}`, 1},
		{"repo-flock", "internal/repo/lock.go", `package repo; import u "golang.org/x/sys/unix";func work(){u.Flock(1,2)}`, 0},
		{"instance-flock", "internal/instance/instance.go", `package instance; import s "syscall";func work(){s.Flock(1,2)}`, 0},
		{"nested-package-flock", "internal/repo/child/lock.go", `package repo; import s "syscall";func work(){s.Flock(1,2)}`, 1},
		{"context-callback", "internal/example/work.go", `package example; import c "context";func work(){c.AfterFunc(nil,func(){})}`, 0},
		{"shadowed-import", "internal/example/work.go", `package example; import "time";func work(){time:=other;time.Sleep(1)}`, 0},
		{"script-string", "internal/example/script.go", "package example; const script = `syscall.Flock(1,2); go work(); time.Sleep(1)`", 0},
		{"unknown-method", "internal/example/work.go", `package example;import "time";type Worker struct{};func(w *Worker) work(){time.Sleep(1)}`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures, err := policyFailure(tt.file, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(failures) != tt.bad {
				t.Fatalf("failures=%v, want %d", failures, tt.bad)
			}
		})
	}
}
