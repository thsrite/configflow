package agents

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"
)

func TestRetiredHooksFailCompilation(t *testing.T) {
	var declaration strings.Builder
	declaration.WriteString("package boundary\ntype Hooks struct {\n")
	shape := reflect.TypeOf(Hooks{})
	for i := range shape.NumField() {
		declaration.WriteString(shape.Field(i).Name + " int\n")
	}
	declaration.WriteString("}\n")
	for _, field := range []string{"AddressFollow", "AddressConfirmed", "InstallationProfile", "InstallationRegister", "HAServiceOperation", "AddressFollowTyped"} {
		t.Run(field, func(t *testing.T) {
			set := token.NewFileSet()
			file, err := parser.ParseFile(set, "hook.go", declaration.String()+"var _ = Hooks{"+field+": 1}\n", 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = new(types.Config).Check("boundary", set, []*ast.File{file}, nil)
			if field == "AddressFollowTyped" {
				if err != nil {
					t.Fatal("positive compile control", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "unknown field "+field) {
				t.Fatalf("obsolete hook must fail compilation: %v", err)
			}
		})
	}
}

func TestRegisterHooksReplacesNativeOwner(t *testing.T) {
	ResetHooks()
	t.Cleanup(ResetHooks)
	calls := 0
	RegisterHooks(Hooks{HAServiceOperationTyped: func(context.Context, *Manager, string, func() (json.RawMessage, error), bool) (json.RawMessage, error) {
		calls++
		return nil, nil
	}})
	RegisterHooks(Hooks{HAServiceOperationTyped: func(context.Context, *Manager, string, func() (json.RawMessage, error), bool) (json.RawMessage, error) {
		calls += 10
		return nil, nil
	}})
	if _, err := CurrentHooks().HAServiceOperationTyped(context.Background(), nil, "synthetic", nil, false); err != nil || calls != 10 {
		t.Fatal("replacement owner not selected once", calls, err)
	}
}
