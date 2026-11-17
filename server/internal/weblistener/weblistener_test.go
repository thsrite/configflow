package weblistener

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

type tokenServer struct {
	mu    sync.Mutex
	token string
	srv   *http.Server
}

func newTokenServer() *tokenServer {
	s := &tokenServer{}
	s.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == HealthPath {
			w.Write([]byte(s.ListenerToken() + "\n"))
			return
		}
		w.WriteHeader(404)
	})}
	return s
}

func (s *tokenServer) Serve(l net.Listener) error { return s.srv.Serve(l) }
func (s *tokenServer) SetListenerToken(t string) {
	s.mu.Lock()
	s.token = t
	s.mu.Unlock()
}
func (s *tokenServer) ListenerToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func manager(t *testing.T, env map[string]string) *Manager {
	m := &Manager{DataDir: t.TempDir(), Server: newTokenServer(), HealthTimeout: 2 * time.Second,
		LookupEnv: func(name string) (string, bool) { v, ok := env[name]; return v, ok }}
	t.Cleanup(m.Close)
	return m
}

func wantError(t *testing.T, err error, status int, message string) {
	t.Helper()
	e, ok := err.(*Error)
	if !ok || e.Status != status || (message != "" && e.Message != message) {
		t.Fatalf("got %v, want %d %q", err, status, message)
	}
}

// test_web_listener.py::test_startup_default_dual_stack_then_saved_port_survives_restart
func TestSavedPortSurvivesRestartAndUpdateMovesTheListener(t *testing.T) {
	first := freePort(t)
	second := freePort(t)
	for second == first {
		second = freePort(t)
	}
	m := manager(t, map[string]string{})
	os.WriteFile(filepath.Join(m.DataDir, "web-listener.json"), []byte(`{"port": `+strconv.Itoa(first)+"}\n"), 0o600)
	status, err := m.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.GetInt("port") != int64(first) || status.Value("source") != "saved" || status.Value("editable") != true {
		t.Fatal(jsonx.Repr(status))
	}
	status, err = m.Update(context.Background(), jsonx.Int(int64(second)))
	if err != nil {
		t.Fatal(err)
	}
	if status.GetInt("port") != int64(second) {
		t.Fatal(jsonx.Repr(status))
	}
	data, _ := os.ReadFile(filepath.Join(m.DataDir, "web-listener.json"))
	if string(data) != `{"port": `+strconv.Itoa(second)+"}\n" {
		t.Fatal(string(data))
	}
	if info, _ := os.Stat(filepath.Join(m.DataDir, "web-listener.json")); info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(second) + HealthPath)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("new port serves", err)
	}
	resp.Body.Close()
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(first), 200*time.Millisecond); err == nil {
		t.Fatalf("old port %d still accepts after moving to %d", first, second)
	}
	same, err := m.Update(context.Background(), jsonx.Int(int64(second)))
	if err != nil || same.GetInt("port") != int64(second) {
		t.Fatal(err)
	}
}

// test_web_listener.py::test_environment_override_preserves_saved_preference_and_is_read_only
func TestEnvironmentOverrideIsReadOnly(t *testing.T) {
	port := freePort(t)
	m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": strconv.Itoa(port)})
	if _, err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, _ := m.Status(context.Background())
	want := jsonx.Obj("supported", true, "port", port, "editable", false, "ipv6", status.Value("ipv6"),
		"source", "environment", "message", "端口由 CONFIGFLOW_WEB_PORT 指定；移除该环境变量并重建容器后可在此修改")
	if !jsonx.Equal(status, want) {
		t.Fatal(jsonx.Repr(status))
	}
	_, err := m.Update(context.Background(), jsonx.Int(int64(freePort(t))))
	wantError(t, err, 409, "端口由 CONFIGFLOW_WEB_PORT 指定；移除该环境变量并重建容器后可在此修改")
}

// test_web_listener.py::test_bad_api_port_does_not_change_deployment and
// test_internal_services_keep_their_ports_even_when_asleep
func TestBadAndReservedPorts(t *testing.T) {
	m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": "x"})
	for _, value := range []any{jsonx.Int(0), jsonx.Int(65536), "80", true, jsonx.Float(80.0), nil} {
		_, err := m.Update(context.Background(), value)
		wantError(t, err, 400, "Web 端口须为 1–65535 的整数")
	}
	_, err := m.Update(context.Background(), jsonx.Int(5001))
	wantError(t, err, 409, "端口 5001 已保留给 ConfigFlow 后端，请选择其他端口")
	_, err = m.Update(context.Background(), jsonx.Int(3001))
	wantError(t, err, 409, "端口 3001 已保留给 Sub-Store，请选择其他端口")
	m.LookupEnv = func(name string) (string, bool) {
		return map[string]string{"SUB_STORE_BACKEND_API_PORT": "5001"}[name], name == "SUB_STORE_BACKEND_API_PORT"
	}
	_, err = m.Update(context.Background(), jsonx.Int(8080))
	wantError(t, err, 409, "ConfigFlow 后端与 Sub-Store 须使用不同的端口")
}

// test_web_listener.py::test_invalid_environment_is_rejected_before_overwriting_config
func TestInvalidEnvironmentIsRejected(t *testing.T) {
	for _, raw := range []string{"", "0", "65536", "abc", "123456", "８０"} {
		m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": raw})
		_, err := m.Start(context.Background())
		wantError(t, err, 400, "CONFIGFLOW_WEB_PORT 须为 1–65535 的整数")
	}
}

// test_web_listener.py::test_recovery_environment_works_with_corrupt_saved_file
func TestCorruptSavedFileAndRecoveryEnvironment(t *testing.T) {
	m := manager(t, map[string]string{})
	os.WriteFile(filepath.Join(m.DataDir, "web-listener.json"), []byte("{broken"), 0o600)
	_, err := m.Start(context.Background())
	wantError(t, err, 503, "无法读取已保存的 Web 端口；可设置 CONFIGFLOW_WEB_PORT 恢复访问")
	port := freePort(t)
	m.LookupEnv = func(name string) (string, bool) {
		if name == "CONFIGFLOW_WEB_PORT" {
			return strconv.Itoa(port), true
		}
		return "", false
	}
	if _, err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// test_web_listener.py::test_concurrent_change_is_rejected_without_touching_state
func TestConcurrentChangeIsRejected(t *testing.T) {
	m := manager(t, map[string]string{})
	_, unlock, err := m.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	// Another Manager for the same directory shares in-process admission.
	other := &Manager{DataDir: m.DataDir}
	_, err = other.Status(context.Background())
	wantError(t, err, 409, "Web 端口正在更新，请稍后重试")
}

func TestPortInUseAndUnsupported(t *testing.T) {
	busy, _ := net.Listen("tcp4", "0.0.0.0:0")
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": strconv.Itoa(port)})
	_, err := m.Start(context.Background())
	wantError(t, err, 409, "端口 "+strconv.Itoa(port)+" 已被占用，请选择其他端口")
	var none *Manager
	status, _ := none.Status(context.Background())
	if !jsonx.Equal(status, Unsupported()) {
		t.Fatal(jsonx.Repr(status))
	}
	_, err = none.Update(context.Background(), jsonx.Int(8080))
	wantError(t, err, 503, "当前部署不支持在 Web 中修改监听端口")
	_, err = (&Manager{DataDir: t.TempDir()}).Status(context.Background())
	wantError(t, err, 503, "Web 监听配置尚未初始化，请重启容器")
}
