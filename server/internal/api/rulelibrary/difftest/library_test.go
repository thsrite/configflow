package difftest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func body(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
func TestRuleLibraryEditAndChecksHTTPParity(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte("+.source.example\n"))
	}))
	defer origin.Close()
	w := newWorld(t, seed(t))
	for _, item := range []map[string]any{{"id": "manual", "name": "Manual", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.manual.example\n", "update_interval": 7200}, {"id": "remote", "name": "Remote", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/ok"}} {
		w.call("POST", "/api/rule-library", body(item), true)
	}
	w.call("GET", "/api/rule-library/content/remote", "", false)
	w.call("GET", "/api/profiles/default/rule-library/content/remote", "", false)
	for _, item := range []map[string]any{{"name": "Renamed", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.manual.example\n"}, {"name": "Edited", "source_type": "content", "behavior": "domain", "format": "text", "content": "+.edited.example\n", "has_ip_part": true, "base_url": "https://ignored.example"}, {"name": "Rejected", "source_type": "content", "behavior": "domain", "format": "text", "content": "IP-CIDR,192.0.2.0/24"}} {
		w.call("PUT", "/api/rule-library/manual", body(item), true)
	}
	w.call("PUT", "/api/rule-library/missing", `{}`, true)
	w.call("PUT", "/api/rule-library/manual", `[]`, true)
	w.call("PUT", "/api/rule-library/remote", body(map[string]any{"name": "Failed edit", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/missing"}), true)
	w.call("PUT", "/api/rule-library/remote", body(map[string]any{"name": "Remote renamed", "source_type": "url", "behavior": "domain", "format": "text", "url": origin.URL + "/ok"}), true)
	w.call("POST", "/api/rule-library/cache", `{"rule_ids":["manual","remote"]}`, true)
	w.call("POST", "/api/rule-library/cache", `{"rule_ids":[]}`, true)
	w.call("POST", "/api/rule-library/cache", `{"rule_ids":["missing"]}`, true)
	for _, path := range []string{"/ok", "/missing"} {
		w.call("POST", "/api/rule-library/test-single", body(map[string]string{"url": origin.URL + path}), true)
	}
	w.call("POST", "/api/rule-library/test-single", `{}`, true)
	w.call("POST", "/api/rule-library/test", `{}`, true)
	w.call("DELETE", "/api/rule-library/manual", "", true)
	w.call("DELETE", "/api/rule-library/manual", "", true)
	for _, methodPath := range []struct{ method, path string }{{"POST", "/api/rule-library/cache"}, {"POST", "/api/rule-library/test"}, {"POST", "/api/rule-library/test-single"}, {"PUT", "/api/rule-library/remote"}, {"DELETE", "/api/rule-library/remote"}} {
		w.call(methodPath.method, methodPath.path, `{}`, false)
	}
}
func TestDraftNativeRevisionContract(t *testing.T) {
	w := newWorld(t, seed(t))
	w.call("POST", "/api/rule-library", `{"id":"manual","name":"中文 <manual>","source_type":"content","behavior":"domain","format":"text","content":"+.old.example\n","extra":{"z":2,"a":1}}`, true)
	path := "/api/rule-library/manual/draft-content"
	read := func() (string, string) {
		py := w.call("GET", path, "", true)
		goReply := w.srv.Get(path, httpxtest.Bearer(w.token)).WantStatus(200)
		return py.Body.(map[string]any)["revision"].(string), goReply.Object().GetString("revision")
	}
	pyFirst, goFirst := read()
	pyAgain, goAgain := read()
	if pyFirst != pyAgain || goFirst != goAgain {
		t.Fatal("per-side draft token is not stable")
	}
	if pyFirst == goFirst {
		t.Fatal("D2 Unicode fixture did not expose the native encoding difference")
	}
	for _, method := range []string{"GET", "PUT"} {
		w.call(method, path, `{}`, false)
	}
	w.call("GET", "/api/rule-library/missing/draft-content", "", true)
	w.callWithBody("PUT", path, body(map[string]string{"revision": pyFirst, "content": "+.new.example\n"}), body(map[string]string{"revision": goFirst, "content": "+.new.example\n"}), true)
	pyNext, goNext := read()
	if pyNext == pyFirst || goNext == goFirst {
		t.Fatal("content changes did not invalidate editor tokens")
	}
	w.callWithBody("PUT", path, body(map[string]string{"revision": pyFirst, "content": "+.stale.example\n"}), body(map[string]string{"revision": goFirst, "content": "+.stale.example\n"}), true)
	w.callWithBody("PUT", path, body(map[string]string{"revision": pyNext, "content": "+.x.example\n", "name": "forbidden"}), body(map[string]string{"revision": goNext, "content": "+.x.example\n", "name": "forbidden"}), true)
	w.call("PUT", "/api/rule-library/missing/draft-content", `{"revision":"x","content":"new.example"}`, true)
}
