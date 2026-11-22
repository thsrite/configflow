package model

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

// HR O18 T1c (gate fuzz): lenient reads accept invalid UTF-8; values kept as
// stored JSON (a damaged manifest, a node's protocol parameters) must be
// writable too, as U+FFFD like string leaves, and stable on the next read.
func TestReviewO18T1cInvalidUTF8KeptValuesAreWritable(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		rec  func() any
	}{
		{"damaged manifest", []byte("{\"ipv6_routes_state\":{\"0\":[{\"\x93\":\"\"}]}}"), func() any { return &Router{} }},
		{"node protocol parameter", []byte("{\"name\":\"n\",\"0\":\"\x95\"}"), func() any { return &Node{} }},
		{"raw member", []byte("{\"id\":\"r\",\"policy\":\"\x95\"}"), func() any { return &RuleLibraryItem{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := tt.rec()
			if _, err := DecodeStored(tt.data, first, ""); err != nil {
				t.Fatal(err)
			}
			out, err := Marshal(first)
			if err != nil || !utf8.Valid(out) || !bytes.Contains(out, []byte("�")) {
				t.Fatalf("write %q: %v", out, err)
			}
			again := tt.rec()
			if _, err := DecodeStored(out, again, ""); err != nil {
				t.Fatal(err)
			}
			if next, err := Marshal(again); err != nil || !bytes.Equal(next, out) {
				t.Fatalf("not stable: %q -> %q %v", out, next, err)
			}
		})
	}
}

