package weblistener

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func savedManager(t *testing.T, env map[string]string) *Manager {
	t.Helper()
	m := manager(t, env)
	m.Spawner = listenerRuntime(t)
	m.IPv6Available = func() bool { return false }
	if err := os.WriteFile(m.statePath(), []byte(`{"port":`+strconv.Itoa(freePort(t))+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m
}

func assertOldListener(t *testing.T, m *Manager, old *active, before []byte) {
	t.Helper()
	after, err := os.ReadFile(m.statePath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("saved selection changed", err)
	}
	if m.active != old || m.Server.ListenerToken() != old.token || !healthy(context.Background(), old.port, old.ipv6, old.token) {
		t.Fatal("old listener no longer serves its token")
	}
}

func TestIPv4OnlyKernelKeepsOneNativeSocketAcrossUpdate(t *testing.T) {
	m := savedManager(t, map[string]string{})
	if m.active.ipv6 || len(m.active.listeners) != 1 {
		t.Fatal("IPv6 socket created without kernel support")
	}
	port := freePort(t)
	status, err := m.Update(context.Background(), port)
	if err != nil || status.Value("ipv6") != false || len(m.active.listeners) != 1 {
		t.Fatal(status, err)
	}
	if !healthy(context.Background(), port, false, m.Server.ListenerToken()) {
		t.Fatal("IPv4 listener unhealthy")
	}
}

func TestNativeHealthFailureLeavesOldListenerAndStateUsable(t *testing.T) {
	m := savedManager(t, map[string]string{})
	old := m.active
	before, _ := os.ReadFile(m.statePath())
	port := freePort(t)
	server := m.Server.(*tokenServer)
	// Serve an actual unsuccessful response only on the new socket.
	server.srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, p, _ := net.SplitHostPort(r.Host)
		if p == strconv.Itoa(port) {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(server.ListenerToken() + "\n"))
	})
	m.HealthTimeout = 20 * time.Millisecond
	_, err := m.Update(context.Background(), port)
	wantError(t, err, 503, msgHealthFailed)
	assertOldListener(t, m, old, before)
	if c, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("failed new socket leaked")
	}
}

func TestPersistenceFailureStillRollsBackLiveSockets(t *testing.T) {
	m := savedManager(t, map[string]string{})
	old := m.active
	before, _ := os.ReadFile(m.statePath())
	m.HealthCheck = func(ctx context.Context, port int, ipv6 bool, token string) bool {
		ok := healthy(ctx, port, ipv6, token)
		if ok {
			if err := os.Chmod(m.DataDir, 0500); err != nil {
				t.Error(err)
			}
		}
		return ok
	}
	t.Cleanup(func() { os.Chmod(m.DataDir, 0700) })
	_, err := m.Update(context.Background(), freePort(t))
	if chmodErr := os.Chmod(m.DataDir, 0700); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	wantError(t, err, 503, msgSaveFailed)
	assertOldListener(t, m, old, before)
}

func TestHealthyProbeCannotCommitAfterRuntimeShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect completes", true: "shutdown after successful probe"}[shutdown], func(t *testing.T) {
			m := savedManager(t, map[string]string{})
			old := m.active
			before, _ := os.ReadFile(m.statePath())
			runtime := listenerRuntime(t)
			request, cancel := context.WithCancel(runtime.BindRequest(context.Background()))
			defer cancel()
			cancel()
			m.HealthCheck = func(ctx context.Context, port int, ipv6 bool, token string) bool {
				ok := healthy(ctx, port, ipv6, token)
				if ok && shutdown {
					runtime.Shutdown(context.Background())
				}
				return ok
			}
			port := freePort(t)
			_, err := m.Update(request, port)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				assertOldListener(t, m, old, before)
			} else if err != nil || m.active.port != port || !healthy(context.Background(), port, false, m.Server.ListenerToken()) {
				t.Fatal("disconnect aborted switch", err)
			}
		})
	}
}

func TestCustomReservedPortsAtStartupAndUpdate(t *testing.T) {
	for _, key := range []string{"CONFIGFLOW_BACKEND_PORT", "SUB_STORE_BACKEND_API_PORT"} {
		t.Run(key, func(t *testing.T) {
			env := map[string]string{}
			m := savedManager(t, env)
			old := m.active
			before, _ := os.ReadFile(m.statePath())
			reserved := freePort(t)
			env[key] = strconv.Itoa(reserved)
			_, err := m.Update(context.Background(), reserved)
			wantError(t, err, 409, "")
			assertOldListener(t, m, old, before)
			env["CONFIGFLOW_WEB_PORT"] = strconv.Itoa(reserved)
			_, err = m.Start(context.Background())
			wantError(t, err, 409, "")
			delete(env, "CONFIGFLOW_WEB_PORT")
			assertOldListener(t, m, old, before)
		})
	}
}

func TestInvalidInternalEnvironmentAndSharedPortPreserveActiveConfig(t *testing.T) {
	for _, key := range []string{"CONFIGFLOW_BACKEND_PORT", "SUB_STORE_BACKEND_API_PORT"} {
		for _, value := range []string{"", "invalid", "0", "65536", "-1", "１", "9999999999999999999"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				env := map[string]string{}
				m := savedManager(t, env)
				old := m.active
				before, _ := os.ReadFile(m.statePath())
				env[key] = value
				_, err := m.Start(context.Background())
				wantError(t, err, 400, key+" 须为 1–65535 的整数")
				assertOldListener(t, m, old, before)
			})
		}
	}
	env := map[string]string{}
	m := savedManager(t, env)
	old := m.active
	before, _ := os.ReadFile(m.statePath())
	env["CONFIGFLOW_BACKEND_PORT"] = "15801"
	env["SUB_STORE_BACKEND_API_PORT"] = "15801"
	_, err := m.Start(context.Background())
	wantError(t, err, 409, "ConfigFlow 后端与 Sub-Store 须使用不同的端口")
	assertOldListener(t, m, old, before)
}
