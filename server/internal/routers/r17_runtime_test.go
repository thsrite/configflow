package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/routers/routerfake"
)

func TestR17IkuaiRuntimeWriteLifetime(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			fake := routerfake.NewIkuai(r17IKState())
			entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var call struct {
					Function string `json:"func_name"`
					Action   string `json:"action"`
				}
				_ = json.Unmarshal(body, &call)
				if call.Function == "dhcp_static" && call.Action == "add" {
					close(entered)
					select {
					case <-r.Context().Done():
						close(cancelled)
						return
					case <-release:
					}
				}
				fake.ServeHTTP(w, r)
			}))
			defer server.Close()
			driver := NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
			defer driver.Close()
			op, err := driver.PreviewFixedIP(t.Context(), "192.0.2.21", "02:00:00:00:00:21", "Synthetic")
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, disconnect := context.WithCancel(t.Context())
			defer disconnect()
			result := make(chan error, 1)
			if err = runtime.Spawn(runtime.BindRequest(request), "r17-ikuai", func(ctx context.Context) { _, err := driver.ApplyFixedIP(ctx, op); result <- err }); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("write not reached")
			}
			if mode == "disconnect" {
				disconnect()
				close(release)
			} else {
				shutdown, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				runtime.Shutdown(shutdown)
				select {
				case <-cancelled:
				case <-time.After(2 * time.Second):
					t.Fatal("shutdown did not cancel production write HTTP")
				}
			}
			select {
			case err := <-result:
				if (err != nil) != (mode == "shutdown") {
					t.Fatalf("%s: %v", mode, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("write owner did not finish")
			}
			rows := fake.Snapshot().Tables["dhcp_static"]
			if (len(rows) == 1) != (mode == "disconnect") {
				t.Fatal("unexpected device outcome")
			}
		})
	}
}
