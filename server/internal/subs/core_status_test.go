package subs

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/logx"
	"configflow/internal/yamlx"
)

func TestUnrecognizedCoreVersionsFollowUnknownPath(t *testing.T) {
	mainYAML := `dns:
  enable: true
  listen: 0.0.0.0:1053
  use-hosts: true
  enhanced-mode: fake-ip
  proxy-server-nameserver:
    - https://dns.example/dns-query
hosts:
  node.example: target.example
`
	merged := yamlx.Map(yamlx.Str("node.example"), yamlx.Str("target.example"))

	on := true
	mihomo := "mihomo"

	tests := []struct {
		name           string
		version        string
		wantRecognized bool
		wantOld        bool
		wantApplied    bool
		wantUnknown    bool
		wantOutdated   int
	}{
		{name: "alpha-100ff73", version: "alpha-100ff73", wantRecognized: false, wantOld: false, wantApplied: true, wantUnknown: true, wantOutdated: 0},
		{name: "empty", version: "", wantRecognized: false, wantOld: false, wantApplied: true, wantUnknown: true, wantOutdated: 0},
		{name: "release-v1.19.32", version: "v1.19.32", wantRecognized: true, wantOld: false, wantApplied: true, wantUnknown: false, wantOutdated: 0},
		{name: "garbage-string", version: "!!!garbage-core###", wantRecognized: false, wantOld: false, wantApplied: true, wantUnknown: true, wantOutdated: 0},
		{name: "old-v1.19.19", version: "v1.19.19", wantRecognized: true, wantOld: true, wantApplied: false, wantUnknown: false, wantOutdated: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := CoreVersion(tc.version)
			if ok != tc.wantRecognized {
				t.Fatalf("CoreVersion(%q) ok=%v, want %v", tc.version, ok, tc.wantRecognized)
			}
			if old := IsCoreTooOld(tc.version); old != tc.wantOld {
				t.Fatalf("IsCoreTooOld(%q)=%v, want %v", tc.version, old, tc.wantOld)
			}
			mainNode, err := yamlx.Parse([]byte(mainYAML))
			if err != nil {
				t.Fatal(err)
			}
			report, err := ApplyDNS(mainNode, merged, tc.version)
			if err != nil {
				t.Fatalf("ApplyDNS(%q): %v", tc.version, err)
			}
			if report == nil || report.Applied != tc.wantApplied {
				t.Fatalf("ApplyDNS(%q) applied=%v, want %v", tc.version, report != nil && report.Applied, tc.wantApplied)
			}
			wantNote := fmt.Sprintf(msgDNSUnknownVersion, MinCoreText)
			hasUnknownNote := false
			for _, note := range report.Notes {
				if note == wantNote {
					hasUnknownNote = true
				}
			}
			if hasUnknownNote != tc.wantUnknown {
				t.Fatalf("ApplyDNS(%q) unknown note=%v, want %v (notes=%v)", tc.version, hasUnknownNote, tc.wantUnknown, report.Notes)
			}
			outdated := OldCoreAgents(
				[]CoreAgent{{ID: "a1", Name: "Agent1", ProfileID: "default", ServiceType: &mihomo, DiscoveryEnabled: &on}},
				"default", "default",
				func(string) CoreStatus { return CoreStatus{MihomoVersion: tc.version} },
			)
			if len(outdated) != tc.wantOutdated {
				t.Fatalf("OldCoreAgents(%q) len=%d, want %d (%+v)", tc.version, len(outdated), tc.wantOutdated, outdated)
			}
		})
	}
}

func TestWarnUnrecognizedCoreVersionThrottlesPerAgent(t *testing.T) {
	ResetCoreVersionWarnState()
	t.Cleanup(ResetCoreVersionWarnState)

	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root.Close()
		_, _ = logx.Setup(logx.Config{Level: logx.LevelFromEnv(), Console: os.Stderr})
	})

	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	// Empty and recognized versions must not emit warnings.
	for _, version := range []string{"", "   ", "v1.19.32", "v1.19.19", "Mihomo Meta v1.19.32 linux amd64"} {
		if WarnUnrecognizedCoreVersion("agent-a", "Alpha", version, base) {
			t.Fatalf("version %q unexpectedly warned", version)
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("unexpected log output: %s", logs.String())
	}

	// First unrecognized version for agent-a emits one warning.
	if !WarnUnrecognizedCoreVersion("agent-a", "Alpha", "alpha-100ff73", base) {
		t.Fatal("expected first unrecognized version warning")
	}
	// Repeat calls for the same agent within interval are suppressed (even with a different version).
	if WarnUnrecognizedCoreVersion("agent-a", "Alpha", "alpha-100ff73", base.Add(time.Minute)) {
		t.Fatal("duplicate warning within interval was not throttled")
	}
	if WarnUnrecognizedCoreVersion("agent-a", "Alpha", "garbage\ninjected", base.Add(UnrecognizedCoreVersionWarnInterval-time.Second)) {
		t.Fatal("same-agent warning within interval was not throttled")
	}
	// A different agent is tracked independently and control chars are sanitized.
	if !WarnUnrecognizedCoreVersion("agent-b", "Beta", "garbage\nline2", base.Add(time.Minute)) {
		t.Fatal("distinct agent warning was suppressed")
	}
	// Advancing past the interval allows agent-a to warn again.
	if !WarnUnrecognizedCoreVersion("agent-a", "Alpha", "alpha-100ff73", base.Add(UnrecognizedCoreVersionWarnInterval)) {
		t.Fatal("warning after interval was not emitted")
	}

	output := logs.String()
	wantAlpha := fmt.Sprintf(msgUnrecognizedCoreVersion, "Alpha (agent-a)", "alpha-100ff73")
	if got := strings.Count(output, wantAlpha); got != 2 {
		t.Fatalf("expected 2 warnings for agent-a, got %d:\n%s", got, output)
	}
	wantBeta := fmt.Sprintf(msgUnrecognizedCoreVersion, "Beta (agent-b)", logx.LogText("garbage\nline2", 64))
	if got := strings.Count(output, wantBeta); got != 1 {
		t.Fatalf("expected 1 sanitized warning for agent-b, got %d:\n%s", got, output)
	}
	if strings.Contains(output, "garbage\nline2") {
		t.Fatalf("raw newline leaked into log:\n%s", output)
	}
}
