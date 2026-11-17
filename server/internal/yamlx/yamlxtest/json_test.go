package yamlxtest

import (
	"testing"

	"configflow/internal/jsonx"
)

func TestEqualJSONRejectsValueAndShapeChanges(t *testing.T) {
	tests := []struct {
		a, b any
		want bool
	}{
		{true, jsonx.Int(1), false},
		{jsonx.Number("1.00000000000000000001"), jsonx.Int(1), false}, {"1", jsonx.Int(1), false}, {nil, []any{}, false},
		{[]any(nil), []any{}, false},
		{nil, []any(nil), true},
		{[]any{jsonx.Int(1), jsonx.Int(2)}, []any{jsonx.Int(2), jsonx.Int(1)}, false},
		{jsonx.Obj("a", true), jsonx.Obj("a", true, "extra", nil), false},
		{jsonx.Obj("a", jsonx.Int(1), "b", false), jsonx.Obj("b", false, "a", jsonx.Float(1)), true},
	}
	for _, tc := range tests {
		if got := EqualJSON(tc.a, tc.b); got != tc.want {
			t.Fatalf("%v/%v got %v", tc.a, tc.b, got)
		}
	}
}
