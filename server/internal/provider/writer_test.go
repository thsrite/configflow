package provider

import (
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// Only the rendered provider content changes byte format in N7-4. Everything
// outside that exact result field, including cache updates and errors, is strict.
func writerOutcomeEqual(kind string, want, got any) bool {
	if yamlxtest.EqualJSON(want, got) {
		return true
	}
	if kind != "prepare_sub" && kind != "bundle" {
		return false
	}
	a, aok := want.(*jsonx.Object)
	b, bok := got.(*jsonx.Object)
	if !aok || !bok || a == nil || b == nil || a.Len() != 1 || b.Len() != 1 {
		return false
	}
	for _, key := range []string{"out", "output"} {
		x, present := a.Get(key)
		y, ok := b.Get(key)
		if !present || !ok {
			continue
		}
		if kind == "prepare_sub" {
			return renderedWriterEqual(x, y)
		}
		if xo, ok := x.(*jsonx.Object); ok {
			yo, ok := y.(*jsonx.Object)
			if !ok || xo == nil || yo == nil || !xo.Has("bundle") || !yo.Has("bundle") {
				return false
			}
			xc, yc := xo.Clone(), yo.Clone()
			xc.Delete("bundle")
			yc.Delete("bundle")
			return yamlxtest.EqualJSON(xc, yc) && renderedWriterListEqual(xo.Value("bundle"), yo.Value("bundle"))
		}
		return renderedWriterListEqual(x, y)
	}
	return false
}

func renderedWriterListEqual(want, got any) bool {
	a, aok := want.([]any)
	b, bok := got.([]any)
	if !aok || !bok || len(a) != len(b) {
		return false
	}
	for i := range a {
		if !renderedWriterEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func renderedWriterEqual(want, got any) bool {
	if yamlxtest.EqualJSON(want, got) {
		return true
	}
	a, aok := want.(*jsonx.Object)
	b, bok := got.(*jsonx.Object)
	if !aok || !bok || a == nil || b == nil || !a.Has("content") || !b.Has("content") {
		return false
	}
	x, xok := a.Value("content").(string)
	y, yok := b.Value("content").(string)
	if !xok || !yok {
		return false
	}
	if !providerWriterYAMLEqual(x, y) {
		return false
	}
	ac, bc := a.Clone(), b.Clone()
	ac.Delete("content")
	bc.Delete("content")
	return yamlxtest.EqualJSON(ac, bc)
}

func providerWriterYAMLEqual(a, b string) bool {
	left, err := yamlx.Parse([]byte(a))
	if err != nil || yamlx.Get(left, "proxies") == nil {
		return false
	}
	right, err := yamlx.Parse([]byte(b))
	return err == nil && yamlx.Equal(left, right)
}

func providerWriterFilesEqual(want, got *jsonx.Object) bool {
	if want.Len() != got.Len() {
		return false
	}
	for name, value := range want.All() {
		actual, ok := got.Get(name)
		if !ok {
			return false
		}
		if yamlxtest.EqualJSON(value, actual) {
			continue
		}
		a, aok := value.(string)
		b, bok := actual.(string)
		if !aok || !bok || !strings.Contains(name, "/providers/") || !strings.HasSuffix(name, ".yaml") || !providerWriterYAMLEqual(a, b) {
			return false
		}
	}
	return true
}

func TestRenderedWriterComparison(t *testing.T) {
	want := jsonx.Obj("output", jsonx.Obj("name", "Feed", "content", "proxies: [{name: A, port: 80}]", "cache_updates", []any{}))
	for _, tc := range []struct {
		name, content string
		updates       any
		equal         bool
	}{
		{"format", "proxies:\n  - port: 80\n    name: A\n", []any{}, true},
		{"type", "proxies: [{name: A, port: '80'}]", []any{}, false},
		{"missing", "proxies: [{name: A}]", []any{}, false},
		{"extra", "proxies: [{name: A, port: 80}]\nextra: true", []any{}, false},
		{"cache", "proxies: [{name: A, port: 80}]", []any{"changed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := jsonx.Obj("output", jsonx.Obj("name", "Feed", "content", tc.content, "cache_updates", tc.updates))
			if equal := writerOutcomeEqual("prepare_sub", want, got); equal != tc.equal {
				t.Fatalf("equal=%v want %v", equal, tc.equal)
			}
		})
	}
}

func TestProviderWriterFileComparison(t *testing.T) {
	name := "profiles/default/providers/feed.yaml"
	want := jsonx.Obj(name, "proxies: [{name: A}]", "state.json", "{\"x\":1}")
	for _, tc := range []struct {
		name  string
		files *jsonx.Object
		equal bool
	}{
		{"format", jsonx.Obj(name, "proxies:\n  - name: A\n", "state.json", "{\"x\":1}"), true},
		{"storage-bytes", jsonx.Obj(name, "proxies: [{name: A}]", "state.json", "{\"x\": 1}"), false},
		{"value", jsonx.Obj(name, "proxies: [{name: B}]", "state.json", "{\"x\":1}"), false},
		{"missing", jsonx.Obj(name, "proxies: [{name: A}]"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if equal := providerWriterFilesEqual(want, tc.files); equal != tc.equal {
				t.Fatalf("equal=%v want %v", equal, tc.equal)
			}
		})
	}
}
