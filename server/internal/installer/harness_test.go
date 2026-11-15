package installer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	env     []string // full environment when replace is set, else added to os.Environ()
	replace bool
	input   string
	timeout time.Duration
	dir     string
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
		cmd.Env = o.env
	} else {
		cmd.Env = append(os.Environ(), o.env...)
	}
	cmd.Dir = o.dir
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

// pythonExe is sys.executable for Python stub commands.
func pythonExe(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("python3 is required for the Python stub commands")
	return ""
}
