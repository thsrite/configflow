package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

// TestChainProviderCaptureGroups compares the complete provider lowering output
// with Python, then exercises its generated sorting substitution on a node name.
func TestChainProviderCaptureGroups(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	cases := []struct{ Pattern, Name string }{
		{`(a)?(?(1)b|c)`, "ab"},
		{`(a)?(?(1)b|c)`, "c"},
		{"(?x:a # ignored ( parentheses\n(b))", "ab"},
		{"(?x:(a) # ignored ) (?P<ignored>\n(b))", "ab"},
		{`(?P<first>a)(b)`, "ab"},
		{`(?P<first>a)?(?(first)b|c)`, "ab"},
		{`(?P<first>a)?(?(first)b|c)`, "c"},
		{`\(a\)(b)`, "(a)b"},
		{`[()](a)`, "(a"},
		{`(?# ignored opening ()(a)`, "a"},
		{`(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)`, "abcdefghijkl"},
		{`(a)`, "z"},
		{`(a)`, "abz"},
	}
	input, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `
import json, re, sys
from backend.utils.proxy_chains import lower_chain_providers
out = []
for case in json.load(sys.stdin):
    p, name = case['Pattern'], case['Name']
    runtime = {'nodes': [{'id':'entry', 'name':'Entry'}], 'proxy_groups': [
        {'id':'chain', 'name':'Chain', '_chain_dialer': {'type':'node', 'id':'entry'}}]}
    main = {'proxy-providers': {'Source': {'type':'http', 'url':'https://example.com/nodes'}},
        'proxy-groups': [{'name':'Chain', 'type':'select', 'use':['Source'], 'filter':p+chr(96)+'z'}]}
    lower_chain_providers(runtime, main)
    renamed = name
    for rule in main['proxy-providers']['Chain_Source']['override']['proxy-name']:
        target = re.sub(r'\$(\d+)', lambda m: r'\g<'+m[1]+'>', rule['target'])
        renamed = re.sub(rule['pattern'], target, renamed)
    out.append({'groups': re.compile(p).groups, 'main': main,
        'renamed': renamed})
json.dump(out, sys.stdout)
`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python provider boundary: %v: %s", err, stderr.String())
	}
	var expected []struct {
		Groups  int
		Main    json.RawMessage
		Renamed string
	}
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(cases) {
		t.Fatal("truncated Python results")
	}
	reference := regexp.MustCompile(`\$[0-9]+`)
	for i, tc := range cases {
		t.Run(tc.Pattern, func(t *testing.T) {
			compiled, err := rules.CompilePyRegex(tc.Pattern, false)
			if err != nil {
				t.Fatal(err)
			}
			if compiled.Groups() != expected[i].Groups {
				t.Fatalf("groups=%d Python=%d", compiled.Groups(), expected[i].Groups)
			}
			runtime := jsonx.Obj("nodes", []any{jsonx.Obj("id", "entry", "name", "Entry")}, "proxy_groups", []any{
				jsonx.Obj("id", "chain", "name", "Chain", "_chain_dialer", jsonx.Obj("type", "node", "id", "entry")),
			})
			main := jsonx.Obj("proxy-providers", jsonx.Obj("Source", jsonx.Obj("type", "http", "url", "https://example.com/nodes")),
				"proxy-groups", []any{jsonx.Obj("name", "Chain", "type", "select", "use", []any{"Source"}, "filter", tc.Pattern+"`z")})
			if err := subs.LowerChainProviders(runtime, main); err != nil {
				t.Fatal(err)
			}
			actual, err := jsonx.Compact(main)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(actual, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(expected[i].Main, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("provider output differs\nGo %s\nPython %s", actual, expected[i].Main)
			}
			// Read the group number from each emitted target, rather than
			// reconstructing it from Groups(). Apply every bucket in order.
			renamed := tc.Name
			for _, item := range main.GetObject("proxy-providers").GetObject("Chain_Source").GetObject("override").GetList("proxy-name") {
				rule := item.(*jsonx.Object)
				re, err := rules.CompilePyRegex(rule.GetString("pattern"), false)
				if err != nil {
					t.Fatal(err)
				}
				renamed, err = re.Sub(renamed, func(m *rules.Match) (string, error) {
					return reference.ReplaceAllStringFunc(rule.GetString("target"), func(ref string) string {
						n, err := strconv.Atoi(ref[1:])
						if err != nil {
							t.Fatal(err)
						}
						value, ok := m.Group(n)
						if !ok {
							t.Fatalf("missing emitted capture %s", ref)
						}
						return value
					}), nil
				}, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			wantName := "排序_1_" + tc.Name
			if tc.Name == "z" {
				wantName = "排序_2_z"
			}
			if renamed != expected[i].Renamed || renamed != wantName {
				t.Fatalf("renamed=%q Python=%q", renamed, expected[i].Renamed)
			}
		})
	}
}
