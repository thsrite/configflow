package dockerha

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// environmentLimit bounds the `docker inspect` environment list on stdin.
const environmentLimit = 1 << 20

var envName = regexp.MustCompile(`\A[A-Za-z_][A-Za-z0-9_]*\z`)

// MigrationCheck is the expected identity of the container being migrated.
type MigrationCheck struct {
	AgentDir   string // the old container's Agent directory (read-only mount)
	AgentName  string
	ServerURL  string
	AgentPort  string
	OxiDNSPath string // expected config_path
}

// orderedEnv keeps first-insertion order; a later value replaces an earlier
// one in place, like the container's own environment.
type orderedEnv struct {
	keys   []string
	values map[string]string
}

func (e *orderedEnv) set(key, value string) {
	if _, ok := e.values[key]; !ok {
		e.keys = append(e.keys, key)
	}
	e.values[key] = value
}

func (e *orderedEnv) get(key, fallback string) string {
	if value, ok := e.values[key]; ok {
		return value
	}
	return fallback
}

func hasLineBreakOrNUL(value string) bool {
	return strings.ContainsAny(value, "\n\r\x00")
}

// CheckMigration verifies that the old single-OxiDNS container may be
// migrated to HA and prints its runtime environment file (KEY=VALUE lines).
// env is the container's `docker inspect --format '{{json .Config.Env}}'`.
// The old container's environment is taken from that list, so the check runs
// in a temporary container of the new image with the old volumes mounted.
func CheckMigration(check MigrationCheck, env io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(env, environmentLimit+1))
	if err != nil || len(raw) > environmentLimit {
		return fail(msgMigrationEnvironment)
	}
	// One JSON array and nothing else: null (a container without any
	// environment) or trailing data is refused like any other non-list.
	var items []any
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	if decoder.Decode(&items) != nil || items == nil {
		return fail(msgMigrationEnvironment)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fail(msgMigrationEnvironment)
	}
	original := orderedEnv{values: map[string]string{}}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || !strings.Contains(text, "=") {
			return fail(msgEnvFormat)
		}
		key, value, _ := strings.Cut(text, "=")
		if !envName.MatchString(key) || hasLineBreakOrNUL(value) {
			return fail(msgEnvName)
		}
		original.set(key, value)
	}
	configRaw, err := os.ReadFile(filepath.Join(check.AgentDir, agentConfig))
	if err != nil {
		return fail(msgOldConfig)
	}
	config, err := decodeObject(configRaw)
	if err != nil {
		return fail(msgOldConfig)
	}
	port, portOK := jsonText(config["agent_port"])
	if !sameValue(config["agent_name"], check.AgentName) || !sameValue(config["server_url"], check.ServerURL) ||
		!portOK || port != check.AgentPort || !sameValue(config["service_type"], "oxidns") ||
		!sameValue(config["config_path"], check.OxiDNSPath) {
		return fail(msgAgentMismatch)
	}
	if original.get("CONFIGFLOW_DNS_HA", "") == "1" ||
		exists(filepath.Join(check.AgentDir, "oxidns-ha.json")) || exists(filepath.Join(check.AgentDir, "oxidns-ha-handoff.json")) {
		return fail(msgHAPresent)
	}
	for _, key := range []string{"ENABLE_MIHOMO", "ENABLE_MOSDNS"} {
		if strings.ToLower(original.get(key, "false")) == "true" {
			return fail(msgOxiDNSOnly)
		}
	}
	if service := original.get("SERVICE_TYPE", ""); service != "" && service != "oxidns" {
		return fail(msgNotOxiDNS)
	}
	if truthy(config["health_url"]) {
		return fail(msgHealthURL)
	}
	settings := []struct {
		env, key string
		fallback any
	}{
		{"HEARTBEAT_INTERVAL", "heartbeat_interval", json.Number("30")},
		{"DEPLOYMENT_HEALTH_TIMEOUT", "deployment_health_timeout", json.Number("30")},
		{"AGENT_HOST", "agent_host", "0.0.0.0"},
		{"OXIDNS_HEALTH_DNS_ADDRESS", "health_dns_address", ""},
		{"OXIDNS_HEALTH_DNS_NAME", "health_dns_name", ""},
	}
	values := make([]string, 0, len(settings))
	for _, s := range settings {
		value, present := config[s.key]
		if !present {
			value = s.fallback
		}
		text, ok := jsonText(value)
		if !ok {
			return fail(msgOldSetting)
		}
		values = append(values, text)
	}
	for i, s := range settings {
		original.set(s.env, values[i])
	}
	original.set("TZ", original.get("TZ", "Asia/Shanghai"))
	var lines strings.Builder
	for _, key := range original.keys {
		value := original.values[key]
		if hasLineBreakOrNUL(value) {
			return fail(msgMultilineValues)
		}
		lines.WriteString(key + "=" + value + "\n")
	}
	_, err = io.WriteString(out, lines.String())
	return err
}

// truthy is the non-empty test of a stored JSON value.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}

// Inspect prints the old container's Agent name, port, service type and
// config path (never its credentials) as one JSON line.
func Inspect(agentDir string, out io.Writer) error {
	raw, err := os.ReadFile(filepath.Join(agentDir, agentConfig))
	if err != nil {
		return fail(msgOldConfig)
	}
	config, err := decodeObject(raw)
	if err != nil {
		return fail(msgOldConfig)
	}
	var line bytes.Buffer
	line.WriteByte('{')
	for i, key := range []string{"agent_name", "agent_port", "service_type", "config_path"} {
		if i > 0 {
			line.WriteString(", ")
		}
		var value bytes.Buffer
		encoder := json.NewEncoder(&value)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(config[key]); err != nil {
			return fmt.Errorf("encode %s: %w", key, err)
		}
		fmt.Fprintf(&line, "%q: %s", key, bytes.TrimSuffix(value.Bytes(), []byte("\n")))
	}
	line.WriteString("}\n")
	_, err = out.Write(line.Bytes())
	return err
}
