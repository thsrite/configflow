package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"configflow/internal/model"
	"configflow/internal/resource"
)

// Command describes one subprocess invocation of the node-probe worker.
type Command struct {
	Argv    []string
	Stdin   []byte
	Timeout time.Duration
	Env     []string
}

// Result holds raw stdout/stderr bytes and the exit status of a finished worker.
type Result struct {
	Stdout, Stderr []byte
	ReturnCode     int
}

// Runner executes the node-probe worker subprocess.
type Runner interface {
	Run(ctx context.Context, command Command) (Result, error)
}

// ErrTimeout reports that a worker subprocess exceeded its execution budget.
var ErrTimeout = errors.New("subprocess.TimeoutExpired")

// OSError wraps an operating-system failure when starting the worker process.
type OSError struct{ Err error }

// Error returns the underlying OS error text.
func (e *OSError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying OS error.
func (e *OSError) Unwrap() error { return e.Err }

// ExecRunner executes worker commands via os/exec.
type ExecRunner struct{}

// Run starts command, feeds Stdin, captures output, and kills the process if
// Timeout elapses. Request context cancellation does not abort a started run.
func (ExecRunner) Run(_ context.Context, command Command) (Result, error) {
	cmd := exec.Command(command.Argv[0], command.Argv[1:]...)
	cmd.Env = command.Env
	cmd.Stdin = bytes.NewReader(command.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, &OSError{Err: err}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(command.Timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
		return Result{}, ErrTimeout
	}
	code := cmd.ProcessState.ExitCode()
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		code = -int(status.Signal())
	}
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ReturnCode: code}, nil
}

// Collaborators provides typed callbacks into neighbour packages.
type Collaborators struct {
	NeedsRemoteConversion   func(proxyString string) bool
	ConvertNode             func(ctx context.Context, node json.RawMessage) (model.Fields, bool)
	LoadSubscriptionCache   func(ctx context.Context, subID string) (json.RawMessage, error)
	FilterSubscriptionNodes func(sub model.Subscription, nodes []json.RawMessage) ([]json.RawMessage, error)
	ReadLatencyFile         func(ctx context.Context) (json.RawMessage, bool)
	WriteLatencyFile        func(ctx context.Context, data json.RawMessage) error
}

// ProbeEnv configures external dependencies for node DNS and latency probes.
type ProbeEnv struct {
	LookupEnv     func(string) (string, bool)
	Environ       func() []string
	Runner        Runner
	Now           func() time.Time
	ProbeBinary   func() (string, error)
	Collaborators Collaborators
}

var currentProbeEnv = &ProbeEnv{
	LookupEnv: os.LookupEnv,
	Environ:   os.Environ,
	Runner:    ExecRunner{},
	Now:       time.Now,
}

// SetProbeEnv replaces the active ProbeEnv and returns a restore callback.
func SetProbeEnv(env *ProbeEnv) (restore func()) {
	previous := currentProbeEnv
	currentProbeEnv = env
	return func() { currentProbeEnv = previous }
}

// CurrentProbeEnv returns the active ProbeEnv.
func CurrentProbeEnv() *ProbeEnv { return currentProbeEnv }

func (e *ProbeEnv) getenv(key, fallback string) string {
	if e != nil && e.LookupEnv != nil {
		if value, ok := e.LookupEnv(key); ok {
			return value
		}
	}
	return fallback
}

func (e *ProbeEnv) workerEnv() []string {
	var env []string
	if e != nil && e.Environ != nil {
		env = append(env, e.Environ()...)
	}
	for _, pair := range [][2]string{{"GOMAXPROCS", "2"}, {"GOMEMLIMIT", "128MiB"}} {
		found := false
		for _, item := range env {
			if strings.HasPrefix(item, pair[0]+"=") {
				found = true
				break
			}
		}
		if !found {
			env = append(env, pair[0]+"="+pair[1])
		}
	}
	return env
}

// ProbeBinaryName is the executable name of the bundled node-probe worker.
const ProbeBinaryName = "configflow-node-probe"

func (e *ProbeEnv) probeBinary() (string, error) {
	if e != nil && e.ProbeBinary != nil {
		return e.ProbeBinary()
	}
	return FindBinary(e)
}

// FindBinary locates the node-probe worker binary using CONFIGFLOW_NODE_PROBE_BIN
// when non-empty, or standard deployment/source/PATH locations otherwise.
func FindBinary(e *ProbeEnv) (string, error) {
	var paths []string
	if configured := e.getenv("CONFIGFLOW_NODE_PROBE_BIN", ""); configured != "" {
		paths = []string{configured}
	} else {
		paths = []string{
			"/opt/configflow/node-probe/" + ProbeBinaryName,
			resource.FromEnv().Backend(filepath.Join("node-probe", ProbeBinaryName)),
		}
		if found, err := exec.LookPath(ProbeBinaryName); err == nil {
			paths = append(paths, found)
		}
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && syscall.Access(path, 1) == nil {
			return path, nil
		}
	}
	return "", &ProbeUnavailable{Message: msgProbeNotInstalled}
}

// ProbeUnavailable reports that the node-probe worker cannot fulfill a request (HTTP 503).
type ProbeUnavailable struct{ Message string }

// Error returns the user-facing unavailability message.
func (e *ProbeUnavailable) Error() string { return e.Message }

// ProbeBusy reports that another node latency test is already running (HTTP 409).
type ProbeBusy struct{ Message string }

// Error returns the user-facing concurrency conflict message.
func (e *ProbeBusy) Error() string { return e.Message }

// ValueError reports an invalid caller or configuration input (HTTP 400).
type ValueError struct{ Message string }

// Error returns the user-facing validation message.
func (e *ValueError) Error() string { return e.Message }

func decodeText(data []byte) (string, error) {
	for i := 0; i < len(data); {
		if data[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(data[i:])
		if r != utf8.RuneError || size > 1 {
			i += size
			continue
		}
		end, reason := utf8ErrorSpan(data, i)
		message := fmt.Sprintf("can't decode byte 0x%02x in position %d: %s", data[i], i, reason)
		if end-i > 1 {
			message = fmt.Sprintf("can't decode bytes in position %d-%d: %s", i, end-1, reason)
		}
		return "", &ValueError{Message: "'utf-8' codec " + message}
	}
	return string(data), nil
}

func utf8ErrorSpan(data []byte, start int) (int, string) {
	b := data[start]
	var need int
	low, high := byte(0x80), byte(0xBF)
	switch {
	case b >= 0xC2 && b <= 0xDF:
		need = 1
	case b >= 0xE0 && b <= 0xEF:
		need = 2
		if b == 0xE0 {
			low = 0xA0
		} else if b == 0xED {
			high = 0x9F
		}
	case b >= 0xF0 && b <= 0xF4:
		need = 3
		if b == 0xF0 {
			low = 0x90
		} else if b == 0xF4 {
			high = 0x8F
		}
	default:
		return start + 1, "invalid start byte"
	}
	i := start + 1
	for k := 0; k < need; k++ {
		if i >= len(data) {
			return len(data), "unexpected end of data"
		}
		c := data[i]
		lo, hi := byte(0x80), byte(0xBF)
		if k == 0 {
			lo, hi = low, high
		}
		if c < lo || c > hi {
			return i, "invalid continuation byte"
		}
		i++
	}
	return i, "invalid continuation byte"
}
