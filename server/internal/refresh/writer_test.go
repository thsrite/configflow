package refresh

import (
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func sourceWriterOutcomeEqual(fn string, want, got *jsonx.Object) bool {
	want, got = taskReceiptOutcome(want), taskReceiptOutcome(got)
	if yamlxtest.EqualJSON(want, got) {
		return true
	}
	if fn != "fetch_subscription_source" {
		return false
	}
	a, b := want.GetObject("ok"), got.GetObject("ok")
	if a == nil || b == nil {
		return false
	}
	x, y := a.GetList("__result__"), b.GetList("__result__")
	if len(x) != 4 || len(y) != 4 || !yamlxtest.EqualJSON(x[1:], y[1:]) {
		return false
	}
	xs, xok := x[0].(string)
	ys, yok := y[0].(string)
	if !xok || !yok {
		return false
	}
	left, err := yamlx.Parse([]byte(xs))
	if err != nil || yamlx.Get(left, "proxies") == nil {
		return false
	}
	right, err := yamlx.Parse([]byte(ys))
	if err != nil || !yamlx.Equal(left, right) {
		return false
	}
	ac, bc := a.Clone(), b.Clone()
	ac.Delete("__result__")
	bc.Delete("__result__")
	wc, gc := want.Clone(), got.Clone()
	wc.Delete("ok")
	gc.Delete("ok")
	return yamlxtest.EqualJSON(ac, bc) && yamlxtest.EqualJSON(wc, gc)
}

func TestSourceWriterComparison(t *testing.T) {
	result := func(text string, metadata any) *jsonx.Object {
		return jsonx.Obj("ok", jsonx.Obj("__result__", []any{text, "cache", metadata, "fixed-time"}))
	}
	want := result("proxies: [{name: A, port: 80}]", nil)
	for _, tc := range []struct {
		name, text string
		metadata   any
		equal      bool
	}{
		{"format", "proxies:\n  - port: 80\n    name: A\n", nil, true},
		{"type", "proxies: [{name: A, port: '80'}]", nil, false},
		{"metadata", "proxies: [{name: A, port: 80}]", jsonx.Obj("total", jsonx.Int(1)), false},
		{"extra-field", "proxies: [{name: A, port: 80}]\nextra: true", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := result(tc.text, tc.metadata)
			if equal := sourceWriterOutcomeEqual("fetch_subscription_source", want, got); equal != tc.equal {
				t.Fatalf("equal=%v want %v", equal, tc.equal)
			}
		})
	}
}

// Only migrated owner channels change from byte comparison to JSON structure.
// Other files, output values and errors retain the original comparator.
func taskReceiptOutcome(value *jsonx.Object) *jsonx.Object {
	result := value.Clone()
	after := value.GetObject("after")
	if after == nil {
		return result
	}
	files := after.Clone()
	for key, raw := range after.All() {
		if filepath.Base(key) != "task-status.json" && filepath.Base(key) != "subscription_health.json" && !strings.HasPrefix(key, "subscribes/") && !strings.HasPrefix(key, "subscription-downloads/") {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			continue
		}
		parsed, err := jsonx.Parse([]byte(text))
		if err != nil {
			continue
		}
		files.Set(key, parsed)
	}
	result.Set("after", files)
	return result
}
func TestTaskReceiptComparisonPreservesValues(t *testing.T) {
	want := jsonx.Obj("after", jsonx.Obj("task-status.json", `{"a":{"run_count":1,"last_status":"failed"}}`))
	for _, tc := range []struct {
		data  string
		equal bool
	}{
		{`{"a":{"last_status":"failed","run_count":1}}`, true},
		{`{"a":{"last_status":"success","run_count":1}}`, false},
		{`{"a":{"last_status":"failed","run_count":2}}`, false},
	} {
		got := jsonx.Obj("after", jsonx.Obj("task-status.json", tc.data))
		if sourceWriterOutcomeEqual("refresh_subscription", want, got) != tc.equal {
			t.Fatal(tc)
		}
	}
}

func TestSourceCacheComparisonPreservesEveryValue(t *testing.T) {
	for _, path := range []string{"subscribes/s.json", "subscription-downloads/x.json", "subscription_health.json"} {
		want := jsonx.Obj("after", jsonx.Obj(path, `{"id":"n","metadata":{"url":"https://example.com/a"},"nodes":[{"name":"A","port":80}]}`))
		for _, tc := range []struct {
			raw   string
			equal bool
		}{
			{`{"metadata":{"url":"https://example.com/a"},"nodes":[{"port":80,"name":"A"}],"id":"n"}`, true},
			{`{"metadata":{"url":"https://example.com/b"},"nodes":[{"port":80,"name":"A"}],"id":"n"}`, false},
			{`{"metadata":{"url":"https://example.com/a"},"nodes":[{"port":80,"name":"A"}],"id":"changed"}`, false},
			{`{"metadata":{"url":"https://example.com/a"},"nodes":[{"port":"80","name":"A"}],"id":"n"}`, false},
			{`{"metadata":{"url":"https://example.com/a"},"nodes":[{"port":80}],"id":"n"}`, false},
		} {
			if sourceWriterOutcomeEqual("refresh_subscription", want, jsonx.Obj("after", jsonx.Obj(path, tc.raw))) != tc.equal {
				t.Fatal(path, tc)
			}
		}
	}
	want := jsonx.Obj("after", jsonx.Obj("unowned.json", `{"a":1,"b":2}`))
	got := jsonx.Obj("after", jsonx.Obj("unowned.json", `{"b":2,"a":1}`))
	if sourceWriterOutcomeEqual("refresh_subscription", want, got) {
		t.Fatal("relaxed unowned file comparison")
	}
}
