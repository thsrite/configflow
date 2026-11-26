package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTUNPreparationCLIStopsBeforeNormalStartup(t *testing.T) {
	for _, tc := range []struct {
		name, core string
		wantCode   int
	}{
		{"disabled", "tun:\n  enable: false\n", 0},
		{"invalid-core", "secret: [PRIVATE-TOKEN\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			corePath := filepath.Join(root, "config.yaml")
			candidate := filepath.Join(root, "candidate.json")
			data, err := json.Marshal(map[string]any{
				"server_url": "http://127.0.0.1:1", "token": "PRIVATE-TOKEN", "agent_port": 8080,
				"native_aio": true, "service_type": "mihomo", "service_manager": "openrc", "config_path": corePath,
			})
			if err != nil {
				t.Fatal(err)
			}
			before := map[string][]byte{candidate: data, corePath: []byte(tc.core)}
			for name, contents := range before {
				if err := os.WriteFile(name, contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentStartupProcess$", "--", "-config", candidate, "-prepare-tun")
			cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
			output, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != tc.wantCode || !strings.Contains(string(output), "TUN 安装准备") || strings.Contains(string(output), "PRIVATE-TOKEN") || strings.Contains(string(output), "仅检测，不自动修改系统") {
				t.Fatalf("wrong preparation result (%d): %s", code, output)
			}
			for name, contents := range before {
				got, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(got, contents) {
					t.Fatalf("preparation modified configuration: %s: %v", name, err)
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 2 {
				t.Fatalf("preparation created migration or runtime state: %v, %v", entries, err)
			}
		})
	}
}

func TestTUNPreparationCLIRejectsAmbiguousOperations(t *testing.T) {
	for _, extra := range [][]string{
		{}, {"-config", "/nonexistent", "-check-environment"},
		{"-config", "/nonexistent", "-environment-peer-config", "/peer"},
		{"-config", "/nonexistent", "-recover-and-exit"},
		{"-config", "/nonexistent", "-upgrade-worker", "/worker"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		args := append([]string{"-test.run=^TestAgentStartupProcess$", "--", "-prepare-tun"}, extra...)
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
		output, err := cmd.CombinedOutput()
		cancel()
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 || !strings.Contains(string(output), "不能与其他操作参数一起使用") {
			t.Fatalf("mixed or implicit preparation must fail before accessing config: %v: %v\n%s", extra, err, output)
		}
	}
}

func TestTUNPreparationCLIAssumeEnabledRequiresPrepare(t *testing.T) {
	for _, extra := range [][]string{
		{"-assume-tun-enabled"},
		{"-config", "/nonexistent", "-assume-tun-enabled"},
		{"-config", "/nonexistent", "-assume-tun-enabled", "-check-environment"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		args := append([]string{"-test.run=^TestAgentStartupProcess$", "--"}, extra...)
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
		output, err := cmd.CombinedOutput()
		cancel()
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 || !strings.Contains(string(output), "只能与 -prepare-tun 一起使用") {
			t.Fatalf("-assume-tun-enabled without -prepare-tun must fail before accessing config: %v: %v\n%s", extra, err, output)
		}
	}
}

func TestTUNPreparationCLIAssumeEnabledDoesNotSkipDisabledBootstrap(t *testing.T) {
	root := t.TempDir()
	corePath := filepath.Join(root, "config.yaml")
	candidate := filepath.Join(root, "candidate.json")
	// A non-native candidate is refused before any host inspection, which
	// proves the disabled bootstrap was not skipped as in ordinary preparation.
	data, err := json.Marshal(map[string]any{
		"server_url": "http://127.0.0.1:1", "token": "PRIVATE-TOKEN", "agent_port": 8080,
		"service_type": "mihomo", "service_manager": "openrc", "config_path": corePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corePath, []byte("tun:\n  enable: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args     []string
		wantCode int
		want     string
	}{
		{[]string{"-prepare-tun"}, 0, "跳过"},
		{[]string{"-prepare-tun", "-assume-tun-enabled"}, 1, "普通 Shell 同机安装"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		args := append([]string{"-test.run=^TestAgentStartupProcess$", "--", "-config", candidate, "-verbose"}, tc.args...)
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
		output, err := cmd.CombinedOutput()
		cancel()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != tc.wantCode || !strings.Contains(string(output), tc.want) {
			t.Fatalf("%v: wrong result (%d): %s", tc.args, code, output)
		}
	}
}
