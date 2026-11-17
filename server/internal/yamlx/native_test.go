package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func parseNative(t *testing.T, src string) *Node {
	t.Helper()
	n, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParseNativePolicies(t *testing.T) {
	tests := []struct {
		name, src string
		want      *Node
	}{
		{"empty", "", Null()}, {"blank", "# comment\n", Null()},
		{"bom", "\ufeffname: 中文\n", Map(Str("name"), Str("中文"))},
		{"cr", "a: 1\rb: 2\r", Map(Str("a"), Int(1), Str("b"), Int(2))},
		{"implicit", "[yes, no, on, off, 0777, 1:30]", Seq(Str("yes"), Str("no"), Str("on"), Str("off"), Int(511), Str("1:30"))},
		{"timestamp", "2001-12-14", &Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: "2001-12-14"}},
		{"non-string keys", "{1: a, true: b, null: c}", Map(Int(1), Str("a"), Bool(true), Str("b"), Null(), Str("c"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if n := parseNative(t, tt.src); n.Kind == yaml.DocumentNode || !Equal(n, tt.want) {
				t.Fatalf("got %#v, want %#v", n, tt.want)
			}
		})
	}
	for _, src := range []string{"a\n---\nb\n", "---\n---\n", "a\n---\n"} {
		if _, err := Parse([]byte(src)); !errors.Is(err, ErrMultipleDocuments) {
			t.Fatalf("%q: %v", src, err)
		}
	}
	for _, src := range []string{"a: [", "a:\n b: 1\n c", "\xff", "x: *absent"} {
		_, err := Parse([]byte(src))
		var syntax *SyntaxError
		if !errors.Is(err, ErrSyntax) || !errors.As(err, &syntax) || syntax.Problem == "" {
			t.Fatalf("%q: %T %v", src, err, err)
		}
	}
}

func TestNativeDuplicateKeys(t *testing.T) {
	tests := []struct {
		name, src, key string
		line           int
	}{
		{"top", "dns: 1\ndns: 2\n", "dns", 2},
		{"nested", "dns:\n  enabled: true\n  enabled: false\n", "enabled", 3},
		{"quoted", "'中文': a\n\"中文\": b\n", "中文", 2},
		{"number", "0xB: a\n11: b\n", "11", 2},
		{"null", "null: a\n~: b\n", "~", 2},
		{"alias key", "? &k name\n: a\n? *k\n: b\n", "name", 3},
		{"collection", "? [a, b]\n: 1\n? [a, b]\n: 2\n", "<collection>", 3},
		{"merge repeated", "a: &a {x: 1}\nb:\n  <<: *a\n  <<: *a\n", "<<", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value any
			for _, load := range []func() error{func() error { _, err := Parse([]byte(tt.src)); return err }, func() error { return Unmarshal([]byte(tt.src), &value) }} {
				err := load()
				var e *SyntaxError
				if !errors.As(err, &e) || !errors.Is(err, ErrSyntax) || e.Line != tt.line || e.Column < 1 || err.Error() != fmt.Sprintf(msgDuplicateKey, tt.key, tt.line) {
					t.Fatalf("got %#v %v", e, err)
				}
			}
		})
	}
	parseNative(t, "{1: integer, '1': string, true: boolean}")
	parseNative(t, "base: &a {x: 1}\nuse: {<<: *a, x: 2}\n")
}

func TestNativeMihomoRoundTrip(t *testing.T) {
	src := "# 中文配置\r\nmixed-port: 7890 # 端口\r\n\r\nproxy-groups:\r\n  - &group\r\n    name: 选择\r\n    type: select\r\n    proxies: [DIRECT, yes, on]\r\ncopy: *group\r\ndns:\r\n  enable: true\r\n  nameserver: [https://dns.example.com/dns-query]\r\n# 结束\r\n"
	n := parseNative(t, src)
	data, err := Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(n, parseNative(t, string(data))) {
		t.Fatal("round trip changed values")
	}
	for _, text := range []string{"# 中文配置", "# 端口", "# 结束", "&group", "*group", "选择"} {
		if !bytes.Contains(data, []byte(text)) {
			t.Errorf("lost %q in %s", text, data)
		}
	}
	if bytes.Contains(data, []byte("\r")) {
		t.Fatal("Marshal must use LF")
	}
	if !strings.Contains(string(data), "  - &group") {
		t.Fatalf("not indent 2: %s", data)
	}
	if Get(n, "copy").Kind != yaml.AliasNode {
		t.Fatal("Marshal mutated input aliases")
	}
}

