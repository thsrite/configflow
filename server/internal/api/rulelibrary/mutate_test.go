package rulelibrary

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

func TestReplaceAndDeletePreserveIndependentState(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	createManual(t, s)
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		v := model.Int(7200)
		p.RuleLibrary[0].UpdateInterval = &v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Put("/api/rule-library/manual", auth, httpxtest.JSON(map[string]string{"name": "Edited", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.edited.example\n"})).WantStatus(200)
	item, err := source(s.Context(), "manual")
	if err != nil || string(field(item, "update_interval")) != "7200" {
		t.Fatal("source edit lost its task interval", err)
	}
	path, err := cachePath(s.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "+.edited.example\n" {
		t.Fatalf("stable cache %q %v", content, err)
	}
	if _, err = os.Stat(path + ".invalid"); !os.IsNotExist(err) {
		t.Fatal("successful replacement left invalid marker", err)
	}
	before := doc(t, s)
	s.Put("/api/rule-library/manual", auth, httpxtest.JSON(map[string]any{"source_type": "content", "behavior": "domain", "format": "text", "content": "IP-CIDR,192.0.2.0/24"})).WantStatus(400)
	if doc(t, s) != before {
		t.Fatal("invalid content changed declaration")
	}
	s.Delete("/api/rule-library/manual", auth).WantStatus(200)
	s.Delete("/api/rule-library/manual", auth).WantStatus(404)
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("delete left stable cache", err)
	}
}
func TestLibraryDeletionRejectsReferencesAtomically(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	_, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"reference","itemType":"ruleset","library_rule_id":"manual","policy":"DIRECT"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before := doc(t, s)
	s.Delete("/api/rule-library/manual", s.Login()).WantStatus(409)
	if doc(t, s) != before {
		t.Fatal("referenced source deletion changed data")
	}
}
func TestConnectivityDoesNotOverwriteConcurrentLibraryEdit(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	started, finish := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	unblock := func() { released.Do(func() { close(finish) }) }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			once.Do(func() { close(started) })
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(404)
			return
		}
		w.Write([]byte("+.download.example\n"))
	}))
	defer origin.Close()
	defer unblock()
	initial := map[string]any{"id": "remote", "name": "Original", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/source"}
	s.Post("/api/rule-library", auth, httpxtest.JSON(initial)).WantStatus(200)
	pending := make(chan *httpxtest.Result, 1)
	go func() { pending <- s.Post("/api/rule-library/test", auth, httpxtest.JSON(map[string]any{})) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("probe did not start")
	}
	updated := map[string]any{"name": "Concurrent edit", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/source", "enabled": true}
	s.Put("/api/rule-library/remote", auth, httpxtest.JSON(updated)).WantStatus(200)
	unblock()
	(<-pending).WantStatus(200)
	item, err := source(s.Context(), "remote")
	if err != nil || text(item, "name") != "Concurrent edit" || string(field(item, "enabled")) != "true" {
		t.Fatalf("test overwrote concurrent edit: %+v %v", item, err)
	}
	s.Post("/api/rule-library/test", auth, httpxtest.JSON(map[string]any{})).WantStatus(200)
	item, err = source(s.Context(), "remote")
	if err != nil || string(field(item, "enabled")) != "false" {
		t.Fatal("unchanged failed source was not disabled", err)
	}
}
func TestManualCacheAndSingleUseLoopbackBoundary(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	createManual(t, s)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("+.download.example\n"))
	}))
	defer origin.Close()
	for _, path := range []string{"/ok", "/missing"} {
		r := s.Post("/api/rule-library/test-single", auth, httpxtest.JSON(map[string]string{"url": origin.URL + path})).WantStatus(200)
		var out struct {
			Available bool `json:"available"`
		}
		if err := json.Unmarshal(r.Body, &out); err != nil {
			t.Fatal(err)
		}
		if out.Available != (path == "/ok") {
			t.Fatal("unexpected availability", r.Text())
		}
	}
	s.Post("/api/rule-library/test-single", auth, httpxtest.JSON(map[string]string{})).WantStatus(400)
	s.Post("/api/rule-library/cache", auth, httpxtest.JSON(map[string]any{"rule_ids": []string{"manual"}})).WantStatus(200)
	for _, test := range []struct {
		ids    []string
		status int
	}{{[]string{}, 400}, {[]string{"missing"}, 404}} {
		s.Post("/api/rule-library/cache", auth, httpxtest.JSON(map[string]any{"rule_ids": test.ids})).WantStatus(test.status)
	}
	files, err := filepath.Glob(filepath.Join(s.DataDir, "shared/rules/by-id/*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, ".invalid") || strings.HasSuffix(file, ".tmp") {
			t.Fatal("cache edit left transient file", file)
		}
	}
}
func FuzzEditableContent(f *testing.F) {
	for _, seed := range []string{"+.example.com\n", "payload:\n  - +.example.com\n", "payload: &self [*self]\n", "payload: []\npayload: []\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		var body record
		for _, entry := range []struct{ key, value string }{{"content", content}, {"behavior", "domain"}, {"format", "yaml"}} {
			if err := put(&body, entry.key, entry.value); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = (&Service{}).Check(body)
	})
}

func TestSuccessfulDraftLeavesNoTemporaryArtifacts(t *testing.T) {
	s := fixture(t)
	createManual(t, s)
	auth := s.Login()
	draftRes := s.Get("/api/rule-library/manual/draft-content", auth).WantStatus(200)
	var draft struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(draftRes.Body, &draft); err != nil {
		t.Fatal(err)
	}
	token := draft.Revision
	s.Put("/api/rule-library/manual/draft-content", auth, httpxtest.JSON(map[string]string{"revision": token, "content": "+.new.example\n"})).WantStatus(200)
	err := filepath.WalkDir(filepath.Join(s.DataDir, "shared/rules"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".tmp") || strings.HasSuffix(path, ".invalid")) {
			t.Error("transient cache artifact", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal([]byte(doc(t, s)), &state); err != nil {
		t.Fatal(err)
	}
}
