package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/rulefetch"
)

func TestPublicProvidersNormalizeOnlyDelivery(t *testing.T) {
	s := fixture(t)
	for _, path := range []string{"/api/rules/local/Ads", "/api/profiles/other/rules/local/Ads"} {
		raw := s.Get(path).WantStatus(200)
		if raw.Text() != "+.ads.example\n" {
			t.Fatalf("raw %q", raw.Text())
		}
		normalized := s.Get(path + "?target=mihomo").WantStatus(200)
		if normalized.Text() != "+.ads.example\n" {
			t.Fatalf("delivery %q", normalized.Text())
		}
		if normalized.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatal(normalized.Header)
		}
	}
	s.Get("/api/rules/local/Absent").WantStatus(404)
	s.Post("/api/rules/match-test", httpxtest.JSON(map[string]string{"query": "example.com"})).WantStatus(401)
	rulesDir, err := rulefetch.GetRulesDir(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(rulesDir, "Legacy.list"), []byte("legacy unchanged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("/api/rules/local/Legacy?target=mihomo").WantStatus(200).Text(); got != "legacy unchanged\n" {
		t.Fatal(got)
	}
	if got := s.Get("/api/rules/local/Ads").WantStatus(200).Text(); got != "+.ads.example\n" {
		t.Fatal("cache modified", got)
	}
}
func TestMatchingLazySourcesAndValidation(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	for _, tc := range []struct {
		query   string
		status  int
		matched bool
	}{{"example.com.", 200, true}, {"ads.example", 200, true}, {"unmatched.example", 200, false}, {"_service._tcp.example", 200, false}, {"", 400, false}, {"not a domain", 400, false}, {"https://example.com", 400, false}} {
		t.Run(tc.query, func(t *testing.T) {
			out := s.Post("/api/rules/match-test?profile=other", auth, httpxtest.JSON(map[string]string{"query": tc.query})).WantStatus(tc.status)
			if tc.status == 200 && out.Object().GetBool("matched") != tc.matched {
				t.Fatalf("%s", out.Body)
			}
		})
	}
	s.Post("/api/rules/match-test", auth, httpxtest.JSON([]string{})).WantStatus(500)
	s.Post("/api/rules/match-test", auth, httpxtest.JSON(map[string]int{"query": 4})).WantStatus(500)
	s.Post("/api/rules/match-test", auth, httpxtest.Body([]byte("{"), "application/json")).WantStatus(500)
}
func TestDuplicateScanKeepsAllOccurrencesAndPriorities(t *testing.T) {
	s := fixture(t)
	ctx := s.Context()
	_, err := s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("rule_library", append(p.GetList("rule_library"), []any{jsonx.Obj("id", "a", "name", "A", "source_type", "content", "behavior", "classical", "content", "# comment\nDOMAIN,First.example\nREGEXP,^Case$\nDOMAIN,three.example\nDOMAIN,THREE.example\nDOMAIN,three.example\nDOMAIN,first.example\n"), jsonx.Obj("id", "b", "name", "B", "source_type", "content", "behavior", "classical", "content", "DOMAIN,Conflict.example\nREGEXP,^Case$\n"), jsonx.Obj("id", "empty", "name", "Empty", "source_type", "content", "content", ""), jsonx.Obj("id", "off", "name", "Off", "source_type", "content", "content", "DOMAIN,three.example", "enabled", false)}...))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("rule_configs", []any{jsonx.Obj("id", "a", "itemType", "ruleset", "library_rule_id", "a", "policy", "DIRECT"), jsonx.Obj("id", "direct", "itemType", "rule", "rule_type", "DOMAIN", "value", "Conflict.example", "policy", "DIRECT"), jsonx.Obj("id", "b", "itemType", "ruleset", "library_rule_id", "b", "policy", "PROXY"), jsonx.Obj("id", "empty", "itemType", "ruleset", "library_rule_id", "empty"), jsonx.Obj("id", "off", "itemType", "ruleset", "library_rule_id", "off"), jsonx.Obj("id", "disabled", "itemType", "rule", "rule_type", "DOMAIN", "value", "Conflict.example", "enabled", false)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out := s.Post("/api/rules/find-duplicates?profile=other", s.Login()).WantStatus(200)
	var result scanResult
	if err = json.Unmarshal(out.Body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rules != 1 || result.Stats.Rulesets != 2 || result.Stats.Groups != 4 || strings.Join(result.Stats.Failed, ",") != "Empty" {
		t.Fatalf("%s", out.Body)
	}
	for i, want := range []struct {
		kind, value string
		count       int
		conflict    bool
	}{{"REGEXP", "^Case$", 2, true}, {"DOMAIN", "Conflict.example", 2, true}, {"DOMAIN", "three.example", 3, false}, {"DOMAIN", "First.example", 2, false}} {
		got := result.Duplicates[i]
		if got.Type != want.kind || got.Value != want.value || got.Count != want.count || got.Conflict != want.conflict {
			t.Fatalf("group%d %+v", i, got)
		}
	}
	if result.Duplicates[3].Occurrences[0].LineNo != 2 || result.Duplicates[3].Occurrences[1].LineNo != 7 {
		t.Fatal(result.Duplicates[3])
	}
}
func TestDuplicateLargeRepeatedSourceIsComplete(t *testing.T) {
	s := fixture(t)
	var content strings.Builder
	const count = 20000
	for i := 0; i < count; i++ {
		fmt.Fprintln(&content, "DOMAIN,repeat.example")
	}
	_, err := s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.GetList("rule_library")[0].(*jsonx.Object).Set("behavior", "classical")
		p.GetList("rule_library")[0].(*jsonx.Object).Set("content", content.String())
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out := s.Post("/api/rules/find-duplicates?profile=other", s.Login()).WantStatus(200)
	groups := out.Object().GetList("duplicates")
	if len(groups) != 1 || groups[0].(*jsonx.Object).GetInt("count") != count {
		t.Fatalf("lost occurrences (%d bytes)", len(out.Body))
	}
}
func TestRemoteProviderValidationAndCache(t *testing.T) {
	var calls atomic.Int32
	var invalid atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.ContentLength > 0 {
			t.Errorf("unexpected download %s length%d", r.Method, r.ContentLength)
		}
		if invalid.Load() {
			fmt.Fprint(w, "IP-CIDR,192.0.2.0/24\n")
		} else {
			fmt.Fprint(w, "+.remote.example\n")
		}
	}))
	defer source.Close()
	s := fixture(t)
	_, err := s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("rule_library", append(p.GetList("rule_library"), jsonx.Obj("id", "remote", "name", "Remote", "source_type", "url", "url", source.URL, "behavior", "domain", "format", "text")))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Get("/api/rules/local/Remote").WantStatus(200)
	s.Get("/api/rules/local/Remote").WantStatus(200)
	if calls.Load() != 1 {
		t.Fatal("validated library cache not reused", calls.Load())
	}
	// Use a different source identity to force actual validation, rather than a cached hit.
	invalid.Store(true)
	_, err = s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.GetList("rule_library")[1].(*jsonx.Object).Set("url", source.URL+"/invalid")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := rulefetch.GetRulesDir(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range mustReadDir(t, root) {
		if strings.Contains(entry.Name(), "Remote") {
			if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	out := s.Get("/api/rules/local/Remote").WantStatus(422)
	if out.Object().GetBool("cache_retained") || len(out.Object().GetList("errors")) == 0 {
		t.Fatalf("%s", out.Body)
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	rows, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
