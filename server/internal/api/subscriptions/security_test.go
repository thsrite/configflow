package subscriptions_test

import (
	"configflow/internal/logx"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/api/subscriptions"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
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
	_, err = substore.SaveSubscriptionNodes(ctx, "s", []any{jsonx.Obj("id", "cached", "name", "Remote", "type", "http", "server", "example.com", "port", 80)}, nil)
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
	logs, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"synthetic-pull-token", "synthetic-internal-token", "synthetic-retired-token"} {
		if strings.Contains(string(logs), token) {
			t.Fatal("provider log exposed token")
		}
	}
}
