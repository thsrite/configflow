package routers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiconfig "configflow/internal/api/config"
	apiprofiles "configflow/internal/api/profiles"
	api "configflow/internal/api/routers"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func TestR15OwnedRecordCredentialRepairUsesRepositoryGuard(t *testing.T) {
	fake := routerfake.New(nil)
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "synthetic-user" || password != "synthetic-rotated-password" {
			t.Error("probe used stale credentials")
		}
		fake.ServeHTTP(w, r)
	}))
	defer device.Close()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(api.Register, apiconfig.Register, apiprofiles.Register)})
	core.Wire()
	createSpare(t, s)
	login := s.Login()
	cfg := inputConfig(device.URL)
	cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)["last_probe"] = map[string]any{"ok": true, "at": "2000-01-01T00:00:00"}
	saveFixtureConfig(t, s, cfg)
	before, err := s.Repo.GetProfile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(before.GetObject("routers").GetList("items")[0])
	_, rows, err := (&core.ManagementService{Repo: s.Repo}).Records(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	held, release, ok := core.JobLock(s.Context(), "default", "router-one", rows[0])
	if !ok {
		t.Fatal("L1 unavailable")
	}
	manifest := json.RawMessage(`{"future":"retained-ownership"}`)
	err = s.Repo.SaveRouterPublicationJSON(held, "default", expected, repo.RouterPublicationJSON{Manifest: repo.JSONUpdate{Set: true, Value: manifest}, LastRelease: repo.JSONUpdate{Set: true, Value: json.RawMessage(`{"ok":true,"at":"2000-01-01T00:00:00"}`)}})
	release()
	if err != nil {
		t.Fatal(err)
	}
	root := "/api/routers/router-one"
	s.Delete(root, login).WantStatus(409)
	s.Put(root, login, httpxtest.JSON(map[string]string{"endpoint": "http://192.0.2.9"})).WantStatus(400)
	changed := s.Put(root, login, httpxtest.JSON(map[string]string{"password": "synthetic-rotated-password"}))
	changed.WantStatus(200)
	if strings.Contains(changed.Text(), "synthetic-rotated-password") || changed.Object().GetObject("router").Value("last_probe") != nil {
		t.Fatal("repair exposed credentials or kept a stale probe")
	}
	profile, err := s.Repo.GetProfile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	record := profile.GetObject("routers").GetList("items")[0].(*jsonx.Object)
	raw, _ := json.Marshal(record.GetObject("ros_policy_state"))
	if string(raw) != string(manifest) || record.GetString("password") != "synthetic-rotated-password" {
		t.Fatal("repair changed ownership or lost the new password")
	}
	if len(fake.Calls()) != 0 {
		t.Fatal("settings touched the device")
	}
	s.Post(root+"/probe", login).WantStatus(200)
	assertBatchProtected(t, s, login)
	s.Put(root, login, httpxtest.JSON(map[string]string{"password": ""})).WantStatus(200)
	profile, _ = s.Repo.GetProfile(s.Context(), "default")
	record = profile.GetObject("routers").GetList("items")[0].(*jsonx.Object)
	if record.GetString("password") != "synthetic-rotated-password" || record.GetObject("last_probe") == nil || !record.GetObject("last_probe").GetBool("ok") {
		t.Fatal("empty password repair cleared credentials or a valid probe")
	}
}

func createSpare(t *testing.T, s *httpxtest.Server) {
	t.Helper()
	if _, err := s.Repo.CreateProfile(s.Context(), jsonx.Obj("id", "spare", "name", "Spare"), ""); err != nil {
		t.Fatal(err)
	}
}
func assertBatchProtected(t *testing.T, s *httpxtest.Server, login ...httpxtest.Option) {
	t.Helper()
	before, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	whole, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	whole.GetObject("profiles").GetObject("default").Set("routers", jsonx.Obj("items", []any{}))
	for _, tc := range []struct {
		method, path string
		body         any
	}{{"POST", "/api/profiles/default/import", map[string]any{"routers": map[string]any{"items": []any{}}}}, {"POST", "/api/config/import", whole}, {"POST", "/api/config/reset", nil}, {"DELETE", "/api/profiles/default?new_default=spare", nil}} {
		options := append([]httpxtest.Option{}, login...)
		if tc.body != nil {
			options = append(options, httpxtest.JSON(tc.body))
		}
		s.Do(tc.method, tc.path, options...).WantStatus(409)
	}
	after, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(before, after) {
		t.Fatal("batch HTTP changed protected configuration")
	}
	afterFingerprint, err := s.Repo.Store().Fingerprint()
	if err != nil || afterFingerprint != fingerprint {
		t.Fatal("batch HTTP committed protected state")
	}
}
