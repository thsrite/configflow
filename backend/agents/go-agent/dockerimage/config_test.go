package dockerimage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"agent/svc"
)

func envOf(values map[string]string) Environment {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

// keysInOrder returns the top-level keys of a JSON object in file order.
func keysInOrder(t *testing.T, raw []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, token.(string))
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

var baseKeys = []string{"server_url", "agent_name", "agent_host", "agent_port", "agent_ip", "service_type",
	"deployment_method", "service_name", "config_path", "restart_command", "service_manager", "service_unit",
	"service_binary", "health_url", "health_dns_address", "health_dns_name", "deployment_health_timeout", "heartbeat_interval"}

func TestWriteAgentConfig(t *testing.T) {
	fullEnv := map[string]string{
		"SERVER_URL": "http://192.0.2.10:5010", "AGENT_IP": "192.0.2.21", "HEARTBEAT_INTERVAL": "30",
		"OXIDNS_HEALTH_DNS_ADDRESS": "127.0.0.1:53", "OXIDNS_HEALTH_DNS_NAME": "example.com",
		"AGENT_OXIDNS_REGISTRATION_TOKEN": "one-use", "AGENT_REGISTRATION_KEY": "key-1",
		"AGENT_MIHOMO_REGISTRATION_TOKEN": "other-core",
	}
	tests := []struct {
		name     string
		existing string // "" = no file
		env      map[string]string
		want     map[string]any // checked members
		keys     []string       // keys after baseKeys
	}{
		{"first start", "", fullEnv, map[string]any{
			"server_url": "http://192.0.2.10:5010", "agent_name": "dns-a", "agent_host": "0.0.0.0", "agent_port": 8082.0,
			"agent_ip": "192.0.2.21", "service_type": "oxidns", "deployment_method": "docker", "service_name": "oxidns",
			"config_path": "/etc/oxidns/config.yaml", "restart_command": "/usr/local/bin/configflow-agent svc restart oxidns",
			"service_manager": "s6", "service_unit": "oxidns", "service_binary": "/usr/local/bin/oxidns",
			"health_url": "", "health_dns_address": "127.0.0.1:53", "health_dns_name": "example.com",
			"deployment_health_timeout": 30.0, "heartbeat_interval": 30.0,
			"registration_token": "one-use", "registration_key": "key-1"},
			[]string{"registration_token", "registration_key"}},
		{"enrolled identity survives", `{"agent_id":"a1","token":"t1","agent_name":"old","registration_token":"used"}`, fullEnv,
			map[string]any{"agent_id": "a1", "token": "t1", "agent_name": "dns-a"}, []string{"agent_id", "token"}},
		{"identity without token keeps registering", `{"agent_id":"a1","token":""}`, fullEnv,
			map[string]any{"agent_id": "a1", "registration_token": "one-use"}, []string{"registration_token", "registration_key", "agent_id"}},
		{"defaults", "", map[string]string{},
			map[string]any{"server_url": "", "agent_host": "0.0.0.0", "deployment_health_timeout": 30.0, "heartbeat_interval": 60.0,
				"registration_token": "", "registration_key": ""}, []string{"registration_token", "registration_key"}},
		{"empty values are kept, not defaulted", "", map[string]string{"AGENT_HOST": "", "DEPLOYMENT_HEALTH_TIMEOUT": " 45 "},
			map[string]any{"agent_host": "", "deployment_health_timeout": 45.0}, []string{"registration_token", "registration_key"}},
		{"quotes and newlines stay data", "", map[string]string{"SERVER_URL": "http://a\"b\nc"},
			map[string]any{"server_url": "http://a\"b\nc"}, []string{"registration_token", "registration_key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config-oxidns.json")
			if tt.existing != "" {
				os.WriteFile(path, []byte(tt.existing), 0o644)
			}
			err := WriteAgentConfig(AgentConfig{Path: path, Service: "oxidns", Name: "dns-a", Port: "8082", ConfigPath: "/etc/oxidns/config.yaml"}, envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(path)
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, raw)
			}
			for key, want := range tt.want {
				if !reflect.DeepEqual(got[key], want) {
					t.Errorf("%s = %#v, want %#v", key, got[key], want)
				}
			}
			if keys := keysInOrder(t, raw); !reflect.DeepEqual(keys, append(append([]string{}, baseKeys...), tt.keys...)) {
				t.Errorf("keys %v", keys)
			}
			if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
				t.Errorf("mode %o", info.Mode().Perm())
			}
			if _, err := os.Stat(path + ".tmp"); err == nil {
				t.Error("temporary file left behind")
			}
		})
	}
}

