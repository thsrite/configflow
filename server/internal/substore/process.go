package substore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/weburl"
)

// ProcessState names the state of the bundled Sub-Store process. The words
// are the ones the runtime has always compared (supervisord's state names).
type ProcessState string

// Process states.
const (
	ProcessStopped  ProcessState = "STOPPED"
	ProcessStarting ProcessState = "STARTING"
	ProcessRunning  ProcessState = "RUNNING"
	ProcessBackoff  ProcessState = "BACKOFF"
	ProcessStopping ProcessState = "STOPPING"
)

// Process controls the bundled Sub-Store process. Start and Stop are
// idempotent; Start during a crash back-off keeps the back-off schedule.
type Process interface {
	Status() ProcessState
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
}

// ErrProcessUnavailable means the process cannot be managed at all here (no
// process controller, or the server is shutting down).
var ErrProcessUnavailable = errors.New("sub-store process cannot be managed")

// Supervision defaults: supervisord's startsecs=1 and stopwaitsecs=5 for the
// old [program:sub-store]; restarts back off from 1 s up to 30 s and reset
// after the process stayed up for 30 s.
const (
	defaultStartSeconds = time.Second
	defaultStopTimeout  = 5 * time.Second
	defaultBackoffMin   = time.Second
	defaultBackoffMax   = 30 * time.Second
	defaultStableAfter  = 30 * time.Second
	maxLogLine          = 4096
	logRedacted         = "[REDACTED]"
)

var processLog = logx.Get("sub-store")

// Supervisor runs the bundled Sub-Store (node <bundle>) as a child process
// of the server: started on demand, restarted with back-off after a crash,
// stopped with SIGTERM and then SIGKILL for its whole process group, and
// stopped when the server shuts down. Its output goes to the server log.
//
// It does not decide when Sub-Store should run (Runtime does) nor which
// bundle is current beyond Env.ActiveBundle.
type Supervisor struct {
	env *Env

	// Command returns the argv to run; nil runs node on Env.ActiveBundle.
	Command func(ctx context.Context) ([]string, error)
	// SpawnService starts the supervising goroutine; nil is lifecycle.SpawnService.
	SpawnService func(ctx context.Context, name string, fn func(context.Context)) error
	// Clock times start-up, back-off and stability; nil is lifecycle.ClockFrom.
	Clock clock.Clock

	StartSeconds, StopTimeout, BackoffMin, BackoffMax, StableAfter time.Duration

	mu    sync.Mutex
	run   *supervision // nil: stopped
	state ProcessState
	since time.Time // start of the current process
}

// supervision is one start → stop period: it may run several processes.
type supervision struct {
	stop    context.CancelFunc // ends the period (Stop)
	stopCtx context.Context
	done    chan struct{} // closed when the supervising goroutine returns
	cmd     *exec.Cmd     // current process (nil before the first and during back-off)
}

// NewSupervisor returns the production supervisor for env.
func NewSupervisor(env *Env) *Supervisor { return &Supervisor{env: env} }

func (s *Supervisor) clock(ctx context.Context) clock.Clock {
	if s.Clock != nil {
		return s.Clock
	}
	return lifecycle.ClockFrom(ctx)
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// Status reports the current state.
func (s *Supervisor) Status() ProcessState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return ProcessStopped
	}
	if s.state == ProcessStarting && s.clock(context.Background()).Now().Sub(s.since) >= durationOr(s.StartSeconds, defaultStartSeconds) {
		return ProcessRunning
	}
	return s.state
}

// Start starts Sub-Store unless it is already supervised. It returns once
// the first process has been spawned, so a spawn failure reaches the caller.
func (s *Supervisor) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		return nil
	}
	stopCtx, stop := context.WithCancel(context.Background())
	run := &supervision{stop: stop, stopCtx: stopCtx, done: make(chan struct{})}
	s.run, s.state, s.since = run, ProcessStarting, s.clock(ctx).Now()
	s.mu.Unlock()
	spawn := s.SpawnService
	if spawn == nil {
		spawn = lifecycle.SpawnService
	}
	first := make(chan error, 1)
	if err := spawn(lifecycle.Detach(ctx), "sub-store-process", func(work context.Context) { s.supervise(work, run, first) }); err != nil {
		s.finish(run)
		stop()
		close(run.done)
		if errors.Is(err, lifecycle.ErrStopping) {
			return fmt.Errorf("start sub-store: %w", ErrProcessUnavailable)
		}
		return fmt.Errorf("start sub-store: %w", err)
	}
	select {
	case err := <-first:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// finish marks the supervision period as over.
func (s *Supervisor) finish(run *supervision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == run {
		s.run, s.state = nil, ProcessStopped
	}
}

// Stop stops Sub-Store and waits until its process group is gone (or ctx ends).
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	run := s.run
	if run != nil {
		s.state = ProcessStopping
		run.stop()
	}
	s.mu.Unlock()
	if run == nil {
		return nil
	}
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop sub-store: %w", ctx.Err())
	}
}

// Restart stops and starts Sub-Store (an update replaced the bundle).
func (s *Supervisor) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

