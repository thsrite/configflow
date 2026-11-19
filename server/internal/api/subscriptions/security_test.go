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
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/substore"
)

func TestGlobalGateAndProfileToken(t *testing.T) {
	mihomoenv.Wire()
	logFile := filepath.Join(t.TempDir(), "provider.log")
	logger, err := logx.Setup(logx.Config{File: logFile, Level: logx.Debug})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logger.Close() })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(subscriptions.Register)})
	ctx := srv.Context()
	_, err = srv.Repo.SaveShared(ctx, jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://feed.example/sub", "interval", 0)}), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		s := system.GetObject("system_config")
		s.Set("config_token", "synthetic-pull-token")
		s.Set("rule_proxy_token", "synthetic-internal-token")
		s.Set("retired_rule_proxy_tokens", []any{"synthetic-retired-token"})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixtureSaveSubscriptionNodes(ctx, "s", []any{jsonx.Obj("id", "cached", "name", "Remote", "type", "http", "server", "example.com", "port", 80)}, nil)
	if err != nil {
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
	expired, err := auth.EncodeHS256(jsonx.Obj("username", "admin", "exp", time.Now().Add(-time.Hour).Unix()), auth.Current().SecretKey)
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
		res.WantJSON(jsonx.Obj("message", "Invalid or missing authentication", "success", false))
	}
	_, err = srv.Repo.UpdateSystemTransaction(srv.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("config_token", "synthetic-retired-token")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Get(prefix + "?token=synthetic-retired-token").WantStatus(401)
	srv.Get(prefix + "?token=synthetic-pull-token").WantStatus(401)
	_, err = srv.Repo.UpdateSystemTransaction(srv.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("config_token", "synthetic-rotated-token")
		return nil, nil
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

func fixtureSaveSubscriptionNodes(ctx context.Context, id string, value any, metadata *jsonx.Object) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var nodes []model.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	var receipt *substore.CacheMetadata
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		receipt = &substore.CacheMetadata{}
		if err := json.Unmarshal(raw, receipt); err != nil {
			return nil, err
		}
	}
	cache, err := substore.SaveCache(ctx, id, nodes, receipt)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return nil, err
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	return parsed.(*jsonx.Object), nil
}
