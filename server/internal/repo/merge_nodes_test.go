package repo

import (
	"bytes"
	"testing"
)

func TestMergeJSON(t *testing.T) {
	tests := []struct {
		name                        string
		current, baseline, proposed string
		want                        string
		changed                     bool
	}{
		{"caller unchanged keeps current bytes", `{"a": 1.0, "b":"é"}`, `{"a":1}`, `{"a":1}`, `{"a": 1.0, "b":"é"}`, false},
		{"concurrent member edits survive", `{"a":1,"b":2}`, `{"a":1,"b":1}`, `{"a":3,"b":1}`, `{"a":3,"b":2}`, true},
		{"removed member dropped when untouched", `{"a":1,"b":2}`, `{"a":1,"b":2}`, `{"b":2}`, `{"b":2}`, true},
		{"removed member kept when changed concurrently", `{"a":5,"b":2}`, `{"a":1,"b":2}`, `{"b":3}`, `{"a":5,"b":3}`, true},
		{"number text of new values kept", `{"a":1}`, `{"a":1}`, `{"a":1.50}`, `{"a":1.50}`, true},
		{"int and float differ", `{"a":1}`, `{"a":1}`, `{"a":1.0}`, `{"a":1.0}`, true},
		{"stale list append keeps concurrent items", `[{"id":"a"},{"id":"b"}]`, `[{"id":"a"}]`, `[{"id":"a"},{"id":"c"}]`,
			`[{"id":"a"},{"id":"b"},{"id":"c"}]`, true},
		{"item edit merged by id", `[{"id":"a","v":1,"w":1},{"id":"b"}]`, `[{"id":"a","v":1,"w":0}]`, `[{"id":"a","v":2,"w":0}]`,
			`[{"id":"a","v":2,"w":1},{"id":"b"}]`, true},
		{"integer ids", `[{"id":1,"v":"x"}]`, `[{"id":1,"v":"x"}]`, `[{"id":1,"v":"y"}]`, `[{"id":1,"v":"y"}]`, true},
		{"reorder follows proposed", `[{"id":"a"},{"id":"b"},{"id":"z"}]`, `[{"id":"a"},{"id":"b"}]`, `[{"id":"b"},{"id":"a"}]`,
			`[{"id":"b"},{"id":"a"},{"id":"z"}]`, true},
		{"plain values by value", `["x","y"]`, `["x"]`, `["x","w"]`, `["x","y","w"]`, true},
		{"type change replaces", `{"a":[1]}`, `{"a":[1]}`, `{"a":{"k":1}}`, `{"a":{"k":1}}`, true},
		{"new object member deep-merged", `{"a":{"x":1}}`, `{}`, `{"a":{"y":2}}`, `{"a":{"x":1,"y":2}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := []byte(tt.current)
			got, changed, err := MergeJSON(current, []byte(tt.baseline), []byte(tt.proposed))
			noError(t, err)
			if changed != tt.changed || string(got) != tt.want {
				t.Fatalf("got %s changed=%v", got, changed)
			}
			if !changed && &got[0] != &current[0] {
				t.Fatal("unchanged merge did not return current itself")
			}
		})
	}
	t.Run("absent current reads as null", func(t *testing.T) {
		got, changed, err := MergeJSON(nil, []byte(`1`), []byte(`2`))
		noError(t, err)
		if !changed || !bytes.Equal(got, []byte(`2`)) {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("inputs are not modified", func(t *testing.T) {
		current, baseline, proposed := []byte(`{"a":[1,2]}`), []byte(`{"a":[1]}`), []byte(`{"a":[1,3]}`)
		copies := [][]byte{bytes.Clone(current), bytes.Clone(baseline), bytes.Clone(proposed)}
		_, _, err := MergeJSON(current, baseline, proposed)
		noError(t, err)
		for i, data := range [][]byte{current, baseline, proposed} {
			if !bytes.Equal(data, copies[i]) {
				t.Fatal("input modified")
			}
		}
	})
}
