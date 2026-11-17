package agents

import (
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSurgeNullRestartPreservesLegacyProtocol(t *testing.T) {
	s, admin, fake := newDeployHTTP(t)
	_, e := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetList("agents")[0].(*jsonx.Object).Set("service_type", "surge")
		return system, nil
	})
	if e != nil {
		t.Fatal(e)
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
	if e = json.Unmarshal(calls[0].Body, &payload); e != nil {
		t.Fatal(e)
	}
	sum := md5.Sum([]byte(payload.Config))
	if payload.MD5 != hex.EncodeToString(sum[:]) || payload.Restart != nil {
		t.Fatal("legacy payload digest/restart", payload)
	}
}
