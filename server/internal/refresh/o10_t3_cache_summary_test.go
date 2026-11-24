package refresh

import (
	"os"
	"testing"

	"configflow/internal/model"
	"configflow/internal/substore"
)

// TestO10T3CacheSummaryMatchesFullCacheDecode pins that the task-row baseline
// read through substore.LoadCacheSummary (envelope only) equals the one read
// through the full typed cache decode for valid and malformed cache files.
func TestO10T3CacheSummaryMatchesFullCacheDecode(t *testing.T) {
	const at = `"2026-10-24T00:00:00Z"`
	node := `{"name":"n1","type":"ss","server":"192.0.2.1","port":443}`
	url := `"` + subURL + `"`
	tests := []struct {
		name string
		body string
	}{
		{"valid", `{"nodes":[` + node + `],"metadata":{"url":` + url + `},"updated_at":` + at + `}`},
		{"other url", `{"nodes":[` + node + `],"metadata":{"url":"https://other.example.com/x"},"updated_at":` + at + `}`},
		{"no nodes", `{"nodes":[],"metadata":{"url":` + url + `},"updated_at":` + at + `}`},
		{"nodes missing", `{"metadata":{"url":` + url + `},"updated_at":` + at + `}`},
		{"nodes not list", `{"nodes":{"a":1},"metadata":{"url":` + url + `},"updated_at":` + at + `}`},
		{"node not object", `{"nodes":[1],"metadata":{"url":` + url + `},"updated_at":` + at + `}`},
		{"metadata missing", `{"nodes":[` + node + `],"updated_at":` + at + `}`},
		{"metadata null", `{"nodes":[` + node + `],"metadata":null,"updated_at":` + at + `}`},
		{"metadata list", `{"nodes":[` + node + `],"metadata":[],"updated_at":` + at + `}`},
		{"url null", `{"nodes":[` + node + `],"metadata":{"url":null},"updated_at":` + at + `}`},
		{"url number", `{"nodes":[` + node + `],"metadata":{"url":7},"updated_at":` + at + `}`},
		{"updated_at missing", `{"nodes":[` + node + `],"metadata":{"url":` + url + `}}`},
		{"updated_at number", `{"nodes":[` + node + `],"metadata":{"url":` + url + `},"updated_at":5}`},
		{"updated_at null", `{"nodes":[` + node + `],"metadata":{"url":` + url + `},"updated_at":null}`},
		{"root list", `[]`},
		{"broken json", `{"nodes":[`},
		{"empty", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t, 60)
			path, err := substore.CachePath(f.ctx, f.repo, "s")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			sub := model.Subscription{ID: "s", URL: subURL}
			summaryAt, err := f.svc.SourceCacheAt(f.ctx, sub)
			if err != nil {
				t.Fatal(err)
			}
			f.env.Sources.LoadCacheSummary = nil
			fullAt, err := f.svc.SourceCacheAt(f.ctx, sub)
			if err != nil {
				t.Fatal(err)
			}
			if (summaryAt == nil) != (fullAt == nil) || (summaryAt != nil && *summaryAt != *fullAt) {
				t.Fatalf("summary=%v full=%v", deref(summaryAt), deref(fullAt))
			}
		})
	}
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
