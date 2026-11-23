package substore

import (
	"testing"
)

// TestReviewO18T1dNodesDamaged: a node list that is not a list, or holds an
// element that is not a node object (null included), fails publication
// closed; a node with a member of the wrong type does not.
func TestReviewO18T1dNodesDamaged(t *testing.T) {
	tests := []struct {
		data    string
		damaged bool
	}{
		{`{"nodes":"abc"}`, true},
		{`{"nodes":{}}`, true},
		{`{"nodes":5}`, true},
		{`{"nodes":[1]}`, true},
		{`{"nodes":[{"name":"a","type":"ss","server":"192.0.2.1","port":1},"x"]}`, true},
		{`{"nodes":[{"name":"a"},null]}`, true},
		{`{"nodes":[null]}`, true},
		{`{"nodes":[ null , {"name":"a"}]}`, true},
		{`{"nodes":[[]]}`, true},
		{`{"nodes":[{"name":5,"port":"x"}]}`, false},
		{`{"nodes":[ {"name":"a"} ]}`, false},
		{`{"nodes":[]}`, false},
		{`{"nodes":null}`, false},
		{`{}`, false},
		{`{"count":"x","metadata":5}`, false},
	}
	for _, tt := range tests {
		cache := decodeCacheData("s", []byte(tt.data))
		if cache == nil {
			t.Fatalf("%s: unreadable", tt.data)
		}
		if got := cache.NodesDamaged(); got != tt.damaged {
			t.Errorf("%s: NodesDamaged = %v, want %v (issues %v)", tt.data, got, tt.damaged, cache.Issues)
		}
	}
}

// TestReviewO18T1dMetadataURLAbsent: only a metadata object without a url
// member is a legacy cache whose URL is not compared; null is a stored URL.
func TestReviewO18T1dMetadataURLAbsent(t *testing.T) {
	tests := []struct {
		data   string
		absent bool
	}{
		{`{"metadata":{"subscription_name":"s","fetched_at":"2026-01-01T00:00:00Z"}}`, true},
		{`{"metadata":{"subscription_name":"s","url":null}}`, false},
		{`{"metadata":{"url":"https://example.com/s"}}`, false},
		{`{"metadata":{"url":5}}`, false},
	}
	for _, tt := range tests {
		cache := decodeCacheData("s", []byte(tt.data))
		if cache == nil || cache.Metadata == nil {
			t.Fatalf("%s: no metadata", tt.data)
		}
		if cache.Metadata.URLAbsent != tt.absent {
			t.Errorf("%s: URLAbsent = %v", tt.data, cache.Metadata.URLAbsent)
		}
		if clone := cloneCache(cache); clone.Metadata.URLAbsent != tt.absent {
			t.Errorf("%s: clone lost URLAbsent", tt.data)
		}
	}
}
