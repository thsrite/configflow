package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func subscriptionAliasChain() string {
	var b strings.Builder
	b.WriteString("x0: &x0 [leaf]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "x%d: &x%d [*x%d, *x%d]\n", i, i, i-1, i-1)
	}
	return b.String()
}

func TestSubscriptionYAMLEntrypointsRejectAliasAmplification(t *testing.T) {
	ops := sharedDialers()
	for _, tail := range []string{subscriptionAliasChain(), "loop: &loop [*loop]\n"} {
		source := "name: safe\ntype: ss\ndialer-proxy: DIRECT\n" + tail
		if n := ParseObject(source); n != nil {
			t.Fatal("object accepted unsafe graph")
		}
		if n := ParseList(source); len(n) != 0 {
			t.Fatal("URI list accepted unsafe graph")
		}
		if n, err := ops.Structured(source); err == nil && n != nil {
			t.Fatal("structured proxy accepted unsafe graph")
		}
		if n, err := ExtractDialers(source); n != nil || err == nil {
			t.Fatalf("dialer graph was retained: %v", err)
		}
		if _, err := readSubscriptionYAMLNode(source, false); err == nil || !strings.Contains(err.Error(), "引用") {
			t.Fatalf("missing native diagnostic: %v", err)
		}
		hosts := "hosts:\n"
		for _, line := range strings.Split(strings.TrimSuffix(tail, "\n"), "\n") {
			hosts += "  " + line + "\n"
		}
		record := ExtractHosts(hosts)
		if record == nil || len(record.Skipped) == 0 || record.Skipped[0].Reason != "parse_error" {
			t.Fatal("hosts did not reject graph")
		}
	}
}

func FuzzSubscriptionYAMLReaders(f *testing.F) {
	ops := sharedDialers()
	for _, source := range []string{"name: n\ntype: ss\n", "hosts:\n  n.example.com: 192.0.2.1\n", "proxies: [{name: n, dialer-proxy: R}]\nproxy-groups: [{name: R, proxies: [DIRECT]}]\n", subscriptionAliasChain(), "name: &self {type: ss, self: *self}"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 65536 {
			t.Skip()
		}
		ParseObject(source)
		ParseList(source)
		ExtractHosts(source)
		_, _ = ExtractDialers(source)
		_, _ = ops.Structured(source)
	})
}

func TestNativeTabSeparationAndURIValues(t *testing.T) {
	for _, source := range []string{"name:\tx", "name:\tx\n"} {
		got := ParseObject(source)
		want := ParseObject("name: x")
		if got == nil || !yamlx.Equal(got, want) || yamlx.Get(got, "name").Value != "x" {
			t.Fatal("Tab separator changed parsed name")
		}
	}
	uri := "trojan://pw@n.example.com:443#tab\there"
	node, err := readSubscriptionYAMLNode("- \n- name: n\n- "+uri, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(node.Content) != 3 || node.Content[0].ShortTag() != "!!null" || yamlx.Get(node.Content[1], "name").Value != "n" || node.Content[2].Value != uri {
		t.Fatalf("native Tab URI value lost: %s", yamlToRaw(t, node))
	}
	got := ParseList(uri)
	spaced := ParseList("trojan://pw@n.example.com:443#tab here")
	if len(got) != 1 || len(spaced) != 1 {
		t.Fatal("URI refused unexpectedly")
	}
	want := spaced[0].Node
	want.Name = "tab\there"
	if !testjson.Equal(want, got[0].Node) {
		t.Fatal("Tab in a literal URI label changed other node fields")
	}
}

func TestJSONSurrogatePairsRemainJSON(t *testing.T) {
	ops := sharedDialers()
	escaped := `{"name":"\ud83c\udded\ud83c\uddf0","type":"ss","server":"n.example.com","port":443}`
	literal := `{"name":"🇭🇰","type":"ss","server":"n.example.com","port":443}`
	if !json.Valid([]byte(escaped)) {
		t.Fatal("invalid test JSON")
	}
	parsers := []func(string) *yamlx.Node{
		ParseObject,
		func(s string) *yamlx.Node {
			n, err := ops.Structured(s)
			if err != nil {
				return nil
			}
			return n
		},
	}
	for _, parse := range parsers {
		got, want := parse(escaped), parse(literal)
		if got == nil || !yamlx.Equal(got, want) || yamlx.Get(got, "name").Value != "🇭🇰" {
			t.Fatal("JSON surrogate pair was refused or changed")
		}
	}
	if got := ParseList(escaped); len(got) != 1 || got[0].Node.Name != "🇭🇰" {
		t.Fatal("JSON URI-list entry lost")
	}
	flow := `{name: "\ud83c\udded\ud83c\uddf0", type: ss}`
	if json.Valid([]byte(flow)) {
		t.Fatal("YAML flow unexpectedly JSON")
	}
	if got := ParseObject(flow); got != nil {
		t.Fatal("non-JSON surrogate YAML accepted")
	}
	if _, err := readSubscriptionYAMLNode(flow, false); !errors.Is(err, yamlx.ErrSyntax) || !strings.HasPrefix(err.Error(), "YAML 语法错误") {
		t.Fatalf("native diagnostic: %v", err)
	}
}