func TestNativeStringQuoting(t *testing.T) {
	for _, s := range []string{"yes", "on", "0777", "1:30", "1_000", "2001-12-14", "no", "off", "y", "N", "1:30.5", "\nhello\n", "\r", "a\u0085b", "a\u2028b", "a\u2029b", strings.Repeat("word ", 200)} {
		t.Run(fmt.Sprintf("%q", s), func(t *testing.T) {
			n := Str(s)
			data, err := Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			if !Equal(n, parseNative(t, string(data))) {
				t.Fatalf("string changed: %q", data)
			}
			if !strings.ContainsAny(s, "\r\n\u0085\u2028\u2029") && s != strings.Repeat("word ", 200) && data[0] != '"' && data[0] != '\'' {
				t.Fatalf("not quoted: %s", data)
			}
			if strings.HasPrefix(s, "word ") && bytes.Count(data, []byte("\n")) != 1 {
				t.Fatal("wrapped a long string")
			}
		})
	}
}

func TestNativeDecodeTypedAndGeneric(t *testing.T) {
	src := "date: 2001-12-14\ntext: 2001-12-14T00:00:00Z\nlist: [2001-12-14]\n"
	var generic map[string]any
	if err := Unmarshal([]byte(src), &generic); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"date": "2001-12-14", "text": "2001-12-14T00:00:00Z", "list": []any{"2001-12-14"}}
	if !reflect.DeepEqual(generic, want) {
		t.Fatalf("got %#v", generic)
	}
	var typed struct {
		Date time.Time   `yaml:"date"`
		Text any         `yaml:"text"`
		List []time.Time `yaml:"list"`
	}
	if err := Unmarshal([]byte(src), &typed); err != nil {
		t.Fatal(err)
	}
	if typed.Date.Format("2006-01-02") != "2001-12-14" || typed.Text != want["text"] || len(typed.List) != 1 || typed.List[0] != typed.Date {
		t.Fatalf("got %#v", typed)
	}
	n, err := Encode(struct {
		Name  string `yaml:"title"`
		Count int    `yaml:"count"`
	}{"中文", 3})
	if err != nil || !Equal(n, Map(Str("title"), Str("中文"), Str("count"), Int(3))) {
		t.Fatalf("Encode: %#v %v", n, err)
	}
	for _, dst := range []any{nil, 42, (*int)(nil)} {
		if err := Decode(n, dst); err == nil {
			t.Fatalf("invalid destination %v accepted", dst)
		}
	}
	if _, err := Marshal(make(chan int)); err == nil {
		t.Fatal("unsupported value accepted")
	}
}

