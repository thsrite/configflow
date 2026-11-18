package difftest

import (
	"reflect"
	"testing"

	apisetup "configflow/internal/api/setup"
)

func TestSetupFlaskHTTPStateAndAuthentication(t *testing.T) {
	w := newWorld(t)
	apisetup.Register(w.srv.App.Router)
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path := "/api/setup/" + id
	call := func(method, path, body string) {
		t.Helper()
		w.check(request{Method: method, Path: path, Body: &body, Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}})
	}
	for _, item := range []struct{ method, path string }{{"GET", "/api/setup"}, {"POST", "/api/setup"}, {"GET", path}, {"PATCH", path}, {"POST", path + "/source"}} {
		w.check(request{Method: item.method, Path: item.path, Headers: map[string]string{"Authorization": ""}})
	}
	call("HEAD", "/api/setup", `{"goal":"router"}`)
	for _, body := range []string{"null", "[]", "true", `{"goal":"bad"}`, `{"goal":"router","name":" "}`, `{"id":"bad","goal":"router","name":"Synthetic"}`} {
		call("POST", "/api/setup", body)
	}
	create := `{"id":"` + id + `","goal":"router","name":"Synthetic wizard"}`
	call("POST", "/api/setup", create)
	call("POST", "/api/setup", create)
	call("GET", path, "")
	call("HEAD", path, `{"step":0}`)
	for _, body := range []string{"null", "[]", `{"step":true}`, `{"step":2}`, `{"step":1.0}`, `{"unrelated":1}`, `{"router_mode":"managed"}`, `{"router_id":"missing"}`, `{"step":0}`} {
		call("PATCH", path, body)
	}
	for _, body := range []string{`{"url":"file:///synthetic"}`, `{"url":null}`, `{"node_ids":[null]}`, `{"subscription_ids":{}}`, `{"node_ids":["missing"]}`, `{}`, `{"url":"https://example.com/sub?token=synthetic"}`} {
		call("POST", path+"/source", body)
	}
	call("GET", "/api/setup", "")
	call("POST", path+"/unknown", `{}`)
	call("POST", path+"/check", `{}`)
	call("POST", path+"/router-draft", `{"router":null}`)
	call("POST", path+"/abandon", `{"dry_run":1}`)
	call("PATCH", "/api/setup/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", `null`)
	call("POST", "/api/setup/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/unknown", `null`)
	before := snapshot(t, w.srv)
	call("HEAD", "/api/setup", "")
	call("HEAD", path, "")
	if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
		t.Fatal("HEAD changed DB, revisions or files")
	}
	// Native owner readiness is an explicit migration boundary. Unlike Python,
	// Go must not invent a negative installation result while A22 is absent.
	for _, operation := range []string{"prepare", "abandon"} {
		response := sendGo(t, w.srv, request{Method: "POST", Path: path + "/" + operation, Body: body(map[string]any{}), Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}})
		if response.Status != 503 || response.Body.(map[string]any)["message"] != "等待首次使用向导模块" {
			t.Fatalf("missing owner %s: %+v", operation, response)
		}
		if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
			t.Fatal("unavailable owner changed state")
		}
	}
}
