package rules

import (
	"bytes"
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
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
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
	rulesDir, err := rulefetch.GetRulesDir(s.Repo)
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
			if tc.status == 200 {
				var body struct {
					Matched bool `json:"matched"`
				}
				if err := json.Unmarshal(out.Body, &body); err != nil || body.Matched != tc.matched {
					t.Fatalf("%s", out.Body)
				}
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
	err := s.Repo.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
		var extra []model.RuleLibraryItem
		if err := json.Unmarshal([]byte(`[{"id":"a","name":"A","source_type":"content","behavior":"classical","content":"# comment\nDOMAIN,First.example\nREGEXP,^Case$\nDOMAIN,three.example\nDOMAIN,THREE.example\nDOMAIN,three.example\nDOMAIN,first.example\n"},{"id":"b","name":"B","source_type":"content","behavior":"classical","content":"DOMAIN,Conflict.example\nREGEXP,^Case$\n"},{"id":"empty","name":"Empty","source_type":"content","content":""},{"id":"off","name":"Off","source_type":"content","content":"DOMAIN,three.example","enabled":false}]`), &extra); err != nil {
			return err
		}
		p.RuleLibrary = append(p.RuleLibrary, extra...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"a","itemType":"ruleset","library_rule_id":"a","policy":"DIRECT"},{"id":"direct","itemType":"rule","rule_type":"DOMAIN","value":"Conflict.example","policy":"DIRECT"},{"id":"b","itemType":"ruleset","library_rule_id":"b","policy":"PROXY"},{"id":"empty","itemType":"ruleset","library_rule_id":"empty"},{"id":"off","itemType":"ruleset","library_rule_id":"off"},{"id":"disabled","itemType":"rule","rule_type":"DOMAIN","value":"Conflict.example","enabled":false}]`))
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
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		behavior, body := "classical", content.String()
		p.RuleLibrary[0].Behavior = &behavior
		p.RuleLibrary[0].Content = &body
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out := s.Post("/api/rules/find-duplicates?profile=other", s.Login()).WantStatus(200)
	var dupResult struct {
		Duplicates []struct {
			Count int `json:"count"`
		} `json:"duplicates"`
	}
	if err := json.Unmarshal(out.Body, &dupResult); err != nil {
		t.Fatal(err)
	}
	if len(dupResult.Duplicates) != 1 || dupResult.Duplicates[0].Count != count {
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
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		p.RuleLibrary = append(p.RuleLibrary, testjson.Decode[model.RuleLibraryItem](t, map[string]any{
			"id": "remote", "name": "Remote", "source_type": "url", "url": source.URL, "behavior": "domain", "format": "text",
		}))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Get("/api/rules/local/Remote").WantStatus(200)
	s.Get("/api/rules/local/Remote").WantStatus(200)
	if calls.Load() != 1 {
		t.Fatal("validated library cache not reused", calls.Load())
	}
	if _, err := taskstatus.New(s.DataDir).SaveResult(s.Context(), "rule_source:remote", taskstatus.Result{OK: false, Error: "synthetic unavailable", CacheRetained: boolPointer(true)}); err != nil {
		t.Fatal(err)
	}
	stale := s.Get("/api/rules/local/Remote").WantStatus(200)
	if stale.Header.Get("X-ConfigFlow-Cache") != "stale" || stale.Header.Get("Warning") != `110 ConfigFlow "Rule refresh failed; using validated cache"` {
		t.Fatalf("missing stale indication %v", stale.Header)
	}
	// Use a different source identity to force actual validation, rather than a cached hit.
	invalid.Store(true)
	err = s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		u := source.URL + "/invalid"
		p.RuleLibrary[1].URL = &u
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out := s.Get("/api/rules/local/Remote").WantStatus(422)
	var errBody struct {
		CacheRetained bool  `json:"cache_retained"`
		Errors        []any `json:"errors"`
	}
	if err := json.Unmarshal(out.Body, &errBody); err != nil || errBody.CacheRetained || len(errBody.Errors) == 0 {
		t.Fatalf("%s", out.Body)
	}
}

func TestSourceEntryPointsUsePersistedProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.ContentLength > 0 || r.URL.Host != "127.0.0.1:1" {
			t.Errorf("unexpected raw proxy request %s %s len%d", r.Method, r.URL, r.ContentLength)
		}
		if r.Header.Get("Proxy-Authorization") != "Basic cHJveHktdXNlcjpzeW50aGV0aWMtcGFzcw==" {
			t.Error("persisted proxy authentication missing")
		}
		fmt.Fprint(w, "+.remote.example\n")
	}))
	defer proxy.Close()
	s := fixture(t)
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		for _, id := range []string{"first-remote", "second-remote"} {
			p.RuleLibrary = append(p.RuleLibrary, testjson.Decode[model.RuleLibraryItem](t, map[string]any{
				"id": id, "name": id, "source_type": "url", "url": "http://127.0.0.1:1/" + id, "behavior": "domain", "format": "text",
			}))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyRaw, _ := json.Marshal(strings.Replace(proxy.URL, "http://", "http://proxy-user:synthetic-pass@", 1))
	err = s.Repo.UpdateSystem(s.Context(), func(_ context.Context, p *model.System) error {
		return p.SystemConfig.SetJSON("rule_fetch_proxy", proxyRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(s.Context(), "other", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"remote","itemType":"ruleset","library_rule_id":"second-remote","policy":"DIRECT"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Get("/api/profiles/other/rules/local/first-remote").WantStatus(200)
	out := s.Post("/api/rules/match-test?profile=other", s.Login(), httpxtest.JSON(map[string]string{"query": "remote.example"})).WantStatus(200)
	var matchOut struct {
		Matched bool `json:"matched"`
	}
	if err := json.Unmarshal(out.Body, &matchOut); err != nil || !matchOut.Matched || calls.Load() != 2 {
		t.Fatalf("proxy requests=%d result=%s", calls.Load(), out.Body)
	}
}
func TestCancelledLocalOwnerCompletesCache(t *testing.T) {
	s := fixture(t)
	ctx, cancel := context.WithCancel(s.Context())
	cancel()
	out, err := (&Service{}).Local(ctx, "other", "Ads", "mihomo", "http://localhost")
	if err != nil || string(out.Content) != "+.ads.example\n" {
		t.Fatalf("cancelled provider %q %v", out.Content, err)
	}
}

func TestDuplicateResponseUsesCentralRedaction(t *testing.T) {
	const secret = "duplicate-private-capability"
	s := fixture(t)
	secretRaw, _ := json.Marshal(secret)
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, p *model.System) error {
		return p.SystemConfig.SetJSON("rule_proxy_token", secretRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	rulesRaw, _ := json.Marshal([]any{
		map[string]any{"id": "one", "itemType": "rule", "rule_type": "DOMAIN", "value": secret, "policy": "DIRECT"},
		map[string]any{"id": "two", "itemType": "rule", "rule_type": "DOMAIN", "value": secret, "policy": "DIRECT"},
	})
	_, err = s.Repo.UpdateProfile(s.Context(), "other", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", rulesRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	out := s.Post("/api/rules/find-duplicates?profile=other", s.Login()).WantStatus(200)
	var dupBody struct {
		Duplicates []any `json:"duplicates"`
	}
	if err := json.Unmarshal(out.Body, &dupBody); err != nil || strings.Contains(out.Text(), secret) || len(dupBody.Duplicates) != 1 {
		t.Fatal("duplicate response bypassed credential redaction")
	}
}

func TestBinaryCacheDeliveryRemainsOpaque(t *testing.T) {
	for _, data := range [][]byte{{0xff, 0, 0x7f}, []byte("opaque-mrs\n")} {
		t.Run(fmt.Sprintf("%x", data), func(t *testing.T) {
			s := fixture(t)
			source := map[string]any{"id": "binary", "name": "Binary", "source_type": "content", "behavior": "domain", "format": "mrs", "content": ""}
			err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
				p.RuleLibrary = append(p.RuleLibrary, testjson.Decode[model.RuleLibraryItem](t, source))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Repo.WriteSharedText(s.Context(), "rules/"+shared.CacheFilename(testjson.Decode[model.RuleConfig](t, source)), data); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{"", "?target=mihomo"} {
				out := s.Get("/api/rules/local/Binary" + query).WantStatus(200)
				if !bytes.Equal(out.Body, data) {
					t.Fatalf("opaque content changed: %x", out.Body)
				}
			}
		})
	}
}

func TestReviewN811DuplicateScanPreservesNullPolicy(t *testing.T) {
	s := fixture(t)
	_, err := s.Repo.UpdateProfile(s.Context(), "other", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", json.RawMessage(`[
			{"id":"null_1","itemType":"rule","rule_type":"DOMAIN","value":"nullpolicy.example","policy":null,"enabled":true},
			{"id":"null_2","itemType":"rule","rule_type":"DOMAIN","value":"nullpolicy.example","policy":"DIRECT","enabled":true},
			{"id":"abs_1","itemType":"rule","rule_type":"DOMAIN","value":"defaultpolicy.example","enabled":true},
			{"id":"abs_2","itemType":"rule","rule_type":"DOMAIN","value":"defaultpolicy.example","policy":"DIRECT","enabled":true}
		]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	out := s.Post("/api/rules/find-duplicates?profile=other", s.Login()).WantStatus(200)
	var result scanResult
	if err := json.Unmarshal(out.Body, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Duplicates) != 2 {
		t.Fatalf("unexpected duplicate groups: %s", out.Body)
	}
	nullGroup, defaultGroup := result.Duplicates[0], result.Duplicates[1]
	// An explicit null policy is an empty reference in the plain rule record
	// (O18 T1c): it still conflicts and never reads as the DIRECT default.
	if nullGroup.Value != "nullpolicy.example" || !nullGroup.Conflict || string(nullGroup.Occurrences[0].Policy) != `""` {
		t.Fatalf("explicit null policy lost: %+v", nullGroup)
	}
	if defaultGroup.Value != "defaultpolicy.example" || defaultGroup.Conflict || string(defaultGroup.Occurrences[0].Policy) != `"DIRECT"` {
		t.Fatalf("absent policy did not default to DIRECT: %+v", defaultGroup)
	}
}
