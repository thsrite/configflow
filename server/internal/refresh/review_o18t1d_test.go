package refresh

import (
	"testing"

	"configflow/internal/model"
	"configflow/internal/substore"
)

// TestReviewO18T1dDamagedMetadataNeverClaims: a metadata member that is not
// an object, or a url that is neither text nor null, never claims a cache
// for any URL; an updated_at that is not text is no cache time.
func TestReviewO18T1dDamagedMetadataNeverClaims(t *testing.T) {
	tests := []struct {
		data      string
		url       string
		malformed bool
		stamp     bool
	}{
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":{"url":"https://example.com/s"}}`, "https://example.com/s", false, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":{"url":null}}`, "", false, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":{}}`, "", false, true},
		{`{"updated_at":"2026-01-01T00:00:00Z"}`, "", false, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":"x"}`, "", true, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":[]}`, "", true, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":5}`, "", true, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":{"url":5}}`, "", true, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":{"url":{"a":1}}}`, "", true, true},
		{`{"updated_at":"2026-01-01T00:00:00Z","metadata":{"url":true}}`, "", true, true},
		{`{"updated_at":5,"metadata":{"url":"https://example.com/s"}}`, "https://example.com/s", false, false},
		{`{"updated_at":null,"metadata":{"url":"https://example.com/s"}}`, "https://example.com/s", false, false},
	}
	for _, tt := range tests {
		var cache substore.Cache
		issues, err := model.DecodeStored([]byte(tt.data), &cache, "")
		if err != nil {
			t.Fatal(err)
		}
		cache.Issues = issues
		view, err := sourceCache(&cache, nil)
		if err != nil {
			t.Fatal(err)
		}
		url, malformed := cacheURL(view)
		got := ""
		if url != nil {
			got = *url
		}
		if got != tt.url || malformed != tt.malformed {
			t.Errorf("%s: cacheURL = %q, %v; want %q, %v", tt.data, got, malformed, tt.url, tt.malformed)
		}
		if stamp := !view.UpdatedAtDamaged && view.UpdatedAt != nil; stamp != tt.stamp {
			t.Errorf("%s: usable updated_at = %v, want %v", tt.data, stamp, tt.stamp)
		}
	}
}
