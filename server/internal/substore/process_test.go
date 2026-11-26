package substore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

// fakeProcessArg makes the test binary act as Sub-Store (see TestMain).
const fakeProcessArg = "substore-fake-process"

// runFakeProcess is the fake Sub-Store: it records "<pid> <unix nanos>" in
// pidFile, writes one stdout line with a secret-bearing URL and one stderr
// line, then behaves by mode.
func runFakeProcess(mode, pidFile string) int {
	f, err := os.OpenFile(pidFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 10
	}
	fmt.Fprintf(f, "%d %d\n", os.Getpid(), time.Now().UnixNano())
	f.Close()
	fmt.Println("Sub-Store fetching https://user:pw@example.com/sub?token=s3cret now")
	fmt.Fprintln(os.Stderr, "fake warning from "+mode)
	switch mode {
	case "crash":
		time.Sleep(30 * time.Millisecond)
		return 3
	case "serve":
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		<-term
		return 0
	case "stubborn":
		signal.Ignore(syscall.SIGTERM)
		select {}
	case "family":
		signal.Ignore(syscall.SIGTERM)
		child := exec.Command(os.Args[0], fakeProcessArg, "stubborn", pidFile)
		if err := child.Start(); err != nil {
			return 11
		}
		select {}
	}
	return 12
}

func fakeProcessArgs(mode, pidFile string) []string {
	return []string{os.Args[0], fakeProcessArg, mode, pidFile}
}

type supervisorWorld struct {
	sup     *Supervisor
	runner  *lifecycle.Runtime
	pidFile string
}

func newSupervisorWorld(t *testing.T, mode string, drain func(context.Context) error) *supervisorWorld {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pids")
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 5 * time.Second, Drain: drain})
	runner.Start(context.Background())
	t.Cleanup(func() { runner.Shutdown(context.Background()) })
	sup := &Supervisor{
		Command:      func(context.Context) ([]string, error) { return fakeProcessArgs(mode, pidFile), nil },
		SpawnService: runner.SpawnService,
		StartSeconds: 20 * time.Millisecond, StopTimeout: 300 * time.Millisecond,
		BackoffMin: 50 * time.Millisecond, BackoffMax: 200 * time.Millisecond, StableAfter: 10 * time.Second,
	}
	t.Cleanup(func() { _ = sup.Stop(context.Background()) })
	return &supervisorWorld{sup: sup, runner: runner, pidFile: pidFile}
}

type started struct {
	pid int
	at  time.Time
}

func (w *supervisorWorld) starts(t *testing.T) []started {
	t.Helper()
	data, err := os.ReadFile(w.pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []started
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		pid, nanos, _ := strings.Cut(line, " ")
		p, _ := strconv.Atoi(pid)
		n, _ := strconv.ParseInt(nanos, 10, 64)
		out = append(out, started{p, time.Unix(0, n)})
	}
	return out
}

func eventually(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// alive reports whether pid exists and is not a zombie.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	_, rest, _ := strings.Cut(string(data), ") ")
	return !strings.HasPrefix(rest, "Z")
}

// reaped reports whether our child pid is fully gone (no zombie left).
func reaped(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }

func TestSupervisorStartStopAndLogs(t *testing.T) {
	logs := captureLogs(t)
	w := newSupervisorWorld(t, "serve", nil)
	ctx := context.Background()
	if got := w.sup.Status(); got != ProcessStopped {
		t.Fatalf("initial status %s", got)
	}
	if err := w.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.sup.Status(); got != ProcessStarting {
		t.Fatalf("status after start %s", got)
	}
	eventually(t, "running", 2*time.Second, func() bool { return w.sup.Status() == ProcessRunning })
	if err := w.sup.Start(ctx); err != nil { // idempotent
		t.Fatal(err)
	}
	eventually(t, "output logged", 2*time.Second, func() bool { return strings.Contains(logs.text(), "fake warning from serve") })
	starts := w.starts(t)
	if len(starts) != 1 {
		t.Fatalf("starts %v", starts)
	}
	if err := w.sup.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.sup.Status(); got != ProcessStopped {
		t.Fatalf("status after stop %s", got)
	}
	if !reaped(starts[0].pid) {
		t.Fatal("process not reaped after stop")
	}
	text := logs.text()
	for _, want := range []string{
		"sub-store - INFO - Sub-Store fetching https://example.com/[REDACTED] now",
		"sub-store - WARNING - fake warning from serve",
		fmt.Sprintf("Sub-Store 已启动（pid %d）", starts[0].pid),
		fmt.Sprintf("Sub-Store 已停止（pid %d）", starts[0].pid),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("log lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "s3cret") || strings.Contains(text, "user:pw") {
		t.Fatalf("secret in log:\n%s", text)
	}
	if strings.Contains(text, "意外退出") {
		t.Fatalf("stop logged as crash:\n%s", text)
	}
	// Start after Stop runs a new process.
	if err := w.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "second start", 2*time.Second, func() bool { return len(w.starts(t)) == 2 })
}

