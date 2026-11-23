package routers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

type r17Timeouts struct {
	DeviceHTTP
	timeouts []time.Duration
}

func (s *r17Timeouts) Do(ctx context.Context, method, url string, body any, timeout time.Duration) (*devhttp.Response, error) {
	s.timeouts = append(s.timeouts, timeout)
	return s.DeviceHTTP.Do(ctx, method, url, body, timeout)
}
func TestR17RawWireAndProductionTimeouts(t *testing.T) {
	for _, vendor := range []string{"routeros", "ikuai"} {
		t.Run(vendor, func(t *testing.T) {
			var claimed atomic.Bool
			var endpoint http.Handler
			f := routerfake.New(nil)
			f.Reset(r17ROSState(true))
			ik := routerfake.NewIkuai(r17IKState())
			if vendor == "routeros" {
				endpoint = f
			} else {
				endpoint = ik
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				r.Body = io.NopCloser(bytes.NewReader(data))
				if r.Method == http.MethodGet || r.Method == http.MethodDelete {
					if len(data) > 0 {
						t.Errorf("unexpected %s bytes %q", r.Method, data)
					}
				} else if r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing JSON content type")
				}
				if vendor == "routeros" {
					user, password, ok := r.BasicAuth()
					if !ok || user != "synthetic-user" || password != "synthetic-pass" {
						t.Error("wrong raw Basic authorization")
					}
					if r.URL.Path == "/rest/ip/dhcp-server/lease/make-static" {
						claimed.Store(true)
						if string(data) != `{".id":"*10"}` {
							t.Errorf("make-static bytes %q", data)
						}
					}
				} else if bytes.Contains(data, []byte(`"func_name":"dhcp_static"`)) && bytes.Contains(data, []byte(`"action":"show"`)) {
					claimed.Store(true)
					if string(data) != `{"action":"show","func_name":"dhcp_static","param":{"TYPE":"static_total,static_data","limit":"0,1000"}}` {
						t.Errorf("static_ view bytes %q", data)
					}
				}
				endpoint.ServeHTTP(w, r)
			}))
			defer server.Close()
			var d FixedIPDriver
			var spy *r17Timeouts
			if vendor == "routeros" {
				driver := NewRouterOS(server.URL, "synthetic-user", "synthetic-pass", false)
				spy = &r17Timeouts{DeviceHTTP: driver.HTTP()}
				driver.SetHTTP(spy)
				d = driver
			} else {
				driver := NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
				spy = &r17Timeouts{DeviceHTTP: driver.client}
				driver.client = spy
				d = driver
			}
			defer d.Close()
			op, err := d.PreviewFixedIP(t.Context(), "192.0.2.21", "02:00:00:00:00:21", "Synthetic")
			if err != nil {
				t.Fatal(err)
			}
			b, err := d.ApplyFixedIP(t.Context(), op)
			if err != nil {
				t.Fatal(err)
			}
			if err = d.RollbackFixedIP(t.Context(), b); err != nil {
				t.Fatal(err)
			}
			if !claimed.Load() {
				t.Fatal("raw protocol branch not reached")
			}
			if len(spy.timeouts) == 0 {
				t.Fatal("transport not exercised")
			}
			for _, timeout := range spy.timeouts {
				if timeout != 10*time.Second {
					t.Fatalf("production timeout overridden: %v", timeout)
				}
			}
		})
	}
}
