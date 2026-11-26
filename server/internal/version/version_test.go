package version

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	body, _ := json.Marshal(Info())
	if string(body) != `{"version":"dev","agent_image":"thsrite/config-flow-agent:3"}` {
		t.Errorf("got %s", body)
	}
	AgentImage = "thsrite/config-flow-agent:3.0.0"
	defer func() { AgentImage = "" }()
	if Info().AgentImage != "thsrite/config-flow-agent:3.0.0" {
		t.Error("Info does not report the paired Agent image")
	}
	Version = "1.2.3"
	defer func() { Version = "dev" }()
	if Info().Version != "1.2.3" {
		t.Error("Info does not follow Version")
	}
}

func TestDockerAgentImage(t *testing.T) {
	defer func(saved string) { AgentImage = saved }(AgentImage)
	tests := []struct {
		name, build, want string
	}{
		{"unset build", "", DefaultAgentImage},
		{"release tag", "thsrite/config-flow-agent:2.0.0-rc.1", "thsrite/config-flow-agent:2.0.0-rc.1"},
		{"fork namespace", "example/config-flow-agent:2.1.0", "example/config-flow-agent:2.1.0"},
		{"registry and digest", "registry.example.com/team/agent:2.0.0@sha256:" + strings.Repeat("a", 64),
			"registry.example.com/team/agent:2.0.0@sha256:" + strings.Repeat("a", 64)},
		{"registry port", "registry.example.com:5000/agent:2.0.0", "registry.example.com:5000/agent:2.0.0"},
		{"shell syntax", "thsrite/config-flow-agent:2.0.0;id", DefaultAgentImage},
		{"space", "thsrite/config-flow-agent 2.0.0", DefaultAgentImage},
		{"upper-case repository", "Thsrite/config-flow-agent:2.0.0", DefaultAgentImage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			AgentImage = tt.build
			if got := DockerAgentImage(); got != tt.want {
				t.Errorf("DockerAgentImage() = %q, want %q", got, tt.want)
			}
		})
	}
}
