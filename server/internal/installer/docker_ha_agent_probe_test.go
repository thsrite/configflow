package installer

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestHAAgentReadyComparesVersions runs the generated probe condition against
// a docker that prints a given configflow-agent -version answer.
func TestHAAgentReadyComparesVersions(t *testing.T) {
	tests := []struct {
		name, output string
		code         int
		want         bool
	}{
		{"minimum", "1.9.3-go", 0, true},
		{"newer patch", "1.9.10-go", 0, true},
		{"newer minor", "1.10.0-go", 0, true},
		{"newer major", "2.0.0-go", 0, true},
		{"previous release (Supervisor image)", "1.9.2-go", 0, false},
		{"older patch", "1.9.1-go", 0, false},
		{"old minor", "1.8.61-go", 0, false},
		{"unknown flag (Agent before -version)", "", 2, false},
		{"no configflow-agent in image", "", 127, false},
		{"garbage", "configflow", 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tmp := t.TempDir()
			fake := "#!/bin/sh\nprintf '%s' " + q(test.output) + "\n[ -z " + q(test.output) + " ] || printf '\\n'\nexit " + strconv.Itoa(test.code) + "\n"
			writeFile(t, filepath.Join(tmp, "docker"), fake, 0o755)
			script := "if " + haAgentReady("repo/agent:1") + "; then echo ready; else echo old; fi\n"
			result := runCommand(t, script, tmp, "PATH="+tmp+":/usr/bin:/bin")
			if got := strings.TrimSpace(result.stdout) == "ready"; result.code != 0 || got != test.want {
				t.Fatalf("code %d stdout %q stderr %q, want ready=%v", result.code, result.stdout, result.stderr, test.want)
			}
		})
	}
}

// TestOldAgentImageStopsNewInstallBeforeChanges: a stale local image without
// the ha-* subcommands ends the script before the port check, enrollment or
// any container is started.
func TestOldAgentImageStopsNewInstallBeforeChanges(t *testing.T) {
	for _, mode := range []string{"host", "macvlan"} {
		t.Run(mode, func(t *testing.T) {
			tmp := t.TempDir()
			_, host, guide := newHost(t, tmp, mode, "old_agent", true)
			result := runCommand(t, mapStr(guide, "install_command"), tmp, host.env...)
			if result.code == 0 || !strings.Contains(result.stderr, haAgentTooOld) {
				t.Fatalf("code %d stderr %q", result.code, result.stderr)
			}
			for _, call := range eventCalls(t, host.events) {
				if call[0] == "run" && !slices.Contains(call, "-version") || call[0] == "start" {
					t.Fatalf("unexpected %v", call)
				}
			}
		})
	}
}

// TestInspectionSkipsOldAgentImage: the read-only inspection prints the
// refusal instead of starting an old Agent against the old volumes.
func TestInspectionSkipsOldAgentImage(t *testing.T) {
	for _, old := range []bool{false, true} {
		tmp := t.TempDir()
		data := guideParams(t, tmp, "migrate", "bind")
		host := fakeHost(t, tmp, data, "old_agent", old)
		command, err := inspectionCommand(mapStr(data, "container_name"), "repo/agent:1")
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(command, "if printf")
		if start < 0 {
			t.Fatalf("no probe in %q", command)
		}
		result := runCommand(t, command[start:], tmp, host.env...)
		inspected := false
		for _, call := range eventCalls(t, host.events) {
			inspected = inspected || slices.Contains(call, "ha-inspect")
		}
		if result.code != 0 || inspected == old || strings.Contains(result.stderr, haAgentTooOld) != old {
			t.Fatalf("old=%v: code %d inspected %v stderr %q", old, result.code, inspected, result.stderr)
		}
	}
}
