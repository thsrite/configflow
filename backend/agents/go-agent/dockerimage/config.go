// Package dockerimage prepares an Agent container at every start, called
// from the image entrypoint: it writes each Agent's config-<core>.json from
// the container environment (keeping an enrolled identity) and the s6
// definitions of the programs this container runs. It does not enroll,
// validate HA installations (package dockerha) or control running programs
// (package svc).
package dockerimage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"agent/svc"
)

// Environment reads container variables; Lookup is os.LookupEnv in the image.
type Environment func(key string) (string, bool)

func (e Environment) get(key, fallback string) string {
	if value, ok := e(key); ok {
		return value
	}
	return fallback
}

// AgentConfig is one docker-config agent invocation.
type AgentConfig struct {
	Path       string // config-<service>.json
	Service    string // mihomo, mosdns or oxidns
	Name       string
	Port       string
	ConfigPath string // core configuration file
}

// member is one key of the ordered configuration object.
type member struct {
	key   string
	value any
}

// WriteAgentConfig regenerates the Agent configuration from the environment.
// An enrolled identity (agent_id, token) survives; registration credentials
// are only written while the Agent has no token yet. The file is replaced
// atomically with mode 0600.
func WriteAgentConfig(c AgentConfig, env Environment) error {
	existing := map[string]any{}
	raw, err := os.ReadFile(c.Path)
	switch {
	case err == nil:
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&existing); err != nil {
			return fmt.Errorf("parse existing %s: %w", filepath.Base(c.Path), err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read existing %s: %w", filepath.Base(c.Path), err)
	}
	port, err := integer(c.Port)
	if err != nil {
		return fmt.Errorf("agent port: %w", err)
	}
	healthTimeout, err := integer(env.get("DEPLOYMENT_HEALTH_TIMEOUT", "30"))
	if err != nil {
		return fmt.Errorf("DEPLOYMENT_HEALTH_TIMEOUT: %w", err)
	}
	heartbeat, err := integer(env.get("HEARTBEAT_INTERVAL", "60"))
	if err != nil {
		return fmt.Errorf("HEARTBEAT_INTERVAL: %w", err)
	}
	prefix := strings.ToUpper(c.Service)
	members := []member{
		{"server_url", env.get("SERVER_URL", "")},
		{"agent_name", c.Name},
		{"agent_host", env.get("AGENT_HOST", "0.0.0.0")},
		{"agent_port", port},
		{"agent_ip", env.get("AGENT_IP", "")},
		{"service_type", c.Service},
		{"deployment_method", "docker"},
		{"service_name", c.Service},
		{"config_path", c.ConfigPath},
		{"restart_command", svc.Binary + " svc restart " + c.Service},
		{"service_manager", ServiceManager},
		{"service_unit", c.Service},
		{"service_binary", "/usr/local/bin/" + c.Service},
		{"health_url", env.get(prefix+"_HEALTH_URL", "")},
		{"health_dns_address", env.get(prefix+"_HEALTH_DNS_ADDRESS", "")},
		{"health_dns_name", env.get(prefix+"_HEALTH_DNS_NAME", "")},
		{"deployment_health_timeout", healthTimeout},
		{"heartbeat_interval", heartbeat},
	}
	if !truthy(existing["token"]) {
		members = append(members,
			member{"registration_token", env.get("AGENT_"+prefix+"_REGISTRATION_TOKEN", "")},
			member{"registration_key", env.get("AGENT_REGISTRATION_KEY", "")})
	}
	for _, key := range []string{"agent_id", "token"} {
		if truthy(existing[key]) {
			members = append(members, member{key, existing[key]})
		}
	}
	data, err := encode(members)
	if err != nil {
		return err
	}
	return replaceFile(c.Path, data)
}

// ServiceManager is the service_manager value of every Docker Agent.
const ServiceManager = svc.ContainerManager

// integer accepts an optionally signed decimal surrounded by whitespace.
func integer(text string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", text)
	}
	return value, nil
}

// truthy is JSON truthiness: present and not null, false, 0, "" or empty.
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

func encode(members []member) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("{")
	for i, m := range members {
		key, err := json.Marshal(m.key)
		if err != nil {
			return nil, err
		}
		value, err := json.MarshalIndent(m.value, "  ", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", m.key, err)
		}
		if i > 0 {
			out.WriteString(",")
		}
		out.WriteString("\n  ")
		out.Write(key)
		out.WriteString(": ")
		out.Write(value)
	}
	out.WriteString("\n}")
	return out.Bytes(), nil
}

// replaceFile writes path.tmp (0600), syncs it, renames it over path and
// syncs the directory.
func replaceFile(path string, data []byte) error {
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(temp), err)
	}
	err = file.Chmod(0o600)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(temp), err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
