package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/model"
)

// TestReviewN810SessionInvalidUTF8NeverWrites pins coordinator ruling 1 for
// session files: encoding/json alone accepts invalid UTF-8 inside strings and
// projects it to U+FFFD; model.Fields must keep refusing such a session before
// any renewal or enrollment.
func TestReviewN810SessionInvalidUTF8NeverWrites(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func([]byte) []byte
		expired bool
	}{
		{"name byte", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"native-dns-oxidns"`), []byte("\"native-dns-oxidns\xff\""), 1)
		}, false},
		{"unknown member byte", func(b []byte) []byte {
			return append(bytes.TrimSuffix(bytes.TrimSpace(b), []byte("}")), []byte(",\"note\":\"\xc3\x28\"}")...)
		}, false},
		{"expired renewal", func(b []byte) []byte {
			b = bytes.Replace(b, []byte(`"native-dns-oxidns"`), []byte("\"native-dns-oxidns\xff\""), 1)
			i := bytes.Index(b, []byte(`"expires":`))
			j := i + len(`"expires":`)
			k := j
			for k < len(b) && b[k] >= '0' && b[k] <= '9' {
				k++
			}
			return append(append(append([]byte{}, b[:j]...), '1'), b[k:]...)
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			capability, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
			if err != nil {
				t.Fatal(err)
			}
			id := capability.URL[strings.LastIndexByte(capability.URL, '/')+1:]
			path := filepath.Join(dir, "docker-ha-installations", id+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			changed := test.mutate(data)
			if bytes.Equal(changed, data) {
				t.Fatal("negative control did not alter the session")
			}
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			before := canonical(readTree(dir))
			var refusal *EnrollmentError
			got, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
			if got != nil || !errors.As(err, &refusal) || refusal.Status != 409 || refusal.Message != enrollmentUnreadable {
				t.Fatalf("issue: %+v, %v", got, err)
			}
			registry := &nativeRegistry{}
			identity, err := EnrollIdentity(context.Background(), dir, registry, id, capability.Token, true)
			if identity != (AgentIdentity{}) || !errors.As(err, &refusal) || refusal.Status != 401 || refusal.Message != enrollmentInvalid || registry.calls != 0 || len(registry.records) != 0 {
				t.Fatalf("enroll: %+v, %v, calls %d", identity, err, registry.calls)
			}
			if canonical(readTree(dir)) != before {
				t.Fatal("invalid UTF-8 session refusal wrote files")
			}
		})
	}
}

// TestReviewN810RootAliasIsNotAnInstallPath covers ruling 6 beyond the
// compact/direct system-directory guard: "//" names the same Linux root as
// "/", which every guide path already refuses before any output or write.
func TestReviewN810RootAliasIsNotAnInstallPath(t *testing.T) {
	const message = "须为规范的绝对路径，不能包含逗号、冒号或换行"
	tests := []struct {
		name, label string
		members     map[string]any
	}{
		{"script data", "持久数据根目录", map[string]any{"data_dir": "//"}},
		{"bundle data", "持久数据根目录", map[string]any{"install_method": "compose", "data_dir": "//", "ha_host_id_path": "//agent/host-id"}},
		{"compact data", "持久数据根目录", map[string]any{"install_method": "compose_compact", "data_dir": "//"}},
		{"identity file", "宿主身份文件", map[string]any{"ha_host_id_path": "//"}},
		{"migration backup", "备份目录", map[string]any{"mode": "migrate", "container_name": "old-oxidns", "existing_agent_name": "native-dns-oxidns", "ha_data_dir": "/srv/new-ha", "backup_dir": "//", "agent_mount": map[string]string{"type": "bind", "source": "/srv/old/agent"}, "core_mount": map[string]string{"type": "bind", "source": "/srv/old/oxidns"}}},
		{"migration mount", "原 Agent 挂载", map[string]any{"mode": "migrate", "container_name": "old-oxidns", "existing_agent_name": "native-dns-oxidns", "ha_data_dir": "/srv/new-ha", "backup_dir": "/srv/backup", "agent_mount": map[string]string{"type": "bind", "source": "//"}, "core_mount": map[string]string{"type": "bind", "source": "/srv/old/oxidns"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fields model.Fields
			if err := json.Unmarshal([]byte(nativeInstallRequest), &fields); err != nil {
				t.Fatal(err)
			}
			for name, value := range test.members {
				if err := setSessionField(&fields, name, value); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			result, err := GenerateGuideJSON(raw, nil)
			var invalid *ValidationError
			if result != nil || !errors.As(err, &invalid) || err.Error() != test.label+message {
				t.Fatalf("root alias accepted or wrong refusal: %v", err)
			}
		})
	}
	// Positive control: the published double-slash spelling of a real
	// subdirectory stays valid.
	raw := strings.Replace(nativeInstallRequest, `"/srv/native-dns"`, `"//srv/native"`, 1)
	if result, err := GenerateGuideJSON([]byte(raw), nil); err != nil || result == nil {
		t.Fatalf("valid double-slash path refused: %v", err)
	}
}

// TestReviewN810NativeDockerRunQuotesPortText keeps the exported native
// Docker renderer safe for callers that pass machine strings directly: port
// text is one shell word, while integer forms keep their published bytes.
func TestReviewN810NativeDockerRunQuotesPortText(t *testing.T) {
	const hostile = "8080;touch /tmp/cf-review-x"
	options := DefaultDockerOptions("https://panel.example.com")
	options.NetworkMode = "bridge"
	options.EnableMosdns = true
	options.EnableOxidns = false
	options.MihomoPort, options.MosdnsPort = hostile, hostile
	script, err := RenderDockerRun(options)
	if err != nil {
		t.Fatal(err)
	}
	rest := script
	for _, word := range []string{"'" + hostile + ":" + hostile + "'", "'AGENT_MIHOMO_PORT=" + hostile + "'", "'AGENT_MOSDNS_PORT=" + hostile + "'"} {
		if !strings.Contains(rest, word) {
			t.Fatalf("port text is not one quoted shell word %s:\n%s", word, script)
		}
		rest = strings.ReplaceAll(rest, word, "")
	}
	if strings.Contains(rest, "cf-review-x") {
		t.Fatalf("port text reached the shell unquoted:\n%s", script)
	}
	options.MihomoPort, options.MosdnsPort = "8080", "8081"
	script, err = RenderDockerRun(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-p 8080:8080", "-p 8081:8081", "-e AGENT_MIHOMO_PORT=8080 \\", "-e AGENT_MOSDNS_PORT=8081 \\"} {
		if !strings.Contains(script, want) {
			t.Fatalf("integer port bytes changed, missing %q", want)
		}
	}
}

// TestReviewN810SSUnicodeBlankLineIsUnknown follows the coordinator's L1
// ruling: a line of non-ASCII white space is not an empty ss record, so it
// can never prove that port 53 is free.
func TestReviewN810SSUnicodeBlankLineIsUnknown(t *testing.T) {
	for _, payload := range []string{" \n", "　\n", "\u0085\n", "\v\n", "\f\n"} {
		output := preflightBlock(preflightEvidence(evidenceField{"udp53", 0, payload}, evidenceField{"tcp53", 0, payload}))
		report, err := EvaluatePreflight(output, "host")
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range report.Checks {
			if (check.ID == "udp53" || check.ID == "tcp53") && check.Status != "unknown" {
				t.Fatalf("payload %q: %s=%s, overall %s", payload, check.ID, check.Status, report.Status)
			}
		}
		if report.Status != "unknown" {
			t.Fatalf("payload %q: overall %s", payload, report.Status)
		}
	}
	// Positive control: ASCII blank lines are still empty evidence.
	output := preflightBlock(preflightEvidence(evidenceField{"udp53", 0, " \t\n\n"}, evidenceField{"tcp53", 0, "\n"}))
	if report, err := EvaluatePreflight(output, "host"); err != nil || report.Status != "pass" {
		t.Fatalf("ASCII blank ss lines: %+v, %v", report, err)
	}
}
