package installer

import (
	"fmt"
	"strings"
	"testing"

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