// spawn starts one process in its own process group; stopCtx ends it.
func (s *Supervisor) spawn(ctx, stopCtx context.Context) (*exec.Cmd, error) {
	argv, err := s.command(ctx)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(stopCtx, argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = &lineLogger{level: logx.Info}
	cmd.Stderr = &lineLogger{level: logx.Warning}
	// Stop: SIGTERM to the group, SIGKILL after StopTimeout (WaitDelay).
	cmd.Cancel = func() error { return signalGroup(cmd, syscall.SIGTERM) }
	cmd.WaitDelay = durationOr(s.StopTimeout, defaultStopTimeout)
	if err := cmd.Start(); err != nil {
		processLog.Errorf(msgProcessSpawnFailed, err)
		return nil, fmt.Errorf("start sub-store: %w", err)
	}
	processLog.Infof(msgProcessStarted, cmd.Process.Pid, strings.Join(argv, " "))
	return cmd, nil
}

// command is sub-store-start.sh: the newer of the image's and the online
// updated bundle, with SUB_STORE_DATA_BASE_PATH created first.
func (s *Supervisor) command(ctx context.Context) ([]string, error) {
	if s.Command != nil {
		return s.Command(ctx)
	}
	dir, _, err := s.env.ActiveBundle()
	if err != nil {
		return nil, err
	}
	if base, ok := s.env.LookupEnv("SUB_STORE_DATA_BASE_PATH"); ok && base != "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return nil, fmt.Errorf("create sub-store data directory: %w", err)
		}
	}
	return []string{"node", filepath.Join(dir, SubStoreAsset)}, nil
}

// supervise starts the period's first process (its result goes to first),
// waits for it and restarts crashed ones with back-off until Stop or server
// shutdown (work is cancelled after the shutdown drain, so Sub-Store keeps
// serving draining requests).
func (s *Supervisor) supervise(work context.Context, run *supervision, first chan<- error) {
	defer close(run.done)
	defer s.finish(run)
	// Shutdown ends the period like Stop does.
	unhook := context.AfterFunc(work, run.stop)
	defer unhook()
	clk := s.clock(work)
	cmd, err := s.spawn(work, run.stopCtx)
	if err != nil {
		// Stopped before the caller learns of the failure, so its retry
		// starts a new period instead of finding this one still set.
		s.finish(run)
		first <- err
		return
	}
	first <- nil
	s.setState(run, ProcessStarting, cmd, clk.Now())
	backoff := durationOr(s.BackoffMin, defaultBackoffMin)
	for {
		if cmd != nil {
			started := clk.Now()
			err := cmd.Wait()
			// Children left in the group (the leader is gone) are killed too.
			_ = signalGroup(cmd, syscall.SIGKILL)
			if run.stopCtx.Err() != nil {
				processLog.Infof(msgProcessStopped, cmd.Process.Pid)
				return
			}
			if clk.Now().Sub(started) >= durationOr(s.StableAfter, defaultStableAfter) {
				backoff = durationOr(s.BackoffMin, defaultBackoffMin)
			}
			processLog.Warningf(msgProcessExited, cmd.Process.Pid, exitText(err), backoff)
		}
		s.setState(run, ProcessBackoff, nil, time.Time{})
		timer := clk.NewTimer(backoff)
		select {
		case <-run.stopCtx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}
		backoff = min(backoff*2, durationOr(s.BackoffMax, defaultBackoffMax))
		next, err := s.spawn(work, run.stopCtx)
		if err != nil {
			cmd = nil
			continue
		}
		cmd = next
		s.setState(run, ProcessStarting, next, clk.Now())
	}
}

func (s *Supervisor) setState(run *supervision, state ProcessState, cmd *exec.Cmd, since time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != run {
		return
	}
	if s.state != ProcessStopping {
		s.state = state
	}
	run.cmd = cmd
	if !since.IsZero() {
		s.since = since
	}
}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func exitText(err error) string {
	if err == nil {
		return "exit status 0"
	}
	return err.Error()
}

// lineLogger writes the process output line by line to the server log,
// with URLs made log-safe (Sub-Store logs subscription addresses).
type lineLogger struct {
	level logx.Level
	mu    sync.Mutex
	buf   []byte
}

var (
	logURL = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)
	// logSecret matches key=value / "key": "value" pairs whose value is a
	// credential (Surge/Loon proxy lines, JSON node dumps).
	logSecret = regexp.MustCompile(`(?i)(\b(?:password|passwd|uuid|psk|token|auth-str|auth|private-key|pre-shared-key|obfs-password|secret)"?\s*[=:]\s*"?)[^"',\s}&]+`)
)

// redactProcessLine makes a line of Sub-Store output log-safe. Unlike the
// server's own URLs, these are mostly subscription addresses and proxy URIs:
// an http(s) URL keeps only its scheme and host (airports put the token in
// the path as often as in the query), any other URL (ss://, vmess://, …)
// keeps only its scheme, and credential key/value pairs lose their values.
func redactProcessLine(text string) string {
	text = logURL.ReplaceAllStringFunc(text, func(raw string) string {
		scheme, _, _ := strings.Cut(raw, "://")
		if lower := strings.ToLower(scheme); lower != "http" && lower != "https" {
			return scheme + "://" + logRedacted
		}
		// SafeForLog drops the user information and the fragment first.
		safeScheme, rest, ok := strings.Cut(weburl.SafeForLog(raw), "://")
		if !ok {
			return weburl.SafeForLog(raw)
		}
		host, tail := rest, ""
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			host, tail = rest[:i], rest[i:]
		}
		if strings.Trim(tail, "/") == "" {
			return safeScheme + "://" + host + tail
		}
		return safeScheme + "://" + host + "/" + logRedacted
	})
	return logSecret.ReplaceAllString(text, "${1}"+logRedacted)
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			if len(l.buf) > maxLogLine {
				l.emit(l.buf)
				l.buf = l.buf[:0]
			}
			return len(p), nil
		}
		l.emit(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
}

func (l *lineLogger) emit(line []byte) {
	text := strings.TrimRight(string(line), "\r")
	if len(text) > maxLogLine {
		text = text[:maxLogLine]
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	text = strings.ToValidUTF8(text, "�")
	processLog.Log(l.level, redactProcessLine(text))
}
