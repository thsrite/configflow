//go:build spike

// Command splice checks the assumption behind the yamlx comment-preserving
// editor in ../../../n7-n8.md: yaml.v3 node positions (Line, Column) locate a
// scalar in the source text exactly (columns in runes, CRLF and CJK
// included), so a single-line scalar can be replaced in place and every
// other byte (comments, blank lines, layout, CRLF) is kept. Each case is
// re-parsed and compared with the expected value tree.
//
// Run: go run -tags spike ./design/spikes/n7n8/splice
package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// lineStarts returns the byte offset of every line (after \n).
func lineStarts(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// offset converts a 1-based line and rune column into a byte offset.
func offset(src string, starts []int, line, column int) int {
	o := starts[line-1]
	for c := 1; c < column; c++ {
		_, size := utf8.DecodeRuneInString(src[o:])
		o += size
	}
	return o
}

// scalarEnd finds the end of a single-line scalar token starting at o.
func scalarEnd(src string, o int, n *yaml.Node, flow bool) (int, bool) {
	lineEnd := strings.IndexAny(src[o:], "\r\n")
	if lineEnd < 0 {
		lineEnd = len(src) - o
	}
	line := src[o : o+lineEnd]
	switch n.Style {
	case yaml.DoubleQuotedStyle:
		for i := 1; i < len(line); i++ {
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '"' {
				return o + i + 1, true
			}
		}
		return 0, false
	case yaml.SingleQuotedStyle:
		for i := 1; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				return o + i + 1, true
			}
		}
		return 0, false
	case 0:
		end := len(line)
		if i := strings.Index(line, " #"); i >= 0 {
			end = i
		}
		if flow {
			if i := strings.IndexAny(line[:end], ",]}"); i >= 0 {
				end = i
			}
		}
		token := strings.TrimRight(line[:end], " \t")
		if token != n.Value { // multi-line or unexpected: refuse
			return 0, false
		}
		return o + len(token), true
	}
	return 0, false
}

func quote(s string) string {
	var n yaml.Node
	n.SetString(s)
	out, _ := yaml.Marshal(&n)
	return strings.TrimSuffix(string(out), "\n")
}

// replaceProxies sets listeners[*].proxy == old to new by splicing.
func replaceProxies(src, old, new string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return "", err
	}
	root := doc.Content[0]
	var listeners *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "listeners" {
			listeners = root.Content[i+1]
		}
	}
	if listeners.Kind == yaml.AliasNode {
		listeners = listeners.Alias
	}
	starts := lineStarts(src)
	type edit struct{ from, to int }
	var edits []edit
	for _, l := range listeners.Content {
		for i := 0; i+1 < len(l.Content); i += 2 {
			v := l.Content[i+1]
			if l.Content[i].Value != "proxy" || v.Value != old {
				continue
			}
			o := offset(src, starts, v.Line, v.Column)
			end, ok := scalarEnd(src, o, v, l.Style == yaml.FlowStyle)
			if !ok {
				return "", fmt.Errorf("unsupported scalar at line %d", v.Line)
			}
			edits = append(edits, edit{o, end})
		}
	}
	for i := len(edits) - 1; i >= 0; i-- {
		src = src[:edits[i].from] + quote(new) + src[edits[i].to:]
	}
	return src, nil
}

func decode(src string) any {
	var v any
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		return err
	}
	return v
}

func main() {
	cases := []struct{ name, src string }{
		{"block", "# 入口\nlisteners:\n  - name: in\n    type: mixed\n    proxy: 旧组 # 注释\n\n  - name: in2\n    proxy: other\n"},
		{"crlf", "# head\r\nlisteners:\r\n  - name: x\r\n    proxy: 旧组 # keep\r\n\r\n# tail\r\n"},
		{"cjk-before", "listeners:\n  - {name: 入口一, proxy: 旧组}\n  - name: 入口二\n    proxy: \"旧组\"\n"},
		{"single-quoted", "listeners:\n- name: a\n  proxy: '旧组'  # x\n"},
		{"anchor", "base: &l\n  - name: a\n    proxy: 旧组\nlisteners: *l\n"},
		{"first-key", "listeners:\n  - proxy: 旧组\n    name: a\n"},
		{"escaped", "listeners:\n  - name: a\n    proxy: \"旧\\u7ec4\"\n"},
	}
	for _, c := range cases {
		out, err := replaceProxies(c.src, "旧组", "新 组: #1")
		if err != nil {
			fmt.Printf("%-14s refused: %v\n", c.name, err)
			continue
		}
		// Expected tree: the original with only listeners[*].proxy replaced.
		want := decode(c.src).(map[string]any)
		var items []any
		for _, l := range want["listeners"].([]any) {
			m := map[string]any{}
			for k, v := range l.(map[string]any) {
				m[k] = v
			}
			if m["proxy"] == "旧组" {
				m["proxy"] = "新 组: #1"
			}
			items = append(items, m)
		}
		want["listeners"] = items
		got := decode(out)
		kept := strings.Count(out, "#") >= strings.Count(c.src, "#") &&
			strings.Count(out, "\r\n") == strings.Count(c.src, "\r\n") &&
			strings.Count(out, "\n") == strings.Count(c.src, "\n")
		fmt.Printf("%-14s value_ok=%v layout_kept=%v out=%s\n", c.name, reflect.DeepEqual(got, any(want)), kept, strconv.Quote(out))
	}
}