func TestSupervisorRestartsCrashesWithBackoff(t *testing.T) {
	logs := captureLogs(t)
	w := newSupervisorWorld(t, "crash", nil)
	if err := w.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sawBackoff := false
	eventually(t, "five starts", 5*time.Second, func() bool {
		sawBackoff = sawBackoff || w.sup.Status() == ProcessBackoff
		return len(w.starts(t)) >= 5
	})
	if !sawBackoff {
		t.Error("never reported BACKOFF")
	}
	starts := w.starts(t)
	// Each process lives ~30 ms; the gaps grow 50 → 100 → 200 → 200 ms (capped).
	minimum := []time.Duration{80, 130, 230, 230}
	for i := 1; i < 5; i++ {
		gap := starts[i].at.Sub(starts[i-1].at)
		if gap < minimum[i-1]*time.Millisecond {
			t.Errorf("gap %d = %v, want ≥ %v ms", i, gap, minimum[i-1])
		}
		if gap > minimum[i-1]*time.Millisecond+time.Second {
			t.Errorf("gap %d = %v too long", i, gap)
		}
	}
	if !strings.Contains(logs.text(), "意外退出：exit status 3；") {
		t.Errorf("crash not logged:\n%s", logs.text())
	}
	// Stop during back-off ends supervision at once and leaves no process.
	eventually(t, "back-off", 2*time.Second, func() bool { return w.sup.Status() == ProcessBackoff })
	begin := time.Now()
	if err := w.sup.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(begin); elapsed > 150*time.Millisecond {
		t.Errorf("stop during back-off took %v", elapsed)
	}
	count := len(w.starts(t))
	time.Sleep(300 * time.Millisecond)
	if len(w.starts(t)) != count {
		t.Fatal("restarted after stop")
	}
	for _, s := range w.starts(t) {
		if !reaped(s.pid) {
			t.Errorf("pid %d not reaped", s.pid)
		}
	}
}

func TestSupervisorBackoffResetsAfterStableRun(t *testing.T) {
	w := newSupervisorWorld(t, "crash", nil)
	w.sup.StableAfter = 10 * time.Millisecond // every 30 ms run counts as stable
	w.sup.BackoffMin, w.sup.BackoffMax = 50*time.Millisecond, 5*time.Second
	if err := w.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "four starts", 5*time.Second, func() bool { return len(w.starts(t)) >= 4 })
	starts := w.starts(t)
	for i := 1; i < 4; i++ {
		if gap := starts[i].at.Sub(starts[i-1].at); gap > 400*time.Millisecond {
			t.Errorf("gap %d = %v: back-off was not reset", i, gap)
		}
	}
}

func TestSupervisorStopKillsWholeGroupAfterTimeout(t *testing.T) {
	w := newSupervisorWorld(t, "family", nil)
	if err := w.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "leader and child", 3*time.Second, func() bool { return len(w.starts(t)) == 2 })
	starts := w.starts(t)
	begin := time.Now()
	if err := w.sup.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(begin)
	if elapsed < 250*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("stop took %v, want about the 300 ms stop timeout", elapsed)
	}
	if !reaped(starts[0].pid) {
		t.Error("leader not reaped")
	}
	eventually(t, "grandchild killed", 3*time.Second, func() bool { return !alive(starts[1].pid) })
}

func TestSupervisorSpawnFailure(t *testing.T) {
	logs := captureLogs(t)
	w := newSupervisorWorld(t, "serve", nil)
	w.sup.Command = func(context.Context) ([]string, error) {
		return []string{filepath.Join(t.TempDir(), "missing-node")}, nil
	}
	if err := w.sup.Start(context.Background()); err == nil {
		t.Fatal("start succeeded")
	}
	if got := w.sup.Status(); got != ProcessStopped {
		t.Fatalf("status %s", got)
	}
	if !strings.Contains(logs.text(), "Sub-Store 启动失败") {
		t.Errorf("failure not logged:\n%s", logs.text())
	}
}

