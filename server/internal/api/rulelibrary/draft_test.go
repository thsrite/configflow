package rulelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/rulefetch"
)

func createManual(t *testing.T, s *httpxtest.Server) {
	t.Helper()
	s.Post("/api/rule-library", s.Login(), httpxtest.JSON(map[string]any{"id": "manual", "name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.old.example\n", "future": map[string]any{"keep": true}})).WantStatus(200)
}
func TestDraftRevisionAndConcurrentSubmission(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	createManual(t, s)
	a := s.Get("/api/rule-library/manual/draft-content", auth).WantStatus(200)
	b := s.Get("/api/rule-library/manual/draft-content", auth).WantStatus(200)
	token := a.Object().GetString("revision")
	if token == "" || token != b.Object().GetString("revision") || a.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("draft token unstable or cacheable")
	}
	gate := make(chan struct{})
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for _, content := range []string{"+.first.example\n", "+.second.example\n"} {
		workers.Add(1)
		go func(content string) {
			defer workers.Done()
			<-gate
			r := s.Put("/api/rule-library/manual/draft-content", auth, httpxtest.JSON(map[string]string{"revision": token, "content": content}))
			statuses <- r.Status
		}(content)
	}
	close(gate)
	workers.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent draft writes", counts)
	}
	next := s.Get("/api/rule-library/manual/draft-content", auth).WantStatus(200).Object().GetString("revision")
	if next == token {
		t.Fatal("modified draft token stayed valid")
	}
	before := doc(t, s)
	for _, body := range []any{map[string]string{"revision": token, "content": "+.stale.example\n"}, map[string]string{"revision": next, "content": "+.x.example\n", "name": "forbidden"}, map[string]any{"revision": next, "content": nil}} {
		status := 400
		if values, ok := body.(map[string]string); ok && values["revision"] == token {
			status = 409
		}
		s.Put("/api/rule-library/manual/draft-content", auth, httpxtest.JSON(body)).WantStatus(status)
	}
	if doc(t, s) != before {
		t.Fatal("rejected draft wrote data")
	}
	s.Get("/api/rule-library/missing/draft-content", auth).WantStatus(404)
}
func TestRevisionUsesRecursiveNativeSortedJSON(t *testing.T) {
	var a, b record
	for _, pair := range []struct {
		dst *record
		raw string
	}{{&a, `{"name":"中文","extra":{"z":2,"a":1},"id":"x"}`}, {&b, `{"id":"x","extra":{"a":1,"z":2},"name":"中文"}`}} {
		if err := json.Unmarshal([]byte(pair.raw), pair.dst); err != nil {
			t.Fatal(err)
		}
	}
	x, err := revision(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := revision(b)
	if err != nil || x != y {
		t.Fatalf("native recursive key sorting unstable: %s %s %v", x, y, err)
	}
}
func TestFailedCacheReplacementNeverServesStableOrLegacyContent(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apirules.Register, func(r *httpx.Router) {
		RegisterWithOptions(r, Options{replaceCache: func(context.Context, string, string) error { return os.ErrPermission }})
	})})
	createManual(t, s)
	auth := s.Login()
	item, err := source(s.Context(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(s.DataDir, "shared/rules/Manual.list")
	if err = os.WriteFile(legacyPath, []byte("+.legacy.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token := s.Get("/api/rule-library/manual/draft-content", auth).WantStatus(200).Object().GetString("revision")
	s.Put("/api/rule-library/manual/draft-content", auth, httpxtest.JSON(map[string]string{"revision": token, "content": "+.new.example\n"})).WantStatus(500)
	item, err = source(s.Context(), "manual")
	if err != nil || text(item, "content") != "+.new.example\n" {
		t.Fatal("committed declaration lost", err)
	}
	if _, err = os.Stat(path + ".invalid"); err != nil {
		t.Fatal("durable invalidation missing", err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old stable cache survived", err)
	}
	// Even an independently restored stable file cannot defeat the durable marker.
	if err = os.WriteFile(path, []byte("+.stale-restored.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := legacy(item)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := rulefetch.ReadValidatedRuleCache(s.Context(), raw, nil)
	if err != nil || cached != nil {
		t.Fatalf("invalid cache returned %q %v", cached, err)
	}
	for _, url := range []string{"/api/rule-library/content/manual", "/api/profiles/default/rule-library/content/manual", "/api/rules/local/Manual"} {
		r := s.Get(url, auth).WantStatus(200)
		if r.Text() != "+.new.example\n" {
			t.Fatalf("stale delivery %s: %q", url, r.Text())
		}
	}
}
