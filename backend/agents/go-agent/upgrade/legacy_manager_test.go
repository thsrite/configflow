package upgrade

import "testing"

func TestDockerRecognizesBothContainerManagers(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   bool
	}{
		{"new image", map[string]any{"service_manager": "s6"}, true},
		{"image before 1.9.3-go", map[string]any{"service_manager": "supervisor"}, true},
		{"old restart command", map[string]any{"restart_command": "supervisorctl restart oxidns"}, true},
		{"docker deployment", map[string]any{"deployment_method": "docker"}, true},
		{"systemd", map[string]any{"service_manager": "systemd"}, false},
	}
	for _, tt := range tests {
		if got := (&Config{Values: tt.values}).Docker(); got != tt.want {
			t.Errorf("%s: %t", tt.name, got)
		}
	}
}
