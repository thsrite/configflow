package nodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"configflow/internal/api/nodes"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func deletionServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
		if err := s.SetJSON("nodes", json.RawMessage(`[{"id":"remove","name":"Remove"},{"id":"keep","name":"Keep"}]`)); err != nil {
			return err
		}
		return s.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"agg","name":"Agg","nodes":["remove","keep"],"subscriptions":[]}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"default", "other"} {
		if id != "default" {
			if _, err := srv.Repo.CreateProfileRecord(srv.Context(), repo.ProfileCreation{ID: id, Name: "Other"}, ""); err != nil {
				t.Fatal(err)
			}
		}
		_, err := srv.Repo.UpdateProfile(srv.Context(), id, func(_ context.Context, p *model.Profile) error {
			return p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g","name":"Group","type":"select","manual_nodes":["remove","keep"],"aggregations":["agg"]}]`))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

func deletionBody(t *testing.T, srv *httpxtest.Server) map[string]any {
	t.Helper()
	res := srv.Post("/api/nodes/delete-preview", httpxtest.JSON(map[string]any{"ids": []string{"remove"}})).WantStatus(200)
	var plan struct {
		Confirmation string `json:"confirmation"`
	}
	if err := json.Unmarshal(res.Body, &plan); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"ids": []string{"remove"}, "confirmation": plan.Confirmation}
}

func TestReviewedDeletionCommitFailureIsAtomic(t *testing.T) {
	srv := deletionServer(t)
	body := deletionBody(t, srv)
	before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
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
	after, err := reopened.ExportAllJSON(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(before, after) {
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
		_, err := srv.Repo.UpdateProfile(srv.Context(), "other", func(_ context.Context, p *model.Profile) error {
			close(locked)
			<-release
			p.ProxyGroups[0].Name = "Changed reference"
			return nil
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
	shared, err := srv.Repo.Shared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.Nodes) != 2 {
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
