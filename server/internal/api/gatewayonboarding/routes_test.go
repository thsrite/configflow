package gatewayonboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/setupflow"
	"configflow/internal/yamlx"
)

const validForm = `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`

func testServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	gateway.Wire()
	mihomoenv.Wire()
	setupflow.Wire()
	h := Handler{DNSLoop: func(*yamlx.Node, []netip.Addr) (bool, error) { return false, nil }}
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(h.Register)})
	ctx := s.Context()
	_, err := s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("nodes", []any{jsonx.Obj("id", "n", "name", "Example", "type", "ss", "server", "192.0.2.2", "port", 443, "cipher", "aes-128-gcm", "password", "synthetic-node-secret")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Proxy", "type", "select", "manual_nodes", []any{"n"})})
		c.Set("rule_configs", []any{jsonx.Obj("id", "match", "itemType", "rule", "rule_type", "MATCH", "policy", "Proxy")})
		c.Set("mihomo", jsonx.Obj("custom_config", "mixed-port: 7890\n"))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func requestBody(text string) httpxtest.Option {
	return func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(text))
		r.ContentLength = int64(len(text))
		r.Header.Set("Content-Type", "application/json")
	}
}
func snapshot(t *testing.T, s *httpxtest.Server) json.RawMessage {
	t.Helper()
	doc, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(s.DataDir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || strings.HasPrefix(e.Name(), "configflow.sqlite3") || strings.HasSuffix(e.Name(), ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.DataDir, path)
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonx.Compact(doc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(struct {
		Document json.RawMessage
		Files    map[string]string
	}{raw, files})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestHEADIsReadOnlyAndMatchesGET(t *testing.T) {
	s := testServer(t)
	admin := s.Login()
	url := "/api/profiles/default/gateway-onboarding"
	before := snapshot(t, s)
	revision, _ := s.Repo.ConfigRevision(s.Context(), "default")
	get := s.Get(url, admin).WantStatus(200)
	head := s.Do("HEAD", url, admin, requestBody(validForm)).WantStatus(200)
	if len(head.Body) != 0 {
		t.Fatal("HEAD body", head.Text())
	}
	for _, key := range []string{"Cache-Control", "Content-Type", "Content-Length"} {
		if !reflect.DeepEqual(head.Header.Values(key), get.Header.Values(key)) {
			t.Fatalf("HEAD %s=%v GET=%v", key, head.Header.Values(key), get.Header.Values(key))
		}
	}
	afterRevision, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if !bytes.Equal(before, snapshot(t, s)) || afterRevision != revision {
		t.Fatal("HEAD wrote DB/files/revision")
	}
}
func TestBodyShapesAndNoPartialWrites(t *testing.T) {
	s := testServer(t)
	admin := s.Login()
	url := "/api/profiles/default/gateway-onboarding"
	for _, suffix := range []string{"", "/preview"} {
		for _, body := range []string{"", "{", "null", "[]", "false", "0", `"x"`, `{"x":NaN}`, `{"x":Infinity}`, `{"x":-Infinity}`, strings.Repeat("x", 262145)} {
			before := snapshot(t, s)
			response := s.Post(url+suffix, admin, requestBody(body))
			response.WantStatus(400)
			if response.Header.Get("Cache-Control") != "no-store" || !bytes.Equal(before, snapshot(t, s)) {
				t.Fatalf("invalid body changed storage: %q %s", body[:min(24, len(body))], response.Text())
			}
		}
	}
	s.Post(url+"/preview", admin, requestBody(validForm), httpxtest.Header("Content-Type", "text/plain")).WantStatus(200)
}
func TestAuthenticationAndMethodMatrix(t *testing.T) {
	s := testServer(t)
	admin := s.Login()
	for _, tc := range []struct{ method, suffix string }{{"GET", ""}, {"HEAD", ""}, {"POST", ""}, {"POST", "/preview"}} {
		path := "/api/profiles/default/gateway-onboarding" + tc.suffix
		for _, option := range []httpxtest.Option{func(*http.Request) {}, httpxtest.Bearer("synthetic-agent-token")} {
			before := snapshot(t, s)
			s.Do(tc.method, path, option, requestBody(validForm)).WantStatus(401)
			if !bytes.Equal(before, snapshot(t, s)) {
				t.Fatal("unauthorized write")
			}
		}
	}
	s.Get("/api/profiles/default/gateway-onboarding", admin).WantStatus(200)
	s.Get("/api/profiles/missing/gateway-onboarding", admin).WantStatus(404)
	s.Do("HEAD", "/api/profiles/default/gateway-onboarding/preview", admin, requestBody(validForm)).WantStatus(405)
	s.Do("DELETE", "/api/profiles/default/gateway-onboarding", admin).WantStatus(405)
}
func TestSetupUnavailableReturns503BeforeCommit(t *testing.T) {
	for _, raw := range []string{`{"profile_id":"default","goal":"gateway","completed":false}`, "{"} {
		t.Run(raw, func(t *testing.T) {
			s := testServer(t)
			admin := s.Login()
			dir := filepath.Join(s.DataDir, "setup-sessions")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, strings.Repeat("a", 32)+".json"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if raw != "{" {
				s.Get("/api/profiles/default/gateway-onboarding", admin).WantStatus(200)
				preview := s.Post("/api/profiles/default/gateway-onboarding/preview", admin, requestBody(validForm)).WantStatus(200)
				if preview.Object().Value("ready") != true {
					t.Fatal(preview.Text())
				}
			}
			before := snapshot(t, s)
			revision, _ := s.Repo.ConfigRevision(s.Context(), "default")
			response := s.Post("/api/profiles/default/gateway-onboarding", admin, requestBody(validForm)).WantStatus(503)
			if response.Object().GetString("message") != "等待首次使用向导模块" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal(response.Text())
			}
			afterRevision, _ := s.Repo.ConfigRevision(s.Context(), "default")
			if !bytes.Equal(before, snapshot(t, s)) || revision != afterRevision {
				t.Fatal("503 wrote state")
			}
		})
	}
}
func TestSaveAndPreviewHTTP(t *testing.T) {
	s := testServer(t)
	admin := s.Login()
	url := "/api/profiles/default/gateway-onboarding"
	before := snapshot(t, s)
	preview := s.Post(url+"/preview", admin, requestBody(validForm)).WantStatus(200)
	if preview.Object().Value("ready") != true || !bytes.Equal(before, snapshot(t, s)) {
		t.Fatal(preview.Text())
	}
	saved := s.Post(url, admin, requestBody(validForm)).WantStatus(200)
	if saved.Object().Value("draft") != true || bytes.Equal(before, snapshot(t, s)) {
		t.Fatal(saved.Text())
	}
}
