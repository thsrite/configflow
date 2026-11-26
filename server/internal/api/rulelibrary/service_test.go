package rulelibrary

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/api/proxygroups"
	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

func fixture(t *testing.T) *httpxtest.Server {
	t.Helper()
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	apirules.New(apirules.Deps{Repo: s.Repo}).Register(s.App.Router)
	proxygroups.NewHandler(s.Repo, nil).Register(s.App.Router)
	New(Deps{Repo: s.Repo}).Register(s.App.Router)
	return s
}
func doc(t *testing.T, s *httpxtest.Server) string {
	t.Helper()
	raw, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestCreateContentAndPublicDelivery(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	source := map[string]any{"id": "manual", "name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.example.com\n", "url": "https://unused.example/source", "has_ip_part": true, "future": map[string]any{"keep": true}}
	r := s.Post("/api/rule-library", auth, httpxtest.JSON(source)).WantStatus(200)
	if r.Object().GetObject("data").Has("url") || r.Object().GetObject("data").Has("has_ip_part") {
		t.Fatal("creation kept inactive/display-only fields")
	}
	s.Get("/api/rule-library/content/manual").WantStatus(200)
	s.Get("/api/profiles/default/rule-library/content/manual").WantStatus(200)
	s.Get("/api/rule-library/content/missing").WantStatus(404)
	s.Get("/api/rule-library", auth).WantStatus(200)
	before := doc(t, s)
	s.Post("/api/rule-library", auth, httpxtest.JSON(source)).WantStatus(409)
	if doc(t, s) != before {
		t.Fatal("duplicate wrote data")
	}
	files, err := filepath.Glob(filepath.Join(s.DataDir, "shared/rules/by-id/*.list"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || string(data) != "+.example.com\n" {
		t.Fatalf("cache: %q %v", data, err)
	}
}
func TestCreateAndCheckRejectWithoutWrites(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	before := doc(t, s)
	for _, body := range []any{nil, []any{}, map[string]any{}, map[string]any{"id": 12}, map[string]any{"id": "new", "source_type": "content", "behavior": "domain", "format": "text", "content": "IP-CIDR,192.0.2.0/24"}} {
		s.Post("/api/rule-library", auth, httpxtest.JSON(body)).WantStatus(400)
	}
	for _, body := range []any{nil, map[string]any{"content": 1}, map[string]any{"content": "a.example", "append_lines": nil}, map[string]any{"content": "a.example", "append_lines": []any{nil}}, map[string]any{"content": "a.example", "append_lines": []string{"b.example\nc.example"}}} {
		s.Post("/api/rule-library/check-content", auth, httpxtest.JSON(body)).WantStatus(400)
	}
	s.Post("/api/rule-library/check-content", auth, httpxtest.JSON(map[string]any{"content": "payload:\n  - +.a.example\n", "behavior": "domain", "format": "yaml", "append_lines": []string{"+.b.example"}})).WantStatus(200)
	s.Post("/api/rule-library/check-content", auth, httpxtest.JSON(map[string]any{"content": "DOMAIN,a.example\n", "behavior": "classical", "format": "text"})).WantStatus(200)
	if doc(t, s) != before {
		t.Fatal("content checking or rejected creation changed data")
	}
}

func TestListClassicalRulesUsesRepositoryCache(t *testing.T) {
	server := fixture(t)
	tests := []struct {
		id      string
		content string
		wantIP  bool
	}{
		{"domains", "DOMAIN,example.test\n", false},
		{"networks", "IP-CIDR,192.0.2.0/24\n", true},
		{"uncached", "", true},
	}
	for _, test := range tests {
		item := model.RuleLibraryItem{
			ID: test.id, Name: test.id, SourceType: new("url"),
			URL: new("https://example.test/" + test.id + ".list"), Behavior: new("classical"), Format: new("text"),
		}
		if test.content == "" {
			if err := server.Repo.UpdateShared(t.Context(), func(_ context.Context, shared *model.Shared) error {
				shared.RuleLibrary = append(shared.RuleLibrary, item)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := server.Repo.CreateRuleLibraryRecord(t.Context(), item, []byte(test.content)); err != nil {
			t.Fatal(err)
		}
	}
	response := server.Get("/api/rule-library", server.Login()).WantStatus(200)
	var items []struct {
		ID        string `json:"id"`
		HasIPPart *bool  `json:"has_ip_part"`
	}
	if err := json.Unmarshal(response.Body, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != len(tests) {
		t.Fatalf("got %d rule library items, want %d", len(items), len(tests))
	}
	for index, test := range tests {
		item := items[index]
		if item.ID != test.id || item.HasIPPart == nil || *item.HasIPPart != test.wantIP {
			t.Errorf("rule %q: want has_ip_part=%t; response: %s", test.id, test.wantIP, response.Body)
		}
	}
}

func TestProxyDomainsAndReorder(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	s.Get("/api/rule-library/proxy-domains", auth).WantStatus(200)
	s.Post("/api/rule-library/proxy-domains", auth, httpxtest.JSON(map[string]string{"proxy_domains": "  https://proxy.example/  "})).WantStatus(200)
	for _, id := range []string{"first", "second"} {
		s.Post("/api/rule-library", auth, httpxtest.JSON(map[string]string{"id": id, "name": id, "source_type": "content", "behavior": "domain", "format": "text", "content": id + ".example"})).WantStatus(200)
	}
	s.Post("/api/rule-library/reorder", auth, httpxtest.JSON(map[string]any{"rules": []any{map[string]string{"id": "second", "content": "display-only.example"}}})).WantStatus(200)
	before := doc(t, s)
	s.Post("/api/rule-library/reorder", auth, httpxtest.JSON(map[string]any{"ids": []string{"missing"}})).WantStatus(404)
	s.Do("HEAD", "/api/rule-library", auth).WantStatus(200)
	if doc(t, s) != before || strings.Contains(before, "display-only.example") {
		t.Fatal("reorder or HEAD wrote display-only data")
	}
}
