package routers_test

import (
	"configflow/internal/routers/oplock"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiconfig "configflow/internal/api/config"
	apiprofiles "configflow/internal/api/profiles"
	api "configflow/internal/api/routers"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/repo"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/testjson"
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
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	api.New(api.Deps{Repo: s.Repo, Routers: core.New(s.Repo, core.Options{})}).Register(s.App.Router)
	apiconfig.New(apiconfig.Deps{Repo: s.Repo}).Register(s.App.Router)
	apiprofiles.New(apiprofiles.Deps{Repo: s.Repo}).Register(s.App.Router)
	wireRouterTestHooks(t)
	createSpare(t, s)
	login := s.Login()
	cfg := inputConfig(device.URL)
	cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)["last_probe"] = map[string]any{"ok": true, "at": "2000-01-01T00:00:00"}
	saveFixtureConfig(t, s, cfg)
	before, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(before.Routers.Items[0])
	records, err := (&core.ManagementService{Repo: s.Repo}).Records(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	held, release, lockErr := oplock.RouterJobLock(s.Context(), "default", "router-one", records.Routers[0])
	if lockErr != nil {
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
	if strings.Contains(changed.Text(), "synthetic-rotated-password") || decodeJSON(t, changed)["router"].(map[string]any)["last_probe"] != nil {
		t.Fatal("repair exposed credentials or kept a stale probe")
	}
	profile, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	record := profile.Routers.Items[0]
	raw, _ := json.Marshal(record.ROSPolicyState)
	if string(raw) != string(manifest) || record.Password != "synthetic-rotated-password" {
		t.Fatal("repair changed ownership or lost the new password")
	}
	if len(fake.Calls()) != 0 {
		t.Fatal("settings touched the device")
	}
	s.Post(root+"/probe", login).WantStatus(200)
	assertBatchProtected(t, s, login)
	s.Put(root, login, httpxtest.JSON(map[string]string{"password": ""})).WantStatus(200)
	profile, _ = s.Repo.Profile(s.Context(), "default")
	record = profile.Routers.Items[0]
	if record.Password != "synthetic-rotated-password" || record.LastProbe == nil || !record.LastProbe.OK {
		t.Fatal("empty password repair cleared credentials or a valid probe")
	}
}

func createSpare(t *testing.T, s *httpxtest.Server) {
	t.Helper()
	if _, err := s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "spare", Name: "Spare"}, ""); err != nil {
		t.Fatal(err)
	}
}
func assertBatchProtected(t *testing.T, s *httpxtest.Server, login ...httpxtest.Option) {
	t.Helper()
	before, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	var whole map[string]any
	if err := json.Unmarshal(before, &whole); err != nil {
		t.Fatal(err)
	}
	whole["profiles"].(map[string]any)["default"].(map[string]any)["routers"] = map[string]any{"items": []any{}}
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
	after, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(before, after) {
		t.Fatal("batch HTTP changed protected configuration")
	}
	afterFingerprint, err := s.Repo.Store().Fingerprint()
	if err != nil || afterFingerprint != fingerprint {
		t.Fatal("batch HTTP committed protected state")
	}
}
