// Package weblistener is the port of backend/utils/web_listener.py for a
// server that is its own web front end: the port the web UI listens on
// (CONFIGFLOW_WEB_PORT, else the port saved from the settings page in
// DATA_DIR/web-listener.json, else 80), the IPv6 listener next to the IPv4
// one, and changing the port from the settings page without a restart.
//
// Python rendered an nginx configuration, reloaded nginx and checked the new
// listener with a random token on /_configflow/listener-health. Here the Go
// process opens the new sockets itself, checks them with the same health
// request and token, saves the port, and only then closes the old sockets
// (connections in progress finish on them). Messages and status codes are
// Python's, except that the nginx-specific failures cannot happen.
package weblistener

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

// HealthPath is the listener check path.
const HealthPath = "/_configflow/listener-health"

// Error is WebListenerError: a message for the user and the HTTP status.
type Error struct {
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

func fail(message string, status int) *Error { return &Error{Message: message, Status: status} }

// Server is what the manager serves on its sockets.
type Server interface {
	// Serve accepts connections on l until it is closed.
	Serve(l net.Listener) error
	// SetListenerToken sets the answer of HealthPath.
	SetListenerToken(token string)
	// ListenerToken returns it.
	ListenerToken() string
}

var logger = logx.Get("backend.utils.web_listener")

// Manager owns the web listener sockets.
type Manager struct {
	// DataDir holds web-listener.json. Legacy web-listener.lock is untouched.
	DataDir string
	Server  Server
	// Spawner tracks Serve jobs; nil uses the default lifecycle Runtime.
	Spawner lockx.Spawner
	// HealthTimeout bounds the check of a new listener (5 s).
	HealthTimeout time.Duration
	// Getenv is os.Getenv (tests).
	Getenv func(string) string
	// LookupEnv is os.LookupEnv (tests).
	LookupEnv func(string) (string, bool)
	// IPv6Available probes kernel support; nil uses the native loopback probe.
	IPv6Available func() bool
	// HealthCheck probes both bound addresses; nil uses token-authenticated HTTP.
	HealthCheck func(context.Context, int, bool, string) bool

	mu     sync.Mutex
	active *active
}

type active struct {
	port      int
	ipv6      bool
	token     string
	listeners []net.Listener
}

func (m *Manager) getenv(name string) string {
	if m.Getenv != nil {
		return m.Getenv(name)
	}
	return os.Getenv(name)
}

func (m *Manager) lookupEnv(name string) (string, bool) {
	if m.LookupEnv != nil {
		return m.LookupEnv(name)
	}
	return os.LookupEnv(name)
}

func (m *Manager) statePath() string { return filepath.Join(m.DataDir, "web-listener.json") }
func (m *Manager) lockPath() string  { return filepath.Join(m.DataDir, "web-listener.lock") }

// checkPort is _port: an int (not a bool) in 1..65535.
func checkPort(value any) (int, error) {
	n, ok := value.(jsonx.Number)
	v, fits := n.Int64()
	if !ok || !fits || v < 1 || v > 65535 {
		if i, isInt := value.(int); isInt && i >= 1 && i <= 65535 {
			return i, nil
		}
		return 0, fail("Web 端口须为 1–65535 的整数", 400)
	}
	return int(v), nil
}

var listenerLocks = lockx.NewKeyed[string](lockx.LevelJob, "web-listener")

// lock is _lock: try-only admission shared by managers of one physical directory.
func (m *Manager) lock(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	key, err := repo.ResolveDataDir(m.DataDir)
	if err != nil {
		return ctx, nil, err
	}
	held, release, ok := listenerLocks.TryLock(ctx, key)
	if !ok {
		return ctx, nil, fail(msgBusy, 409)
	}
	return held, release, nil
}

// EnvironmentPort is _environment_port(name, default).
func (m *Manager) EnvironmentPort(name string, def int) (int, error) {
	raw, ok := m.lookupEnv(name)
	if !ok {
		raw = strconv.Itoa(def)
	}
	raw = strings.TrimFunc(raw, isPySpace)
	if len([]rune(raw)) > 5 || !isASCIIDecimal(raw) {
		return 0, fail(name+" 须为 1–65535 的整数", 400)
	}
	value, _ := strconv.Atoi(raw)
	if value < 1 || value > 65535 {
		return 0, fail(name+" 须为 1–65535 的整数", 400)
	}
	return value, nil
}

func isASCIIDecimal(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x1f, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// selection is _selection: the port and where it comes from.
func (m *Manager) selection() (int, string, error) {
	if _, ok := m.lookupEnv("CONFIGFLOW_WEB_PORT"); ok {
		port, err := m.EnvironmentPort("CONFIGFLOW_WEB_PORT", 80)
		return port, "environment", err
	}
	data, err := os.ReadFile(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return 80, "default", nil
	}
	unreadable := fail("无法读取已保存的 Web 端口；可设置 CONFIGFLOW_WEB_PORT 恢复访问", 503)
	if err != nil {
		return 0, "", unreadable
	}
	saved, err := jsonx.Parse(data)
	object, ok := saved.(*jsonx.Object)
	if err != nil || !ok {
		return 0, "", unreadable
	}
	value, present := object.Get("port")
	if !present {
		return 0, "", unreadable
	}
	port, err := checkPort(value)
	if err != nil {
		return 0, "", unreadable
	}
	return port, "saved", nil
}

// Port is the port the listener should use now (for startup messages).
func (m *Manager) Port() (int, error) {
	port, _, err := m.selection()
	return port, err
}

func (m *Manager) checkReserved(port int) error {
	backend, err := m.EnvironmentPort("CONFIGFLOW_BACKEND_PORT", 5001)
	if err != nil {
		return err
	}
	subStore, err := m.EnvironmentPort("SUB_STORE_BACKEND_API_PORT", 3001)
	if err != nil {
		return err
	}
	if backend == subStore {
		return fail("ConfigFlow 后端与 Sub-Store 须使用不同的端口", 409)
	}
	if port == backend || port == subStore {
		service := "Sub-Store"
		if port == backend {
			service = "ConfigFlow 后端"
		}
		return fail("端口 "+strconv.Itoa(port)+" 已保留给 "+service+"，请选择其他端口", 409)
	}
	return nil
}

// ipv6Available is _ipv6_available.
func ipv6Available() bool {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// listen opens the IPv4 and (when available) IPv6 sockets of a port.
func listen(port int, ipv6 bool) ([]net.Listener, error) {
	var listeners []net.Listener
	addresses := [][2]string{{"tcp4", "0.0.0.0:" + strconv.Itoa(port)}}
	if ipv6 {
		addresses = append(addresses, [2]string{"tcp6", "[::]:" + strconv.Itoa(port)})
	}
	for _, address := range addresses {
		l, err := net.Listen(address[0], address[1])
		if err != nil {
			for _, open := range listeners {
				open.Close()
			}
			if errors.Is(err, syscall.EADDRINUSE) {
				return nil, fail("端口 "+strconv.Itoa(port)+" 已被占用，请选择其他端口", 409)
			}
			return nil, fail("无法监听端口 "+strconv.Itoa(port)+"，请检查权限和网络配置", 409)
		}
		listeners = append(listeners, l)
	}
	return listeners, nil
}

// randomToken is secrets.token_hex(16).
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create listener health token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (m *Manager) ipv6Available() bool {
	if m.IPv6Available != nil {
		return m.IPv6Available()
	}
	return ipv6Available()
}

func (m *Manager) serve(ctx context.Context, listeners []net.Listener) error {
	spawner := m.Spawner
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	for _, listener := range listeners {
		l := listener
		if err := spawner.Spawn(ctx, "web-listener", func(ctx context.Context) {
			stop := context.AfterFunc(ctx, func() { _ = l.Close() })
			defer stop()
			defer l.Close()
			if err := m.Server.Serve(l); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil {
				logger.Warningf("Web listener serving failed (%T)", err)
			}
		}); err != nil {
			for _, opened := range listeners {
				_ = opened.Close()
			}
			return fmt.Errorf("start web listener: %w", err)
		}
	}
	return nil
}

// Start is prepare_listener at startup: open the selected port (IPv4, and
// IPv6 when the host supports it) and serve on it.
func (m *Manager) Start(ctx context.Context) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	ctx, unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	port, _, err := m.selection()
	if err != nil {
		return nil, err
	}
	if err := m.checkReserved(port); err != nil {
		return nil, err
	}
	ipv6 := m.ipv6Available()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listeners, err := listen(port, ipv6)
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		for _, l := range listeners {
			l.Close()
		}
		return nil, err
	}
	previousToken := m.Server.ListenerToken()
	m.Server.SetListenerToken(token)
	if err := m.serve(ctx, listeners); err != nil {
		m.Server.SetListenerToken(previousToken)
		return nil, err
	}
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		for _, l := range listeners {
			l.Close()
		}
		m.Server.SetListenerToken(previousToken)
		return nil, err
	}
	m.active = &active{port: port, ipv6: ipv6, token: token, listeners: listeners}
	m.mu.Unlock()
	return m.status()
}

