package gatewayonboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/setupflow"
)

const validForm = `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`

func decodeBody(t *testing.T, res *httpxtest.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.Body, &out); err != nil {
		t.Fatalf("%v: %s", err, res.Body)
	}
	return out
}

func testServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	gateway.Wire()
	mihomoenv.Wire()
	setupflow.Wire()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(Register)})
	ctx := s.Context()
	err := s.Repo.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		return c.SetJSON("nodes", json.RawMessage(`[{"id":"n","name":"Example","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, c *model.Profile) error {
		if err := c.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g","name":"Proxy","type":"select","manual_nodes":["n"]}]`)); err != nil {
			return err
		}
		if err := c.SetJSON("rule_configs", json.RawMessage(`[{"id":"match","itemType":"rule","rule_type":"MATCH","policy":"Proxy"}]`)); err != nil {
			return err
		}
		return c.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mixed-port: 7890\n"}`))
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
	raw, err := s.Repo.ExportAllJSON(s.Context(), false)
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
				if decodeBody(t, preview)["ready"] != true {
					t.Fatal(preview.Text())
				}
			}
			before := snapshot(t, s)
			revision, _ := s.Repo.ConfigRevision(s.Context(), "default")
			response := s.Post("/api/profiles/default/gateway-onboarding", admin, requestBody(validForm)).WantStatus(503)
			if decodeBody(t, response)["message"] != "等待首次使用向导模块" || response.Header.Get("Cache-Control") != "no-store" {
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
	if decodeBody(t, preview)["ready"] != true || !bytes.Equal(before, snapshot(t, s)) {
		t.Fatal(preview.Text())
	}
	saved := s.Post(url, admin, requestBody(validForm)).WantStatus(200)
	if decodeBody(t, saved)["draft"] != true || bytes.Equal(before, snapshot(t, s)) {
		t.Fatal(saved.Text())
	}
}

func TestProductionCheckerRejectsLoopWithoutWrites(t *testing.T) {
	s := testServer(t)
	admin := s.Login()
	_, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mihomo", json.RawMessage(`{"custom_config":"dns:\n  nameserver: [udp://127.0.0.1:1053]\n"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/profiles/default/gateway-onboarding/preview", 200}, {"/api/profiles/default/gateway-onboarding", 400}} {
		response := s.Post(tc.path, admin, requestBody(validForm)).WantStatus(tc.status)
		if tc.status == 200 {
			var report struct {
				Ready    bool     `json:"ready"`
				Blockers []string `json:"blockers"`
			}
			if err := json.Unmarshal(response.Body, &report); err != nil || report.Ready || len(report.Blockers) == 0 {
				t.Fatal("preview did not report the DNS blocker", err)
			}
		}
		if !bytes.Equal(before, snapshot(t, s)) {
			t.Fatal("rejected DNS loop wrote storage")
		}
	}
}
