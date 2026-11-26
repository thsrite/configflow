package main

import (
	"agent/routes"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAgentStartupProcess(t *testing.T) {
	if os.Getenv("CONFIGFLOW_STARTUP_TEST_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
}

func TestStrictDeploymentPreflightCleanDoesNotRegisterOrMigrate(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "agent.json")
	config := []byte(`{"service_type":"mihomo","config_path":"` + filepath.Join(root, "config.yaml") + `","service_manager":"systemd","native_aio":true}`)
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentStartupProcess$", "--", "-config", configPath, "-require-clean-deployments")
	cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clean preflight failed: %v\n%s", err, output)
	}
	got, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(got, config) {
		t.Fatalf("preflight changed config: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("preflight created state: %v, %v", entries, err)
	}
}

func TestStrictNativeAIOPreflightRejectsPendingAgentUpdate(t *testing.T) {
	for _, journal := range []string{`{"status":"checking","update_id":"aio-preflight-check"}`, `not-readable-json`} {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.json")
		config := []byte(`{"service_type":"mihomo","config_path":"` + filepath.Join(root, "core/config.yaml") + `","service_manager":"systemd","native_aio":true}`)
		if err := os.WriteFile(configPath, config, 0600); err != nil {
			t.Fatal(err)
		}
		journalPath := filepath.Join(root, ".configflow-updates/current.json")
		if err := os.MkdirAll(filepath.Dir(journalPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journalPath, []byte(journal), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentStartupProcess$", "--", "-config", configPath, "-require-clean-deployments")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
		output, err := cmd.CombinedOutput()
		cancel()
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "Agent 更新记录需要处理") {
			t.Fatalf("pending Agent update did not block install: %v\n%s", err, output)
		}
		if got, err := os.ReadFile(journalPath); err != nil || string(got) != journal {
			t.Fatalf("preflight changed Agent update journal: %v", err)
		}
	}
}

func TestAgentStartupOnlyInspectsInterruptedDeployments(t *testing.T) {
	for _, kind := range []string{"mihomo", "mosdns", "oxidns"} {
		for _, mode := range []string{"normal", "-check-deployments", "-require-clean-deployments", "-recover-only"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port := listener.Addr().(*net.TCPAddr).Port
				listener.Close()
				metrics := false
				cfg := &Config{ConfigPath: filepath.Join(root, "config.yaml"), ServiceType: kind,
					AgentHost: "127.0.0.1", AgentPort: port, AgentID: "startup-test", Token: "test-token",
					DeploymentMethod: "shell", ServiceManager: "command", HeartbeatInterval: 3600,
					StopCommand: "echo stopped >> lifecycle", StartCommand: "echo started >> lifecycle",
					StatusCommand: "exit 0", ServerURL: "http://127.0.0.1:1", EnableMetrics: &metrics,
					filePath: filepath.Join(root, "agent.json")}
				statePath := filepath.Join(root, ".configflow-deployments", "interrupted", "state.json")
				if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
					t.Fatal(err)
				}
				journal := []byte(`{"deployment_id":"interrupted","status":"replacing","was_running":true}`)
				config, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				files := map[string][]byte{statePath: journal, cfg.ConfigPath: []byte("mode: current\n"), cfg.filePath: config}
				if kind == "oxidns" {
					stateDir := filepath.Join(root, "state")
					if err := os.Mkdir(stateDir, 0700); err != nil {
						t.Fatal(err)
					}
					files[filepath.Join(stateDir, "learning.rules")] = []byte("learned runtime rules")
					files[filepath.Join(stateDir, "recorder.db")] = []byte{0, 1, 2, 255}
				}
				for path, data := range files {
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				// A legacy gate may execute while a publisher/old upgrade worker
				// owns the lock. Inspection must still succeed immediately.
				if mode != "normal" {
					release, err := routes.AcquireServiceOperation(toRoutesConfig(cfg))
					if err != nil {
						t.Fatal(err)
					}
					defer release()
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				args := []string{"-test.run=^TestAgentStartupProcess$", "--", "-config", cfg.filePath}
				if mode != "normal" {
					args = append(args, mode)
				}
				cmd := exec.CommandContext(ctx, os.Args[0], args...)
				cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1")
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				if mode == "normal" {
					client := &http.Client{Timeout: 100 * time.Millisecond}
					ready := false
					for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
						response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/health")
						if err == nil {
							response.Body.Close()
							ready = response.StatusCode == http.StatusOK
							if ready {
								break
							}
						}
					}
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					if !ready {
						t.Fatalf("Agent API did not start with pending recovery: %s", output.String())
					}
				} else {
					err := cmd.Wait()
					if mode == "-require-clean-deployments" {
						if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
							t.Fatalf("strict preflight must fail with exit 1: %v\n%s", err, output.String())
						}
					} else if err != nil {
						t.Fatalf("inspection must not block startup: %v\n%s", err, output.String())
					}
				}
				warning := "发布记录需要处理"
				if mode == "normal" {
					warning = "Deployment check requires attention"
				}
				if !strings.Contains(output.String(), warning) || !strings.Contains(output.String(), "-recover-and-exit") {
					t.Fatalf("missing actionable startup warning: %s", output.String())
				}
				for path, want := range files {
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("startup modified %s: %v", path, err)
					}
				}
				if _, err := os.Stat(filepath.Join(root, "lifecycle")); !os.IsNotExist(err) {
					t.Fatal("startup invoked service lifecycle")
				}
			})
		}
	}
}

