package routers

import (
	"context"
	"errors"
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

// r06TimeoutRecorder records the per-request timeout every device call uses.
type r06TimeoutRecorder struct {
	client   DeviceHTTP
	mu       sync.Mutex
	requests []string
	timeouts []time.Duration
}

func (c *r06TimeoutRecorder) Do(ctx context.Context, method, target string, body any, timeout time.Duration) (*devhttp.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, method+" "+target)
	c.timeouts = append(c.timeouts, timeout)
	c.mu.Unlock()
	return c.client.Do(ctx, method, target, body, timeout)
}
func (c *r06TimeoutRecorder) Close() { c.client.Close() }

// F1: every manual RouterOS request, including writes and compensation, keeps
// the 10 s device timeout (Python TIMEOUT); the edit is detached from request
// cancellation, so a request without one can block forever.
func TestR06RouterOSRequestsKeepDeviceTimeout(t *testing.T) {
	tests := []struct {
		name   string
		action string
		faults []routerfake.Fault
	}{
		{"add", "add", nil},
		{"update", "update", nil},
		{"delete", "delete", nil},
		{"update compensation", "update", []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 3, Status: 503}}},
		{"delete compensation", "delete", []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 3, Status: 503}}},
		{"add compensation", "add", []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 3, Status: 503}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := r06Base("routeros", tt.action)
			tc.ROS.Faults = tt.faults
			fake := routerfake.New(nil)
			fake.Reset(tc.ROS)
			server := httptest.NewServer(fake)
			defer server.Close()
			recorder := &r06TimeoutRecorder{client: devhttp.New(devhttp.Config{BasicAuth: true, Username: "synthetic-admin", Password: "synthetic-secret"})}
			driver := NewRouterOSWithHTTP(server.URL, recorder)
			defer driver.Close()
			rows, err := driver.ReadRoutes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var expected *EditableRoute
			if tt.action != "add" {
				expected = rows[0]
			}
			_, _ = driver.MutateRoute(context.Background(), tt.action, expected, tc.Values)
			writes := 0
			for i, timeout := range recorder.timeouts {
				if !strings.HasPrefix(recorder.requests[i], "GET ") {
					writes++
				}
				if timeout != rosTimeout {
					t.Errorf("%s used timeout %v, want %v", recorder.requests[i], timeout, rosTimeout)
				}
			}
			if writes == 0 {
				t.Fatal("no write observed")
			}
		})
	}
}

// F1 end to end: a device that accepts a PATCH and never answers must not
// hang the detached edit (and the router operation lock) forever.
func TestR06RouterOSUnansweredWriteTimesOut(t *testing.T) {
	tc := r06Base("routeros", "update")
	fake := routerfake.New(nil)
	fake.Reset(tc.ROS)
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-stop:
			case <-r.Context().Done():
			}
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer server.CloseClientConnections()
	defer close(stop)
	driver := NewRouterOS(server.URL, "synthetic-admin", "synthetic-secret", false)
	defer driver.Close()
	rows, err := driver.ReadRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := driver.MutateRoute(context.Background(), "update", rows[0], tc.Values)
		done <- err
	}()
	select {
	case err := <-done:
		if routerErrorKind(err) != KindUncertainWrite {
			t.Fatalf("unanswered write error = %v, want an uncertain write after reconciliation", err)
		}
	case <-time.After(3 * rosTimeout):
		t.Fatal("manual RouterOS write without a device answer did not time out")
	}
	if got := fake.Snapshot().Tables["/ip/route"][0]["comment"]; got != "office" {
		t.Fatalf("table changed: comment=%v", got)
	}
}

// F3: RouterOS GET and DELETE carry no request body, like Python requests
// (`json=` only when values are given). A nil Row must not become "null".
func TestR06RouterOSBodylessRequests(t *testing.T) {
	tc := r06Base("routeros", "delete")
	fake := routerfake.New(nil)
	fake.Reset(tc.ROS)
	var mu sync.Mutex
	bodies := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies[r.Method] = append(bodies[r.Method], string(data)+"|"+r.Header.Get("Content-Type"))
		mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	driver := NewRouterOS(server.URL, "synthetic-admin", "synthetic-secret", false)
	defer driver.Close()
	rows, err := driver.ReadRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.MutateRoute(context.Background(), "delete", rows[0], nil); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if len(bodies[method]) == 0 {
			t.Fatalf("no %s observed", method)
		}
		for _, body := range bodies[method] {
			if body != "|" {
				t.Errorf("%s sent body/content type %q, want none", method, body)
			}
		}
	}
}

// F2: only secrets are hidden. A device user named like a word of the fixed
// diagnostics (RouterOS users are often called "api") must not replace the
// actionable permission message, while a password echo stays hidden.
func TestR06RedactionHidesOnlySecrets(t *testing.T) {
	tests := []struct {
		name, username, echo string
		status               int
		want                 string // exact message; empty: must contain echo and not be hidden
	}{
		{"username in permission help", "api", "", 403, messageROSForbidden + permissionHelp},
		{"username echoed by device", "synthetic-admin", "synthetic-admin", 400, ""},
		{"password echoed by device", "api", "synthetic-secret", 400, messageEditSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := r06Base("routeros", "add")
			tc.ROS.Faults = []routerfake.Fault{{Method: "PUT", Path: "/ip/route", Count: 1, Status: tt.status, Body: routerfake.Row{"message": tt.echo}}}
			fake := routerfake.New(nil)
			fake.Reset(tc.ROS)
			server := httptest.NewServer(fake)
			defer server.Close()
			driver := NewRouterOS(server.URL, tt.username, "synthetic-secret", false)
			defer driver.Close()
			_, err := driver.MutateRoute(context.Background(), "add", nil, tc.Values)
			e, ok := errors.AsType[*RouterError](err)
			if !ok {
				t.Fatalf("error = %#v, want RouterError", err)
			}
			if tt.want != "" && e.Message != tt.want {
				t.Fatalf("message = %q, want %q", e.Message, tt.want)
			}
			if tt.want == "" && (e.Message == messageEditSecret || !strings.Contains(e.Message, tt.echo)) {
				t.Fatalf("message = %q, want the device text %q", e.Message, tt.echo)
			}
		})
	}
}
