package yamlx

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestReaderErrorKindsStayDistinct(t *testing.T) {
	cycle := Seq()
	cycle.Content = []*Node{{Kind: yaml.AliasNode, Alias: cycle, Line: 1}}
	base := Seq(Str("leaf"))
	top := base
	for i := 0; i < 40; i++ {
		top = Seq(&Node{Kind: yaml.AliasNode, Alias: top}, &Node{Kind: yaml.AliasNode, Alias: top})
	}
	var source strings.Builder
	source.WriteString("x0: &x0 [leaf]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&source, "x%d: &x%d [*x%d, *x%d]\n", i, i, i-1, i-1)
	}
	cases := []struct {
		name, source string
		node         *Node
		want         error
	}{
		{"syntax", "a: [", nil, ErrSyntax},
		{"recursion", "a: &a [*a]", cycle, ErrAliasRecursion},
		{"budget", source.String(), top, ErrAliasBudget},
		{"duplicate", "a: 1\na: 2", Map(Str("a"), Int(1), Str("a"), Int(2)), ErrPolicy},
		{"shape", "!!str [a]", &Node{Kind: yaml.SequenceNode, Tag: "!!str"}, ErrPolicy},
		{"merge", "<<: 5", Map(&Node{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"}, Int(5)), ErrPolicy},
		{"scalar", "!!bool yes", &Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "yes", Style: yaml.TaggedStyle}, ErrPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := func(err error) {
				t.Helper()
				if !errors.Is(err, tc.want) {
					t.Fatalf("got %v want %v", err, tc.want)
				}
				for _, kind := range []error{ErrSyntax, ErrPolicy, ErrAliasRecursion, ErrAliasBudget} {
					if kind != tc.want && errors.Is(err, kind) {
						t.Fatalf("leaked kind %v: %v", kind, err)
					}
				}
			}
			if tc.source != "" {
				_, err := Parse([]byte(tc.source))
				check(err)
			}
			if tc.node != nil {
				var dst any
				check(Decode(tc.node, &dst))
				_, err := ToJSON(tc.node)
				check(err)
				_, err = Marshal(tc.node)
				check(err)
			}
		})
	}
}
