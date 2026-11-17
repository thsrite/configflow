package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	"configflow/internal/httpx"
	"configflow/internal/instance"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

func TestServiceProcess(t *testing.T) {
	mode := os.Getenv("O33_SERVICE_SCENARIO")
	if mode == "" {
		return
	}
	dir := os.Getenv("DATA_DIR")
	var traceMu sync.Mutex
	trace := func(event string) {
		traceMu.Lock()
		defer traceMu.Unlock()
		file, err := os.OpenFile(filepath.Join(dir, "test-events"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		_, _ = fmt.Fprintln(file, event)
	}
	firstSaved := make(chan struct{})
	opts := runOptions{
		listen: func(network, _ string) (net.Listener, error) {
			trace("listen")
			return net.Listen(network, "127.0.0.1:0")
		},
		ready: func(addr net.Addr) {
			data, _ := json.Marshal(addr.String())
			if err := os.WriteFile(filepath.Join(dir, "test-ready"), data, 0o600); err != nil {
				t.Error(err)
			}
		},
		register: func(app *httpx.App) {
			lifecycle.RegisterStartup(lifecycle.Startup{Name: "test-observe-startup", Run: func(ctx context.Context) error {
				// All production startup work, including history loading, is done.
				trace("startup")
				_, err := metrics.For(dir).AddMetrics("edge", jsonx.Obj("cpu", jsonx.Obj("usage_percent", 1)))
				return err
			}})
			lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "test-observe-first", Phase: lifecycle.PhaseStopSignal, Run: func(context.Context) {
				entry, err := taskstatus.New(dir).Get(metrics.TaskKey)
				if err != nil || entry.GetInt("run_count") != 1 || metrics.For(dir).PendingCount() != 0 {
					t.Errorf("first flush did not precede drain: entry=%v err=%v", entry, err)
				}
				trace("first")
				close(firstSaved)
			}})
			lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "test-observe-last", Phase: lifecycle.PhaseAfterDrain, Run: func(context.Context) {
				want := int64(1)
				if mode == "publish" {
					want = 2
				}
				entry, err := taskstatus.New(dir).Get(metrics.TaskKey)
				if err != nil || entry.GetInt("run_count") != want || metrics.For(dir).PendingCount() != 0 {
					t.Errorf("last flush did not follow work: entry=%v err=%v", entry, err)
				}
				trace("last")
			}})
			app.Router.POST("/__test/publish", "test.publish", func(c *httpx.Ctx) (*httpx.Response, error) {
				err := lifecycle.Spawn(c.Context(), "synthetic-publish", func(ctx context.Context) {
					<-firstSaved
					if ctx.Err() != nil {
						t.Error("publish cancelled before drain completed")
						return
					}
					_, err := metrics.For(dir).AddMetrics("edge", jsonx.Obj("cpu", jsonx.Obj("usage_percent", 2)))
					if err != nil {
						t.Error(err)
					}
					trace("job-done")
				})
				return httpx.Empty(202), err
			})
			app.Router.GET("/__test/block", "test.block", func(c *httpx.Ctx) (*httpx.Response, error) {
				trace("handler-start")
				<-c.Request.Context().Done()
				return httpx.Empty(200), nil
			})
		},
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := run(context.Background(), "", signals, opts); err != nil {
		logx.Get("backend.app").Error(err.Error())
		os.Exit(1)
	}
}

type serviceProcess struct {
	cmd    *exec.Cmd
	done   chan error
	dir    string
	log    string
	exited chan struct{}
}

func startService(t *testing.T, mode string) *serviceProcess {
	t.Helper()
	dir := t.TempDir()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "edge", "name", "Synthetic", "service_type", "mihomo", "profile_id", "default", "host", "example.com", "port", 80)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return startProcess(t, dir, mode)
}

func startProcess(t *testing.T, dir, mode string) *serviceProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServiceProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "DATA_DIR="+dir, "O33_SERVICE_SCENARIO="+mode,
		"ADMIN_USERNAME=", "ADMIN_PASSWORD=", "CONFIGFLOW_BACKEND_HOST=127.0.0.1", "CONFIGFLOW_BACKEND_PORT=5001",
		"CONFIGFLOW_SHUTDOWN_GRACE_SECONDS=8", "PRODDIFF_ROUTES="+filepath.Join(dir, "test-routes"))
	log := filepath.Join(dir, "test-process.log")
	output, err := os.Create(log)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		cancel()
		output.Close()
		t.Fatal(err)
	}
	p := &serviceProcess{cmd: cmd, done: make(chan error, 1), dir: dir, log: log, exited: make(chan struct{})}
	go func() { defer close(p.exited); err := cmd.Wait(); _ = output.Close(); p.done <- err }()
	t.Cleanup(func() {
		cancel()
		_ = cmd.Process.Kill()
		<-p.exited
		if t.Failed() {
			data, _ := os.ReadFile(p.log)
			t.Logf("service log:\n%s", data)
		}
	})
	return p
}

