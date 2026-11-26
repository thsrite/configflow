package auth_test

import (
	"encoding/json"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testjson"
)

func TestLoginBindingKeepsCredentialErrors(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "user", Password: "pw", SecretKey: "test-secret"})
	for _, body := range []string{`{}`, `{"username":true,"password":"pw"}`, `{"username":"user","password":123}`, `{"username":[],"password":{}}`, `{"Username":"user","password":"pw"}`, `{"username":"user","Password":"pw"}`} {
		srv.Post("/api/auth/login", httpxtest.Body([]byte(body), "application/json")).WantStatus(401)
	}
	for _, ct := range []string{"application/json", "application/json; charset=utf-8"} {
		srv.Post("/api/auth/login", httpxtest.Body([]byte(`{"username":"user","password":"pw"}`), ct)).WantStatus(200)
	}
	wantBadRequest := map[string]any{"success": false, "message": httpx.DescBadRequest}
	for _, body := range []string{"", `{"username":`, "{} {}", "[]", "null", "true"} {
		res := srv.Post("/api/auth/login", httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
		if diff := testjson.Diff(wantBadRequest, json.RawMessage(res.Body)); diff != "" {
			t.Fatalf("body %q:\n%s", body, diff)
		}
	}
	srv.Post("/api/auth/login", httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(415)
}
