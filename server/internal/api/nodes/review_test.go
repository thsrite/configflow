package nodes_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"configflow/internal/api/nodes"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func deletionServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	nodes.Wire()
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	_, err := srv.Repo.SaveShared(srv.Context(), jsonx.Obj("nodes", []any{
		jsonx.Obj("id", "remove", "name", "Remove"), jsonx.Obj("id", "keep", "name", "Keep")},
		"subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Agg", "nodes", []any{"remove", "keep"}, "subscriptions", []any{})}), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"default", "other"} {
		if id != "default" {
			if _, err := srv.Repo.CreateProfile(srv.Context(), jsonx.Obj("id", id, "name", "Other"), ""); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := srv.Repo.UpdateProfileTransaction(srv.Context(), id, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Group", "type", "select", "manual_nodes", []any{"remove", "keep"}, "aggregations", []any{"agg"})})
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

func deletionBody(t *testing.T, srv *httpxtest.Server) *jsonx.Object {
	t.Helper()
	plan := srv.Post("/api/nodes/delete-preview", httpxtest.JSON(jsonx.Obj("ids", []any{"remove"}))).WantStatus(200).Object()
	return jsonx.Obj("ids", []any{"remove"}, "confirmation", plan.GetString("confirmation"))
}

func TestReviewedDeletionCommitFailureIsAtomic(t *testing.T) {
	srv := deletionServer(t)
	body := deletionBody(t, srv)
	before, err := srv.Repo.ExportAll(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := srv.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	srv.Repo.Store().BeforeCommit = func() error { return errors.New("synthetic commit failure") }
	srv.Post("/api/nodes/delete", httpxtest.JSON(body)).WantStatus(500)
	srv.Repo.Store().BeforeCommit = nil
	afterFingerprint, err := srv.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fingerprint, afterFingerprint) {
		t.Fatal("failed deletion changed database/revision")
	}
	reopened, err := repo.Open(srv.DataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.ExportAll(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(before, after) {
		t.Fatal("failed deletion detached references or deleted nodes")
	}
	srv.Post("/api/nodes/delete", httpxtest.JSON(body)).WantStatus(200)
}

func TestReviewedDeletionSerializesConcurrentReferenceWrite(t *testing.T) {
	srv := deletionServer(t)
	body := deletionBody(t, srv)
	locked, release := make(chan struct{}), make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		_, _, err := srv.Repo.UpdateProfileTransaction(srv.Context(), "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			close(locked)
			<-release
			p.GetList("proxy_groups")[0].(*jsonx.Object).Set("name", "Changed reference")
			return nil, nil
		})
		writeDone <- err
	}()
	<-locked
	deleted := make(chan *httpxtest.Result, 1)
	go func() { deleted <- srv.Post("/api/nodes/delete", httpxtest.JSON(body)) }()
	close(release)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	(<-deleted).WantStatus(409)
	shared, err := srv.Repo.GetShared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.GetList("nodes")) != 2 {
		t.Fatal("stale preview deleted nodes")
	}
	// The fresh preview is safe to execute even after request cancellation.
	body = deletionBody(t, srv)
	srv.Post("/api/nodes/delete", httpxtest.JSON(body), func(r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		cancel()
		*r = *r.WithContext(ctx)
	}).WantStatus(200)
}
