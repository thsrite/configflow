package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/installer/installertest"
	"configflow/internal/jsonx"
)

func TestL1HAVerificationUsesAvailableToolsAndExactVIP(t *testing.T) {
	want, err := installertest.HAVerificationModule()
	if err != nil || want != haVerification {
		t.Fatalf("verification source drift: %v", err)
	}
	for _, tool := range []string{"dig", "nslookup", "busybox", "none", "failed-dig"} {
		t.Run(tool, func(t *testing.T) {
			dir := t.TempDir()
			events := filepath.Join(dir, "events")
			command := mustGuide(t, jsonx.Obj("action", "verify", "vip", "192.0.2.53"), nil).GetString("verification_command")
			writeFile(t, filepath.Join(dir, "getent"), "#!/bin/sh\nprintf wrong-resolver >> \"$EVENTS\"\n", 0o755)
			selected := tool
			if tool == "failed-dig" {
				selected = "dig"
			}
			if selected != "none" {
				mock := "#!/bin/sh\nprintf '%s %s\\n' \"${0##*/}\" \"$*\" >> \"$EVENTS\"\n"
				if selected == "busybox" {
					mock = "#!/bin/sh\nif [ \"$1\" = --list ]; then printf 'nslookup\\n'; exit 0; fi\n" + strings.TrimPrefix(mock, "#!/bin/sh\n")
					grep, _ := exec.LookPath("grep")
					if err := os.Symlink(grep, filepath.Join(dir, "grep")); err != nil {
						t.Fatal(err)
					}
				}
				if tool == "failed-dig" {
					mock += "exit 9\n"
				}
				writeFile(t, filepath.Join(dir, selected), mock, 0o755)
			}
			r := runCommand(t, command, dir, "PATH="+dir, "EVENTS="+events)
			if tool == "none" {
				if r.code != 127 {
					t.Fatalf("missing tools: %d %s", r.code, r.stderr)
				}
				contains(t, r.stderr, "dnsutils")
				contains(t, r.stderr, "bind-tools")
				if _, err := os.Stat(events); !os.IsNotExist(err) {
					t.Fatal("used default-resolver getent")
				}
				return
			}
			if tool == "failed-dig" {
				if r.code != 9 {
					t.Fatalf("query failure swallowed: %d", r.code)
				}
			} else if r.code != 0 {
				t.Fatalf("%s: %d %s", tool, r.code, r.stderr)
			}
			called := readFile(t, events)
			contains(t, called, "192.0.2.53")
			contains(t, called, "example.com")
			notContains(t, called, "getent")
			if selected == "dig" {
				contains(t, called, "@192.0.2.53")
			}
		})
	}
}