func TestOxiDNSValidationCommandDoesNotMigrateOrRecover(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "agent.json")
	config, err := json.Marshal(&Config{ServiceType: "oxidns", ServiceManager: "systemd", ServiceUnit: "oxidns.service", ConfigPath: filepath.Join(root, "config.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, ".configflow-deployments", "interrupted", "state.json")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0700); err != nil {
		t.Fatal(err)
	}
	journal := []byte(`{"deployment_id":"interrupted","status":"replacing","was_running":true}`)
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	managerLog := filepath.Join(root, "manager.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CONFIGFLOW_TEST_COMMAND_LOG\"\n[ \"$1\" = show ] || exit 42\nprintf 'ExecStart=/usr/bin/oxidns start -d %s\\nWorkingDirectory=%s\\n' \"$CONFIGFLOW_TEST_CORE_DIR\" \"$CONFIGFLOW_TEST_CORE_DIR\"\n"
	if err := os.WriteFile(filepath.Join(root, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentStartupProcess$", "--", "-config", configPath, "-validate-service", "-verbose")
	cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1", "PATH="+root, "CONFIGFLOW_TEST_CORE_DIR="+root, "CONFIGFLOW_TEST_COMMAND_LOG="+managerLog)
	if output, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(output), "OxiDNS 服务目录校验完成") {
		t.Fatalf("validation: %v\n%s", err, output)
	}
	if got, err := os.ReadFile(managerLog); err != nil || string(got) != "show oxidns.service --property=ExecStart --property=WorkingDirectory\n" {
		t.Fatalf("validation changed service lifecycle: %q %v", got, err)
	}
	if got, err := os.ReadFile(journalPath); err != nil || !bytes.Equal(got, journal) {
		t.Fatal("validation changed deployment journal")
	}
	for _, path := range []string{filepath.Join(root, ".agent-migration.json"), filepath.Join(root, ".configflow-deployments", "operation.lock")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("validation created %s: %v", path, err)
		}
	}
}

func TestServiceValidationReportsActualScope(t *testing.T) {
	for _, tc := range []struct{ kind, manager, want string }{
		{"mosdns", "systemd", "MosDNS 无需服务目录专项校验"},
		{"mihomo", "openrc", "Mihomo 无需服务目录专项校验"},
		{"oxidns", "command", "OxiDNS 当前服务管理方式无需校验 systemd 工作目录"},
		{"custom", "command", "当前服务类型及管理方式无需服务目录专项校验"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "agent.json")
			data, err := json.Marshal(&Config{ServiceType: tc.kind, ServiceManager: tc.manager, ConfigPath: filepath.Join(root, "config.yaml")})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentStartupProcess$", "--", "-config", configPath, "-validate-service", "-verbose")
			cmd.Env = append(os.Environ(), "CONFIGFLOW_STARTUP_TEST_HELPER=1", "PATH="+root)
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), tc.want) || strings.Contains(string(output), "OxiDNS 服务目录校验完成") {
				t.Fatalf("incorrect validation result: %v\n%s", err, output)
			}
		})
	}
}
