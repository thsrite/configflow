package routes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The commit reports each stage with its own message and accepts a managed
// file that appeared concurrently, whatever the link error was.
func TestAIOCommitForwardingConfigStages(t *testing.T) {
	errSync := errors.New("sync failed")
	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string)
		sync     func(string) error
		wantErr  string
		wantFile string
	}{
		{name: "fresh", wantFile: aioForwardingContent},
		{name: "directory not writable", wantErr: "IPv4 转发持久化目录不可写：", setup: func(t *testing.T, dir string) {
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			if probe, err := os.CreateTemp(dir, "probe"); err == nil {
				probe.Close()
				os.Remove(probe.Name())
				t.Skip("running with permission to write a read-only directory")
			}
		}},
		{name: "managed file already present", wantFile: aioForwardingContent, setup: func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "99-configflow-aio-forward.conf"), []byte(aioForwardingContent), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign file present", wantErr: "无法提交 IPv4 转发持久化配置：", wantFile: "foreign\n", setup: func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "99-configflow-aio-forward.conf"), []byte("foreign\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory sync failure after publish", sync: func(string) error { return errSync }, wantErr: errSync.Error(), wantFile: aioForwardingContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldDir, oldSync := aioSysctlDir, syncDirectory
			t.Cleanup(func() { aioSysctlDir, syncDirectory = oldDir, oldSync })
			aioSysctlDir = t.TempDir()
			if tc.sync != nil {
				syncDirectory = tc.sync
			}
			if tc.setup != nil {
				tc.setup(t, aioSysctlDir)
			}
			err := aioCommitForwardingConfig(aioForwardingConfigPath())
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want prefix %q", err, tc.wantErr)
			}
			if tc.wantFile != "" {
				data, readErr := os.ReadFile(aioForwardingConfigPath())
				if readErr != nil || string(data) != tc.wantFile {
					t.Fatalf("target = %q, %v; want %q", data, readErr, tc.wantFile)
				}
			}
			if leftovers, _ := filepath.Glob(filepath.Join(aioSysctlDir, ".configflow-aio-forward.*")); len(leftovers) != 0 {
				t.Fatalf("temporary files left: %v", leftovers)
			}
		})
	}
}
