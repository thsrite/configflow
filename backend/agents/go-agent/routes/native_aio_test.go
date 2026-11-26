package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeAIOUninstallRejectsSharedOrMismatchedInstallation(t *testing.T) {
	valid := Config{NativeAIO: true, ServiceType: "mihomo", AgentConfigFile: "/opt/configflow-agent/mihomo/config.json", AgentServiceUnit: "configflow-agent-mihomo", ServiceManager: "systemd"}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.AgentConfigFile = "/opt/configflow-agent/config-mihomo.json" },
		func(c *Config) { c.AgentConfigFile = "/opt/configflow-agent/oxidns/config.json" },
		func(c *Config) { c.AgentServiceUnit = "configflow-agent" },
		func(c *Config) { c.AgentServiceUnit = "configflow-agent-oxidns" },
		func(c *Config) { c.ServiceType = "mosdns" },
		func(c *Config) { c.ServiceManager = "s6" },
	} {
		cfg := valid
		mutate(&cfg)
		if _, err := nativeAIOInstallationFor(&cfg); err == nil {
			t.Fatalf("unsafe installation accepted: %+v", cfg)
		}
		w := httptest.NewRecorder()
		UninstallHandler(&cfg)(w, httptest.NewRequest(http.MethodPost, "/uninstall", nil))
		if w.Code != http.StatusConflict {
			t.Fatalf("unsafe uninstall not rejected before asynchronous work: %d", w.Code)
		}
	}
}

func TestNativeAIOUninstallOnlyRemovesSelectedInstance(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, kind := range []string{"mihomo", "oxidns"} {
			t.Run(manager+"/"+kind, func(t *testing.T) {
				cfg := &Config{NativeAIO: true, ServiceType: kind, AgentConfigFile: "/opt/configflow-agent/" + kind + "/config.json", AgentServiceUnit: "configflow-agent-" + kind, ServiceManager: manager}
				instance, err := nativeAIOInstallationFor(cfg)
				if err != nil {
					t.Fatal(err)
				}
				root := t.TempDir()
				script := strings.ReplaceAll(instance.uninstallScript(), "'/", "'"+root+"/")
				sibling := "oxidns"
				if kind == sibling {
					sibling = "mihomo"
				}
				removed := []string{instance.Directory + "/config.json", instance.Log, "/etc/systemd/system/" + instance.Unit + ".service"}
				if manager == "openrc" {
					removed[2] = "/etc/init.d/" + instance.Unit
				}
				removed = append(removed, instance.Log+".1", "/etc/logrotate.d/"+instance.Unit)
				preserved := []string{"/opt/configflow-agent/" + sibling + "/config.json", "/usr/local/bin/configflow-agent", "/etc/" + kind + "/config.yaml", "/var/log/configflow-agent-" + sibling + ".log"}
				for _, path := range append(append([]string{}, removed...), preserved...) {
					full := filepath.Join(root, path)
					if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(full, []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				for _, command := range []string{"systemctl", "rc-service", "rc-update"} {
					if err := os.WriteFile(filepath.Join(root, command), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_MANAGER_LOG\"\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("/bin/sh", "-c", script)
				cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "TEST_MANAGER_LOG="+filepath.Join(root, "manager.log"))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("uninstall failed: %v %s", err, output)
				}
				for _, path := range removed {
					if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
						t.Fatalf("instance file remains: %s", path)
					}
				}
				for _, path := range preserved {
					if got, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(got) != "keep" {
						t.Fatalf("uninstall touched sibling/core file: %s (%v)", path, err)
					}
				}
				calls, _ := os.ReadFile(filepath.Join(root, "manager.log"))
				if strings.Contains(string(calls), sibling) || !strings.Contains(string(calls), instance.Unit) {
					t.Fatalf("wrong service lifecycle: %s", calls)
				}
			})
		}
	}
}

func TestNativeAIOLogSelection(t *testing.T) {
	for _, kind := range []string{"mihomo", "oxidns"} {
		if got := AgentLogPath(&Config{NativeAIO: true, ServiceType: kind}); got != "/var/log/configflow-agent-"+kind+".log" {
			t.Fatalf("wrong instance log: %s", got)
		}
	}
	if got := AgentLogPath(&Config{ServiceType: "mihomo"}); got != "/var/log/configflow-agent.log" {
		t.Fatalf("single-service log changed: %s", got)
	}
}