// Close closes the listener sockets.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		for _, l := range m.active.listeners {
			l.Close()
		}
	}
}

func (m *Manager) status() (*jsonx.Object, error) {
	port, source, err := m.selection()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	current := m.active
	m.mu.Unlock()
	if current == nil {
		return nil, fail("Web 监听配置尚未初始化，请重启容器", 503)
	}
	if current.port != port {
		return nil, fail("Web 监听配置与保存的端口不一致，请重启容器", 503)
	}
	status := jsonx.Obj("supported", true, "port", port, "editable", source != "environment",
		"ipv6", current.ipv6, "source", source)
	if source == "environment" {
		status.Set("message", "端口由 CONFIGFLOW_WEB_PORT 指定；移除该环境变量并重建容器后可在此修改")
	}
	return status, nil
}

// Unsupported is listener_status() of a deployment whose web front end is
// not served by this process.
func Unsupported() *jsonx.Object {
	return jsonx.Obj("supported", false, "port", nil, "editable", false, "ipv6", false, "source", "default",
		"message", "当前部署不支持在 Web 中修改监听端口，请通过部署配置调整")
}

// Status is listener_status(). A nil manager is the unsupported deployment.
func (m *Manager) Status(ctx context.Context) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if m == nil {
		return Unsupported(), nil
	}
	ctx, unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return m.status()
}

