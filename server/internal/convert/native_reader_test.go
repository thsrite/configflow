package convert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"configflow/internal/model"
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
		{"duplicate", "type: ss\nport: 80\nport: 443", yamlx.ErrPolicy},
		{"shape", "type: ss\nx: !!str [x]", yamlx.ErrPolicy},
		{"merge", "type: ss\n<<: 7", yamlx.ErrPolicy},
		{"explicit scalar", "type: ss\nx: !!bool yes", yamlx.ErrPolicy},
		{"self", "type: ss\na: &a [*a]", yamlx.ErrAliasRecursion},
		{"diamond40", diamond.String(), yamlx.ErrAliasBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateCache(t)
			if got, err := StructuredProxy(tc.source); got != nil || !errors.Is(err, tc.kind) {
				t.Fatalf("parse: %v %v", got, err)
			}
			converter := &fakeConverter{}
			src := tc.source
			if got, err := NodeProxy(context.Background(), model.Node{Name: "example", ProxyString: &src}, converter); got != nil || !errors.Is(err, tc.kind) {
				t.Fatalf("conversion: %v %v", got, err)
			}
			if len(converter.calls) != 0 || cacheLen() != 0 {
				t.Fatal("invalid document was retried or cached")
			}
			if _, err := NodeDialer(model.Node{ProxyString: &src}); !errors.Is(err, tc.kind) {
				t.Fatalf("dialer: %v", err)
			}
		})
	}
}

func TestNativeProxySyntaxKeepsFallback(t *testing.T) {
	converter := &fakeConverter{}
	src := "type: [broken"
	value, err := NodeProxy(context.Background(), model.Node{Name: "example", ProxyString: &src}, converter)
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
		_, _ = StructuredProxy(source)
	})
}

func TestNativeProxyLinearAlias40PreservesSharing(t *testing.T) {
	isolateCache(t)
	var b strings.Builder
	b.WriteString("type: ss\na0: &a0 {v: example.com}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {child: *a%d}\n", i, i, i-1)
	}
	value, err := StructuredProxy(b.String())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 40; i++ {
		if yamlx.Resolve(yamlx.Get(yamlx.Get(value, fmt.Sprintf("a%d", i)), "child")) != yamlx.Resolve(yamlx.Get(value, fmt.Sprintf("a%d", i-1))) {
			t.Fatalf("alias %d copied or replaced", i)
		}
	}
	if cacheLen() != 0 {
		t.Fatal("shared containers reached the cache copy path")
	}
}

func TestProxyUsesSharedKeyProjectionAndBigIntegers(t *testing.T) {
	isolateCache(t)
	text := "type: ss\n1: first\n\"1\": second\nbig: !!int " + strings.Repeat("9", 100)
	got, err := StructuredProxy(text)
	if err != nil || scalarText(yamlx.Get(got, "1")) != "second" {
		t.Fatalf("shared projection: %v", err)
	}
	bigNode := yamlx.Get(got, "big")
	if bigNode == nil || bigNode.ShortTag() != "!!int" || bigNode.Value != strings.Repeat("9", 100) {
		t.Fatal("large integer lost precision")
	}
}

func TestProxyScalarAliasesKeepBackingDataShared(t *testing.T) {
	isolateCache(t)
	var b strings.Builder
	b.WriteString("type: ss\nvalue: &value " + strings.Repeat("x", 1<<20) + "\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "copy%d: *value\n", i)
	}
	got, err := StructuredProxy(b.String())
	if err != nil {
		t.Fatal(err)
	}
	original := scalarText(yamlx.Get(got, "value"))
	for i := 0; i < 100; i++ {
		if unsafe.StringData(scalarText(yamlx.Get(got, fmt.Sprintf("copy%d", i)))) != unsafe.StringData(original) {
			t.Fatalf("scalar alias %d copied its backing data", i)
		}
	}
}
