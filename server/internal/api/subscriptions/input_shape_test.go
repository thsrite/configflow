package subscriptions_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/api/nodes"
	"configflow/internal/api/subscriptions"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/substore"
)

func TestConfigFindingsRejectInvalidRequestShapesWithoutEffects(t *testing.T) {
	calls := 0
	env := refresh.DefaultEnv()
	env.FetchSubscription = func(context.Context, string, string, substore.FetchOptions) (*substore.SubscriptionResult, error) {
		calls++
		return nil, errors.New("synthetic source must not be called")
	}
	t.Cleanup(refresh.SetEnv(env))
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register, subscriptions.Register)})
	_, err := srv.Repo.SaveShared(srv.Context(), jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Synthetic", "url", "https://feed.example/sub")}), nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := srv.Repo.ExportAll(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ path, body, message string }{
		{"/api/nodes/reorder", `[1]`, "请求体必须是 JSON 对象"},
		{"/api/subscriptions/reorder", `[1]`, "请求体必须是 JSON 对象"},
		{"/api/subscriptions/s/fetch", `[1]`, "请求体必须是 JSON 对象"},
		{"/api/nodes/reorder", `{"nodes":[1]}`, "nodes 必须是对象数组"},
		{"/api/subscriptions/reorder", `{"subscriptions":[1]}`, "subscriptions 必须是对象数组"},
		{"/api/subscriptions/reorder", `{"ids":1}`, "ids 必须是数组"},
		{"/api/subscriptions/reorder", `{"ids":["s","s"]}`, "id 必须是互不重复的字符串"},
	}
	for _, test := range tests {
		t.Run(test.path+test.body, func(t *testing.T) {
			value, err := jsonx.Parse([]byte(test.body))
			if err != nil {
				t.Fatal(err)
			}
			result := srv.Post(test.path, httpxtest.JSON(value))
			if result.Status != 400 {
				t.Errorf("status %d: %s", result.Status, result.Body)
				return
			}
			if !strings.HasPrefix(result.Header.Get("Content-Type"), "application/json") {
				t.Fatal("non-JSON failure")
			}
			body := result.Object()
			if body.GetBool("success") || body.GetString("message") != test.message {
				t.Fatalf("unexpected error: %s", result.Body)
			}
			after, err := srv.Repo.ExportAll(srv.Context(), false)
			if err != nil || !jsonx.Equal(before, after) {
				t.Fatalf("invalid request changed storage: %v", err)
			}
			if calls != 0 {
				t.Fatal("invalid request reached upstream")
			}
		})
	}
}

func TestConfigFindingsMalformedCacheFailureIsJSON(t *testing.T) {
	env := refresh.DefaultEnv()
	env.FetchSubscription = func(context.Context, string, string, substore.FetchOptions) (*substore.SubscriptionResult, error) {
		return nil, errors.New("synthetic source failure")
	}
	t.Cleanup(refresh.SetEnv(env))
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(subscriptions.Register)})
	_, err := srv.Repo.SaveShared(srv.Context(), jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Synthetic", "url", "https://feed.example/sub")}), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(srv.DataDir, "shared", "subscribes", "s.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"subscription_id":"s","nodes":[1],"count":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	srv.Post("/api/subscriptions/s/fetch", httpxtest.JSON(jsonx.Obj())).WantStatus(500).WantJSON(jsonx.Obj("success", false, "message", "订阅操作失败"))
}
