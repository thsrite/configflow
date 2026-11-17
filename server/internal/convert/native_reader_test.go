package convert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func TestNativeProxyPolicyErrorsDoNotReachSubStore(t *testing.T) {
	var diamond strings.Builder
	diamond.WriteString("type: ss\na0: &a0 [example.com]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&diamond, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, tc := range []struct {
		name, source string
		kind         error
	}{
		{"converted key collision", "type: ss\n1: first\n\"1\": second", yamlx.ErrPolicy},
		{"duplicate", "type: ss\nport: 80\nport: 443", yamlx.ErrPolicy},
		{"shape", "type: ss\nx: !!str [x]", yamlx.ErrPolicy},
		{"merge", "type: ss\n<<: 7", yamlx.ErrPolicy},
		{"explicit scalar", "type: ss\nx: !!bool yes", yamlx.ErrPolicy},
		{"self", "type: ss\na: &a [*a]", yamlx.ErrAliasRecursion},
		{"diamond40", diamond.String(), yamlx.ErrAliasBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateCache(t)
			for _, parse := range []func(string) (*jsonx.Object, error){ParseStructuredProxyString, InspectStructuredProxyString} {
				if got, err := parse(tc.source); got != nil || !errors.Is(err, tc.kind) {
					t.Fatalf("parse: %v %v", got, err)
				}
			}
			converter := &fakeConverter{}
			if got, err := ConvertNodeToMihomo(context.Background(), jsonx.Obj("name", "example", "proxy_string", tc.source), converter); got != nil || !errors.Is(err, tc.kind) {
				t.Fatalf("conversion: %v %v", got, err)
			}
			if len(converter.calls) != 0 || cacheLen() != 0 {
				t.Fatal("invalid document was retried or cached")
			}
			if _, err := rawDialer(jsonx.Obj("proxy_string", tc.source)); !errors.Is(err, tc.kind) {
				t.Fatalf("dialer: %v", err)
			}
		})
	}
}

func TestNativeProxySyntaxKeepsFallback(t *testing.T) {
	converter := &fakeConverter{}
	value, err := ConvertNodeToMihomo(context.Background(), jsonx.Obj("name", "example", "proxy_string", "type: [broken"), converter)
	if err != nil || value == nil || len(converter.calls) != 1 {
		t.Fatalf("%v %v %d", value, err, len(converter.calls))
	}
}

func FuzzStructuredProxyYAML(f *testing.F) {
	for _, s := range []string{"type: ss\nserver: example.com", "type: ss\na: &a [*a]", "type: ss\nx: !!str [x]", "type: [broken"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		// Only the reader is exercised; serialization of the returned graph could
		// independently expand aliases and is outside this reader's contract.
		_, _ = ParseStructuredProxyString(source)
	})
}

func TestNativeProxyLinearAlias40PreservesSharing(t *testing.T) {
	isolateCache(t)
	var b strings.Builder
	b.WriteString("type: ss\na0: &a0 {v: example.com}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {child: *a%d}\n", i, i, i-1)
	}
	value, err := ParseStructuredProxyString(b.String())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 40; i++ {
		if value.GetObject(fmt.Sprintf("a%d", i)).GetObject("child") != value.GetObject(fmt.Sprintf("a%d", i-1)) {
			t.Fatalf("alias %d copied or replaced", i)
		}
	}
	if cacheLen() != 0 {
		t.Fatal("shared containers reached the cache copy path")
	}
}