// Update is update_listener(port): move the web listener to another port.
func (m *Manager) Update(ctx context.Context, value any) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	port, err := checkPort(value)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if err := (&Manager{}).checkReserved(port); err != nil {
			return nil, err
		}
		return nil, fail("当前部署不支持在 Web 中修改监听端口", 503)
	}
	if err := m.checkReserved(port); err != nil {
		return nil, err
	}
	ctx, unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	status, err := m.status()
	if err != nil {
		return nil, err
	}
	if editable, _ := status.Value("editable").(bool); !editable {
		return nil, fail(status.GetString("message"), 409)
	}
	if port == int(status.GetInt("port")) {
		return status, nil
	}
	m.mu.Lock()
	old := m.active
	m.mu.Unlock()
	ipv6 := m.ipv6Available()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listeners, err := listen(port, ipv6)
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		for _, l := range listeners {
			l.Close()
		}
		return nil, err
	}
	m.Server.SetListenerToken(token)
	if err := m.serve(ctx, listeners); err != nil {
		m.Server.SetListenerToken(old.token)
		return nil, err
	}
	restore := func() {
		for _, l := range listeners {
			l.Close()
		}
		m.Server.SetListenerToken(old.token)
	}
	if err := m.waitHealthy(ctx, port, ipv6, token); err != nil {
		restore()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		restore()
		return nil, err
	}
	previous, readErr := os.ReadFile(m.statePath())
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		restore()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fail(msgSaveFailed, 503)
	}
	if err := atomicWrite(ctx, m.statePath(), []byte(`{"port": `+strconv.Itoa(port)+"}\n"), 0o600); err != nil {
		restore()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fail(msgSaveFailed, 503)
	}
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		// A cancellation between the file and active pointer commits must not
		// leave the saved selection pointing at sockets we are about to close.
		var rollbackErr error
		if errors.Is(readErr, os.ErrNotExist) {
			rollbackErr = os.Remove(m.statePath())
		} else {
			rollbackErr = atomicWrite(ctxx.Uninterruptible(ctx), m.statePath(), previous, 0600)
		}
		restore()
		return nil, errors.Join(err, rollbackErr)
	}
	m.active = &active{port: port, ipv6: ipv6, token: token, listeners: listeners}
	m.mu.Unlock()
	for _, l := range old.listeners {
		l.Close()
	}
	return m.status()
}

func (m *Manager) healthTimeout() time.Duration {
	if m.HealthTimeout != 0 {
		return m.HealthTimeout
	}
	return 5 * time.Second
}

func (m *Manager) waitHealthy(ctx context.Context, port int, ipv6 bool, token string) error {
	probe := m.HealthCheck
	if probe == nil {
		probe = healthy
	}
	clock := lifecycle.ClockFrom(ctx)
	deadline := clock.Now().Add(m.healthTimeout())
	for {
		if probe(ctx, port, ipv6, token) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return fail(msgHealthFailed, 503)
		}
		timer := clock.NewTimer(min(100*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C():
		}
		timer.Stop()
	}
}

func healthy(ctx context.Context, port int, ipv6 bool, token string) bool {
	hosts := []string{"127.0.0.1"}
	if ipv6 {
		hosts = append(hosts, "[::1]")
	}
	client := &http.Client{Timeout: 300 * time.Millisecond, Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil}}
	for _, host := range hosts {
		probe, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		req, _ := http.NewRequestWithContext(probe, "GET", "http://"+host+":"+strconv.Itoa(port)+HealthPath, nil)
		req.Header.Set("Connection", "close")
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return false
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		cancel()
		if resp.StatusCode != 200 || strings.TrimSpace(string(body)) != token {
			return false
		}
	}
	return true
}

func atomicWrite(ctx context.Context, path string, content []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
