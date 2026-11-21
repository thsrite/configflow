package model

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestMapSetAllMatchesSequentialSet(t *testing.T) {
	type pair struct {
		name  string
		value string
	}
	tests := []struct {
		name    string
		initial string // "" leaves the Map zero
		pairs   []pair
	}{
		{"zero map, new keys", "", []pair{{"b", "1"}, {"a", "2"}}},
		{"zero map, empty sequence", "", nil},
		{"null map, empty sequence", "null", nil},
		{"null map, one key", "null", []pair{{"a", "1"}}},
		{"decoded map keeps positions and untouched bytes", `{"x": "keep" , "y":"old","z":"3"}`, []pair{{"y", "new"}, {"w", "4"}}},
		{"repeated name in sequence", `{"a":"1"}`, []pair{{"b", "1"}, {"a", "2"}, {"b", "3"}}},
		{"decoded map, empty sequence", `{ "a" : "1" }`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sequential, bulk Map[string]
			if tt.initial != "" {
				if err := json.Unmarshal([]byte(tt.initial), &sequential); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tt.initial), &bulk); err != nil {
					t.Fatal(err)
				}
			}
			before := bulk
			for _, p := range tt.pairs {
				sequential.Set(p.name, p.value)
			}
			bulk.SetAll(func(yield func(string, string) bool) {
				for _, p := range tt.pairs {
					if !yield(p.name, p.value) {
						return
					}
				}
			})
			want, err := Marshal(sequential)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Marshal(bulk)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) || !slices.Equal(bulk.Keys(), sequential.Keys()) {
				t.Fatalf("SetAll = %s %v, sequential Set = %s %v", got, bulk.Keys(), want, sequential.Keys())
			}
			if len(tt.pairs) > 0 {
				// Copy-on-write: the value copied before SetAll is unchanged.
				if beforeJSON, _ := Marshal(before); tt.initial != "" && string(beforeJSON) == string(got) {
					t.Fatalf("SetAll changed a shared copy: %s", beforeJSON)
				}
			}
		})
	}
}
