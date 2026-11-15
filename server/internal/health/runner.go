package health

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/resource"
	"configflow/internal/substore"
)

// Command is one subprocess.run([binary], input=…, capture_output=True,
// text=True, timeout=…, env=…) call of the node-probe worker.
type Command struct {
	Argv    []string
	Stdin   []byte // the JSON payload (UTF-8)
	Timeout time.Duration
	Env     []string // os.environ plus GOMAXPROCS=2 / GOMEMLIMIT=128MiB unless set
}

// Result is the finished process: raw stdout/stderr bytes and the return
// code (negative signal number when killed by a signal, as Popen.returncode).
type Result struct {
	Stdout, Stderr []byte
	ReturnCode     int
}

// Runner runs the node-probe worker. Errors: ErrTimeout (TimeoutExpired,
// the process was killed) or an *OSError (it could not be started). Image
// package #40 supplies the real binary; ExecRunner runs it.
type Runner interface {
	Run(ctx context.Context, command Command) (Result, error)
}

// ErrTimeout is subprocess.TimeoutExpired.
var ErrTimeout = errors.New("subprocess.TimeoutExpired")

// OSError is the OSError of starting the process.
type OSError struct{ Err error }

func (e *OSError) Error() string { return e.Err.Error() }
func (e *OSError) Unwrap() error { return e.Err }

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run starts the command, writes Stdin, collects the output and kills the
// process when Timeout passes (subprocess.run's kill + wait).
func (ExecRunner) Run(ctx context.Context, command Command) (Result, error) {
	cmd := exec.Command(command.Argv[0], command.Argv[1:]...)
	cmd.Env = command.Env
	cmd.Stdin = bytes.NewReader(command.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, &OSError{err}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(command.Timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		cmd.Process.Kill()
		<-done
		return Result{}, ErrTimeout
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return Result{}, ErrTimeout
	}
	code := cmd.ProcessState.ExitCode()
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		code = -int(status.Signal())
	}
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ReturnCode: code}, nil
}

// Env is the outside world of node latency and node DNS checks.
type Env struct {
	// LookupEnv and Environ read the process environment (os.LookupEnv,
	// os.Environ).
	LookupEnv func(string) (string, bool)
	Environ   func() []string
	// Runner runs the worker.
	Runner Runner
	// Now is datetime.now(timezone.utc).
	Now func() time.Time
	// ProbeBinary replaces the lookup of _probe_binary (tests); nil uses
	// FindProbeBinary.
	ProbeBinary func() (string, error)
	// Converter converts URI node strings (Sub-Store); nil uses
	// substore.Converter().
	Converter convert.ProxyStringConverter
}

// DefaultEnv is the production environment.
func DefaultEnv() *Env {
	return &Env{LookupEnv: os.LookupEnv, Environ: os.Environ, Runner: ExecRunner{}, Now: time.Now}
}

var currentEnv = DefaultEnv()

// SetEnv replaces the environment (tests) and returns a function restoring
// the previous one.
func SetEnv(env *Env) (restore func()) {
	previous := currentEnv
	currentEnv = env
	return func() { currentEnv = previous }
}

// CurrentEnv is the environment in use.
func CurrentEnv() *Env { return currentEnv }

func (e *Env) getenv(key, fallback string) string {
	if value, ok := e.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func (e *Env) converter() convert.ProxyStringConverter {
	if e.Converter != nil {
		return e.Converter
	}
	return substore.Converter()
}

// workerEnv is dict(os.environ) with GOMAXPROCS and GOMEMLIMIT defaulted.
func (e *Env) workerEnv() []string {
	env := append([]string{}, e.Environ()...)
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

// ProbeBinaryName is the worker's file name.
const ProbeBinaryName = "configflow-node-probe"

// probeBinary is _probe_binary().
func (e *Env) probeBinary() (string, error) {
	if e.ProbeBinary != nil {
		return e.ProbeBinary()
	}
	return FindProbeBinary(e)
}

// FindProbeBinary is the lookup of _probe_binary: CONFIGFLOW_NODE_PROBE_BIN
// alone when set (non-empty); otherwise /opt/configflow/node-probe/…, the
// source tree's backend/node-probe/… and the PATH, the first executable
// regular file wins.
func FindProbeBinary(e *Env) (string, error) {
	var paths []string
	if configured := e.getenv("CONFIGFLOW_NODE_PROBE_BIN", ""); configured != "" {
		paths = []string{configured}
	} else {
		paths = []string{"/opt/configflow/node-probe/" + ProbeBinaryName,
			resource.FromEnv().Backend(filepath.Join("node-probe", ProbeBinaryName))}
		if found, err := exec.LookPath(ProbeBinaryName); err == nil {
			paths = append(paths, found)
		}
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && syscall.Access(path, 1) == nil {
			return path, nil
		}
	}
	return "", &ProbeUnavailable{"独立测速组件未安装，请更新 ConfigFlow 镜像；源码部署请先构建 backend/node-probe"}
}

// ProbeUnavailable is node_latency.ProbeUnavailable (503).
type ProbeUnavailable struct{ Message string }

func (e *ProbeUnavailable) Error() string { return e.Message }

// ProbeBusy is node_latency.ProbeBusy (409).
type ProbeBusy struct{ Message string }

func (e *ProbeBusy) Error() string { return e.Message }

// ValueError is a ValueError raised by this package (400 in the routes).
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

// decodeText is the strict UTF-8 decoding of text=True output: the
// UnicodeDecodeError (a ValueError) carries CPython's message.
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
		return "", &ValueError{"'utf-8' codec " + message}
	}
	return string(data), nil
}

// utf8ErrorSpan is the end and reason of CPython's UTF-8 decoding error at
// start.
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
