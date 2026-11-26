package faultprobe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFaultBarrierIsTestBuildOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		tagged := strings.HasPrefix(string(source), "//go:build configflow_difftest\n")
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "Point" && !tagged && len(fn.Body.List) != 0 {
				t.Fatal("production Point must be an empty inlineable function")
			}
		}
		if (strings.Contains(string(source), "pause.") || strings.Contains(string(source), "events.jsonl") || strings.Contains(string(source), "os.Getenv")) && !tagged {
			t.Fatalf("barrier implementation outside difftest build: %s", name)
		}
	}
	if len(Catalog()) != 5 {
		t.Fatal("update the documented harness phase registry")
	}
	for _, name := range Catalog() {
		if !registered(name) || strings.ContainsAny(string(name), "/\\.") {
			t.Fatalf("invalid registered phase: %q", name)
		}
	}
}
