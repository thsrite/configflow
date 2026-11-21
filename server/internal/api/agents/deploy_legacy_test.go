package agents

import (
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestSurgeNullRestartPreservesLegacyProtocol(t *testing.T) {
	s, admin, fake := newDeployHTTP(t)
	if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.Agents[0].ServiceType = "surge"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	response := s.Post("/api/agents/publish-agent/push-config", httpxtest.JSON(map[string]any{"restart": nil}), admin)
	response.WantStatus(200)
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Path != "/api/config/update" || calls[0].Method != "POST" {
		t.Fatal("legacy null caused restart or transactional publication", calls)
	}
	if calls[0].Headers.Get("Content-Type") != "application/json; charset=utf-8" || calls[0].Headers.Get("Authorization") != "Bearer synthetic-publish-token" {
		t.Fatal("legacy raw headers", calls[0].Headers)
	}
	var payload struct {
		Config  string `json:"config"`
		MD5     string `json:"md5"`
		Restart *bool  `json:"restart_after_update"`
	}
	if err := json.Unmarshal(calls[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum([]byte(payload.Config))
	if payload.MD5 != hex.EncodeToString(sum[:]) || payload.Restart != nil {
		t.Fatal("legacy payload digest/restart", payload)
	}
}

// TestLegacyRestartAfterAgentDeletedKeepsPublishResult: an Agent deleted
// between a successful legacy publish and the follow-up restart keeps the
// publish result (200) and reports the restart failure in "restart", rather
// than turning the whole request into a 500 (review batch-20 L1).
func TestLegacyRestartAfterAgentDeletedKeepsPublishResult(t *testing.T) {
	s, admin, fake := newDeployHTTP(t)
	deleted := make(chan error, 1)
	wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.ServeHTTP(w, r)
		if r.URL.Path != "/api/config/update" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		deleted <- s.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
			return system.SetJSON("agents", json.RawMessage(`[]`))
		})
	}))
	t.Cleanup(wrapped.Close)
	u, _ := url.Parse(wrapped.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	n, _ := strconv.Atoi(port)
	// Version below 1.1.0-go: the panel itself sends the follow-up restart.
	if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", mustRawJSON(t, []map[string]any{{
			"id": "publish-agent", "name": "Synthetic", "host": host, "port": n,
			"token": "synthetic-publish-token", "profile_id": "default", "service_type": "surge",
			"deployment_method": "docker", "version": "1.0.0", "enabled": true,
		}}))
	}); err != nil {
		t.Fatal(err)
	}
	response := s.Post("/api/agents/publish-agent/push-config", httpxtest.JSON(map[string]any{}), admin)
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatal("delete Agent during publish:", err)
		}
	default:
		t.Fatal("publish did not reach /api/config/update", fake.Calls())
	}
	response.WantStatus(200)
	var body struct {
		Success bool           `json:"success"`
		Restart map[string]any `json:"restart"`
	}
	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatal(err, string(response.Body))
	}
	want := map[string]any{"success": false, "message": "Agent not found"}
	if !body.Success || !reflect.DeepEqual(body.Restart, want) {
		t.Fatalf("publish result = %s", response.Body)
	}
}
