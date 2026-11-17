package difftest

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// A20 review: Flask serves HEAD through the GET/POST view and the handler
// takes every method other than GET as the POST branch, so an authorised HEAD
// with a JSON body persisted IPv4 forwarding on the device. Go keeps HEAD on
// the read branch. The exact Python write is asserted, then Go's zero writes.
func TestA20PythonHEADForwardingWriteIsFixed(t *testing.T) {
	w := newWorld(t)
	w.call("EDIT", "default", `{"routers":{"items":[{"id":"router","name":"Main","proxy_gateway_agent_id":"gateway"}]}}`, true)
	path := "/api/agents/gateway-forwarding"
	body := `{"agent_id":"gateway"}`
	headers := map[string]string{"Authorization": "Bearer " + w.token, "Content-Type": "application/json"}

	input, _ := json.Marshal(map[string]any{"method": "HEAD", "path": path, "headers": headers, "body": body})
	_, _ = fmt.Fprintln(w.stdin, string(input))
	if !w.scanner.Scan() {
		t.Fatalf("oracle stopped %v %s", w.scanner.Err(), w.stderr.String())
	}
	var want answer
	if err := json.Unmarshal(w.scanner.Bytes(), &want); err != nil || want.Error != "" {
		t.Fatalf("oracle %s err %v", w.scanner.Bytes(), err)
	}
	pyCalls := w.fake.take()

	before := snapshot(t, w.srv)
	request := httptest.NewRequest("HEAD", "http://localhost"+path, strings.NewReader(body))
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	w.srv.Handler().ServeHTTP(rec, request)
	goCalls := w.fake.take()

	pyWrote := false
	for _, call := range pyCalls {
		pyWrote = pyWrote || call.Method == "POST" && call.Path == "/api/linux-gateway/forwarding"
	}
	if want.Status != 200 || !pyWrote {
		t.Fatalf("Python HEAD contract changed: status=%d calls=%+v", want.Status, pyCalls)
	}
	for _, call := range goCalls {
		if call.Method != "GET" {
			t.Fatalf("Go HEAD wrote to the Agent: %+v", goCalls)
		}
	}
	if rec.Code != 200 {
		t.Fatalf("Go HEAD answered %d", rec.Code)
	}
	if !reflect.DeepEqual(jsonValue(t, before), jsonValue(t, snapshot(t, w.srv))) {
		t.Fatal("Go HEAD changed stored state")
	}
}
