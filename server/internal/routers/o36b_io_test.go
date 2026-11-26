package routers

import (
	"bytes"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"configflow/internal/lockx"
	"configflow/internal/routers/routerfake"
)

func TestO36bOperationWaitRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			violations := lockx.Violations()
			device := *jobConnection("http://192.0.2.70")
			_, release, err := oplock.RouterOperationLock(t.Context(), device, false)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			runtime, request, disconnect := o36bRuntime(t)
			done := make(chan error, 1)
			go func() {
				_, unlock, err := oplock.RouterOperationLock(request, device, true)
				if unlock != nil {
					unlock()
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatal("writer escaped held L2", err)
			case <-time.After(50 * time.Millisecond):
			}
			if shutdown {
				runtime.Shutdown(context.Background())
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatal("shutdown became a busy error", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("L2 waiter ignored shutdown")
				}
			} else {
				disconnect()
				select {
				case err := <-done:
					t.Fatal("disconnect interrupted lock wait", err)
				case <-time.After(50 * time.Millisecond):
				}
				release()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("released L2 did not wake writer")
				}
			}
			if lockx.Violations() != violations || violations != 0 {
				t.Fatal("L2 waiter changed lock order")
			}
		})
	}
}

func TestO36bIkuaiWriteInFlightRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			fake := routerfake.NewIkuai(r06Base("ikuai", "delete").IK)
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := json.RawMessage{}
				if r.Body != nil {
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					data, _ = json.Marshal(body)
					var action string
					_ = json.Unmarshal(body["action"], &action)
					if action == "del" {
						close(entered)
						select {
						case <-r.Context().Done():
							close(cancelled)
							return
						case <-release:
						}
					}
				}
				r.Body = http.NoBody
				if len(data) > 0 {
					r.Body = io.NopCloser(bytes.NewReader(data))
				}
				fake.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(unblock)
			d := NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
			defer d.Close()
			runtime, request, disconnect := o36bRuntime(t)
			before, _ := json.Marshal(fake.Snapshot().Tables)
			done := make(chan error, 1)
			go func() {
				_, err := d.Write(request, "static_rt", "del", ros.Row{"id": "1"}, 0)
				done <- err
			}()
			select {
			case <-entered:
			case err := <-done:
				t.Fatal("write did not reach device", err)
			case <-time.After(10 * time.Second):
				t.Fatal("write did not reach device")
			}
			if shutdown {
				runtime.Shutdown(context.Background())
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatal("shutdown lost native cancellation", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("in-flight I/O ignored shutdown")
				}
				select {
				case <-cancelled:
				case <-time.After(10 * time.Second):
					t.Fatal("device HTTP request did not receive cancellation")
				}
				after, _ := json.Marshal(fake.Snapshot().Tables)
				if !bytes.Equal(before, after) {
					t.Fatal("shutdown write committed to device")
				}
			} else {
				disconnect()
				select {
				case <-cancelled:
					t.Fatal("disconnect canceled device I/O")
				case err := <-done:
					t.Fatal("disconnect completed before fake reply", err)
				case <-time.After(50 * time.Millisecond):
				}
				unblock()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("disconnected write did not finish")
				}
				if len(fake.Snapshot().Tables["static_rt"]) != 0 {
					t.Fatal("disconnected write did not commit")
				}
			}
		})
	}
}
