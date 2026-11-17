package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	apisettings "configflow/internal/api/settings"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/instance"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/substore"
	"configflow/internal/version"
	"configflow/internal/weblistener"
)

// runOptions supplies local test boundaries, never environment-controlled
// production modes. Tests can use an ephemeral listener and loopback handlers.
type runOptions struct {
	listen   func(string, string) (net.Listener, error)
	register func(*httpx.App)
	ready    func(net.Addr)
}

func shutdownGrace() (time.Duration, error) {
	raw := os.Getenv("CONFIGFLOW_SHUTDOWN_GRACE_SECONDS")
	if raw == "" {
		return 8 * time.Second, nil
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return 0, errors.New(msgInvalidGrace)
	}
	grace := time.Duration(seconds * float64(time.Second))
	if grace <= 0 {
		return 0, errors.New(msgInvalidGrace)
	}
	return grace, nil
}

func registerStartup() {
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "load-config", Run: func(ctx context.Context) error {
		_, err := repo.PrepareConfig(ctx)
		return err
	}})
	substore.RegisterStartup()
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "agent-manager-init", Run: func(ctx context.Context) error {
		_, err := agents.Init(ctx)
		return err
	}})
	metrics.RegisterStartup()
}

func run(ctx context.Context, frontendDir string, signals <-chan os.Signal, opts runOptions) error {
	tuneMemory()
	grace, err := shutdownGrace()
	if err != nil {
		return err
	}
	dataDir := repo.DataDir()
	acquiring, cancelAcquire := context.WithTimeout(ctx, 30*time.Second)
	owner, err := instance.Acquire(acquiring, dataDir)
	cancelAcquire()
	if err != nil {
		return err
	}
	defer owner.Release()
	root, err := logx.Setup(logx.Config{Level: logx.LevelFromEnv(), File: logx.DefaultFile(dataDir), Console: os.Stderr})
	if err != nil {
		return err
	}
	var closeLog sync.Once
	flushLog := func() { closeLog.Do(func() { _ = root.Close() }) }
	defer flushLog()
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
	work := repo.WithRepository(context.WithoutCancel(ctx), repository)
	backend := newHTTPServer(nil)
	var web *http.Server
	var manager *weblistener.Manager
	drain := func(ctx context.Context) error {
		if manager != nil {
			manager.Close()
		}
		return drainServers(ctx, backend, web)
	}
	if err := lifecycle.Configure(lifecycle.Options{Grace: grace, Drain: drain}); err != nil {
		return err
	}
	defer lifecycle.Stop(grace)
	registerStartup()
	app := server.NewApp()
	app.RegisterLocal()
	backend.Handler = app
	if opts.register != nil {
		opts.register(app)
	}
	lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "http-close", Phase: lifecycle.PhaseClose, Run: func(context.Context) {
		_ = backend.Close()
		if web != nil {
			_ = web.Close()
		}
	}})
	// Store, ranking and traffic database connections are operation-scoped and
	// closed by their owners; there is no process-wide sql.DB to close here.
	lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "logs-close", Phase: lifecycle.PhaseClose, Run: func(context.Context) { flushLog() }})
	lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "instance-release", Phase: lifecycle.PhaseClose, Run: func(context.Context) { owner.Release() }})
	if err := writeTestManifest(app); err != nil {
		return err
	}
	lifecycle.RunStartup(work)
	listen := opts.listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", net.JoinHostPort(address.Host, strconv.Itoa(address.Port)))
	if err != nil {
		return err
	}
	served := make(chan struct{})
	serveErrors := make(chan error, 1)
	go func() {
		defer close(served)
		defer func() {
			if recover() != nil {
				serveErrors <- lifecycle.ErrPanic
			}
		}()
		serveErrors <- backend.Serve(listener)
	}()
	defer func() { _ = backend.Close(); <-served }()
	if frontendDir != "" {
		edge := &httpx.Edge{API: app, Root: frontendDir, AccessLog: os.Stdout}
		web = newHTTPServer(edge)
		manager = &weblistener.Manager{DataDir: dataDir, Server: webServer{web, edge}}
		status, err := manager.Start()
		if err != nil {
			return fmt.Errorf("[web-listener] %w", err)
		}
		defer manager.Close()
		apisettings.SetWebListener(manager)
		logx.Get("backend.app").Info("Web UI listening on port " + strconv.FormatInt(status.GetInt("port"), 10))
	}
	printStartupBanner(address)
	lifecycle.Start(work)
	logx.Get("backend.app").Info(msgInitialized)
	if opts.ready != nil {
		opts.ready(listener.Addr())
	}
	var serveErr error
	select {
	case <-ctx.Done():
	case <-signals:
	case serveErr = <-serveErrors:
	}
	stopped := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if recover() != nil {
				err = lifecycle.ErrPanic
			}
			stopped <- err
		}()
		lifecycle.Stop(grace)
	}()
	select {
	case <-signals:
		// A second stop signal is the explicitly documented force-exit path.
		os.Exit(0)
	case err := <-stopped:
		if err != nil {
			return err
		}
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}

func drainServers(ctx context.Context, servers ...*http.Server) error {
	results := make(chan error, len(servers))
	count := 0
	for _, srv := range servers {
		if srv == nil {
			continue
		}
		count++
		go func() {
			var err error
			defer func() {
				if recover() != nil {
					err = lifecycle.ErrPanic
				}
				results <- err
			}()
			err = srv.Shutdown(ctx)
		}()
	}
	var result error
	// http.Server.Shutdown honours ctx. Join both workers even when it expires,
	// so this fan-out never leaves an untracked control goroutine behind.
	for range count {
		result = errors.Join(result, <-results)
	}
	return result
}

type webServer struct {
	server *http.Server
	edge   *httpx.Edge
}

func (w webServer) Serve(l net.Listener) error    { return w.server.Serve(l) }
func (w webServer) SetListenerToken(token string) { w.edge.SetListenerToken(token) }
func (w webServer) ListenerToken() string         { return w.edge.ListenerToken() }

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 60 * time.Second, IdleTimeout: 300 * time.Second}
}

func printStartupBanner(address server.BackendAddress) {
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
}
