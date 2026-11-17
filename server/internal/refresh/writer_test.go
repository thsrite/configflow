package refresh

import (
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func sourceWriterOutcomeEqual(fn string, want, got *jsonx.Object) bool {
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
