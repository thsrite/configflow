package difftest

import (
	"reflect"
	"strings"
	"testing"

	apisetup "configflow/internal/api/setup"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/setup"
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
	call("POST", "/api/setup", strings.Repeat(" ", 32769))
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
	// A21 now wires the real DNS preparation owner. As in Python, validate
	// sources before generating: this subscription has no cached nodes.
	// Installation evidence does not block preparation source validation.
	prepared := w.check(request{Method: "POST", Path: path + "/prepare", Body: body(map[string]any{})})
	if prepared.Status != 400 || prepared.Body.(map[string]any)["message"] != "当前没有可用节点，请先读取订阅或选择支持的节点" {
		t.Fatalf("empty source prepare: %+v", prepared)
	}
	if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
		t.Fatal("failed preparation changed state")
	}
	// Keep the fail-closed dependency contract explicitly isolated. The full
	// batch tree now wires A22, so it also exercises real abandonment below.
	func() {
		unwired := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, DataDir: w.srv.DataDir, Register: httpxtest.Routes(func(r *httpx.Router) {
			apisetup.NewHandler(w.srv.Repo, &setup.ExternalOwners{}).Register(r)
		})})
		response := sendGo(t, unwired, request{Method: "POST", Path: path + "/abandon", Body: body(map[string]any{}), Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}})
		if response.Status != 503 || response.Body.(map[string]any)["message"] != "等待首次使用向导模块" {
			t.Fatalf("missing installation owner: %+v", response)
		}
		if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
			t.Fatal("unavailable owner changed state")
		}
	}()
	abandoned := w.check(request{Method: "POST", Path: path + "/abandon", Body: body(map[string]any{})})
	if abandoned.Status != 200 {
		t.Fatalf("wired A22 abandonment: %+v", abandoned)
	}
}
