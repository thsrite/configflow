package subscriptions_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/api/subscriptions"
	"configflow/internal/auth"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/testjson"
)

func TestGlobalGateAndProfileToken(t *testing.T) {
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	logFile := filepath.Join(t.TempDir(), "provider.log")
	logger, err := logx.Setup(logx.Config{File: logFile, Level: logx.Debug})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logger.Close() })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	subscriptions.NewHandler(srv.Repo, nil, nil, nil, nil).Register(srv.App.Router)
	ctx := srv.Context()
	err = srv.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		raw, err := json.Marshal([]any{map[string]any{"id": "s", "name": "Feed", "url": "https://feed.example/sub", "interval": 0}})
		if err != nil {
			return err
		}
		return shared.SetJSON("subscriptions", raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = srv.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "synthetic-pull-token"
		system.SystemConfig.RuleProxyToken = "synthetic-internal-token"
		system.SystemConfig.RetiredRuleProxyTokens = []string{"synthetic-retired-token"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixtureSaveSubscriptionNodes(ctx, "s", []any{map[string]any{"id": "cached", "name": "Remote", "type": "http", "server": "example.com", "port": 80}}, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/subscriptions/s/proxies?token=synthetic-pull-token", "/api/subscriptions/proxies?token=synthetic-pull-token"} {
		srv.Get(path).WantStatus(401)
	}
	prefix := "/api/profiles/default/subscriptions/s/proxies"
	srv.Get(prefix + "?token=synthetic-pull-token").WantStatus(200)
	for _, token := range []string{"", "incorrect", "synthetic-internal-token", "synthetic-retired-token"} {
		srv.Get(prefix + "?token=" + token).WantStatus(401)
	}
	srv.Get(prefix, srv.Login()).WantStatus(200)
	srv.Get("/api/profiles/default/subscriptions/missing/proxies?token=synthetic-pull-token").WantStatus(404)
	// A valid JWT cannot turn a current/retired internal capability into a
	// public credential, even when legacy storage makes the tokens equal.
	admin := srv.Login()
	for _, token := range []string{"synthetic-internal-token", "synthetic-retired-token"} {
		srv.Get(prefix+"?token="+token, admin).WantStatus(401)
	}
	expired, err := auth.EncodeHS256(map[string]any{"username": "admin", "exp": time.Now().Add(-time.Hour).Unix()}, auth.Current().SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	srv.Get(prefix, httpxtest.Bearer(expired)).WantStatus(401)
	srv.Get(prefix+"?token=synthetic-pull-token", httpxtest.Bearer(expired)).WantStatus(200)
	for _, header := range []string{"bearer ignored", "Bearer  ignored", "Bearer ignored extra"} {
		srv.Get(prefix, httpxtest.Header("Authorization", header)).WantStatus(401)
	}
	for _, path := range []string{prefix, "/api/profiles/default/subscriptions/missing/proxies"} {
		res := srv.Get(path + "?token=synthetic-other-profile-token").WantStatus(401)
		if diff := testjson.Diff(map[string]any{"message": "Invalid or missing authentication", "success": false}, json.RawMessage(res.Body)); diff != "" {
			t.Fatalf("%s: %s", path, diff)
		}
	}
	err = srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "synthetic-retired-token"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Get(prefix + "?token=synthetic-retired-token").WantStatus(401)
	srv.Get(prefix + "?token=synthetic-pull-token").WantStatus(401)
	err = srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "synthetic-rotated-token"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	res := srv.Get(prefix + "?token=synthetic-rotated-token").WantStatus(200)
	served, err := os.ReadFile(filepath.Join(srv.DataDir, "profiles", "default", "providers", "served", "subscription-s.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"synthetic-rotated-token", expired} {
		if strings.Contains(res.Text(), token) || strings.Contains(string(served), token) {
			t.Fatal("provider body/artifact exposed request credential")
		}
		for _, values := range res.Header {
			for _, value := range values {
				if strings.Contains(value, token) {
					t.Fatal("provider header exposed request credential")
				}
			}
		}
	}
	logs, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"synthetic-pull-token", "synthetic-internal-token", "synthetic-retired-token", "synthetic-rotated-token", expired} {
		if strings.Contains(string(logs), token) {
			t.Fatal("provider log exposed token")
		}
	}
}

func fixtureSaveSubscriptionNodes(ctx context.Context, id string, value any, metadata map[string]any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var nodes []model.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return err
	}
	var receipt *substore.CacheMetadata
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		receipt = &substore.CacheMetadata{}
		if err := json.Unmarshal(raw, receipt); err != nil {
			return err
		}
	}
	_, err = substore.SaveCache(ctx, func() *repo.Repository { r, _ := repo.GetRepository(ctx); return r }(), id, nodes, receipt)
	return err
}
