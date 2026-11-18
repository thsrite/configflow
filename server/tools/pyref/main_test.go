package main

import (
	"configflow/internal/testgolden"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditFindsAliasedAndNestedExec(t *testing.T) {
	source := []byte(`package test
import (cmd "os/exec"; "os"
 "encoding/json"
 "strings"; y "configflow/internal/yamlx/yamlxtest")
func TestExample(){ _ = func(){cmd.Command("python", "-c", "pass")}; cmd.CommandContext(nil,os.Args[0]); y.NewOracle(nil,"oracle.py") }
`)
	calls, err := inspect("server/internal/example/x_test.go", source)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"exec.Command", "exec.CommandContext", "yamlxtest.NewOracle"} {
		if calls["server/internal/example/x_test.go:TestExample:"+name] != 1 {
			t.Fatalf("missed %s: %v", name, calls)
		}
	}
}
func TestAuditRejectsNewAndStaleCallSites(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "server/internal/example")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source := []byte("package example\nimport \"os/exec\"\nfunc TestExample(){exec.Command(\"python\")}\n")
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	scopes := []string{"server/internal/example"}
	if err := audit(root, scopes, nil); err == nil {
		t.Fatal("new process call silently allowed")
	}
	row := exception{ID: "one", File: "server/internal/example/x_test.go", Function: "TestExample", Callee: "exec.Command", Guard: "record callback only", Kind: "record-only", Reason: "reference stdout"}
	if err := audit(root, scopes, []exception{row}); err != nil {
		t.Fatal(err)
	}
	row.Count = 2
	if err := audit(root, scopes, []exception{row}); err == nil {
		t.Fatal("changed call count silently allowed")
	}
	row.Count = 1
	row.Function = "Removed"
	if err := audit(root, scopes, []exception{row}); err == nil {
		t.Fatal("stale registration silently allowed")
	}
}

func TestAuditRejectsInitializerAndFunctionValueBypasses(t *testing.T) {
	calls, err := inspect("x_test.go", []byte(`package test
import ("os/exec")
var process = exec.Command("python")
var start = exec.Command
func TestExample(){run:=exec.CommandContext;run(nil,"python"); _ = exec.Cmd{Path:"python"}}
`))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"x_test.go:<package>:exec.Command": 2, "x_test.go:TestExample:exec.CommandContext": 1, "x_test.go:TestExample:exec.Cmd": 1} {
		if calls[key] != want {
			t.Fatalf("missed %s: %v", key, calls)
		}
	}
	if _, err := inspect("dot_test.go", []byte(`package test
import . "os/exec"
func TestExample(){Command("python")}
`)); err == nil {
		t.Fatal("dot import bypass silently allowed")
	}
}

// TestAuditRepository is the ordinary go-test enforcement point, not a manual
// CLI convention. Each integrated group supplies its own scope/registry pair.
func TestAuditRepository(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := filepath.Glob(filepath.Join(root, "server/tools/pyref/scopes-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		data, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var scopes []string
		if err = json.Unmarshal(data, &scopes); err != nil {
			t.Fatal(err)
		}
		if len(scopes) == 0 {
			t.Fatalf("empty process audit scope manifest: %s", manifest)
		}
		group := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(manifest), "scopes-"), ".json")
		registryPath := filepath.Join(filepath.Dir(manifest), "live-exceptions-"+group+".json")
		data, err = os.ReadFile(registryPath)
		if err != nil {
			t.Fatal(err)
		}
		var rows []exception
		if err = json.Unmarshal(data, &rows); err != nil {
			t.Fatal(err)
		}
		if err = audit(root, scopes, rows); err != nil {
			t.Fatal(err)
		}
		if err = testgolden.AuditSources(root, scopes); err != nil {
			t.Fatal(err)
		}
	}
	registries, err := filepath.Glob(filepath.Join(root, "server/tools/pyref/live-exceptions-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, registry := range registries {
		group := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(registry), "live-exceptions-"), ".json")
		if _, err := os.Stat(filepath.Join(filepath.Dir(registry), "scopes-"+group+".json")); err != nil {
			t.Fatalf("registry lacks exact scope manifest: %s", registry)
		}
	}
}

func TestAuditRejectsHelperPackageProcesses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "server/internal/example/helper/process.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package helper\nimport cmd \"os/exec\"\nfunc probe() { cmd.Command(\"python3\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	scopes := []string{"server/internal/example"}
	if err := audit(root, scopes, nil); err == nil || !strings.Contains(err.Error(), "helper/process.go:probe:exec.Command") {
		t.Fatal("non-test helper constructor bypassed registration", err)
	}
	row := exception{ID: "helper", File: "server/internal/example/helper/process.go", Function: "probe", Callee: "exec.Command", Guard: "record callback only", Kind: "record-only", Reason: "reference output"}
	if err := audit(root, scopes, []exception{row}); err != nil {
		t.Fatal(err)
	}
}
