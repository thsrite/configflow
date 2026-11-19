package agents

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"configflow/internal/lifecycle"
)

func TestNativeLoggingDisconnectAndShutdownCommitBoundary(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown-in-reply"}[shutdown], func(t *testing.T) {
			e := newEnv(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(e.ctx)
			defer runtime.Shutdown(context.Background())
			bound := runtime.BindRequest(e.ctx)
			request, disconnect := context.WithCancel(bound)
			disconnect()
			before := remoteReferenceHead(t, e)
			calls := 0
			e.fake.set(func(Request) (*Response, error) {
				calls++
				if shutdown {
					runtime.Shutdown(context.Background())
				}
				return &Response{StatusCode: 200, Body: []byte(`{"success":true}`)}, nil
			})
			_, err := e.m.SetLoggingModel(request, e.id(), []byte(`true`), http.Header{"Content-Type": {"application/json"}}, []byte(`{"enabled": true}`))
			if calls != 1 {
				t.Fatalf("requests=%d", calls)
			}
			after := remoteReferenceHead(t, e)
			if shutdown {
				if !errors.Is(err, context.Canceled) || before != after {
					t.Fatalf("shutdown committed logging setting: %v", err)
				}
				_, err = e.m.SetLoggingModel(bound, e.id(), []byte(`false`), nil, []byte(`{"enabled": false}`))
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("shutdown admitted another device request: calls=%d err=%v", calls, err)
				}
			} else {
				if err != nil || before == after || e.stored(e.id()).GetObject("logging_config").Value("enabled") != true {
					t.Fatal("disconnect canceled durable setting", err)
				}
			}
		})
	}
}
