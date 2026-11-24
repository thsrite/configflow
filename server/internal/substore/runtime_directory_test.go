package substore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"configflow/internal/lifecycle"
)

func TestN89RuntimeDirectoryExplainsEnvironmentAndDoesNotWrite(t *testing.T) {
	for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
		for _, mode := range []string{"missing", "readonly"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				h, owner, _ := lifecycleService(t)
				base := t.TempDir()
				selected := filepath.Join(base, "selected")
				if mode == "readonly" {
					if err := os.Mkdir(selected, 0500); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.Chmod(selected, 0700) })
				}
				logs := captureLogs(t)
				for _, variable := range []string{"TMPDIR", "TEMP", "TMP"} {
					t.Setenv(variable, "")
				}
				t.Setenv(name, selected)
				owner.Directory = ""
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				h.service.Register(rt)
				var invalid *RuntimeDirectoryError
				_, err := owner.Snapshot(context.Background(), false)
				if !errors.As(err, &invalid) || invalid.Variable != name || invalid.Path != selected {
					t.Fatalf("environment diagnostic: %v", err)
				}
				reason := msgRuntimeDirectoryMissing
				if mode == "readonly" {
					reason = msgRuntimeDirectoryPermission
				}
				if err.Error() != fmt.Sprintf(msgRuntimeDirectory, name, selected, reason) {
					t.Fatal("non-Chinese directory failure", err)
				}
				if !strings.Contains(logs.text(), err.Error()) || !strings.Contains(err.Error(), fmt.Sprintf("环境变量 %s", name)) {
					t.Fatalf("startup warning lost setting: %s", logs.text())
				}
				called := false
				err = owner.Use(context.Background(), func(context.Context) error { called = true; return nil })
				starts, stops := h.commands.counts()
				if !errors.As(err, &invalid) || called || starts != 0 || stops != 0 {
					t.Fatalf("invalid directory reached runtime: %v called=%v starts=%d stops=%d", err, called, starts, stops)
				}
				entries, readErr := os.ReadDir(selected)
				if mode == "missing" {
					if !os.IsNotExist(readErr) {
						t.Fatal("missing root created", readErr)
					}
				} else if readErr != nil || len(entries) != 0 {
					t.Fatal("readonly root changed", readErr)
				}
			})
		}
	}
}

func TestN89RuntimeReadOnlyMountDiagnosticRetainsCause(t *testing.T) {
	err := &RuntimeDirectoryError{Variable: "TMPDIR", Path: "/synthetic/temporary", Err: syscall.EROFS}
	if !errors.Is(err, syscall.EROFS) || err.Error() != fmt.Sprintf(msgRuntimeDirectory, "TMPDIR", "/synthetic/temporary", msgRuntimeDirectoryReadOnly) {
		t.Fatal(err)
	}
}
