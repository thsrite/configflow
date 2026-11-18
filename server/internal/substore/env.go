package substore

import (
	"bytes"
	"configflow/internal/lifecycle"
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

	"configflow/internal/model"
	"configflow/internal/repo"
	"encoding/json"
	"errors"
	"path/filepath"
	"unicode/utf8"
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

// CommandRunner executes a bounded external command; a non-zero exit is a result.
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

// ErrCommandEncoding identifies non-UTF-8 process output.
var ErrCommandEncoding = errors.New(msgCommandUTF8)

// ErrVersionEncoding identifies a non-UTF-8 VERSION file.
var ErrVersionEncoding = errors.New(msgVersionUTF8)

// Run implements CommandRunner.
func (ExecRunner) Run(ctx context.Context, argv []string, timeout time.Duration) (CommandResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	if len(argv) == 0 {
		return CommandResult{}, errors.New("empty command")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if canceled := ctx.Err(); canceled != nil {
		return CommandResult{}, canceled
	}
	if canceled := runCtx.Err(); canceled != nil {
		return CommandResult{}, fmt.Errorf("run %s: %w", argv[0], canceled)
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return CommandResult{}, fmt.Errorf("run %s: %w", argv[0], err)
	}
	if !utf8.Valid(stdout.Bytes()) || !utf8.Valid(stderr.Bytes()) {
		return CommandResult{}, ErrCommandEncoding
	}
	normalize := func(text string) string {
		return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	}
	result := CommandResult{Stdout: normalize(stdout.String()), Stderr: normalize(stderr.String())}
	if exit != nil {
		result.ReturnCode = exit.ExitCode()
	}
	return result, nil
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
// returns a typed PortError.
func (e *Env) BuiltinSubStoreURL() (string, error) {
	raw := strings.TrimSpace(e.getenv("SUB_STORE_BACKEND_API_PORT", defaultPortSetting))
	var port model.Int
	if len(raw) > 5 || port.UnmarshalJSON([]byte(strconv.Quote(raw))) != nil || port < 1 || port > 65535 {
		return "", &PortError{Value: raw}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// PortError identifies an invalid local Sub-Store port setting.
type PortError struct{ Value string }

// Error describes the required range without echoing environment secrets.
func (e *PortError) Error() string { return msgBuiltinPort }

// RuntimeDir is sub_store_runtime_dir(): DATA_DIR/sub-store/runtime.
func (e *Env) RuntimeDir() string { return filepath.Join(e.DataDir(), "sub-store", "runtime") }

// VersionParts parses dotted arbitrary-precision decimal version components.
func VersionParts(text string) []*big.Int {
	text = strings.TrimLeft(strings.TrimSpace(text), "vV")
	var parts []*big.Int
	for _, part := range strings.Split(text, ".") {
		n, ok := new(big.Int).SetString(part, 10)
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

// VersionNewer compares two native version strings.
func VersionNewer(candidate, current string) bool {
	a, b := VersionParts(candidate), VersionParts(current)
	return a != nil && (b == nil || compareVersions(a, b) > 0)
}

// readVersionFile is _read_version_file: VERSION stripped, "" when it
// cannot be read; undecodable text is UnicodeDecodeError, as in Python.
func readVersionFile(directory string) (string, error) {
	data, err := os.ReadFile(filepath.Join(directory, "VERSION"))
	if err != nil {
		return "", nil
	}
	if !utf8.Valid(data) {
		return "", ErrVersionEncoding
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	return strings.TrimSpace(text), nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// IsBuiltinAvailable is is_builtin_available(): the bundle of the official
// image exists.
func (e *Env) IsBuiltinAvailable() bool { return isFile(filepath.Join(e.BuiltinDir(), SubStoreAsset)) }

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
	if isFile(filepath.Join(runtimeDir, SubStoreAsset)) && VersionNewer(runtimeVersion, builtinVersion) {
		return runtimeDir, runtimeVersion, nil
	}
	return builtin, builtinVersion, nil
}

// FetchRunningVersion is fetch_running_version(base_url, timeout): the
// version /api/utils/env reports, "" on any failure.
func (e *Env) FetchRunningVersion(ctx context.Context, baseURL string, timeout float64) string {
	if ctx.Err() != nil {
		return ""
	}
	resp, err := e.HTTP.Do(ctx, &Request{Method: "GET", URL: baseURL + "/api/utils/env", Timeout: Seconds(timeout)})
	if ctx.Err() != nil || err != nil || resp.RaiseForStatus() != nil {
		return ""
	}
	data, err := resp.JSONData()
	if err != nil {
		return ""
	}
	var envelope struct {
		Data    json.RawMessage `json:"data"`
		Version model.String    `json:"version"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return ""
	}
	if len(envelope.Data) != 0 {
		var payload struct {
			Version model.String `json:"version"`
		}
		if json.Unmarshal(envelope.Data, &payload) != nil {
			return ""
		}
		return string(payload.Version)
	}
	return string(envelope.Version)
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
