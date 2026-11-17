package installer

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func TestN73ComposeYAMLGraphs(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("services: {agent: {options: *a40}}\n")
	for _, text := range []string{chain.String(), "services: &s {agent: *s}\n"} {
		if _, err := readCompose(text); err == nil {
			t.Fatal("unsafe Compose graph materialized")
		}
		if _, err := annotateCompose(text, jsonx.Obj("services", jsonx.Obj())); err == nil {
			t.Fatal("unsafe Compose graph annotated")
		}
	}
}

func FuzzComposeYAML(f *testing.F) {
	f.Add("services: {agent: {image: synthetic}}")
	f.Add("services: &s {agent: *s}")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = readCompose(text)
		_, _ = annotateCompose(text, jsonx.Obj("services", jsonx.Obj()))
	})
}

// Plain nesting has no alias limit; test the native limit without extrapolating
// Python's recursion ceiling. Durations are logged, not compared to wall time.
func TestN73PlainDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
			started := time.Now()
			source := "services: {}\nextra: " + nested
			_, err := readCompose(source)
			if depth < 10000 && err != nil || depth > 10000 && err == nil {
				t.Fatalf("readCompose: %v", err)
			}
			_, err = annotateCompose(source, jsonx.Obj("services", jsonx.Obj()))
			if (err == nil) != (depth <= 10000) {
				t.Fatalf("annotateCompose: %v", err)
			}
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}

func TestN73ComposeCompositeKeysAreNotMaterialized(t *testing.T) {
	text := "services: {}\n? [a,b]\n: ignored\n"
	if _, err := readCompose(text); err == nil || !strings.HasPrefix(err.Error(), "Compose YAML 无效：") {
		t.Fatalf("non-JSON key refusal: %v", err)
	}
	if _, err := annotateCompose(text, jsonx.Obj("services", jsonx.Obj())); err != nil {
		t.Fatal(err)
	}
}
