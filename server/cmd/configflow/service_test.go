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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	"configflow/internal/httpx"
	"configflow/internal/instance"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/server"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
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
	var outbound atomic.Int64
	defer func() {
		if n := outbound.Load(); n != 0 {
			t.Errorf("unexpected scheduled network attempts: %d", n)
		}
		if err := os.WriteFile(filepath.Join(dir, "test-outbound"), []byte(strconv.FormatInt(outbound.Load(), 10)), 0o600); err != nil {
			t.Error(err)
		}
	}()
	firstSaved := make(chan struct{})
	detachedCancelled := make(chan struct{})
	var srvApp *server.App
	opts := runOptions{
		onApp: func(srv *server.App) {
			srvApp = srv
			srv.Refresh.SetEnv(schedulerObserver(srv.Repo, srv.SubStore, func() { outbound.Add(1); trace("outbound") }))
		},
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
				_, err := srvApp.Metrics.History().AddSample(ctx, "edge", json.RawMessage(`{"cpu":{"usage_percent":1}}`))
				return err
			}})
			lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "test-observe-first", Phase: lifecycle.PhaseStopSignal, Run: func(context.Context) {
				snap, err := taskstatus.New(dir).Read()
				entry := snap.Entry(metrics.TaskKey)
				var runs int64
				if entry != nil {
					if c, cerr := taskstatus.Count(entry.RunCount); cerr == nil && c.IsInt64() {
						runs = c.Int64()
					}
				}
				if err != nil || runs != 1 || srvApp.Metrics.History().PendingCount() != 0 {
					t.Errorf("first flush did not precede drain: entry=%v err=%v", entry, err)
				}
				trace("first")
				close(firstSaved)
			}})
			if mode == "signal-during-save" {
				// Registered after metrics' first save and before main's marker: a
				// slow first save during which a second signal arrives.
				lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "test-slow-first-save", Phase: lifecycle.PhaseStopSignal, Run: func(context.Context) {
					trace("save-start")
					deadline := time.Now().Add(5 * time.Second)
					for time.Now().Before(deadline) {
						if _, err := os.Stat(filepath.Join(dir, "test-second-sent")); err == nil {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					time.Sleep(500 * time.Millisecond) // the signal is delivered while the save runs
					trace("save-end")
				}})
			}
			lifecycle.RegisterShutdown(lifecycle.ShutdownHook{Name: "test-observe-last", Phase: lifecycle.PhaseAfterDrain, Run: func(context.Context) {
				if mode == "detached-handler" {
					select {
					case <-detachedCancelled:
					case <-time.After(2 * time.Second):
						t.Error("shutdown cancellation did not reach work detached from a request")
					}
				}
				want := int64(1)
				if mode == "publish" {
					want = 2
				}
				snap, err := taskstatus.New(dir).Read()
				entry := snap.Entry(metrics.TaskKey)
				var runs int64
				if entry != nil {
					if c, cerr := taskstatus.Count(entry.RunCount); cerr == nil && c.IsInt64() {
						runs = c.Int64()
					}
				}
				if err != nil || runs != want || srvApp.Metrics.History().PendingCount() != 0 {
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
					_, err := srvApp.Metrics.History().AddSample(ctx, "edge", json.RawMessage(`{"cpu":{"usage_percent":2}}`))
					if err != nil {
						t.Error(err)
					}
					trace("job-done")
				})
				return httpx.Empty(202), err
			})
			app.Router.GET("/__test/detached", "test.detached", func(c *httpx.Ctx) (*httpx.Response, error) {
				// An owner entry point detaches (PORTING 4b): client disconnects do
				// not stop it, the shutdown cancellation phase does.
				work := lifecycle.Detach(c.Context())
				trace("handler-start")
				<-work.Done()
				trace("detached-cancelled")
				close(detachedCancelled)
				return httpx.Empty(200), nil
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
		reportStartupError(err)
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
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Start from the empty native test repository, never the production template.
	err = r.UpdateShared(context.Background(), func(_ context.Context, shared *model.Shared) error {
		shared.Subscriptions = []model.Subscription{}
		shared.RuleLibrary = []model.RuleLibraryItem{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{testjson.Decode[model.Agent](t, map[string]any{
			"id": "edge", "name": "Synthetic", "service_type": "mihomo", "profile_id": "default", "host": "example.com", "port": 80,
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return startProcess(t, dir, mode)
}

func startProcess(t *testing.T, dir, mode string, environment ...string) *serviceProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServiceProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "DATA_DIR="+dir, "O33_SERVICE_SCENARIO="+mode,
		"ADMIN_USERNAME=", "ADMIN_PASSWORD=", "CONFIGFLOW_BACKEND_HOST=127.0.0.1", "CONFIGFLOW_BACKEND_PORT=5001",
		"CONFIGFLOW_SHUTDOWN_GRACE_SECONDS=8", "PRODDIFF_ROUTES="+filepath.Join(dir, "test-routes"))
	cmd.Env = append(cmd.Env, environment...)
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
	for _, mode := range []string{"idle", "publish", "blocked-drain", "second-signal", "signal-during-save", "detached-handler"} {
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
			} else if mode == "blocked-drain" || mode == "second-signal" || mode == "detached-handler" {
				path := "/__test/block"
				if mode == "detached-handler" {
					path = "/__test/detached"
				}
				requestDone = make(chan struct{})
				go func() {
					defer close(requestDone)
					resp, err := client.Get("http://" + address + path)
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
			if mode == "signal-during-save" {
				waitFor(t, func() bool {
					data, _ := os.ReadFile(filepath.Join(p.dir, "test-events"))
					return strings.Contains(string(data), "save-start")
				})
				if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.dir, "test-second-sent"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p.wait(t)
			elapsed := time.Since(start)
			if elapsed > 10*time.Second || mode == "second-signal" && elapsed > 3*time.Second {
				t.Fatalf("shutdown exceeded its grace: %v", elapsed)
			}
			if (mode == "blocked-drain" || mode == "detached-handler") && elapsed < 5*time.Second {
				t.Fatalf("shutdown abandoned active HTTP drain: %v", elapsed)
			}
			if mode == "signal-during-save" {
				data, _ := os.ReadFile(p.log)
				if !strings.Contains(string(data), msgSignalDuringSave) {
					t.Fatalf("second signal during the first save was not ignored:\n%s", data)
				}
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
			if strings.Contains(text, "outbound") {
				t.Fatalf("unexpected scheduled network attempt: %s", text)
			}
			if strings.Index(text, "startup") < 0 || strings.Index(text, "listen") < 0 || strings.Index(text, "startup") > strings.Index(text, "listen") || !strings.Contains(text, "first") {
				t.Fatalf("startup/flush order: %s", text)
			}
			if mode != "second-signal" && !strings.Contains(text, "last") {
				t.Fatalf("missing final flush: %s", text)
			}
			if mode == "signal-during-save" && !(strings.Index(text, "save-start") < strings.Index(text, "save-end") && strings.Index(text, "save-end") < strings.Index(text, "last")) {
				t.Fatalf("second signal interrupted the first save: %s", text)
			}
			if mode == "detached-handler" && !(strings.Index(text, "detached-cancelled") > 0 && strings.Index(text, "detached-cancelled") < strings.Index(text, "last")) {
				t.Fatalf("detached request work was not cancelled before the final save: %s", text)
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
	probe, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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

// O3 review: a stop signal while waiting up to 30 s for the instance lock ends
// the wait at once (Python had no such wait; Docker would SIGKILL after 10 s).
func TestStopSignalDuringInstanceWaitExitsAtOnce(t *testing.T) {
	dir := t.TempDir()
	holder, err := os.OpenFile(filepath.Join(dir, ".task-scheduler.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	p := startProcess(t, dir, "held-locks")
	waitFor(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, ".configflow-server.lock"))
		return err == nil
	})
	time.Sleep(time.Second) // inside the 500 ms retry loop
	start := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatalf("stopped server exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop signal ignored while waiting for the data directory")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("stop took %v", elapsed)
	}
	data, _ := os.ReadFile(p.log)
	if !strings.Contains(string(data), msgStoppedBeforeStart) {
		t.Fatalf("missing stop diagnostic: %s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "configflow.sqlite3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stopped server opened the repository: %v", err)
	}
	acquiring, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	owner, err := instance.Acquire(acquiring, dir)
	if err != nil {
		t.Fatalf("stopped server retained a lock: %v", err)
	}
	owner.Release()
}

func TestMainRejectsDataDirectoryAliases(t *testing.T) {
	for _, mode := range []string{"home-alias", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			first := startService(t, "idle")
			waitFor(t, func() bool { _, err := os.Stat(filepath.Join(first.dir, "test-ready")); return err == nil })
			home := filepath.Dir(first.dir)
			alias := "~/" + filepath.Base(first.dir)
			if mode == "symlink" {
				alias = filepath.Join(t.TempDir(), "data-link")
				if err := os.Symlink(first.dir, alias); err != nil {
					t.Fatal(err)
				}
			}
			probe, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, expected := instance.Acquire(probe, first.dir)
			cancel()
			if !errors.Is(expected, instance.ErrInUse) {
				t.Fatal(expected)
			}
			second := startProcess(t, t.TempDir(), "held-alias", "DATA_DIR="+alias, "HOME="+home)
			select {
			case err := <-second.done:
				var exited *exec.ExitError
				if !errors.As(err, &exited) || exited.ExitCode() != 1 {
					t.Fatalf("alias server exit: %v", err)
				}
			case <-time.After(40 * time.Second):
				t.Fatal("alias server did not respect the 30s acquisition deadline")
			}
			data, _ := os.ReadFile(second.log)
			if !strings.Contains(string(data), expected.Error()) {
				t.Fatalf("different ownership path: %s", data)
			}
			if err := first.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			first.wait(t)
			outbound, err := os.ReadFile(filepath.Join(first.dir, "test-outbound"))
			if err != nil || string(outbound) != "0" {
				t.Fatalf("scheduled requests during the 30s ownership window: count=%q err=%v", outbound, err)
			}
		})
	}
}

// The only remote scheduler eligible in the 30s ownership window is refresh.
// Record and refuse all of its I/O boundaries, without changing its real clock.
func schedulerObserver(r *repo.Repository, sub *substore.Service, record func()) *refresh.RefreshEnv {
	env := refresh.DefaultRefreshEnvFor(r, sub)
	env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*refresh.SourceResult, error) {
		record()
		return nil, errors.New("scheduled subscription request blocked by test observer")
	}
	env.RuleClient = &rulefetch.Client{Transport: blockedRuleRequests{record}, Sleep: func(time.Duration) {}}
	env.HTTP = blockedUserinfoRequests{record}
	return env
}

type blockedRuleRequests struct{ record func() }

func (b blockedRuleRequests) Do(context.Context, rulefetch.TransportRequest) (*rulefetch.Response, error) {
	b.record()
	return nil, errors.New("scheduled rule request blocked by test observer")
}

type blockedUserinfoRequests struct{ record func() }

func (b blockedUserinfoRequests) Do(context.Context, *substore.Request) (*substore.Response, error) {
	b.record()
	return nil, errors.New("scheduled userinfo request blocked by test observer")
}

func TestSchedulerObserverDetectsEveryDownloadBoundary(t *testing.T) {
	count := 0
	env := schedulerObserver(nil, nil, func() { count++ })
	_, subErr := env.Sources.Fetch(context.Background(), "edge", "http://example.com/disabled", substore.FetchOptions{})
	_, ruleErr := env.RuleClient.Transport.Do(context.Background(), rulefetch.TransportRequest{URL: "http://example.com/disabled"})
	_, infoErr := env.HTTP.Do(context.Background(), &substore.Request{URL: "http://example.com/disabled"})
	if count != 3 || subErr == nil || ruleErr == nil || infoErr == nil {
		t.Fatalf("observer missed network boundary: %d %v %v %v", count, subErr, ruleErr, infoErr)
	}
}
