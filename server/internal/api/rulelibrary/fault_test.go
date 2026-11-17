package rulelibrary

import (
	"configflow/internal/jsonx"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
)

func TestStageWriteFailureDoesNotPublishDeclaration(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(path)
	if err = os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	before := doc(t, s)
	s.Put("/api/rule-library/manual", s.Login(), httpxtest.JSON(map[string]string{"name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.unpublished.example\n"})).WantStatus(500)
	if doc(t, s) != before {
		t.Fatal("staging failure published the new declaration")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "+.old.example\n" {
		t.Fatalf("staging failure changed the valid old cache: %q %v", content, err)
	}
	if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
		t.Fatal("pre-commit failure left invalidation", err)
	}
}
func TestCanceledConnectivityRequestCompletesAndJoinsWorkers(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	started, finish := make(chan struct{}), make(chan struct{})
	var signaled, released sync.Once
	unblock := func() { released.Do(func() { close(finish) }) }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			signaled.Do(func() { close(started) })
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(200)
			return
		}
		w.Write([]byte("+.download.example\n"))
	}))
	defer origin.Close()
	defer unblock()
	s.Post("/api/rule-library", auth, httpxtest.JSON(map[string]string{"id": "remote", "name": "Remote", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/ok"})).WantStatus(200)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pending := make(chan *httpxtest.Result, 1)
	go func() {
		pending <- s.Post("/api/rule-library/test", auth, func(r *http.Request) { *r = *r.WithContext(ctx) })
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("connectivity check did not start")
	}
	cancel()
	unblock()
	var reply *httpxtest.Result
	select {
	case reply = <-pending:
	case <-time.After(10 * time.Second):
		t.Fatal("connectivity workers did not finish")
	}
	reply.WantStatus(200)
	results := reply.Object().GetList("results")
	if len(results) != 1 {
		t.Fatal(reply.Text())
	}
	if !reply.Contains(`"available":true`) {
		t.Fatal("request cancellation reached the detached probe", reply.Text())
	}
	item, err := source(s.Context(), "remote")
	if err != nil || string(field(item, "enabled")) == "false" {
		t.Fatal("cancellation disabled an available source", err)
	}
}

func TestUnknownCommitFailureRetainsDurableInvalidation(t *testing.T) {
	for _, draft := range []bool{false, true} {
		name := "put"
		if draft {
			name = "draft"
		}
		t.Run(name, func(t *testing.T) {
			s := fixture(t)
			auth := s.Login()
			createManual(t, s)
			item, err := source(s.Context(), "manual")
			if err != nil {
				t.Fatal(err)
			}
			path, err := cachePath(s.Context(), item)
			if err != nil {
				t.Fatal(err)
			}
			token := s.Get("/api/rule-library/manual/draft-content", auth).WantStatus(200).Object().GetString("revision")
			s.Repo.Store().BeforeCommit = func() error { return errors.New("controlled unknown storage failure") }
			if draft {
				s.Put("/api/rule-library/manual/draft-content", auth, httpxtest.JSON(map[string]string{"revision": token, "content": "+.attempt.example\n"})).WantStatus(500)
			} else {
				s.Put("/api/rule-library/manual", auth, httpxtest.JSON(map[string]string{"name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.attempt.example\n"})).WantStatus(500)
			}
			s.Repo.Store().BeforeCommit = nil
			if _, err = os.Stat(path + ".invalid"); err != nil {
				t.Fatal("unknown commit outcome cleared invalidation", err)
			}
			item, err = source(s.Context(), "manual")
			if err != nil || text(item, "content") != "+.old.example\n" {
				t.Fatal("test's rejected commit did not roll back", err)
			}
			// A later observed committed head can be newer than the rejected call's
			// baseline; the preserved receipt must still prevent old bytes being served.
			_, err = s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
				p.GetList("rule_library")[0].(*jsonx.Object).Set("content", "+.committed.example\n")
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte("+.restored-old.example\n"), 0600); err != nil {
				t.Fatal(err)
			}
			r := s.Get("/api/rules/local/Manual", auth).WantStatus(200)
			if r.Text() != "+.committed.example\n" {
				t.Fatal("unknown commit fallback delivered old bytes", r.Text())
			}
			s.Post("/api/rule-library/cache", auth, httpxtest.JSON(map[string]any{"rule_ids": []string{"manual"}})).WantStatus(200)
			if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
				t.Fatal("manual cache repair did not clear receipt", err)
			}
		})
	}
}