func TestNativeBuildersNavigationAndClone(t *testing.T) {
	n := parseNative(t, "base: &base {a: 1, b: 2}\nother: &other {a: 3, c: 4}\nuse: {<<: [*base, *other], b: 5, 7: seven}\n")
	m := Get(n, "use")
	if !Equal(m, Map(Str("a"), Int(1), Str("b"), Int(5), Str("c"), Int(4), Int(7), Str("seven"))) {
		t.Fatal("merge semantics differ")
	}
	var order []string
	for k := range Pairs(m) {
		order = append(order, k)
	}
	if !reflect.DeepEqual(order, []string{"a", "b", "c"}) {
		t.Fatalf("pairs: %v", order)
	}
	count := 0
	for range Pairs(m) {
		count++
		break
	}
	if count != 1 {
		t.Fatal("iterator did not stop")
	}
	Set(m, "a", Str("new"))
	Set(m, "d", nil)
	if Get(m, "a").Value != "new" || Get(m, "d").Tag != "!!null" {
		t.Fatal("Set failed")
	}
	if !Delete(m, "a") || Get(m, "a") != nil || Delete(m, "missing") || Get(m, "7") != nil {
		t.Fatal("Delete/Get failed")
	}
	if !Equal(Get(n, "base"), Map(Str("a"), Int(1), Str("b"), Int(2))) {
		t.Fatal("mutation changed merge source")
	}
	alias := &Node{Kind: yaml.AliasNode, Alias: Str("original")}
	cloned := Clone(Seq(alias.Alias, alias, alias))
	cloned.Content[1].Value = "changed"
	if cloned.Content[0].Value != "original" || cloned.Content[2].Value != "original" || alias.Alias.Value != "original" || cloned.Content[1].Kind == yaml.AliasNode {
		t.Fatal("clone shares nodes")
	}
	cycle := Seq()
	cycle.Content = []*Node{cycle}
	if c := Clone(cycle); !Equal(c, Seq(Null())) {
		t.Fatalf("manual cycle clone: %#v", c)
	}
	for _, scalar := range []*Node{Int(1), Float(1), Bool(true), Null(), Str("1")} {
		data, err := Marshal(scalar)
		if err != nil || !Equal(scalar, parseNative(t, string(data))) {
			t.Fatalf("scalar %#v: %s %v", scalar, data, err)
		}
	}
	for _, f := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		n := Float(f)
		b, err := Marshal(n)
		if err != nil || !Equal(n, parseNative(t, string(b))) {
			t.Fatalf("nonfinite %s %v", b, err)
		}
	}
	if Equal(Int(1), Float(1)) || Equal(Int(1), Bool(true)) || !Equal(parseNative(t, "0x10"), Int(16)) || !Equal(parseNative(t, "1e2"), Float(100)) {
		t.Fatal("numeric equality changed tags or values")
	}
	if Get(nil, "x") != nil || Delete(nil, "x") || Resolve(nil) != nil || Clone(nil) != nil {
		t.Fatal("nil navigation")
	}
}

