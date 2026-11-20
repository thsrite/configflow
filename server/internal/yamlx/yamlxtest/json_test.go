package yamlxtest

import (
	"encoding/json"
	"testing"

	"configflow/internal/testjson"
)

func TestEqualJSONRejectsValueAndShapeChanges(t *testing.T) {
	tests := []struct {
		a, b any
		want bool
	}{
		{true, json.Number("1"), false},
		{json.Number("1.00000000000000000001"), json.Number("1"), false}, {"1", json.Number("1"), false}, {nil, []any{}, false},
		{[]any(nil), []any{}, false},
		{nil, []any(nil), true},
		{[]any{json.Number("1"), json.Number("2")}, []any{json.Number("2"), json.Number("1")}, false},
		{testjson.Obj(testjson.Field{Key: "a", Value: true}), testjson.Obj(testjson.Field{Key: "a", Value: true}, testjson.Field{Key: "extra", Value: nil}), false},
		{testjson.Obj(testjson.Field{Key: "a", Value: json.Number("1")}, testjson.Field{Key: "b", Value: false}), testjson.Obj(testjson.Field{Key: "b", Value: false}, testjson.Field{Key: "a", Value: json.Number("1.0")}), true},
	}
	for _, tc := range tests {
		if got := EqualJSON(tc.a, tc.b); got != tc.want {
			t.Fatalf("%v/%v got %v", tc.a, tc.b, got)
		}
	}
}
