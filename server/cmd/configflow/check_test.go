package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/repo"
)

func TestCheckStartup(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, dir string)
		env         map[string]string
		frontendDir string
		wantErr     string
		wantNoDB    bool
	}{
		{
			name:        "empty data dir passes without creating database",
			frontendDir: "/usr/share/configflow/html",
			wantNoDB:    true,
		},
		{
			name: "existing sqlite database passes",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if _, err := repo.OpenDefault(dir); err != nil {
					t.Fatalf("open default: %v", err)
				}
			},
			frontendDir: "/usr/share/configflow/html",
		},
		{
			name: "legacy config.json without sqlite is refused",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600); err != nil {
					t.Fatalf("write legacy file: %v", err)
				}
			},
			frontendDir: "/usr/share/configflow/html",
			wantErr:     "不再迁移 SQLite 之前的旧格式",
			wantNoDB:    true,
		},
		{
			name: "legacy split profile config without sqlite is refused",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				profileDir := filepath.Join(dir, "profiles", "default")
				if err := os.MkdirAll(profileDir, 0o755); err != nil {
					t.Fatalf("mkdir profile dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(profileDir, "config.json"), []byte("{}"), 0o600); err != nil {
					t.Fatalf("write legacy profile file: %v", err)
				}
			},
			frontendDir: "/usr/share/configflow/html",
			wantErr:     "不再迁移 SQLite 之前的旧格式",
			wantNoDB:    true,
		},
		{
			name: "invalid web port fails",
			env: map[string]string{
				"CONFIGFLOW_WEB_PORT": "not-a-port",
			},
			frontendDir: "/usr/share/configflow/html",
			wantErr:     "[web-listener]",
			wantNoDB:    true,
		},
		{
			name: "web port colliding with backend port fails",
			env: map[string]string{
				"CONFIGFLOW_WEB_PORT":     "5001",
				"CONFIGFLOW_BACKEND_PORT": "5001",
			},
			frontendDir: "/usr/share/configflow/html",
			wantErr:     "[web-listener]",
			wantNoDB:    true,
		},
		{
			name: "invalid backend port fails",
			env: map[string]string{
				"CONFIGFLOW_BACKEND_PORT": "99999",
			},
			frontendDir: "/usr/share/configflow/html",
			wantErr:     "CONFIGFLOW_BACKEND_PORT",
			wantNoDB:    true,
		},
		{
			name: "invalid shutdown grace fails",
			env: map[string]string{
				"CONFIGFLOW_SHUTDOWN_GRACE_SECONDS": "0",
			},
			wantErr:  msgInvalidGrace,
			wantNoDB: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("DATA_DIR", dir)
			t.Setenv("CONFIGFLOW_WEB_PORT", "")
			_ = os.Unsetenv("CONFIGFLOW_WEB_PORT")
			t.Setenv("CONFIGFLOW_BACKEND_PORT", "")
			_ = os.Unsetenv("CONFIGFLOW_BACKEND_PORT")
			t.Setenv("CONFIGFLOW_BACKEND_HOST", "")
			_ = os.Unsetenv("CONFIGFLOW_BACKEND_HOST")
			t.Setenv("SUB_STORE_BACKEND_API_PORT", "")
			_ = os.Unsetenv("SUB_STORE_BACKEND_API_PORT")
			t.Setenv("CONFIGFLOW_SHUTDOWN_GRACE_SECONDS", "")
			_ = os.Unsetenv("CONFIGFLOW_SHUTDOWN_GRACE_SECONDS")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			err := checkStartup(tc.frontendDir)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got error %v, want substring %q", err, tc.wantErr)
				}
			}
			if tc.wantNoDB {
				if _, statErr := os.Stat(filepath.Join(dir, repo.DatabaseFilename)); !os.IsNotExist(statErr) {
					t.Fatalf("checkStartup must not create database, stat err = %v", statErr)
				}
			}
		})
	}
}
