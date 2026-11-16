package nodes_test

import (
	"context"
	"testing"

	"configflow/internal/api/nodes"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestReviewedDeletionAndConcurrentChanges(t *testing.T) {
	nodes.Wire()
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	ctx := srv.Context()
	_, err := srv.Repo.SaveShared(ctx, jsonx.Obj("nodes", []any{jsonx.Obj("id", "remove", "name", "Remove", "secret", "never-preview"), jsonx.Obj("id", "keep", "name", "Keep")}, "subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Shared", "nodes", []any{"remove", "keep"}, "subscriptions", []any{})}), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = srv.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Group", "type", "select", "manual_nodes", []any{"remove", "keep"}, "aggregations", []any{"agg"})})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := srv.Post("/api/nodes/delete-preview", httpxtest.JSON(jsonx.Obj("ids", []any{"remove"}))).WantStatus(200).Object()
	if len(preview.GetList("usages")) != 2 || len(preview.GetList("groups")) != 1 || len(preview.GetList("blockers")) != 0 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	srv.Delete("/api/nodes/remove").WantStatus(409)
	srv.Post("/api/nodes/delete", httpxtest.JSON(jsonx.Obj("ids", []any{"remove"}, "confirmation", preview.GetString("confirmation")))).WantStatus(200)
	shared, err := srv.Repo.GetShared(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.GetList("nodes")) != 1 || len(shared.GetList("subscription_aggregations")[0].(*jsonx.Object).GetList("nodes")) != 1 {
		t.Fatal("delete did not detach")
	}
	srv.Post("/api/nodes/delete", httpxtest.JSON(jsonx.Obj("ids", []any{"keep"}, "confirmation", preview.GetString("confirmation")))).WantStatus(409)
}

func TestNodeWritesAndValidation(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	for _, body := range []string{`null`, `[]`, `{"dialer_ref":null}`} {
		data, _ := jsonx.Parse([]byte(body))
		srv.Post("/api/nodes", httpxtest.JSON(data)).WantStatus(400)
	}
	srv.Post("/api/nodes", httpxtest.JSON(jsonx.Obj("id", "n", "name", "Node", "extra", jsonx.Obj("unknown", true)))).WantStatus(200)
	srv.Put("/api/nodes/n", httpxtest.JSON(jsonx.Obj("id", "other", "name", "Node"))).WantStatus(400)
	srv.Put("/api/nodes/missing", httpxtest.JSON(jsonx.Obj("name", "Node"))).WantStatus(404)
	srv.Post("/api/nodes/reorder", httpxtest.JSON(jsonx.Obj("ids", []any{"n"}))).WantStatus(200)
	srv.Delete("/api/nodes/n").WantStatus(200).WantJSON(jsonx.Obj("success", true))
}
