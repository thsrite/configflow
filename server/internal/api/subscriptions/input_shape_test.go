package subscriptions_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"configflow/internal/api/nodes"
	"configflow/internal/api/subscriptions"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/testjson"
)

func TestConfigFindingsRejectInvalidRequestShapesWithoutEffects(t *testing.T) {
	calls := 0
	var h *subscriptions.Handler
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		nodes.Register(r)
		h = subscriptions.NewHandler(nil, nil, nil, nil)
		h.Register(r)
	})})
	env := refresh.DefaultRefreshEnvFor(srv.Repo, nil)
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		calls++
		return nil, errors.New("synthetic source must not be called")
	}
	h.Repo = srv.Repo
	h.Refresh = refresh.New(refresh.Options{Repo: srv.Repo, Env: env})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
		return s.SetJSON("subscriptions", json.RawMessage(`[{"id":"s","name":"Synthetic","url":"https://feed.example/sub"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
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
	for _, path := range []string{"/api/nodes/reorder", "/api/subscriptions/reorder", "/api/subscriptions/s/fetch", "/api/subscriptions/missing/fetch"} {
		for _, body := range []string{`null`, `[]`, `false`, `0`, `""`} {
			tests = append(tests, struct{ path, body, message string }{path, body, "请求体必须是 JSON 对象"})
		}
	}
	for _, test := range tests {
		t.Run(test.path+test.body, func(t *testing.T) {
			result := srv.Post(test.path, httpxtest.Body([]byte(test.body), "application/json"))
			if result.Status != 400 {
				t.Errorf("status %d: %s", result.Status, result.Body)
				return
			}
			if !strings.HasPrefix(result.Header.Get("Content-Type"), "application/json") {
				t.Fatal("non-JSON failure")
			}
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(result.Body, &body); err != nil || body.Success || body.Message != test.message {
				t.Fatalf("unexpected error: %s", result.Body)
			}
			after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
			if err != nil || !testjson.Equal(before, after) {
				t.Fatalf("invalid request changed storage: %v", err)
			}
			if calls != 0 {
				t.Fatal("invalid request reached upstream")
			}
		})
	}
}

func TestConfigFindingsMalformedCacheFailureIsJSON(t *testing.T) {
	var h *subscriptions.Handler
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		h = subscriptions.NewHandler(nil, nil, nil, nil)
		h.Register(r)
	})})
	env := refresh.DefaultRefreshEnvFor(srv.Repo, nil)
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		return nil, errors.New("synthetic source failure")
	}
	h.Repo = srv.Repo
	h.Refresh = refresh.New(refresh.Options{Repo: srv.Repo, Env: env})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
		return s.SetJSON("subscriptions", json.RawMessage(`[{"id":"s","name":"Synthetic","url":"https://feed.example/sub"}]`))
	})
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
	res := srv.Post("/api/subscriptions/s/fetch", httpxtest.JSON(map[string]any{})).WantStatus(500)
	if diff := testjson.Diff(map[string]any{"success": false, "message": "订阅操作失败"}, json.RawMessage(res.Body)); diff != "" {
		t.Fatal(diff)
	}
}

func TestConfigFindingsFailedImportCommitIsJSON(t *testing.T) {
	var h *subscriptions.Handler
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		h = subscriptions.NewHandler(nil, nil, nil, nil)
		h.Register(r)
	})})
	env := refresh.DefaultRefreshEnvFor(srv.Repo, nil)
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		return &refresh.SourceResult{Text: "proxies: [{name: Synthetic, type: socks5, server: 192.0.2.1, port: 1080}]", Source: "direct_yaml"}, nil
	}
	h.Repo = srv.Repo
	h.Refresh = refresh.New(refresh.Options{Repo: srv.Repo, Env: env})
	if err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
		return s.SetJSON("subscriptions", json.RawMessage(`[{"id":"s","name":"Synthetic","url":"https://feed.example/sub"}]`))
	}); err != nil {
		t.Fatal(err)
	}
	before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(srv.DataDir, "configflow.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// R218-F13 uses SQLite triggers, not directory permissions: a successful
	// download must still report a JSON failure when importing nodes cannot commit.
	if _, err := db.Exec("CREATE TRIGGER deny_import BEFORE INSERT ON fields BEGIN SELECT RAISE(ABORT, 'synthetic-write-denied'); END"); err != nil {
		t.Fatal(err)
	}
	res := srv.Post("/api/subscriptions/s/fetch", httpxtest.JSON(map[string]any{})).WantStatus(500)
	if diff := testjson.Diff(map[string]any{"success": false, "message": "订阅操作失败"}, json.RawMessage(res.Body)); diff != "" {
		t.Fatal(diff)
	}
	after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
	if err != nil || !testjson.Equal(before, after) {
		t.Fatalf("failed import changed configuration: %v", err)
	}
}
