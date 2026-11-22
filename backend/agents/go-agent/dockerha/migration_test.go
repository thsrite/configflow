package dockerha

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	oldName   = "dns-old"
	oldServer = "http://panel.example"
)

func oldConfig(extra string) string {
	return `{"agent_name": "` + oldName + `", "server_url": "` + oldServer + `", "agent_port": 8082, "service_type": "oxidns", "config_path": "/etc/oxidns/config.yaml"` + extra + `}`
}

func runMigration(t *testing.T, dir, env string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := CheckMigration(MigrationCheck{AgentDir: dir, AgentName: oldName, ServerURL: oldServer, AgentPort: "8082", OxiDNSPath: oxidnsConfigPath}, strings.NewReader(env), &out)
	return out.String(), err
}

func TestMigrationPrintsRuntimeEnvironment(t *testing.T) {
	tests := []struct {
		name, config, env, want string
	}{
		{"defaults", oldConfig(""), `["PATH=/usr/bin", "TZ=UTC"]`,
			"PATH=/usr/bin\nTZ=UTC\nHEARTBEAT_INTERVAL=30\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0\nOXIDNS_HEALTH_DNS_ADDRESS=\nOXIDNS_HEALTH_DNS_NAME=\n"},
		{"stored settings and secrets kept verbatim", oldConfig(`, "heartbeat_interval": 42, "deployment_health_timeout": "45", "agent_host": "192.0.2.5", "health_dns_address": "127.0.0.1:53", "health_dns_name": "example.com"`),
			`["HTTPS_PROXY=http://private-user:private$password@proxy:8080", "A=b=c", "HEARTBEAT_INTERVAL=5"]`,
			"HTTPS_PROXY=http://private-user:private$password@proxy:8080\nA=b=c\nHEARTBEAT_INTERVAL=42\nDEPLOYMENT_HEALTH_TIMEOUT=45\nAGENT_HOST=192.0.2.5\nOXIDNS_HEALTH_DNS_ADDRESS=127.0.0.1:53\nOXIDNS_HEALTH_DNS_NAME=example.com\nTZ=Asia/Shanghai\n"},
		{"duplicate keeps first position, last value", oldConfig(""), `["X=1", "TZ=UTC", "X=2"]`,
			"X=2\nTZ=UTC\nHEARTBEAT_INTERVAL=30\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0\nOXIDNS_HEALTH_DNS_ADDRESS=\nOXIDNS_HEALTH_DNS_NAME=\n"},
		{"port as string, single service type", strings.Replace(oldConfig(""), `8082`, `"8082"`, 1), `["SERVICE_TYPE=oxidns", "ENABLE_MIHOMO=false", "CONFIGFLOW_DNS_HA=0"]`,
			"SERVICE_TYPE=oxidns\nENABLE_MIHOMO=false\nCONFIGFLOW_DNS_HA=0\nHEARTBEAT_INTERVAL=30\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0\nOXIDNS_HEALTH_DNS_ADDRESS=\nOXIDNS_HEALTH_DNS_NAME=\nTZ=Asia/Shanghai\n"},
		{"empty health url", oldConfig(`, "health_url": ""`), `[]`,
			"HEARTBEAT_INTERVAL=30\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0\nOXIDNS_HEALTH_DNS_ADDRESS=\nOXIDNS_HEALTH_DNS_NAME=\nTZ=Asia/Shanghai\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, agentConfig), test.config, 0o600)
			out, err := runMigration(t, dir, test.env)
			if err != nil || out != test.want {
				t.Fatalf("got %q %v\nwant %q", out, err, test.want)
			}
		})
	}
}

