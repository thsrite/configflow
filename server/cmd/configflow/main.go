// Command configflow is the ConfigFlow server: the REST API (as the Flask
// backend served it on CONFIGFLOW_BACKEND_HOST:CONFIGFLOW_BACKEND_PORT) and,
// when a frontend build is given, the web UI with nginx.conf's rules on the
// web port (CONFIGFLOW_WEB_PORT / the port saved in the settings page / 80).
//
// Environment: DATA_DIR, LOG_LEVEL, LOG_FILE, ADMIN_USERNAME,
// ADMIN_PASSWORD, JWT_SECRET_KEY, CONFIGFLOW_BACKEND_HOST,
// CONFIGFLOW_BACKEND_PORT, CONFIGFLOW_WEB_PORT, CONFIGFLOW_FRONTEND_DIR,
// CONFIGFLOW_LOCK_TIMEOUT_SECONDS (read by the repository),
// CONFIGFLOW_SHUTDOWN_GRACE_SECONDS (default 8), GOMEMLIMIT and
// GOGC (Go runtime), CONFIGFLOW_MEMORY_LIMIT_RATIO. Packages read their own
// variables (AGENT_REGISTRATION_KEY, SUB_STORE_*, ...).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"configflow/internal/instance"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

func main() {
	frontend := flag.String("frontend", os.Getenv("CONFIGFLOW_FRONTEND_DIR"),
		"frontend build directory served on the web port (empty: API only)")
	flag.Parse()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := run(context.Background(), *frontend, signals, runOptions{}); err != nil {
		reportStartupError(err)
		os.Exit(1)
	}
}

// reportStartupError preserves storage recovery diagnostics in both configured
// sinks. A busy directory is owned by another server: report only to console.
func reportStartupError(err error) {
	if !errors.Is(err, instance.ErrInUse) {
		root, setupErr := logx.Setup(logx.Config{Level: logx.Error, File: logx.DefaultFile(repo.DataDir()), Console: os.Stderr})
		if setupErr == nil { defer root.Close() }
	}
	logx.Get("configflow").Error(err.Error())
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
