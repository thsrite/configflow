// Command configflow is the ConfigFlow server: the REST API (as the Flask
// backend served it on CONFIGFLOW_BACKEND_HOST:CONFIGFLOW_BACKEND_PORT) and,
// when a frontend build is given, the web UI with nginx.conf's rules on the
// web port (CONFIGFLOW_WEB_PORT / the port saved in the settings page / 80).
//
// Environment: DATA_DIR, LOG_LEVEL, LOG_FILE, ADMIN_USERNAME,
// ADMIN_PASSWORD, JWT_SECRET_KEY, CONFIGFLOW_BACKEND_HOST,
// CONFIGFLOW_BACKEND_PORT, CONFIGFLOW_WEB_PORT, CONFIGFLOW_FRONTEND_DIR,
// CONFIGFLOW_LOCK_TIMEOUT_SECONDS (read by the repository), GOMEMLIMIT and
// GOGC (Go runtime), CONFIGFLOW_MEMORY_LIMIT_RATIO. Packages read their own
// variables (AGENT_REGISTRATION_KEY, SUB_STORE_*, ...).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	apisettings "configflow/internal/api/settings"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/version"
	"configflow/internal/weblistener"
)

func main() {
	frontend := flag.String("frontend", os.Getenv("CONFIGFLOW_FRONTEND_DIR"),
		"frontend build directory served on the web port (empty: API only)")
	flag.Parse()
	if err := run(*frontend); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(frontendDir string) error {
	tuneMemory()
	dataDir := repo.DataDir()
	if err := os.MkdirAll(dataDir, 0o777); err != nil {
		return err
	}
	level := logx.LevelFromEnv()
	root, err := logx.Setup(logx.Config{Level: level, File: logx.DefaultFile(dataDir), Console: os.Stderr})
	if err != nil {
		return err
	}
	defer root.Close()
	address, err := server.BackendAddressFromEnv()
	if err != nil {
		return err
	}

	auth.Configure(auth.FromEnv())
	if !auth.IsAuthEnabled() {
		printNoAuthBanner()
	}

	repository, err := repo.OpenDefault(dataDir)
	if err != nil {
		return err
	}
	repo.SetRepository(repository)
	app := server.NewApp()
	app.RegisterLocal()
	if err := writeTestManifest(app); err != nil {
		return err
	}
	startup := repo.WithScope(context.Background())
	if _, err := repo.LoadConfig(startup); err != nil {
		return err
	}
	repo.ResetConfigContext(startup)

	levelName := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	if levelName == "" {
		levelName = "INFO"
	}
	display := address.Host
	if strings.Contains(display, ":") {
		display = "[" + display + "]"
	}
	line := strings.Repeat("=", 60)
	fmt.Printf("\n%s\n🚀 Config Flow API Server\n%s\nVersion: %s\nBuild: unknown\nLog Level: %s\n%s\nServer starting on http://%s:%d\nPress Ctrl+C to stop\n%s\n\n",
		line, line, version.Version, levelName, line, display, address.Port, line)

	backend := newHTTPServer(app)
	listener, err := net.Listen("tcp", net.JoinHostPort(address.Host, strconv.Itoa(address.Port)))
	if err != nil {
		return err
	}
	go func() {
		if err := backend.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Get("backend.app").Error("API server stopped: " + err.Error())
		}
	}()

	var web *http.Server
	var manager *weblistener.Manager
	if frontendDir != "" {
		edge := &httpx.Edge{API: app, Root: frontendDir, AccessLog: os.Stdout}
		web = newHTTPServer(edge)
		manager = &weblistener.Manager{DataDir: dataDir, Server: webServer{web, edge}}
		status, err := manager.Start()
		if err != nil {
			return fmt.Errorf("[web-listener] %w", err)
		}
		apisettings.SetWebListener(manager)
		logx.Get("backend.app").Info("Web UI listening on port " + strconv.FormatInt(status.GetInt("port"), 10))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	lifecycle.Start(ctx)
	logx.Get("backend.app").Info("✅ 应用初始化完成")
	<-ctx.Done()

	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if manager != nil {
		manager.Close()
		_ = web.Shutdown(shutdown)
	}
	_ = backend.Shutdown(shutdown)
	lifecycle.Stop(30 * time.Second)
	return nil
}

type webServer struct {
	server *http.Server
	edge   *httpx.Edge
}

func (w webServer) Serve(l net.Listener) error    { return w.server.Serve(l) }
func (w webServer) SetListenerToken(token string) { w.edge.SetListenerToken(token) }
func (w webServer) ListenerToken() string         { return w.edge.ListenerToken() }

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 60 * time.Second,
		// waitress channel_timeout and nginx keepalive.
		IdleTimeout: 300 * time.Second,
	}
}

// printNoAuthBanner is validate_required_env_vars().
func printNoAuthBanner() {
	line := strings.Repeat("=", 80)
	fmt.Printf("\n%s\nINFO: Running WITHOUT authentication\n%s\n⚠️  Authentication is DISABLED. Anyone can access the application.\n\nTo enable authentication, set:\n  ADMIN_USERNAME=admin\n  ADMIN_PASSWORD=your-password\n  JWT_SECRET_KEY=your-secret-key\n%s\n\n",
		line, line, line)
}

// tuneMemory replaces the Python malloc tuning: unless GOMEMLIMIT is set,
// the soft memory limit is a share (CONFIGFLOW_MEMORY_LIMIT_RATIO, default
// 0.8) of the container's cgroup memory limit, so the GC works harder only
// when the process gets close to being OOM-killed and stays lazy otherwise.
// GOGC keeps Go's default (100) and its environment override.
func tuneMemory() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	ratio := 0.8
	if raw := os.Getenv("CONFIGFLOW_MEMORY_LIMIT_RATIO"); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value > 0 && value <= 1 {
			ratio = value
		}
	}
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		limit, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil || limit <= 0 || limit > 1<<50 {
			return
		}
		debug.SetMemoryLimit(int64(float64(limit) * ratio))
		return
	}
}
