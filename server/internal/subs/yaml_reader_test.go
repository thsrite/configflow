package subs

import (
	"configflow/internal/yamlx"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
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
	for _, tail := range []string{subscriptionAliasChain(), "loop: &loop [*loop]\n"} {
		source := "name: safe\ntype: ss\ndialer-proxy: DIRECT\n" + tail
		if n := ParseYAMLObject(source); n != nil {
			t.Fatal("object accepted unsafe graph")
		}
		if n := ParseURIList(source); len(n) != 0 {
			t.Fatal("URI list accepted unsafe graph")
		}
		if n := parseStructuredProxyString(source); n != nil {
			t.Fatal("structured proxy accepted unsafe graph")
		}
		if n, err := ExtractSubscriptionDialers(source); n != nil || err == nil {
			t.Fatalf("dialer graph was retained: %v", err)
		}
		if _, err := readSubscriptionYAML(source, false); err == nil || !strings.Contains(err.Error(), "引用") {
			t.Fatalf("missing native diagnostic: %v", err)
		}
		hosts := "hosts:\n"
		for _, line := range strings.Split(strings.TrimSuffix(tail, "\n"), "\n") {
			hosts += "  " + line + "\n"
		}
		record := ExtractSubscriptionHosts(hosts)
		if record == nil || record.GetList("skipped")[0].(*jsonx.Object).GetString("reason") != "parse_error" {
			t.Fatal("hosts did not reject graph")
		}
	}
}

func FuzzSubscriptionYAMLReaders(f *testing.F) {
	for _, source := range []string{"name: n\ntype: ss\n", "hosts:\n  n.example.com: 192.0.2.1\n", "proxies: [{name: n, dialer-proxy: R}]\nproxy-groups: [{name: R, proxies: [DIRECT]}]\n", subscriptionAliasChain(), "name: &self {type: ss, self: *self}"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 65536 {
			t.Skip()
		}
		ParseYAMLObject(source)
		ParseURIList(source)
		ExtractSubscriptionHosts(source)
		_, _ = ExtractSubscriptionDialers(source)
		parseStructuredProxyString(source)
	})
}

func TestNativeTabSeparationAndURIValues(t *testing.T) {
	for _, source := range []string{"name:\tx", "name:\tx\n"} {
		got := ParseYAMLObject(source)
		want := ParseYAMLObject("name: x")
		if !yamlxtest.EqualJSON(got, want) || got.GetString("name") != "x" {
			t.Fatal("Tab separator changed parsed name")
		}
	}
	uri := "trojan://pw@n.example.com:443#tab\there"
	value, err := readSubscriptionYAML("- \n- name: n\n- "+uri, false)
	if err != nil {
		t.Fatal(err)
	}
	list := value.([]any)
	if len(list) != 3 || list[0] != nil || list[1].(*jsonx.Object).GetString("name") != "n" || list[2] != uri {
		t.Fatalf("native Tab URI value lost: %v", value)
	}
	got := ParseURIList(uri)
	spaced := ParseURIList("trojan://pw@n.example.com:443#tab here")
	if len(got) != 1 || len(spaced) != 1 {
		t.Fatal("URI refused unexpectedly")
	}
	want := spaced[0].DeepCopy()
	want.Set("name", "tab\there")
	if !yamlxtest.EqualJSON(got[0], want) {
		t.Fatal("Tab in a literal URI label changed other node fields")
	}
}

func TestJSONSurrogatePairsRemainJSON(t *testing.T) {
	escaped := `{"name":"\ud83c\udded\ud83c\uddf0","type":"ss","server":"n.example.com","port":443}`
	literal := `{"name":"🇭🇰","type":"ss","server":"n.example.com","port":443}`
	if !json.Valid([]byte(escaped)) {
		t.Fatal("invalid test JSON")
	}
	for _, parse := range []func(string) *jsonx.Object{ParseYAMLObject, parseStructuredProxyString} {
		got, want := parse(escaped), parse(literal)
		if got == nil || !yamlxtest.EqualJSON(got, want) || got.GetString("name") != "🇭🇰" {
			t.Fatal("JSON surrogate pair was refused or changed")
		}
	}
	if got := ParseURIList(escaped); len(got) != 1 || got[0].GetString("name") != "🇭🇰" {
		t.Fatal("JSON URI-list entry lost")
	}
	flow := `{name: "\ud83c\udded\ud83c\uddf0", type: ss}`
	if json.Valid([]byte(flow)) {
		t.Fatal("YAML flow unexpectedly JSON")
	}
	if got := ParseYAMLObject(flow); got != nil {
		t.Fatal("non-JSON surrogate YAML accepted")
	}
	if _, err := readSubscriptionYAML(flow, false); !errors.Is(err, yamlx.ErrSyntax) || !strings.HasPrefix(err.Error(), "YAML 语法错误") {
		t.Fatalf("native diagnostic: %v", err)
	}
}
