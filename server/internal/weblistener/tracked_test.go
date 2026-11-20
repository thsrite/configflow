package weblistener

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func listenerRuntime(t *testing.T) *lifecycle.Runtime {
	t.Helper()
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	r.Start(context.Background())
	t.Cleanup(func() { r.Shutdown(context.Background()) })
	return r
}
func listenerIdle(t *testing.T, r *lifecycle.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestServeJobsEndOnCloseAndRuntimeCancellation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "Close", true: "Runtime cancellation"}[shutdown], func(t *testing.T) {
			runner := listenerRuntime(t)
			port := freePort(t)
			m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": strconv.Itoa(port)})
			m.Spawner = runner
			if _, err := m.Listen(context.Background()); err != nil {
				t.Fatal(err)
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(runner.WaitIdle(cancelled), context.Canceled) {
				t.Fatal("Serve is not tracked")
			}
			if shutdown {
				runner.Shutdown(context.Background())
			} else {
				m.Close()
			}
			listenerIdle(t, runner)
			conn, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond)
			if err == nil {
				conn.Close()
				t.Fatal("listener leaked")
			}
		})
	}
}

type partialSpawner struct {
	runner *lifecycle.Runtime
	calls  int
}

func (s *partialSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	s.calls++
	if s.calls > 1 {
		return lifecycle.ErrStopping
	}
	return s.runner.Spawn(ctx, name, fn)
}
func TestPartialServeAdmissionClosesAllSockets(t *testing.T) {
	runner := listenerRuntime(t)
	listeners := []net.Listener{}
	for range 2 {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, l)
		t.Cleanup(func() { l.Close() })
	}
	m := &Manager{Server: newTokenServer(), Spawner: &partialSpawner{runner: runner}}
	if err := m.serve(context.Background(), listeners); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	listenerIdle(t, runner)
	for _, l := range listeners {
		conn, err := net.DialTimeout("tcp4", l.Addr().String(), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Fatal("partial listener leaked")
		}
	}
}

type refusingListenerSpawner struct{}

func (refusingListenerSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}
func TestRefusedStartRestoresTokenAndState(t *testing.T) {
	m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": strconv.Itoa(freePort(t))})
	m.Spawner = refusingListenerSpawner{}
	m.Server.SetListenerToken("previous")
	if _, err := m.Listen(context.Background()); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	if m.active != nil || m.Server.ListenerToken() != "previous" {
		t.Fatal("refused start published state")
	}
}
func TestProductionHealthTimeout(t *testing.T) {
	if got := (&Manager{}).healthTimeout(); got != 5*time.Second {
		t.Fatal(got)
	}
}