func TestMigrationRefusals(t *testing.T) {
	tests := []struct {
		name, config, env, file, want string
	}{
		{"env not json", oldConfig(""), `nope`, "", msgMigrationEnvironment},
		{"env not list", oldConfig(""), `{"A": "b"}`, "", msgMigrationEnvironment},
		{"env item not string", oldConfig(""), `[1]`, "", msgEnvFormat},
		{"env item without equals", oldConfig(""), `["PATH"]`, "", msgEnvFormat},
		{"env invalid name", oldConfig(""), `["1A=b"]`, "", msgEnvName},
		{"env empty name", oldConfig(""), `["=b"]`, "", msgEnvName},
		{"env multiline value", oldConfig(""), `["A=b\nc"]`, "", msgEnvName},
		{"env carriage return", oldConfig(""), `["A=b\rc"]`, "", msgEnvName},
		{"env NUL", oldConfig(""), `["A=b\u0000c"]`, "", msgEnvName},
		{"config missing", "", `[]`, "", msgOldConfig},
		{"config corrupt", `{`, `[]`, "", msgOldConfig},
		{"config not object", `[]`, `[]`, "", msgOldConfig},
		{"other agent name", strings.Replace(oldConfig(""), oldName, "other", 1), `[]`, "", msgAgentMismatch},
		{"other server", strings.Replace(oldConfig(""), oldServer, "http://other.example", 1), `[]`, "", msgAgentMismatch},
		{"other port", strings.Replace(oldConfig(""), "8082", "8083", 1), `[]`, "", msgAgentMismatch},
		{"float port", strings.Replace(oldConfig(""), "8082", "8082.0", 1), `[]`, "", msgAgentMismatch},
		{"port missing", strings.Replace(oldConfig(""), `"agent_port": 8082, `, "", 1), `[]`, "", msgAgentMismatch},
		{"other service", strings.Replace(oldConfig(""), `"service_type": "oxidns"`, `"service_type": "mosdns"`, 1), `[]`, "", msgAgentMismatch},
		{"other config path", strings.Replace(oldConfig(""), "/etc/oxidns/config.yaml", "/data/config.yaml", 1), `[]`, "", msgAgentMismatch},
		{"ha env", oldConfig(""), `["CONFIGFLOW_DNS_HA=1"]`, "", msgHAPresent},
		{"ha settings file", oldConfig(""), `[]`, "oxidns-ha.json", msgHAPresent},
		{"ha handoff file", oldConfig(""), `[]`, "oxidns-ha-handoff.json", msgHAPresent},
		{"mihomo enabled", oldConfig(""), `["ENABLE_MIHOMO=TRUE"]`, "", msgOxiDNSOnly},
		{"mosdns enabled", oldConfig(""), `["ENABLE_MOSDNS=true"]`, "", msgOxiDNSOnly},
		{"other service type", oldConfig(""), `["SERVICE_TYPE=mihomo"]`, "", msgNotOxiDNS},
		{"health url", oldConfig(`, "health_url": "http://127.0.0.1/health"`), `[]`, "", msgHealthURL},
		{"null setting", oldConfig(`, "heartbeat_interval": null`), `[]`, "", msgOldSetting},
		{"list setting", oldConfig(`, "agent_host": ["a"]`), `[]`, "", msgOldSetting},
		{"multiline setting", oldConfig(`, "health_dns_name": "a\nb"`), `[]`, "", msgMultilineValues},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.config != "" {
				writeFile(t, filepath.Join(dir, agentConfig), test.config, 0o600)
			}
			if test.file != "" {
				writeFile(t, filepath.Join(dir, test.file), `{}`, 0o600)
			}
			out, err := runMigration(t, dir, test.env)
			if message(err) != test.want || out != "" {
				t.Fatalf("got %q (%q) want %q", message(err), out, test.want)
			}
		})
	}
}

func TestInspect(t *testing.T) {
	tests := []struct{ name, config, want string }{
		{"stored", oldConfig(`, "token": "secret-value"`), `{"agent_name": "dns-old", "agent_port": 8082, "service_type": "oxidns", "config_path": "/etc/oxidns/config.yaml"}` + "\n"},
		{"missing fields and unicode", `{"agent_name": "名称<&>"}`, `{"agent_name": "名称<&>", "agent_port": null, "service_type": null, "config_path": null}` + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, agentConfig), test.config, 0o600)
			var out bytes.Buffer
			if err := Inspect(dir, &out); err != nil || out.String() != test.want {
				t.Fatalf("%q %v", out.String(), err)
			}
		})
	}
	if err := Inspect(t.TempDir(), &bytes.Buffer{}); message(err) != msgOldConfig {
		t.Fatal(err)
	}
}

func TestRunUsageAndDispatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, agentConfig), oldConfig(""), 0o600)
	tests := []struct {
		name  string
		args  []string
		stdin string
		code  int
		out   string
		err   string
	}{
		{"unknown", []string{"ha-other"}, "", 2, "", msgUsage + "\n"},
		{"check needs url", []string{"ha-enroll-check"}, "", 2, "", msgUsage + "\n"},
		{"migration needs three operands", []string{"ha-migration-check", "-agent-dir", dir, oldName}, "", 2, "", msgUsage + "\n"},
		{"inspect extra operand", []string{"ha-inspect", "x"}, "", 2, "", msgUsage + "\n"},
		{"inspect", []string{"ha-inspect", "-agent-dir", dir}, "", 0, `{"agent_name": "dns-old", "agent_port": 8082, "service_type": "oxidns", "config_path": "/etc/oxidns/config.yaml"}` + "\n", ""},
		{"migration refusal", []string{"ha-migration-check", "-agent-dir", dir, "other", oldServer, "8082"}, "[]", 1, "", msgAgentMismatch + "\n"},
		{"migration", []string{"ha-migration-check", "-agent-dir", dir, oldName, oldServer, "8082"}, `["TZ=UTC"]`, 0,
			"TZ=UTC\nHEARTBEAT_INTERVAL=30\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0\nOXIDNS_HEALTH_DNS_ADDRESS=\nOXIDNS_HEALTH_DNS_NAME=\n", ""},
		{"proof refusal", []string{"ha-enroll-check", "-agent-dir", dir, "http://panel.example/x"}, "", 1, "", msgProofMissing + "\n"},
		{"enroll refusal", []string{"ha-enroll", "-agent-dir", dir}, "{}", 1, "", msgSettings + "\n"},
		{"bootstrap refusal", []string{"ha-bootstrap", "-agent-dir", dir, "-settings", filepath.Join(dir, "missing"), "-setup", filepath.Join(dir, "missing")}, "", 1, "", msgFileUnreadable + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			name, args := test.args[0], test.args[1:]
			code := Run(context.Background(), name, args, strings.NewReader(test.stdin), &out, &errOut)
			if code != test.code || out.String() != test.out || errOut.String() != test.err {
				t.Fatalf("code %d out %q err %q", code, out.String(), errOut.String())
			}
		})
	}
}

// TestBootstrapHandsOverToEntrypoint runs the whole command: a verified
// identity execs the entrypoint with the image's default service command.
func TestBootstrapHandsOverToEntrypoint(t *testing.T) {
	if os.Getenv("DOCKERHA_HANDOVER_CHILD") == "1" {
		paths := BootstrapPaths{Settings: os.Getenv("SETTINGS"), Setup: os.Getenv("SETUP")}
		os.Exit(Run(context.Background(), "ha-bootstrap", []string{"-agent-dir", os.Getenv("AGENT"), "-settings", paths.Settings, "-setup", paths.Setup, "-entrypoint", os.Getenv("ENTRY")}, os.Stdin, os.Stdout, os.Stderr))
	}
	o := newOrigin(t)
	paths := bundleFixture(t, o.url())
	dir := t.TempDir()
	entry := filepath.Join(dir, "entrypoint.sh")
	writeFile(t, entry, "#!/bin/sh\nprintf 'ENTRYPOINT %s\\n' \"$*\"\n", 0o755)
	cmd := testCommand(t, "-test.run=^TestBootstrapHandsOverToEntrypoint$")
	cmd.Env = append(os.Environ(), "DOCKERHA_HANDOVER_CHILD=1", "SETTINGS="+paths.Settings, "SETUP="+paths.Setup, "AGENT="+paths.AgentDir, "ENTRY="+entry)
	output, err := cmd.CombinedOutput()
	want := msgIdentitySaved + "\nENTRYPOINT /usr/bin/supervisord -c /etc/supervisor/supervisord.conf\n"
	if err != nil || string(output) != want {
		t.Fatalf("%q %v", output, err)
	}
}

func testCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	return exec.Command(os.Args[0], args...)
}
