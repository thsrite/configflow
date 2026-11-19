package installer

import (
	"bytes"
	"encoding/json"
)

// singleAgentConfig is the shell installer's wire configuration. Its declaration
// order keeps generated diffs stable; credentials are emitted only when present.
type singleAgentConfig struct {
	ServerURL               string      `json:"server_url"`
	AgentName               string      `json:"agent_name"`
	AgentHost               string      `json:"agent_host"`
	AgentPort               json.Number `json:"agent_port"`
	AgentIP                 string      `json:"agent_ip"`
	ServiceType             string      `json:"service_type"`
	DeploymentMethod        string      `json:"deployment_method"`
	ServiceName             string      `json:"service_name"`
	ConfigPath              string      `json:"config_path"`
	RestartCommand          string      `json:"restart_command"`
	ServiceManager          string      `json:"service_manager"`
	ServiceUnit             string      `json:"service_unit"`
	ServiceBinary           string      `json:"service_binary"`
	StopCommand             string      `json:"stop_command"`
	StartCommand            string      `json:"start_command"`
	StatusCommand           string      `json:"status_command"`
	HealthURL               string      `json:"health_url"`
	HealthDNSAddress        string      `json:"health_dns_address"`
	HealthDNSName           string      `json:"health_dns_name"`
	DeploymentHealthTimeout json.Number `json:"deployment_health_timeout"`
	HeartbeatInterval       int         `json:"heartbeat_interval"`
	AgentID                 string      `json:"agent_id"`
	Token                   string      `json:"token"`
	RegistrationToken       string      `json:"registration_token,omitempty"`
	RegistrationKey         string      `json:"registration_key,omitempty"`
}

type aioAgentConfig struct {
	ServerURL               string      `json:"server_url"`
	AgentName               string      `json:"agent_name"`
	AgentHost               string      `json:"agent_host"`
	AgentPort               json.Number `json:"agent_port"`
	AgentIP                 string      `json:"agent_ip"`
	ServiceType             string      `json:"service_type"`
	ServiceName             string      `json:"service_name"`
	DeploymentMethod        string      `json:"deployment_method"`
	NativeAIO               bool        `json:"native_aio"`
	AgentServiceUnit        string      `json:"agent_service_unit"`
	ConfigPath              string      `json:"config_path"`
	ServiceManager          string      `json:"service_manager"`
	ServiceUnit             string      `json:"service_unit"`
	ServiceBinary           string      `json:"service_binary"`
	RestartCommand          string      `json:"restart_command"`
	HeartbeatInterval       int         `json:"heartbeat_interval"`
	DeploymentHealthTimeout int         `json:"deployment_health_timeout"`
	AgentID                 string      `json:"agent_id"`
	Token                   string      `json:"token"`
	RegistrationToken       string      `json:"registration_token,omitempty"`
	RegistrationKey         string      `json:"registration_key,omitempty"`
	InstallationID          string      `json:"installation_id,omitempty"`
	InstallationToken       string      `json:"installation_token,omitempty"`
}

func renderConfigJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

// inlineSettingsJSON is the JSON literal embedded in the shipped bootstrap
// program. Separators remain readable; ordinary protocol JSON uses encoding/json.
func inlineSettingsJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	quoted, escaped := false, false
	for _, b := range raw {
		out.WriteByte(b)
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		if b == '"' {
			quoted = true
		} else if b == ':' || b == ',' {
			out.WriteByte(' ')
		}
	}
	return out.Bytes(), nil
}
