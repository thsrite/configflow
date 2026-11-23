package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
var ErrTimeout = errors.New("worker execution timed out")

var errMissingWorkerCommand = errors.New("missing worker command")

// ExecRunner executes worker commands via os/exec.
type ExecRunner struct{}

// Run executes a worker within both the command budget and the owner context.
// The caller detaches request cancellation while preserving Runtime shutdown.
func (ExecRunner) Run(ctx context.Context, command Command) (Result, error) {
	if len(command.Argv) == 0 {
		return Result{}, errMissingWorkerCommand
	}
	budget, cancel := context.WithTimeout(ctx, command.Timeout)
	defer cancel()
	cmd := exec.CommandContext(budget, command.Argv[0], command.Argv[1:]...)
	cmd.WaitDelay = time.Second
	cmd.Env = command.Env
	cmd.Stdin = bytes.NewReader(command.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if budget.Err() != nil {
		return Result{}, ErrTimeout
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return Result{}, err
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

// ValidationError reports an invalid caller or configuration input (HTTP 400).
type ValidationError struct{ Message string }

// Error returns the user-facing validation message.
func (e *ValidationError) Error() string { return e.Message }

func decodeText(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", invalidInput(WorkerOutputEncodingError)
	}
	return string(data), nil
}