func (p *serviceProcess) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-p.done:
		if err != nil {
			data, _ := os.ReadFile(p.log)
			t.Fatalf("service process: %v\n%s", err, data)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("service did not exit")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("service observation deadline exceeded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRealSIGTERMServiceShutdown(t *testing.T) {
	for _, mode := range []string{"idle", "publish", "blocked-drain", "second-signal"} {
		t.Run(mode, func(t *testing.T) {
			p := startService(t, mode)
			var address string
			waitFor(t, func() bool {
				data, err := os.ReadFile(filepath.Join(p.dir, "test-ready"))
				return err == nil && json.Unmarshal(data, &address) == nil
			})
			client := &http.Client{Timeout: 20 * time.Second}
			var requestDone chan struct{}
			if mode == "publish" {
				resp, err := client.Post("http://"+address+"/__test/publish", "", nil)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != 202 {
					t.Fatal(resp.Status)
				}
			} else if mode == "blocked-drain" || mode == "second-signal" {
				requestDone = make(chan struct{})
				go func() {
					defer close(requestDone)
					resp, err := client.Get("http://" + address + "/__test/block")
					if err == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
					}
				}()
				waitFor(t, func() bool {
					data, _ := os.ReadFile(filepath.Join(p.dir, "test-events"))
					return strings.Contains(string(data), "handler-start")
				})
			}
			start := time.Now()
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if requestDone != nil {
				waitFor(t, func() bool {
					conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
					if err == nil {
						_ = conn.Close()
					}
					return err != nil
				})
			}
			if mode == "second-signal" {
				if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
					t.Fatal(err)
				}
			}
			p.wait(t)
			elapsed := time.Since(start)
			if elapsed > 10*time.Second || mode == "second-signal" && elapsed > 3*time.Second {
				t.Fatalf("shutdown exceeded its grace: %v", elapsed)
			}
			if mode == "blocked-drain" && elapsed < 5*time.Second {
				t.Fatalf("shutdown abandoned active HTTP drain: %v", elapsed)
			}
			if requestDone != nil {
				select {
				case <-requestDone:
				case <-time.After(time.Second):
					t.Fatal("HTTP client goroutine leaked")
				}
			}
			data, _ := os.ReadFile(filepath.Join(p.dir, "test-events"))
			text := string(data)
			if strings.Index(text, "startup") < 0 || strings.Index(text, "listen") < 0 || strings.Index(text, "startup") > strings.Index(text, "listen") || !strings.Contains(text, "first") {
				t.Fatalf("startup/flush order: %s", text)
			}
			if mode != "second-signal" && !strings.Contains(text, "last") {
				t.Fatalf("missing final flush: %s", text)
			}
			if mode == "publish" && !(strings.Index(text, "first") < strings.Index(text, "job-done") && strings.Index(text, "job-done") < strings.Index(text, "last")) {
				t.Fatalf("publish drain order: %s", text)
			}
			acquiring, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			owner, err := instance.Acquire(acquiring, p.dir)
			if err != nil {
				t.Fatalf("process retained instance lock: %v", err)
			}
			owner.Release()
			t.Logf("SIGTERM %s completed in %v", mode, elapsed)
		})
	}
}

func TestShutdownGraceConfiguration(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Duration
	}{
		{"", 8 * time.Second}, {"2.5", 2500 * time.Millisecond}, {"28", 28 * time.Second},
		{"0", 0}, {"-1", 0}, {"invalid", 0}, {"NaN", 0}, {"+Inf", 0}, {"1e100", 0}, {"0.0000000001", 0},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("CONFIGFLOW_SHUTDOWN_GRACE_SECONDS", tt.value)
			got, err := shutdownGrace()
			if got != tt.want || (err != nil) != (tt.want == 0) {
				t.Fatalf("grace=%v err=%v", got, err)
			}
			if err != nil && err.Error() != msgInvalidGrace {
				t.Fatal(err)
			}
		})
	}
}

func TestPythonLifetimeLocksRejectServerBeforeRepositoryOpen(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".task-scheduler.lock", "router-rule-sync/.scheduler.lock", ".oxidns-rankings.lock", "probes/.auto.lock"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
	}
	probe, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, expected := instance.Acquire(probe, dir)
	if !errors.Is(expected, instance.ErrInUse) {
		t.Fatal(expected)
	}
	p := startProcess(t, dir, "held-locks")
	select {
	case err := <-p.done:
		var exited *exec.ExitError
		if !errors.As(err, &exited) || exited.ExitCode() != 1 {
			t.Fatalf("busy server exit: %v", err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("server exceeded the 30s acquisition deadline")
	}
	data, _ := os.ReadFile(p.log)
	if !strings.Contains(string(data), expected.Error()) {
		t.Fatalf("missing Chinese busy diagnostic: %s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "configflow.sqlite3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("server opened the repository before locking: %v", err)
	}
}
