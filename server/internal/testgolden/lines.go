package testgolden

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Lines replays a JSON-lines oracle. The real process starts lazily, only in an
// explicit recording/check mode, and is always reaped before the test ends.
type Lines struct {
	reference *Reference
	owner     *testing.T
	script    string
	env       []string
	input     io.WriteCloser
	output    *bufio.Scanner
	cmd       *exec.Cmd
	stderr    bytes.Buffer
	mu        sync.Mutex
}

// NewLines opens a transcript. Sources include the script and all Python backend
// files conservatively; Sources audits the explicitly declared harness files.
func NewLines(t *testing.T, script string) *Lines {
	t.Helper()
	lines := &Lines{script: script, owner: t}
	lines.reference = New(t, Spec{Sources: Sources(t, "backend", script), Compress: true})
	return lines
}

// Env adds recording-only environment variables before the first Call.
func (l *Lines) Env(values ...string) { l.env = append(l.env, values...) }

// Call returns the exact recorded response for input. keyInput, when non-nil,
// substitutes only the transcript key (for an allocated loopback fixture port).
func (l *Lines) Call(t *testing.T, input, keyInput []byte) []byte {
	t.Helper()
	if keyInput == nil {
		keyInput = input
	}
	return l.reference.Call(t, keyInput, func() ([]byte, error) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.cmd == nil {
			root, err := Root()
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			l.cmd = exec.CommandContext(ctx, Python(), "-u", l.script)
			l.cmd.Dir = root
			l.cmd.WaitDelay = 5 * time.Second
			l.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+l.owner.TempDir(), "PYTHONDONTWRITEBYTECODE=1")
			l.cmd.Env = append(l.cmd.Env, l.env...)
			l.cmd.Stderr = &l.stderr
			l.input, err = l.cmd.StdinPipe()
			if err != nil {
				cancel()
				return nil, err
			}
			out, err := l.cmd.StdoutPipe()
			if err != nil {
				cancel()
				return nil, err
			}
			if err = l.cmd.Start(); err != nil {
				cancel()
				return nil, err
			}
			l.output = bufio.NewScanner(out)
			l.output.Buffer(make([]byte, 65536), 1<<27)
			// This cleanup is registered after Reference's cleanup: reap first, then
			// decide whether a successful recording may be saved.
			l.owner.Cleanup(func() {
				_ = l.input.Close()
				err := l.cmd.Wait()
				cancel()
				if err != nil {
					l.owner.Errorf("reference shutdown: %v: %s", err, l.stderr.String())
				}
			})
		}
		if _, err := fmt.Fprintln(l.input, string(input)); err != nil {
			return nil, err
		}
		if !l.output.Scan() {
			return nil, fmt.Errorf("reference ended: %v", l.output.Err())
		}
		return bytes.Clone(l.output.Bytes()), nil
	})
}
