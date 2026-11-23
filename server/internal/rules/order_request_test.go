package rules

import (
	"fmt"
	"slices"
	"testing"
)

func TestDecodeOrder(t *testing.T) {
	tests := []struct {
		body, err, position string
		ids                 []string
	}{
		{`{"ids":["b","a"],"position":"bottom"}`, "", "bottom", []string{"b", "a"}},
		{`{"ids":null,"nodes":[{"id":"a","name":"x"}]}`, "", "top", []string{"a"}},
		{`{"nodes":[null]}`, fmt.Sprintf(msgOrderObjects, "nodes"), "", nil},
		{`{"nodes":"a"}`, fmt.Sprintf(msgOrderObjects, "nodes"), "", nil},
		{`{}`, fmt.Sprintf(msgOrderObjects, "nodes"), "", nil},
		{`{"ids":"a"}`, msgOrderIDs, "", nil},
		{`{"ids":["a","a"]}`, msgOrderUnique, "", nil},
		{`{"ids":["a",null]}`, msgOrderUnique, "", nil},
		{`{"ids":["a"],"position":5}`, "", "", []string{"a"}},
		{`{"IDS":["a"],"Nodes":[{"id":"a"}]}`, fmt.Sprintf(msgOrderObjects, "nodes"), "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.body, func(t *testing.T) {
			got, err := DecodeOrder([]byte(tc.body), "nodes")
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil || !slices.Equal(got.IDs, tc.ids) || got.Position != tc.position {
				t.Fatalf("order %+v err %v", got, err)
			}
		})
	}
}
