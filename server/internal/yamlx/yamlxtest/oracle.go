package yamlxtest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// Oracle runs an independent Python function on policy-normalized inputs.
// It performs no network requests and uses a temporary data directory.
type Oracle struct {
	input       io.WriteCloser
	output      *bufio.Scanner
	command     *exec.Cmd
	diagnostics oracleDiagnostics
}

// oracleDiagnostics can be inspected when stdout ends before os/exec has
// finished draining stderr. A failed reference process must not introduce a race.
type oracleDiagnostics struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (d *oracleDiagnostics) Write(data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buffer.Write(data)
}

func (d *oracleDiagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buffer.String()
}

// NewOracle starts a fixture program relative to the repository root.
func NewOracle(t *testing.T, script string) *Oracle {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if selected := os.Getenv("CONFIGFLOW_DIFF_PYTHON"); selected != "" {
		python = selected
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	command := exec.CommandContext(ctx, python, "-u", script)
	command.Dir = root
	command.WaitDelay = 5 * time.Second
	command.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+t.TempDir(), "PYTHONDONTWRITEBYTECODE=1")
	oracle := &Oracle{command: command}
	command.Stderr = &oracle.diagnostics
	oracle.input, err = command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	oracle.output = bufio.NewScanner(output)
	oracle.output.Buffer(make([]byte, 65536), 1<<27)
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = oracle.input.Close(); _ = command.Wait(); cancel() })
	return oracle
}

// Call returns one reference result; all fields remain available to compare.
func (o *Oracle) Call(t *testing.T, request any) *jsonx.Object {
	t.Helper()
	data, err := jsonx.Compact(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(o.input, string(data)); err != nil {
		t.Fatal(err)
	}
	if !o.output.Scan() {
		t.Fatalf("native-policy Python oracle: %v %s", o.output.Err(), o.diagnostics.String())
	}
	value, err := jsonx.Parse(o.output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}
