package agents

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/model"
)

func TestTypedDeploymentBusyRetrySendsIdenticalRawRequests(t *testing.T) {
	e := newEnv(t)
	type request struct {
		method, path, authorization, contentType string
		body                                     []byte
	}
	seen := make(chan request, 2)
	var count atomic.Int32
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		seen <- request{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), raw}
		w.Header().Set("Content-Type", "application/json")
		if count.Add(1) == 1 {
			w.WriteHeader(409)
			io.WriteString(w, `{"success":false,"message":"another service operation is in progress"}`)
			return
		}
		io.WriteString(w, `{"success":true,"status":"ready"}`)
	}))
	defer device.Close()
	parsed, _ := url.Parse(device.URL)
	host, port, _ := net.SplitHostPort(parsed.Host)
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	a := &model.Agent{Host: host, Port: model.Int(number), Token: "synthetic-original"}
	e.m.SetClient(HTTPClient{})
	e.m.sleep = func(_ time.Duration) {}
	raw := []byte("{ \"config\": \"日志\", \"x\": 2.00 }\n")
	header := http.Header{"authorization": {"Bearer synthetic-override"}, "Content-Type": {"application/json; charset=utf-8"}}
	result, err := e.m.DeploymentRequestModel(e.ctx, a, http.MethodPost, "deployments", header, raw, nil)
	if err != nil || result.Status != "ready" {
		t.Fatal(result, err)
	}
	for range 2 {
		request := <-seen
		if request.method != "POST" || request.path != "/api/deployments" || request.authorization != "Bearer synthetic-override" || request.contentType != "application/json; charset=utf-8" || !bytes.Equal(request.body, raw) {
			t.Fatalf("raw request changed: %#v", request)
		}
	}
	if header.Get("Authorization") != "" || header["authorization"][0] != "Bearer synthetic-override" {
		t.Fatal("caller header mutated")
	}
}
