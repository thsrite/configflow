package subscriptions_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/api/subscriptions"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

// TestReviewBatch48LenientMembers pins the create/replace decoding the typed
// request kept from the field-map version: declared members of another type
// are read with the stored-data rules (not refused), and a creation's legacy
// health_check_url becomes the default probe URL before validation. Refused
// requests leave the catalog unchanged.
func TestReviewBatch48LenientMembers(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		status                   int
		message                  string
		id                       string
		stored                   string // the stored subscription, when status is 200
	}{
		{"create number type", "POST", "/api/subscriptions", `{"id":"new","name":"S","url":"https://source.example/s","type":5,"health_check":{"enable":false}}`, 200, "", "new",
			`{"id":"new","name":"S","url":"https://source.example/s","type":"5","health_check":{"enable":false}}`},
		{"create number id", "POST", "/api/subscriptions", `{"id":5,"name":"S","url":"https://source.example/s","health_check":{"enable":false}}`, 200, "", "5",
			`{"id":"5","name":"S","url":"https://source.example/s","health_check":{"enable":false}}`},
		{"replace text enabled", "PUT", "/api/subscriptions/feed", `{"name":"S","url":"https://source.example/feed","enabled":"x","description":5}`, 200, "", "feed",
			`{"id":"feed","name":"S","url":"https://source.example/feed","enabled":true,"description":"5"}`},
		{"replace scalar health_check", "PUT", "/api/subscriptions/feed", `{"name":"S","url":"https://source.example/feed","health_check":"bad","health_check_url":5}`, 200, "", "feed",
			`{"id":"feed","name":"S","url":"https://source.example/feed","health_check_url":"5"}`},
		{"replace health_check member type", "PUT", "/api/subscriptions/feed", `{"name":"S","url":"https://source.example/feed","health_check":{"interval":"x"}}`, 200, "", "feed",
			`{"id":"feed","name":"S","url":"https://source.example/feed","health_check":{}}`},
		{"create number legacy URL", "POST", "/api/subscriptions", `{"id":"new","name":"S","url":"https://source.example/s","health_check_url":5}`, 400, "探测 URL 必须是无凭据的 HTTP(S) 地址", "", ""},
		{"create legacy URL", "POST", "/api/subscriptions", `{"id":"new","name":"S","url":"https://source.example/s","health_check_url":"http://probe.example/"}`, 200, "", "new",
			`{"id":"new","name":"S","url":"https://source.example/s","health_check":{"enable":true,"url":"http://probe.example/","interval":300,"timeout":5000,"lazy":true,"expected-status":"204"},"health_check_url":"http://probe.example/"}`},
		{"bad interval", "PUT", "/api/subscriptions/feed", `{"name":"S","url":"https://source.example/feed","interval":"abc"}`, 400, "订阅源更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
			err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
				return s.SetJSON("subscriptions", json.RawMessage(`[{"id":"feed","name":"Feed","url":"https://source.example/feed","interval":3600}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
			before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			res := srv.Do(tc.method, tc.path, httpxtest.Body([]byte(tc.body), "application/json"))
			if res.Status != tc.status {
				t.Fatalf("status %d, want %d: %s", res.Status, tc.status, res.Body)
			}
			if tc.status != 200 {
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
				return
			}
			shared, err := srv.Repo.Shared(srv.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, sub := range shared.Subscriptions {
				if sub.ID == tc.id {
					got, err := json.Marshal(sub)
					if err != nil {
						t.Fatal(err)
					}
					if !testjson.Equal(got, []byte(tc.stored)) {
						t.Fatalf("stored %s, want %s", got, tc.stored)
					}
					return
				}
			}
			t.Fatalf("subscription %q not stored", tc.id)
		})
	}
}
