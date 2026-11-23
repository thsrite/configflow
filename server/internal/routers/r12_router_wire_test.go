package routers

import (
	"bytes"
	"configflow/internal/routers/ros"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

func TestR12RouterOSRawStageRequests(t *testing.T) {
	state := r08State()
	state.Tables[ros.PathRoutes] = state.Tables[ros.PathRoutes][:1]
	state.Tables[ros.PathNAT][0]["out-interface"] = "bridge"
	state.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathNAT, Count: 1, Status: 403}}
	f := routerfake.New(nil)
	f.Reset(state)
	var mu sync.Mutex
	seen := []r09Wire{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		mu.Lock()
		seen = append(seen, r09Wire{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body), ContentType: r.Header.Get("Content-Type"), Authorization: r.Header.Get("Authorization")})
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.ServeHTTP(w, r)
	}))
	defer server.Close()
	recorder := &r06TimeoutRecorder{client: devhttp.New(devhttp.Config{BasicAuth: true, Username: "synthetic-user", Password: "synthetic-password"})}
	d := NewRouterOSWithHTTP(server.URL, recorder)
	defer d.Close()
	desired := r12Desired()
	s, e := d.ReadState(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	report, e := BuildReport(desired, s)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := BuildPlan(t.Context(), &Router{RouterSettings: RouterSettings{Type: "routeros"}}, &Inspection{Desired: desired, State: s, Report: report}, d)
	if e != nil {
		t.Fatal(e)
	}
	result := Execute(t.Context(), plan, d, nil)
	if result.OK || result.FailedStage == nil || *result.FailedStage != StageNATBypass {
		t.Fatalf("failure boundary not reached %+v", result)
	}
	routeBody := `{"comment":"cf-fake-ip","disabled":"false","dst-address":"198.18.0.0/16","gateway":"192.0.2.4"}`
	found, removed := false, false
	mu.Lock()
	defer mu.Unlock()
	for _, r := range seen {
		if r.Authorization != "Basic c3ludGhldGljLXVzZXI6c3ludGhldGljLXBhc3N3b3Jk" {
			t.Fatal("Basic auth bytes", r)
		}
		if r.Method == "GET" || r.Method == "DELETE" {
			if r.Body != "" || r.ContentType != "" {
				t.Fatal("read/delete changed wire contract", r)
			}
		}
		if r.Method == "PUT" && r.Path == "/rest/ip/route" && r.Body == routeBody && r.ContentType == "application/json" {
			found = true
		}
		if r.Method == "DELETE" && strings.HasPrefix(r.Path, "/rest/ip/route/") {
			removed = true
		}
	}
	if !found || !removed {
		t.Fatalf("raw write/rollback boundary missing: route=%v delete=%v", found, removed)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, v := range recorder.timeouts {
		if v != 10*time.Second {
			t.Fatalf("production timeout changed %v", v)
		}
	}
}