func TestNativeAliasSafety(t *testing.T) {
	for _, src := range []string{"&a [*a]", "a: &a {b: *a}", "a: &a [ &b [*a] ]"} {
		_, err := Parse([]byte(src))
		var e *SyntaxError
		if !errors.As(err, &e) || !errors.Is(err, ErrSyntax) || e.Line != 1 || e.Problem != fmt.Sprintf(msgRecursiveAlias, 1) {
			t.Fatalf("recursive %q: %v", src, err)
		}
	}
	cycle := Seq()
	alias := &Node{Kind: yaml.AliasNode, Value: "a", Alias: cycle, Line: 2}
	cycle.Content = []*Node{alias}
	var dst any
	for _, load := range []func() error{func() error { return Decode(cycle, &dst) }, func() error { _, err := Marshal(cycle); return err }} {
		if err := load(); !errors.Is(err, ErrSyntax) {
			t.Fatalf("manual cycle: %v", err)
		}
	}
	var b strings.Builder
	b.WriteString("a: &a [laugh, laugh, laugh, laugh, laugh, laugh, laugh, laugh, laugh]\n")
	for c := byte('b'); c <= 'j'; c++ {
		fmt.Fprintf(&b, "%c: &%c [%s]\n", c, c, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*%c, ", c-1), 9), ", "))
	}
	_, err := Parse([]byte(b.String()))
	var e *SyntaxError
	if !errors.As(err, &e) || e.Problem != fmt.Sprintf(msgExcessiveAliases, e.Line) {
		t.Fatalf("alias bomb: %v", err)
	}
	var manual Node
	if err := yaml.Unmarshal([]byte(b.String()), &manual); err != nil {
		t.Fatal(err)
	}
	if _, err := Marshal(&manual); !errors.Is(err, ErrSyntax) {
		t.Fatalf("manual alias bomb: %v", err)
	}
	cloned := Clone(manual.Content[0])
	nulls := 0
	var countNulls func(*Node)
	countNulls = func(n *Node) {
		if n.Tag == "!!null" {
			nulls++
		}
		for _, child := range n.Content {
			countNulls(child)
		}
	}
	countNulls(cloned)
	if nulls == 0 {
		t.Fatal("Clone did not truncate an over-budget manual alias expansion")
	}
	var rules strings.Builder
	rules.WriteString("rules:\n")
	for i := range 60000 {
		fmt.Fprintf(&rules, "  - DOMAIN,rule-%d.example.com,DIRECT\n", i)
	}
	// Over 100k ordinary nodes, but zero alias expansion: no size gate applies.
	rules.WriteString("additional-rules:\n")
	for i := range 60000 {
		fmt.Fprintf(&rules, "  - DOMAIN,extra-%d.example.com,DIRECT\n", i)
	}
	n := parseNative(t, rules.String())
	if len(Get(n, "rules").Content) != 60000 {
		t.Fatal("large plain document truncated")
	}
	if _, err := Marshal(n); err != nil {
		t.Fatal(err)
	}
}

func TestNativeTimestampAliasesAndInline(t *testing.T) {
	// One alias can have different destination types, including a merged date
	// in a typed struct and a generic timestamp elsewhere in the same document.
	var dst struct {
		Stamp any       `yaml:"stamp"`
		Date  time.Time `yaml:"date"`
		Use   struct {
			Date  time.Time `yaml:"date"`
			Other any       `yaml:"other"`
		} `yaml:"use"`
		Dates map[string]time.Time `yaml:"dates"`
	}
	src := "stamp: &stamp 2001-12-14\ndate: *stamp\nbase: &base {date: *stamp, other: *stamp}\nuse: {<<: [*base]}\ndates: {first: *stamp}\n"
	if err := Unmarshal([]byte(src), &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Stamp != "2001-12-14" || dst.Date.IsZero() || dst.Use.Date != dst.Date || dst.Use.Other != dst.Stamp || dst.Dates["first"] != dst.Date {
		t.Fatalf("typed alias decode: %#v", dst)
	}
	var inline struct {
		Extra map[string]any `yaml:",inline"`
		Date  time.Time      `yaml:"date"`
	}
	if err := Unmarshal([]byte("date: 2001-12-14\nother: 2001-12-14\n"), &inline); err != nil {
		t.Fatal(err)
	}
	if inline.Date.IsZero() || inline.Extra["other"] != "2001-12-14" {
		t.Fatalf("inline decode: %#v", inline)
	}
}

func TestNativeRejectsMalformedManualNodes(t *testing.T) {
	for _, n := range []*Node{Map(Str("odd")), {Kind: yaml.AliasNode}, {Kind: 99}, {Kind: yaml.DocumentNode}, {Kind: yaml.SequenceNode, Content: []*Node{nil}}} {
		var dst any
		for _, check := range []func() error{func() error { return Decode(n, &dst) }, func() error { _, err := Marshal(n); return err }, func() error { _, err := ToJSON(n); return err }} {
			if err := check(); !errors.Is(err, ErrSyntax) {
				t.Fatalf("manual node %#v: %v", n, err)
			}
		}
	}
}

func TestNativeDecodeTypeError(t *testing.T) {
	for _, src := range []string{"value: not-an-int\n", "value: [1, 2]\n", "value: {nested: 1}\n"} {
		var dst struct {
			Value int `yaml:"value"`
		}
		err := Unmarshal([]byte(src), &dst)
		var cause *yaml.TypeError
		if !errors.Is(err, ErrType) || errors.Is(err, ErrSyntax) || !errors.As(err, &cause) {
			t.Fatalf("lost native type error: %T %v", err, err)
		}
		if err.Error() != fmt.Sprintf(msgTypeMismatch, cause) {
			t.Fatalf("unexpected type error message: %v", err)
		}
	}
}
