package substore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// SUB_STORE_* constants of dependencies.py / sub_store_runtime.py.
const (
	SubStoreRepo       = "sub-store-org/Sub-Store"
	SubStoreAsset      = "sub-store.bundle.js"
	SubStoreProgram    = "sub-store"
	DefaultBuiltinDir  = "/opt/sub-store"
	SupervisorConfig   = "/etc/supervisor/conf.d/supervisord.conf"
	defaultPortSetting = "3001"
)

// Clock is the time source: Now (time.time / datetime.now), Monotonic
// (time.monotonic, seconds) and Sleep (time.sleep).
type Clock interface {
	Now() time.Time
	Monotonic() float64
	Sleep(d time.Duration)
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Monotonic is CLOCK_MONOTONIC in seconds, the value Python's
// time.monotonic() returns (it is stored in the runtime's state.json).
func (systemClock) Monotonic() float64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return float64(time.Now().UnixNano()) / 1e9
	}
	return float64(ts.Sec) + float64(ts.Nsec)/1e9
}

func (systemClock) Sleep(d time.Duration) { time.Sleep(d) }

// SystemClock is the real clock.
var SystemClock Clock = systemClock{}

// CommandResult is subprocess.run(..., capture_output=True, text=True).
type CommandResult struct {
	Stdout     string
	Stderr     string
	ReturnCode int
}

// CommandRunner runs a program: subprocess.run(argv, capture_output=True,
// text=True, timeout=timeout). Errors are what subprocess.run raises (a
// *PyError of class FileNotFoundError/PermissionError/OSError when the
// program cannot be started, TimeoutExpired after the timeout); a non-zero
// exit is a result.
//
// The process control of the bundled Sub-Store goes through this interface
// only: `supervisorctl -c SupervisorConfig <status|start|stop|restart>
// sub-store` and `node --version` / `node --check <file>`. The image
// (package #40) installs its own runner when it replaces supervisord.
type CommandRunner interface {
	Run(ctx context.Context, argv []string, timeout time.Duration) (CommandResult, error)
}

// ExecRunner runs programs with os/exec.
type ExecRunner struct{}

// Run implements CommandRunner.
func (ExecRunner) Run(ctx context.Context, argv []string, timeout time.Duration) (CommandResult, error) {
	// subprocess.run is not interrupted when the HTTP client goes away.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return CommandResult{}, pyErr("TimeoutExpired", fmt.Sprintf("Command %s timed out after %s seconds",
			jsonx.Repr(pyArgv(argv)), formatSeconds(timeout)))
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			if e, ok := err.(*exec.Error); ok {
				return CommandResult{}, &PyError{Class: "FileNotFoundError", Err: err,
					Message: fmt.Sprintf("[Errno 2] No such file or directory: %s", jsonx.Repr(e.Name))}
			}
			return CommandResult{}, osError(err, argv[0])
		}
	}
	// text=True: strict UTF-8 (stdout first) with universal newlines.
	var result CommandResult
	for _, stream := range []struct {
		data []byte
		into *string
	}{{stdout.Bytes(), &result.Stdout}, {stderr.Bytes(), &result.Stderr}} {
		if message := utf8DecodeError(stream.data); message != "" {
			return CommandResult{}, pyErr("UnicodeDecodeError", message)
		}
		*stream.into = strings.ReplaceAll(strings.ReplaceAll(string(stream.data), "\r\n", "\n"), "\r", "\n")
	}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ReturnCode = exit.ExitCode()
	}
	return result, nil
}

func pyArgv(argv []string) []any {
	out := make([]any, len(argv))
	for i, a := range argv {
		out[i] = a
	}
	return out
}

func formatSeconds(d time.Duration) string {
	s := d.Seconds()
	if s == float64(int64(s)) {
		return fmt.Sprint(int64(s))
	}
	return fmt.Sprint(s)
}

// Env is what dependencies.py reads from the process: the environment,
// DATA_DIR, the network, programs, time and randomness.
type Env struct {
	LookupEnv func(string) (string, bool)
	DataDir   func() string
	HTTP      HTTPClient
	Commands  CommandRunner
	Clock     Clock
	// RandomHex is uuid.uuid4().hex[:8].
	RandomHex func() string
}

// NewEnv returns the production environment.
func NewEnv() *Env {
	return &Env{LookupEnv: os.LookupEnv, DataDir: repo.DataDir, HTTP: RequestsClient{}, Commands: ExecRunner{},
		Clock: SystemClock, RandomHex: randomHex}
}

