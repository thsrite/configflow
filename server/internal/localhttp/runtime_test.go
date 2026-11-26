package localhttp_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/lifecycle"
	"configflow/internal/localhttp"
)

type privateKey struct{}
type refusingSpawner struct{ err error }

func (s refusingSpawner) Spawn(context.Context, string, func(context.Context)) error { return s.err }

func TestDispatchRequiresTrackedSpawner(t *testing.T) {
	var called atomic.Bool
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) })
	defer localhttp.Register(nil, nil)
	localhttp.Register(handler, nil)
	if _, err := localhttp.Do(t.Context(), localhttp.Request{URL: "http://example.com/api/stream"}); !errors.Is(err, localhttp.ErrMissingSpawner) {
		t.Fatal(err)
	}
	refusal := errors.New("synthetic admission refusal")
	localhttp.Register(handler, refusingSpawner{refusal})
	if _, err := localhttp.Do(t.Context(), localhttp.Request{URL: "http://example.com/api/stream"}); !errors.Is(err, refusal) {
		t.Fatal(err)
	}
	if called.Load() {
		t.Fatal("untracked or rejected handler ran")
	}
}

func TestStreamingDispatchKeepsRuntimeLifetimeAndFreshContext(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			// A short explicit grace exercises job cancellation, not a production timeout.
			rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
			rt.Start(t.Context())
			defer rt.Shutdown(t.Context())
			release := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			entered, cancelled := make(chan struct{}), make(chan struct{})
			probe := make(chan chan error)
			var leaked atomic.Bool
			localhttp.Register(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Context().Value(privateKey{}) != nil {
					leaked.Store(true)
				}
				w.WriteHeader(http.StatusOK)
				close(entered)
				for {
					select {
					case reply := <-probe:
						reply <- ctxx.Detach(r.Context()).Err()
					case <-r.Context().Done():
						close(cancelled)
						<-release
						return
					case <-release:
						return
					}
				}
			}), rt)
			defer localhttp.Register(nil, nil)
			request, disconnect := context.WithCancel(rt.BindRequest(context.WithValue(t.Context(), privateKey{}, "private caller state")))
			defer disconnect()
			response, err := localhttp.Do(request, localhttp.Request{URL: "http://example.com/api/stream", Stream: true})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			<-entered
			stopped := make(chan struct{})
			if shutdown {
				go func() { rt.Shutdown(t.Context()); close(stopped) }()
				select {
				case <-cancelled:
				case <-time.After(3 * time.Second):
					t.Fatal("Runtime did not cancel streaming handler")
				}
			} else {
				disconnect()
				reply := make(chan error, 1)
				select {
				case probe <- reply:
				case <-cancelled:
					t.Fatal("disconnect cancelled handler")
				}
				if err := <-reply; err != nil {
					t.Fatal("nested owner lost detached lifetime", err)
				}
			}
			waiting, cancel := context.WithCancel(t.Context())
			cancel()
			if err := rt.WaitIdle(waiting); !errors.Is(err, context.Canceled) {
				t.Fatal("Runtime stopped tracking an active handler", err)
			}
			if leaked.Load() {
				t.Fatal("caller values leaked into fresh handler context")
			}
			close(release)
			idle, cancelIdle := context.WithTimeout(t.Context(), time.Second)
			defer cancelIdle()
			if err := rt.WaitIdle(idle); err != nil {
				t.Fatal("handler did not finish", err)
			}
			if shutdown {
				<-stopped
			}
		})
	}
}
