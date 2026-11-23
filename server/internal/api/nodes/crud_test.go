package nodes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/api/nodes"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func TestReviewedDeletionAndConcurrentChanges(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	ctx := srv.Context()
	err := srv.Repo.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
		if err := s.SetJSON("nodes", json.RawMessage(`[{"id":"remove","name":"Remove","secret":"never-preview"},{"id":"keep","name":"Keep"}]`)); err != nil {
			return err
		}
		return s.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"agg","name":"Shared","nodes":["remove","keep"],"subscriptions":[]}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g","name":"Group","type":"select","manual_nodes":["remove","keep"],"aggregations":["agg"]}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	previewRes := srv.Post("/api/nodes/delete-preview", httpxtest.JSON(map[string]any{"ids": []string{"remove"}})).WantStatus(200)
	var preview struct {
		Usages       []any  `json:"usages"`
		Groups       []any  `json:"groups"`
		Blockers     []any  `json:"blockers"`
		Confirmation string `json:"confirmation"`
	}
	if err := json.Unmarshal(previewRes.Body, &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Usages) != 2 || len(preview.Groups) != 1 || len(preview.Blockers) != 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	srv.Delete("/api/nodes/remove").WantStatus(409)
	srv.Post("/api/nodes/delete", httpxtest.JSON(map[string]any{"ids": []string{"remove"}, "confirmation": preview.Confirmation})).WantStatus(200)
	shared, err := srv.Repo.Shared(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.Nodes) != 1 || len(shared.Aggregations[0].Nodes) != 1 {
		t.Fatal("delete did not detach")
	}
	srv.Post("/api/nodes/delete", httpxtest.JSON(map[string]any{"ids": []string{"keep"}, "confirmation": preview.Confirmation})).WantStatus(409)
}

func TestNodeWritesAndValidation(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	for _, body := range []string{`null`, `[]`, `{"dialer_ref":null}`} {
		srv.Post("/api/nodes", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
	srv.Post("/api/nodes", httpxtest.JSON(map[string]any{"id": "n", "name": "Node", "extra": map[string]any{"unknown": true}})).WantStatus(200)
	srv.Put("/api/nodes/n", httpxtest.JSON(map[string]any{"id": "other", "name": "Node"})).WantStatus(400)
	srv.Put("/api/nodes/missing", httpxtest.JSON(map[string]any{"name": "Node"})).WantStatus(404)
	srv.Post("/api/nodes/reorder", httpxtest.JSON(map[string]any{"ids": []string{"n"}})).WantStatus(200)
	res := srv.Delete("/api/nodes/n").WantStatus(200)
	if diff := testjson.Diff(map[string]any{"success": true}, json.RawMessage(res.Body)); diff != "" {
		t.Fatal(diff)
	}
}

// A deletion naming a node that no longer exists answers Python's plain 404
// message, not the repository's "Profile not found" mapping (review W0–W8).
func TestDeletionOfMissingNodesAnswersNodesGone(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
		return s.SetJSON("nodes", json.RawMessage(`[{"id":"keep","name":"Keep"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"success": false, "message": "部分节点已不存在，请刷新节点列表"}
	for _, path := range []string{"/api/nodes/delete-preview", "/api/nodes/delete"} {
		srv.Post(path, httpxtest.JSON(map[string]any{"ids": []string{"keep", "ghost"}, "confirmation": strings.Repeat("a", 64)})).WantStatus(404).WantJSON(want)
	}
}