func randomHex() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (e *Env) getenv(key, fallback string) string {
	if value, ok := e.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// BuiltinDir is SUB_STORE_BUILTIN_DIR.
func (e *Env) BuiltinDir() string { return e.getenv("SUB_STORE_BUILTIN_DIR", DefaultBuiltinDir) }

// BuiltinSubStoreURL is builtin_sub_store_url(): the bundled Node process on
// loopback, port SUB_STORE_BACKEND_API_PORT (default 3001). An invalid port
// is a ValueError (*PyError).
func (e *Env) BuiltinSubStoreURL() (string, error) {
	raw := pyStrip(e.getenv("SUB_STORE_BACKEND_API_PORT", defaultPortSetting))
	valid := raw != "" && len(raw) <= 5
	for i := 0; valid && i < len(raw); i++ {
		valid = raw[i] >= '0' && raw[i] <= '9'
	}
	port := 0
	if valid {
		port, _ = strconv.Atoi(raw)
		valid = port >= 1 && port <= 65535
	}
	if !valid {
		return "", pyErr("ValueError", "SUB_STORE_BACKEND_API_PORT must be an integer between 1 and 65535")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// pyJoin is os.path.join.
func pyJoin(parts ...string) string {
	path := ""
	for i, part := range parts {
		switch {
		case i == 0 || strings.HasPrefix(part, "/"):
			path = part
		case path == "" || strings.HasSuffix(path, "/"):
			path += part
		default:
			path += "/" + part
		}
	}
	return path
}

// RuntimeDir is sub_store_runtime_dir(): DATA_DIR/sub-store/runtime.
func (e *Env) RuntimeDir() string { return pyJoin(e.DataDir(), "sub-store", "runtime") }

// ParseVersion is parse_version: '2.42.3' / 'v2.42.3' → [2 42 3]; nil when
// a part is not an int().
func ParseVersion(value any) []*big.Int {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	text = strings.TrimLeft(pyStrip(text), "vV")
	var parts []*big.Int
	for _, part := range strings.Split(text, ".") {
		n, ok := pyInt(part)
		if !ok {
			return nil
		}
		parts = append(parts, n)
	}
	return parts
}

// compareVersions is tuple comparison.
func compareVersions(a, b []*big.Int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := a[i].Cmp(b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

func sameVersion(a, b []*big.Int) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return compareVersions(a, b) == 0
}

// IsNewer is is_newer(candidate, current).
func IsNewer(candidate, current any) bool {
	a, b := ParseVersion(candidate), ParseVersion(current)
	return a != nil && (b == nil || compareVersions(a, b) > 0)
}

// readVersionFile is _read_version_file: VERSION stripped, "" when it
// cannot be read; undecodable text is UnicodeDecodeError, as in Python.
func readVersionFile(directory string) (string, error) {
	data, err := os.ReadFile(pyJoin(directory, "VERSION"))
	if err != nil {
		return "", nil
	}
	if message := utf8DecodeError(data); message != "" {
		return "", pyErr("UnicodeDecodeError", message)
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	return pyStrip(text), nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// IsBuiltinAvailable is is_builtin_available(): the bundle of the official
// image exists.
func (e *Env) IsBuiltinAvailable() bool { return isFile(pyJoin(e.BuiltinDir(), SubStoreAsset)) }

// ActiveBundle is active_bundle(): (directory, version) chosen like
// sub-store-start.sh — the online update when it is newer than the image's.
func (e *Env) ActiveBundle() (string, string, error) {
	builtin := e.BuiltinDir()
	builtinVersion, err := readVersionFile(builtin)
	if err != nil {
		return "", "", err
	}
	runtimeDir := e.RuntimeDir()
	runtimeVersion, err := readVersionFile(runtimeDir)
	if err != nil {
		return "", "", err
	}
	if isFile(pyJoin(runtimeDir, SubStoreAsset)) && IsNewer(runtimeVersion, builtinVersion) {
		return runtimeDir, runtimeVersion, nil
	}
	return builtin, builtinVersion, nil
}

// FetchRunningVersion is fetch_running_version(base_url, timeout): the
// version /api/utils/env reports, "" on any failure.
func (e *Env) FetchRunningVersion(ctx context.Context, baseURL string, timeout float64) string {
	resp, err := e.HTTP.Do(ctx, &Request{Method: "GET", URL: baseURL + "/api/utils/env", Timeout: Seconds(timeout)})
	if err != nil || resp.RaiseForStatus() != nil {
		return ""
	}
	data, err := resp.JSON()
	if err != nil {
		return ""
	}
	payload := any(jsonx.NewObject())
	if object, ok := data.(*jsonx.Object); ok {
		payload = object.GetOr("data", object)
	}
	object, ok := payload.(*jsonx.Object)
	if !ok {
		return ""
	}
	version, _ := object.GetOr("version", "").(string)
	return version
}

// BaseURL is sub_store_client._get_base_url(): the bundled Sub-Store in the
// official image, otherwise SUB_STORE_URL (development), trailing "/"
// removed.
func (e *Env) BaseURL() (string, error) {
	if e.IsBuiltinAvailable() {
		return e.BuiltinSubStoreURL()
	}
	value, _ := e.LookupEnv("SUB_STORE_URL")
	if value == "" {
		builtin, err := e.BuiltinSubStoreURL()
		if err != nil {
			return "", err
		}
		value = builtin
	}
	return strings.TrimRight(value, "/"), nil
}