func TestWriteAgentConfigRefusals(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		port     string
		env      map[string]string
	}{
		{"damaged existing file", "{not json", "8082", nil},
		{"port is not a number", "", "80a", nil},
		{"empty heartbeat", "", "8082", map[string]string{"HEARTBEAT_INTERVAL": ""}},
		{"bad health timeout", "", "8082", map[string]string{"DEPLOYMENT_HEALTH_TIMEOUT": "1.5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config-oxidns.json")
			if tt.existing != "" {
				os.WriteFile(path, []byte(tt.existing), 0o600)
			}
			if err := WriteAgentConfig(AgentConfig{Path: path, Service: "oxidns", Name: "a", Port: tt.port}, envOf(tt.env)); err == nil {
				t.Fatal("accepted")
			}
			if raw, _ := os.ReadFile(path); string(raw) != tt.existing {
				t.Errorf("file changed to %q", raw)
			}
		})
	}
}

func TestRunCommands(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		bundle []string
		argv   map[string]string
	}{
		{"single mihomo with config", []string{"services", "mihomo=/etc/mihomo/config.yaml"}, 0,
			[]string{"agent-mihomo", "mihomo", "mihomo-log"},
			map[string]string{"mihomo.argv": `["/usr/local/bin/mihomo","-d","/etc/mihomo","-f","/etc/mihomo/config.yaml"]`}},
		{"aio defaults", []string{"services", "mihomo", "mosdns"}, 0,
			[]string{"agent-mihomo", "agent-mosdns", "mihomo", "mihomo-log", "mosdns", "mosdns-log"}, map[string]string{}},
		{"macvlan HA", []string{"services", "-macvlan-ha", "oxidns=/etc/oxidns/config.yaml"}, 0,
			[]string{"agent-oxidns", "ha-release", "oxidns", "oxidns-log"},
			map[string]string{"oxidns.argv": `["/usr/local/bin/oxidns","start","-c","/etc/oxidns/config.yaml","-d","/etc/oxidns"]`}},
		{"nothing enabled", []string{"services"}, 0, nil, map[string]string{}},
		{"companion", []string{"companion"}, 0, []string{"ha-companion-release", svc.CompanionServ}, map[string]string{}},
		{"unknown core", []string{"services", "sshd"}, 2, nil, nil},
		{"duplicate core", []string{"services", "mihomo", "mihomo"}, 2, nil, nil},
		{"relative config", []string{"services", "mihomo=config.yaml"}, 2, nil, nil},
		{"bad agent service", []string{"agent", "sshd", "a", "1", "/x"}, 2, nil, nil},
		{"no subcommand", nil, 2, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			layout := svc.Layout{SourceDir: filepath.Join(root, "src"), BundleDir: filepath.Join(root, "bundle"),
				StateDir: filepath.Join(root, "state"), LogRoot: filepath.Join(root, "log")}
			var stderr strings.Builder
			if code := run(tt.args, envOf(nil), layout, root, &stderr); code != tt.code {
				t.Fatalf("code %d, want %d: %s", code, tt.code, stderr.String())
			}
			if tt.code != 0 {
				return
			}
			entries, _ := os.ReadDir(layout.BundleDir)
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, tt.bundle) {
				t.Errorf("bundle %v, want %v", names, tt.bundle)
			}
			state, _ := os.ReadDir(layout.StateDir)
			got := map[string]string{}
			for _, entry := range state {
				raw, _ := os.ReadFile(filepath.Join(layout.StateDir, entry.Name()))
				got[entry.Name()] = string(raw)
			}
			if !reflect.DeepEqual(got, tt.argv) {
				t.Errorf("state %v, want %v", got, tt.argv)
			}
		})
	}
}

func TestRunAgentWritesIntoAgentDir(t *testing.T) {
	root := t.TempDir()
	var stderr strings.Builder
	if code := run([]string{"agent", "mosdns", "mosdns-agent", "8081", "/etc/mosdns/config.yaml"}, envOf(nil), svc.DefaultLayout, root, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(root, "config-mosdns.json"))
	if err != nil || !strings.Contains(string(raw), `"agent_port": 8081`) {
		t.Fatalf("%s %v", raw, err)
	}
}
