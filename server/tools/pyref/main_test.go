package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditFindsAliasedAndNestedExec(t *testing.T) {
	source := []byte(`package test
import (cmd "os/exec"; "os"; y "configflow/internal/yamlx/yamlxtest")
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
