package aggregations_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/api/aggregations"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

// TestReviewBatch48TypeErrors pins the messages of refused aggregation
// create/replace requests: a creation's legacy health_check_url is checked as
// the default probe URL before the member's own type, and a refused request
// leaves the catalog unchanged.
func TestReviewBatch48TypeErrors(t *testing.T) {
	tests := []struct {
		name, method, path, body, message string
	}{
		{"create number legacy URL", "POST", "/api/aggregations", `{"name":"N","subscriptions":[],"nodes":[],"health_check_url":5}`, "探测 URL 必须是无凭据的 HTTP(S) 地址"},
		{"create invalid legacy URL", "POST", "/api/aggregations", `{"name":"N","subscriptions":[],"nodes":[],"health_check_url":"ftp://probe.example/"}`, "探测 URL 必须是无凭据的 HTTP(S) 地址"},
		{"replace number legacy URL", "PUT", "/api/aggregations/a", `{"name":"N","subscriptions":[],"nodes":[],"health_check_url":5}`, "subscription_aggregations 类型不正确"},
		{"replace text enabled", "PUT", "/api/aggregations/a", `{"name":"N","subscriptions":[],"nodes":[],"enabled":"x"}`, "subscription_aggregations 类型不正确"},
		{"create number name", "POST", "/api/aggregations", `{"name":5,"subscriptions":[],"nodes":[]}`, "资源 name 必须是非空字符串"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(aggregations.Register)})
			err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
				return s.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"a","name":"A","subscriptions":[],"nodes":[],"description":"d"}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
			before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			res := srv.Do(tc.method, tc.path, httpxtest.Body([]byte(tc.body), "application/json"))
			if res.Status != 400 {
				t.Fatalf("status %d, want 400: %s", res.Status, res.Body)
			}
			if got := res.Object().GetString("message"); got != tc.message {
				t.Fatalf("message %q, want %q", got, tc.message)
			}
			after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			if !testjson.Equal(before, after) {
				t.Fatal("refused request changed the repository")
			}
		})
	}
}