// Server shutdown keeps Sub-Store up while requests drain, then stops it
// and waits for it before returning.
func TestSupervisorStopsWithServerAfterDrain(t *testing.T) {
	var w *supervisorWorld
	aliveDuringDrain := make(chan bool, 1)
	w = newSupervisorWorld(t, "serve", func(context.Context) error {
		starts := w.starts(t)
		aliveDuringDrain <- len(starts) == 1 && alive(starts[0].pid)
		return nil
	})
	if err := w.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "started", 2*time.Second, func() bool { return len(w.starts(t)) == 1 })
	begin := time.Now()
	w.runner.Shutdown(context.Background())
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Errorf("shutdown took %v", elapsed)
	}
	if !<-aliveDuringDrain {
		t.Error("Sub-Store was not running during the drain")
	}
	if pid := w.starts(t)[0].pid; !reaped(pid) {
		t.Error("process left after shutdown")
	}
	if got := w.sup.Status(); got != ProcessStopped {
		t.Fatalf("status %s", got)
	}
	if err := w.sup.Start(context.Background()); !errors.Is(err, ErrProcessUnavailable) {
		t.Fatalf("start after shutdown: %v", err)
	}
	if len(w.starts(t)) != 1 {
		t.Fatal("a process was left running after a refused start")
	}
}

// The production command is sub-store-start.sh: the newer bundle, with the
// data directory created first.
func TestSupervisorCommandUsesActiveBundle(t *testing.T) {
	base := t.TempDir()
	builtin, data := filepath.Join(base, "opt"), filepath.Join(base, "data")
	runtimeDir := filepath.Join(data, "sub-store", "runtime")
	for dir, version := range map[string]string{builtin: "2.40.0", runtimeDir: "2.41.0"} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, SubStoreAsset), []byte("x"), 0o644)
		os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version), 0o644)
	}
	dbPath := filepath.Join(data, "sub-store-db")
	vars := map[string]string{"SUB_STORE_BUILTIN_DIR": builtin, "SUB_STORE_DATA_BASE_PATH": dbPath}
	env := &Env{LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }, DataDir: func() string { return data }}
	argv, err := NewSupervisor(env).command(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv, " ") != "node "+filepath.Join(runtimeDir, SubStoreAsset) {
		t.Fatalf("argv %v", argv)
	}
	if info, err := os.Stat(dbPath); err != nil || !info.IsDir() {
		t.Fatalf("data directory not created: %v", err)
	}
	os.WriteFile(filepath.Join(runtimeDir, "VERSION"), []byte("2.39.0"), 0o644)
	argv, _ = NewSupervisor(env).command(context.Background())
	if argv[1] != filepath.Join(builtin, SubStoreAsset) {
		t.Fatalf("argv %v", argv)
	}
}

func TestRedactProcessLine(t *testing.T) {
	tests := []struct {
		name, line, want string
		secrets          []string
	}{
		{"query token", "GET https://sub.example.com/api/v1/client/subscribe?token=q1secret done",
			"GET https://sub.example.com/[REDACTED] done", []string{"q1secret", "subscribe"}},
		{"path token", "Downloading https://sub.example.com/link/p4thsecret?clash=1",
			"Downloading https://sub.example.com/[REDACTED]", []string{"p4thsecret"}},
		{"userinfo", "from http://alice:pw9secret@198.51.100.7:8080/", "from http://198.51.100.7:8080/", []string{"pw9secret", "alice"}},
		{"fragment", "url https://sub.example.com/#fr4gsecret", "url https://sub.example.com/", []string{"fr4gsecret"}},
		{"host only", "base https://sub.example.com", "base https://sub.example.com", nil},
		{"proxy uri", "Failed to parse line: ss://YWVzLTI1Ni1nY206c2VjcmV0@192.0.2.11:8388#node",
			"Failed to parse line: ss://[REDACTED]", []string{"YWVz", "192.0.2.11"}},
		{"vmess", `bad vmess://eyJ1dWlkIjoieCJ9 x`, "bad vmess://[REDACTED] x", []string{"eyJ1"}},
		{"surge line", "node = trojan, 192.0.2.12, 443, password=tr0jansecret, sni=example.com",
			"node = trojan, 192.0.2.12, 443, password=[REDACTED], sni=example.com", []string{"tr0jansecret"}},
		{"json", `{"name":"n","uuid":"0b5e2c0e-uuid-secret","port":443}`, `{"name":"n","uuid":"[REDACTED]","port":443}`, []string{"uuid-secret"}},
		{"plain", "Sub-Store backend listening on 3001", "Sub-Store backend listening on 3001", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactProcessLine(tt.line)
			if got != tt.want {
				t.Errorf("redactProcessLine(%q) = %q, want %q", tt.line, got, tt.want)
			}
			for _, secret := range tt.secrets {
				if strings.Contains(got, secret) {
					t.Errorf("%q leaks %q", got, secret)
				}
			}
		})
	}
}
