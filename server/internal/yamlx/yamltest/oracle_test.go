package yamltest

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOracleDiagnosticsDuringReferenceFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", "-c", `i=0; while [ "$i" -lt 1000 ]; do printf 'reference failure\n' >&2; i=$((i+1)); done; exit 7`)
	var diagnostics oracleDiagnostics
	command.Stderr = &diagnostics
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	// A failed stdout scan can inspect diagnostics before os/exec has finished
	// copying stderr. Exercise that overlap with a real failing subprocess.
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var processErr error
reading:
	for {
		select {
		case processErr = <-done:
			break reading
		default:
			_ = diagnostics.String()
		}
	}
	if processErr == nil || command.ProcessState.ExitCode() != 7 {
		t.Fatalf("reference process did not fail as intended: %v", processErr)
	}
	if got := diagnostics.String(); got != strings.Repeat("reference failure\n", 1000) {
		t.Fatalf("reference diagnostics lost bytes: %d", len(got))
	}
}
