package svc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Runner executes one tool and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// S6 is the Manager of the image: programs are s6 service directories under
// Scandir, controlled with s6-svc and inspected with s6-svstat.
type S6 struct {
	Scandir    string
	CommandDir string
	StateDir   string
	Run        Runner
	// Poll is the interval at which Start and Stop re-check the state.
	Poll time.Duration
}

// NewS6 returns the Manager for the image's own s6 supervision tree.
func NewS6() *S6 {
	return &S6{Scandir: Scandir, CommandDir: CommandDir, StateDir: StateDir, Run: execRunner, Poll: 100 * time.Millisecond}
}

// svstatFields is the s6-svstat -o selection ParseSvstat expects.
const svstatFields = "up,wantedup,pid,updownfor"

// Bookkeeping files in StateDir, shared with Finish and Exec.
func startedFile(dir, name string) string  { return filepath.Join(dir, name+".started") }
func failuresFile(dir, name string) string { return filepath.Join(dir, name+".failures") }
func fatalFile(dir, name string) string    { return filepath.Join(dir, name+".fatal") }
func argvFile(dir, name string) string     { return filepath.Join(dir, name+".argv") }

func (m *S6) serviceDir(name string) (string, Spec, error) {
	spec, ok := Specs[name]
	if !ok {
		return "", Spec{}, ErrUnknown
	}
	dir := filepath.Join(m.Scandir, name)
	if _, err := os.Stat(dir); err != nil {
		return "", Spec{}, ErrUnknown
	}
	return dir, spec, nil
}

// Status reads the program state from s6-svstat and the FATAL marker.
func (m *S6) Status(ctx context.Context, name string) (Status, error) {
	dir, spec, err := m.serviceDir(name)
	if err != nil {
		return Status{}, err
	}
	out, err := m.Run(ctx, filepath.Join(m.CommandDir, "s6-svstat"), "-o", svstatFields, dir)
	if err != nil {
		return Status{}, fmt.Errorf("s6-svstat %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	_, statErr := os.Lstat(fatalFile(m.StateDir, name))
	return ParseSvstat(name, string(out), statErr == nil, spec.StartSecs)
}

// ParseSvstat maps `s6-svstat -o up,wantedup,pid,updownfor` output to a
// Supervisor-style state. fatal is the marker Finish leaves after the last
// allowed quick exit; startSecs is the program's start period.
func ParseSvstat(name, out string, fatal bool, startSecs time.Duration) (Status, error) {
	fields := strings.Fields(out)
	if len(fields) != 4 {
		return Status{}, fmt.Errorf("unexpected s6-svstat output for %s: %q", name, strings.TrimSpace(out))
	}
	up, err1 := strconv.ParseBool(fields[0])
	wanted, err2 := strconv.ParseBool(fields[1])
	pid, err3 := strconv.Atoi(fields[2])
	seconds, err4 := strconv.Atoi(fields[3])
	if err := errors.Join(err1, err2, err3, err4); err != nil || seconds < 0 {
		return Status{}, fmt.Errorf("unexpected s6-svstat output for %s: %q", name, strings.TrimSpace(out))
	}
	status := Status{Name: name}
	switch {
	case up && !wanted:
		status.State = StateStopping
	case up && time.Duration(seconds)*time.Second < startSecs:
		status.State = StateStarting
	case up:
		status.State = StateRunning
	case fatal:
		status.State = StateFatal
	case wanted:
		status.State = StateBackoff
	default:
		status.State = StateStopped
	}
	if up && pid > 0 {
		status.PID = pid
		status.Uptime = time.Duration(seconds) * time.Second
	}
	return status, nil
}

func (m *S6) svc(ctx context.Context, flag, dir string) error {
	if out, err := m.Run(ctx, filepath.Join(m.CommandDir, "s6-svc"), flag, dir); err != nil {
		return fmt.Errorf("s6-svc %s %s: %w: %s", flag, filepath.Base(dir), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Start clears the retry bookkeeping (as supervisorctl start does after
// FATAL), asks s6 to bring the program up and waits until it has stayed up
// for its start period or has failed for good.
func (m *S6) Start(ctx context.Context, name string) error {
	dir, spec, err := m.serviceDir(name)
	if err != nil {
		return err
	}
	for _, path := range []string{fatalFile(m.StateDir, name), failuresFile(m.StateDir, name)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("reset %s: %w", name, err)
		}
	}
	if err := m.svc(ctx, "-u", dir); err != nil {
		return err
	}
	attempts := time.Duration(spec.StartRetries + 1)
	limit := attempts*(spec.StartSecs+2*time.Second) + 5*time.Second
	return m.wait(ctx, name, limit, func(s Status) (bool, error) {
		switch s.State {
		case StateRunning:
			return true, nil
		case StateFatal:
			return true, ErrFatal
		}
		return false, nil
	})
}

// Stop asks s6 to bring the program down (SIGTERM, then SIGKILL after the
// program's stop timeout) and waits until it is down.
func (m *S6) Stop(ctx context.Context, name string) error {
	dir, spec, err := m.serviceDir(name)
	if err != nil {
		return err
	}
	if err := m.svc(ctx, "-d", dir); err != nil {
		return err
	}
	return m.wait(ctx, name, spec.StopTimeout+5*time.Second, func(s Status) (bool, error) {
		return s.State == StateStopped || s.State == StateFatal, nil
	})
}

// Restart stops and starts the program, like supervisorctl restart.
func (m *S6) Restart(ctx context.Context, name string) error {
	if err := m.Stop(ctx, name); err != nil {
		return err
	}
	return m.Start(ctx, name)
}

func (m *S6) wait(ctx context.Context, name string, limit time.Duration, done func(Status) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	poll := m.Poll
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	last := ""
	for {
		status, err := m.Status(ctx, name)
		if err == nil {
			last = string(status.State)
			if finished, result := done(status); finished {
				return result
			}
		}
		select {
		case <-ctx.Done():
			if last == "" {
				last = "unknown"
			}
			return fmt.Errorf("%s did not settle (last state %s): %w", name, last, ctx.Err())
		case <-ticker.C:
		}
	}
}
