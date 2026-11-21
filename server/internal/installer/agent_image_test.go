package installer

import (
	"strings"
	"testing"

	"configflow/internal/version"
)

// pairAgentImage sets the build's paired Docker Agent image for one test.
func pairAgentImage(t *testing.T, image string) {
	t.Helper()
	saved := version.AgentImage
	version.AgentImage = image
	t.Cleanup(func() { version.AgentImage = saved })
}

// New Docker installations run the Agent image built with this panel, so a
// release panel never pairs its registration protocol with an older latest.
func TestDockerInstallersUsePairedAgentImage(t *testing.T) {
	const paired = "thsrite/config-flow-agent:2.0.0-rc.1"
	run := func(o DockerInstallOptions) string {
		t.Helper()
		return ok(RenderDockerRun(o))
	}
	compose := func(o DockerInstallOptions) string {
		t.Helper()
		return ok(RenderDockerCompose(o))
	}
	tests := []struct {
		name   string
		render func(DockerInstallOptions) string
		opts   DockerInstallOptions
		count  int
	}{
		{"docker run", run, DefaultDockerOptions("https://config.example"), 1},
		{"compose", compose, DefaultDockerOptions("https://config.example"), 1},
		{"HA run host", run, haParams("host"), 2},
		{"HA run macvlan", run, haParams("macvlan"), 1},
		{"HA compose host", compose, haParams("host"), 2},
		{"HA compose macvlan", compose, haParams("macvlan"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pairAgentImage(t, "")
			unset := tt.render(tt.opts)
			if got := strings.Count(unset, version.DefaultAgentImage); got != tt.count {
				t.Fatalf("unpaired build: %d × %s, want %d\n%s", got, version.DefaultAgentImage, tt.count, unset)
			}
			pairAgentImage(t, paired)
			out := tt.render(tt.opts)
			if strings.Contains(out, ":latest") || strings.Count(out, paired) != tt.count {
				t.Fatalf("paired build: want %d × %s and no latest\n%s", tt.count, paired, out)
			}
			if strings.ReplaceAll(out, paired, version.DefaultAgentImage) != unset {
				t.Error("pairing changed more than the image reference")
			}
		})
	}
}

func TestGuideDefaultsToPairedAgentImage(t *testing.T) {
	const paired = "thsrite/config-flow-agent:2.0.0-rc.1"
	pairAgentImage(t, paired)
	tests := []struct {
		name, request, want string
	}{
		{"omitted", nativeInstallRequest, paired},
		{"explicit", strings.Replace(nativeInstallRequest, `{`, `{"image":"registry.example/agent:ha",`, 1), "registry.example/agent:ha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := DecodeGuideRequest([]byte(tt.request))
			if err != nil {
				t.Fatal(err)
			}
			if request.Image != tt.want {
				t.Errorf("Image = %q, want %q", request.Image, tt.want)
			}
		})
	}
}
