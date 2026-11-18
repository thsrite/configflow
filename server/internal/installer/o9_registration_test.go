package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the real Docker entrypoint's JSON writer, including its restart
// branch; comparing only generated environment text cannot prove this handoff.
func TestO9DockerEntrypointCarriesBootstrapProofUntilDurableToken(t *testing.T) {
	source, err := os.ReadFile("../../../docker/docker-agent-entrypoint-service.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, function, found := strings.Cut(string(source), "generate_agent_config() {")
	if !found {
		t.Fatal("entrypoint config writer missing")
	}
	function, _, found = strings.Cut(function, "\nconfigure_supervisor_core() {")
	if !found {
		t.Fatal("entrypoint writer boundary missing")
	}
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentDir, "config-mihomo.json")
	script := writeFile(t, filepath.Join(dir, "write-config.sh"), "set -eu\nAGENT_DIR="+shlexQuote(agentDir)+"\ngenerate_agent_config() {"+function+"\ngenerate_agent_config mihomo docker-device-mihomo 8080 /etc/mihomo/config.yaml\n", 0700)
	env := []string{"SERVER_URL=https://panel.example.com", "AGENT_MIHOMO_REGISTRATION_TOKEN=synthetic-bootstrap-proof", "AGENT_REGISTRATION_KEY=synthetic-key"}
	result := runShell(t, "sh", script, runOptions{env: env})
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	data, _ := os.ReadFile(path)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config["registration_token"] != "synthetic-bootstrap-proof" || config["registration_key"] != "synthetic-key" {
		t.Fatal("Docker proof not written")
	}
	if strings.Contains(result.stdout+result.stderr, "synthetic-bootstrap-proof") || strings.Contains(result.stdout+result.stderr, "synthetic-key") {
		t.Fatal("entrypoint leaked proof")
	}
	config["agent_id"], config["token"] = "durable-device", "durable-token"
	delete(config, "registration_token")
	delete(config, "registration_key")
	data, _ = json.Marshal(config)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result = runShell(t, "sh", script, runOptions{env: env})
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	data, _ = os.ReadFile(path)
	config = map[string]any{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config["token"] != "durable-token" || config["agent_id"] != "durable-device" {
		t.Fatal("Docker restart lost durable identity")
	}
	if _, present := config["registration_token"]; present {
		t.Fatal("Docker restart reinstated used proof")
	}
}
