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

// HR O18 T1c (gate proddiff): members the API and the configuration export
// echo keep their presence, so "" / {} stay as stored and a missing member
// stays missing (Python returned the stored dict).
func TestReviewO18T1cEchoedMembersKeepPresence(t *testing.T) {
	tests := []struct {
		name string
		rec  func() any
		data []string
	}{
		{"rule", func() any { return &RuleConfig{} }, []string{`{"id":"r","value":"","remark":"","group_name":""}`, `{"id":"r"}`}},
		{"node", func() any { return &Node{} }, []string{`{"name":"n","remark":""}`, `{"name":"n"}`}},
		{"rule set", func() any { return &RuleLibraryItem{} }, []string{`{"id":"s","remark":""}`, `{"id":"s"}`}},
		{"aggregation", func() any { return &Aggregation{} }, []string{`{"id":"a","description":""}`, `{"id":"a"}`}},
		{"subscription", func() any { return &Subscription{} }, []string{`{"id":"s","description":""}`, `{"id":"s"}`}},
		{"domain discovery", func() any { return &DomainDiscovery{} }, []string{`{"regional_rulesets":{}}`, `{}`}},
	}
	for _, tt := range tests {
		for _, data := range tt.data {
			t.Run(tt.name+data, func(t *testing.T) {
				rec := tt.rec()
				if _, err := DecodeStored([]byte(data), rec, ""); err != nil {
					t.Fatal(err)
				}
				if out, err := Marshal(rec); err != nil || string(out) != data {
					t.Fatalf("%s written as %s (%v)", data, out, err)
				}
			})
		}
	}
	// A profile row without client_fixed_ips stays without it; a stored {}
	// stays {}.
	for _, data := range []string{`{}`, `{"client_fixed_ips":{}}`} {
		var profile Profile
		if _, err := DecodeStored([]byte(data), &profile, ""); err != nil {
			t.Fatal(err)
		}
		out, err := Marshal(&profile)
		if err != nil || bytes.Contains(out, []byte("client_fixed_ips")) != (data != `{}`) {
			t.Fatalf("%s: %s %v", data, out, err)
		}
	}
}
