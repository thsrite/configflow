package convert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestN73CustomReaderAliasBoundaries(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(nativeGenerationEnv(t)))
	var diamond, linear strings.Builder
	diamond.WriteString("mixed-port: 7890\na0: &a0 [leaf]\n")
	linear.WriteString("mixed-port: 7890\na0: &a0 {v: leaf}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&diamond, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
		fmt.Fprintf(&linear, "a%d: &a%d {child: *a%d}\n", i, i, i-1)
	}
	for _, test := range []struct {
		name, source string
		want         error
	}{{"depth40", diamond.String(), yamlx.ErrAliasBudget}, {"self", "mixed-port: 7890\na: &a [*a]", yamlx.ErrAliasRecursion}, {"non-string-key", "mixed-port: 7890\n1: value", yamlx.ErrPolicy}} {
		t.Run(test.name, func(t *testing.T) {
			config := &model.Config{Profile: &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: test.source}}}
			if out, err := MihomoConfig(context.Background(), config, MihomoOptions{SkipProviderPreflight: true}); out != "" || !errors.Is(err, test.want) {
				t.Fatalf("config: %q %v", out, err)
			}
			if out, err := MihomoDocument(context.Background(), config, MihomoOptions{SkipProviderPreflight: true}); out != nil || !errors.Is(err, test.want) {
				t.Fatalf("document: %v %v", out, err)
			}
		})
	}
	value, err := readMihomoBase(linear.String())
	if err != nil {
		t.Fatal(err)
	}
	object := value
	for i := 1; i <= 40; i++ {
		if yamlx.Resolve(yamlx.Get(yamlx.Get(object, fmt.Sprintf("a%d", i)), "child")) != yamlx.Get(object, fmt.Sprintf("a%d", i-1)) {
			t.Fatalf("linear alias %d expanded", i)
		}
	}
}

func FuzzCustomConfigYAML(f *testing.F) {
	for _, source := range []string{"mixed-port: 7890\nmode: rule", "a: &a [*a]", "v: !!binary YWJj", "1: bad", "when: 2001-01-01\nv: .inf"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) { _, _ = readMihomoBase(source) })
}
