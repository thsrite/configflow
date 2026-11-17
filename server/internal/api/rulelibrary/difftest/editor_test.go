package difftest

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestDraftHEADCannotWrite(t *testing.T) {
	w := newWorld(t, seed(t))
	w.call("POST", "/api/rule-library", `{"id":"manual","name":"Manual","source_type":"content","behavior":"domain","format":"text","content":"+.old.example\n"}`, true)
	path := "/api/rule-library/manual/draft-content"
	pyDraft := w.call("GET", path, "", true)
	pyToken := pyDraft.Body.(map[string]any)["revision"].(string)
	goToken := w.srv.Get(path, httpxtest.Bearer(w.token)).WantStatus(200).Object().GetString("revision")
	before := snapshot(t, w.srv)
	request := map[string]any{"method": "HEAD", "path": path, "body": body(map[string]string{"revision": pyToken, "content": "+.head-write.example\n"}), "headers": map[string]string{"Authorization": "Bearer " + w.token}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.stdin.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if !w.scanner.Scan() {
		t.Fatal(w.stderr.String())
	}
	var py answer
	if err = json.Unmarshal(w.scanner.Bytes(), &py); err != nil {
		t.Fatal(err)
	}
	if py.Status != 200 || py.Error != "" {
		t.Fatalf("Python HEAD: %+v", py)
	}
	document := py.State.(map[string]any)["document"].(map[string]any)
	library := document["shared"].(map[string]any)["rule_library"].([]any)
	if library[0].(map[string]any)["content"] != "+.head-write.example\n" {
		t.Fatal("Python no longer demonstrates HEAD write")
	}
	w.srv.Do("HEAD", path, httpxtest.Bearer(w.token), httpxtest.Body([]byte(body(map[string]string{"revision": goToken, "content": "+.head-write.example\n"})), "application/json")).WantStatus(200)
	if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
		t.Fatal("Go HEAD wrote a draft or cache")
	}
}
func TestBothSidesRejectConcurrentDraftSubmissions(t *testing.T) {
	w := newWorld(t, seed(t))
	w.call("POST", "/api/rule-library", `{"id":"manual","name":"中文","source_type":"content","behavior":"domain","format":"text","content":"+.old.example\n"}`, true)
	path := "/api/rule-library/manual/draft-content"
	py := w.call("GET", path, "", true)
	pyToken := py.Body.(map[string]any)["revision"].(string)
	goToken := w.srv.Get(path, httpxtest.Bearer(w.token)).WantStatus(200).Object().GetString("revision")
	reply := w.callWithBody("TWIN_DRAFT", path, body(map[string]string{"revision": pyToken, "content": "+.new.example\n"}), body(map[string]string{"revision": goToken, "content": "+.new.example\n"}), true)
	statuses := reply.Body.(map[string]any)["statuses"].([]any)
	if !reflect.DeepEqual(statuses, []any{float64(200), float64(409)}) {
		t.Fatal("concurrent Python edits accepted", statuses)
	}
	w.call("GET", path, "", true)
}
