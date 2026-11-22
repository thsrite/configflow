package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Shell harness shared by the ported script tests: they render a script with
// the Go code and run it with sh/dash/bash in a temporary directory with
// stub commands, as the Python tests do with subprocess.

// shells is SHELLS: sh, dash and bash where installed.
func shells() []string {
	var out []string
	for _, name := range []string{"sh", "dash", "bash"} {
		if path, err := exec.LookPath(name); err == nil {
			out = append(out, path)
		}
	}
	return out
}

type runResult struct {
	code           int
	stdout, stderr string
}

type runOptions struct {
	args    []string
	env     []string // full environment when replace is set, else added to os.Environ(); UTF-8 locale is always set
	replace bool
	input   string
	timeout time.Duration
	dir     string
	setsid  bool // start_new_session=True: no controlling terminal
}

func runShell(t *testing.T, shell, script string, o runOptions) runResult {
	t.Helper()
	timeout := o.timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, append([]string{script}, o.args...)...)
	if o.replace {
		cmd.Env = append([]string(nil), o.env...)
	} else {
		cmd.Env = append(os.Environ(), o.env...)
	}
	// Start the interpreter in a UTF-8 locale. Dash 0.5.13.4 corrupts
	// non-ASCII assignments in the C locale and cannot switch locale at runtime.
	// Append after caller overrides, which may include an inherited locale.
	cmd.Env = append(cmd.Env, "LANG=C.UTF-8", "LC_ALL=C.UTF-8")
	cmd.Dir = o.dir
	if o.setsid {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	cmd.Stdin = strings.NewReader(o.input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out\nstdout:\n%s\nstderr:\n%s", script, stdout.String(), stderr.String())
	}
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return runResult{code, stdout.String(), stderr.String()}
}

func writeFile(t *testing.T, path, text string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func must(t *testing.T, text string, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func contains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("missing %q in:\n%s", needle, haystack)
	}
}

func notContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("unexpected %q in:\n%s", needle, haystack)
	}
}

// agentBinary builds the Go Agent (backend/agents/go-agent) once per test
// binary and returns its path: the Docker HA tests run the shipped
// configflow-agent ha-* subcommands exactly as the generated commands do.
// A missing Go toolchain fails the tests; it never skips them.
func agentBinary(t *testing.T) string {
	t.Helper()
	agentBuild.once.Do(func() {
		dir, err := os.MkdirTemp("", "configflow-agent-")
		if err != nil {
			agentBuild.err = err
			return
		}
		agentBuild.dir = dir
		path := filepath.Join(dir, "configflow-agent")
		cmd := exec.Command("go", "build", "-o", path, ".")
		cmd.Dir = filepath.Join("..", "..", "..", "backend", "agents", "go-agent")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			agentBuild.err = fmt.Errorf("go build agent: %w: %s", err, output)
			return
		}
		agentBuild.path = path
	})
	if agentBuild.err != nil {
		t.Fatal(agentBuild.err)
	}
	return agentBuild.path
}

// agentBuild is the per-binary Agent build; TestMain removes its directory.
var agentBuild struct {
	once      sync.Once
	dir, path string
	err       error
}

// ok returns a rendered value, panicking (failing the test) on an error.
func ok(text string, err error) string {
	if err != nil {
		panic(err)
	}
	return text
}
