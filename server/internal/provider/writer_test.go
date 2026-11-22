package provider

import (
	"maps"
	"strings"
	"testing"

	"configflow/internal/testjson"
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
	a, aok := want.(map[string]any)
	b, bok := got.(map[string]any)
	if !aok || !bok || len(a) != 1 || len(b) != 1 {
		return false
	}
	for _, key := range []string{"out", "output"} {
		x, present := a[key]
		y, ok := b[key]
		if !present || !ok {
			continue
		}
		if kind == "prepare_sub" {
			return renderedWriterEqual(x, y)
		}
		if xo, ok := x.(map[string]any); ok {
			yo, ok := y.(map[string]any)
			if !ok {
				return false
			}
			_, xHas := xo["bundle"]
			_, yHas := yo["bundle"]
			if !xHas || !yHas {
				return false
			}
			xc, yc := maps.Clone(xo), maps.Clone(yo)
			delete(xc, "bundle")
			delete(yc, "bundle")
			return yamlxtest.EqualJSON(xc, yc) && renderedWriterListEqual(xo["bundle"], yo["bundle"])
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
	a, aok := want.(map[string]any)
	b, bok := got.(map[string]any)
	if !aok || !bok {
		return false
	}
	x, xok := a["content"].(string)
	y, yok := b["content"].(string)
	if !xok || !yok {
		return false
	}
	if !providerWriterYAMLEqual(x, y) {
		return false
	}
	ac, bc := maps.Clone(a), maps.Clone(b)
	delete(ac, "content")
	delete(bc, "content")
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

func providerWriterFilesEqual(want, got map[string]any) bool {
	if len(want) != len(got) {
		return false
	}
	for name, value := range want {
		actual, ok := got[name]
		if !ok {
			return false
		}
		if yamlxtest.EqualJSON(value, actual) {
			continue
		}
		a, aok := value.(string)
		b, bok := actual.(string)
		if aok && bok && strings.HasSuffix(name, ".json") {
			// Cached nodes are plain Node records (O18 T1c): members are
			// written in declaration order (id first), Python appended id
			// last. Readers parse the file, so the value is compared.
			wantValue, errA := testjson.Parse([]byte(a))
			gotValue, errB := testjson.Parse([]byte(b))
			if errA != nil || errB != nil || !testjson.Equal(wantValue, gotValue) {
				return false
			}
			continue
		}
		if !aok || !bok || !strings.Contains(name, "/providers/") || !strings.HasSuffix(name, ".yaml") || !providerWriterYAMLEqual(a, b) {
			return false
		}
	}
	return true
}

func TestRenderedWriterComparison(t *testing.T) {
	want := map[string]any{"output": map[string]any{"name": "Feed", "content": "proxies: [{name: A, port: 80}]", "cache_updates": []any{}}}
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
			got := map[string]any{"output": map[string]any{"name": "Feed", "content": tc.content, "cache_updates": tc.updates}}
			if equal := writerOutcomeEqual("prepare_sub", want, got); equal != tc.equal {
				t.Fatalf("equal=%v want %v", equal, tc.equal)
			}
		})
	}
}

func TestProviderWriterFileComparison(t *testing.T) {
	name := "profiles/default/providers/feed.yaml"
	want := map[string]any{name: "proxies: [{name: A}]", "state.json": "{\"x\":1}"}
	for _, tc := range []struct {
		name  string
		files map[string]any
		equal bool
	}{
		{"format", map[string]any{name: "proxies:\n  - name: A\n", "state.json": "{\"x\":1}"}, true},
		{"storage-order", map[string]any{name: "proxies: [{name: A}]", "state.json": "{\"x\": 1}"}, true},
		{"storage-value", map[string]any{name: "proxies: [{name: A}]", "state.json": "{\"x\": 2}"}, false},
		{"value", map[string]any{name: "proxies: [{name: B}]", "state.json": "{\"x\":1}"}, false},
		{"missing", map[string]any{name: "proxies: [{name: A}]"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if equal := providerWriterFilesEqual(want, tc.files); equal != tc.equal {
				t.Fatalf("equal=%v want %v", equal, tc.equal)
			}
		})
	}
}
