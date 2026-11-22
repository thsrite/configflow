package installer

// Port of backend/test_docker_ha_preflight.py (non-route cases): structured
// host preflight is complete, bounded, read-only and never executes input.
// The collector script runs with bash in temporary directories with stub
// commands, as in the Python tests. The three API cases are in
// internal/api/agents/docker_ha_test.go.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type evidenceField struct {
	key   string
	code  int
	value string
}

// preflightEvidence is evidence(**changes) (ordered like the Python dict).
func preflightEvidence(changes ...evidenceField) []evidenceField {
	values := []evidenceField{
		{"kernel", 0, "Linux"}, {"docker_endpoint", 0, "local-unix"},
		{"docker_version", 0, `{"client":"29.9.2","server":"29.9.2"}`},
		{"daemon_os", 0, "linux"}, {"daemon_platform", 0, `{"os":"Arch Linux","kernel":"6.18.3"}`},
		{"daemon_security", 0, `["name=seccomp,profile=builtin","name=cgroupns"]`},
		{"compose", 0, "2.30.3"}, {"ipv4", 0, "2: eth0    inet 192.168.10.20/24 brd 192.168.10.255 scope global eth0\n"},
		{"udp53", 0, ""}, {"tcp53", 0, ""}, {"nesting", 0, "none"}, {"machine_fingerprint", 0, strings.Repeat("a", 64)},
	}
	for _, change := range changes {
		for i := range values {
			if values[i].key == change.key {
				values[i] = change
			}
		}
	}
	return values
}

func without(values []evidenceField, key string) []evidenceField {
	var out []evidenceField
	for _, v := range values {
		if v.key != key {
			out = append(out, v)
		}
	}
	return out
}

// preflightBlock is block(values).
func preflightBlock(values []evidenceField) string {
	lines := []string{PreflightBegin}
	for _, v := range values {
		lines = append(lines, "CFHA1\t"+v.key+"\t"+itoa(v.code)+"\t"+base64.StdEncoding.EncodeToString([]byte(v.value)))
	}
	return strings.Join(append(lines, PreflightEnd), "\n") + "\n"
}

func report(t *testing.T, output any, mode ...string) map[string]any {
	t.Helper()
	var m any = "host"
	if len(mode) > 0 {
		m = mode[0]
	}
	result, err := ParsePreflightReport(output, m)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func reportItem(t *testing.T, r map[string]any, ident string) map[string]any {
	t.Helper()
	for _, c := range mapList(r, "checks") {
		if m, ok := c.(map[string]any); ok && m["id"] == ident {
			return m
		}
	}
	t.Fatalf("no check %s in %v", ident, r)
	return nil
}

func TestCompleteNativeLocalOutputOnlyPassesBasicChecks(t *testing.T) {
	r := report(t, preflightBlock(preflightEvidence()))
	if r["status"] != "pass" || r["host_fingerprint"] != strings.Repeat("a", 64) {
		t.Fatal(r)
	}
	contains(t, mapStr(r, "summary"), "基础检查")
	contains(t, mapStr(r, "summary"), "VIP")
	contains(t, mapStr(reportItem(t, r, "ipv4"), "detail"), "Docker bridge")
}

func TestTerminalPromptsAndOldContainerSuffixDoNotReplaceProtocolEvidence(t *testing.T) {
	r := report(t, "root@nas:~# pasted command\n"+preflightBlock(preflightEvidence())+"\nold mounts: /persistent/core\n")
	if r["status"] != "pass" {
		t.Fatal(r)
	}
}

func TestOldDuplicateTruncatedOrMalformedOutputIsNeverAPass(t *testing.T) {
	block := preflightBlock(preflightEvidence())
	for _, output := range []string{
		"Linux\nDocker version 29.9.2\n", "", strings.Replace(block, PreflightEnd, "", 1),
		block + block, strings.Replace(block, PreflightBegin, "CONFIGFLOW_DOCKER_HA_PREFLIGHT_V2_BEGIN", 1),
		preflightBlock(without(preflightEvidence(), "udp53")),
		strings.Replace(block, "CFHA1\tudp53\t0\t", "CFHA1\tipv4\t0\t", 1),
		strings.Replace(block, "CFHA1\tudp53\t0\t", "CFHA1\tunexpected\t0\t", 1),
		strings.Replace(block, "CFHA1\tudp53\t0\t", "CFHA1\tudp53\t999\t", 1),
		strings.Replace(block, "CFHA1\tudp53\t0\t", "CFHA1\tudp53\t0\t!invalid!", 1),
		"CFHA1\ttcp53\t0\t\n" + block,
		preflightBlock(preflightEvidence(evidenceField{"ipv4", 0, strings.Repeat("x", PreflightMaxFieldBytes+1)})),
	} {
		if s := report(t, output)["status"]; s != "unknown" {
			t.Errorf("%.80q: %v", output, s)
		}
	}
}

func TestBinaryAndDeepJSONCannotTriggerUnhandledParserError(t *testing.T) {
	invalid := strings.Replace(preflightBlock(preflightEvidence()), "CFHA1\tudp53\t0\t", "CFHA1\tudp53\t0\t/w==", 1)
	if report(t, invalid)["status"] != "unknown" {
		t.Fatal("binary accepted")
	}
	deep := strings.Repeat("[", 2000) + "0" + strings.Repeat("]", 2000)
	r := report(t, preflightBlock(preflightEvidence(evidenceField{"daemon_security", 0, deep})))
	if r["status"] != "unknown" || reportItem(t, r, "docker_rootful")["status"] != "unknown" {
		t.Fatal(r)
	}
}

func TestParserRejectsWrongTypeEncodingOrOversizedInput(t *testing.T) {
	// "\ud800" (a lone surrogate) is invalid UTF-8 in a Go string.
	for _, output := range []any{nil, []any{}, "\xed\xa0\x80", strings.Repeat("x", PreflightMaxOutputBytes+1)} {
		if _, err := ParsePreflightReport(output, "host"); err == nil {
			t.Errorf("%.40v accepted", output)
		} else if _, ok := err.(*ValueError); !ok {
			t.Errorf("%T", err)
		}
	}
}

func TestKnownUnsupportedEnvironmentsFailWithoutEchoingRawErrors(t *testing.T) {
	for _, c := range []struct {
		change evidenceField
		ident  string
	}{
		{evidenceField{"kernel", 0, "Darwin"}, "linux"},
		{evidenceField{"docker_endpoint", 0, "remote"}, "docker_endpoint"},
		{evidenceField{"docker_version", 1, "Cannot connect to the Docker daemon"}, "docker_daemon"},
		{evidenceField{"docker_version", 127, "docker: command not found"}, "docker_daemon"},
		{evidenceField{"daemon_os", 0, "windows"}, "docker_os"},
		{evidenceField{"daemon_platform", 0, `{"os":"Docker Desktop","kernel":"linuxkit"}`}, "docker_platform"},
		{evidenceField{"daemon_security", 0, `["name=rootless"]`}, "docker_rootful"},
	} {
		r := report(t, preflightBlock(preflightEvidence(c.change)))
		if r["status"] != "fail" || reportItem(t, r, c.ident)["status"] != "fail" {
			t.Errorf("%v: %v", c.change, r)
		}
	}
}

func TestFailedRequiredProbeIsUnknownNotEmptySuccess(t *testing.T) {
	for _, c := range [][2]string{{"ipv4", "ipv4"}, {"udp53", "udp53"}, {"tcp53", "tcp53"}, {"docker_endpoint", "docker_endpoint"}, {"daemon_security", "docker_rootful"}} {
		r := report(t, preflightBlock(preflightEvidence(evidenceField{c[0], 127, "missing tool"})))
		if r["status"] != "unknown" || reportItem(t, r, c[1])["status"] != "unknown" {
			t.Errorf("%s: %v", c[0], r)
		}
	}
}

func TestHostLoopbackOrWildcardPortCollisionFailsButMacvlanDoesNot(t *testing.T) {
	for _, c := range [][2]string{{"udp53", "UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:*"}, {"udp53", "UNCONN 0 0 [::]:53 [::]:*"},
		{"tcp53", "LISTEN 0 4096 127.0.0.54:53 0.0.0.0:*"}} {
		data := preflightBlock(preflightEvidence(evidenceField{c[0], 0, c[1]}))
		r := report(t, data, "host")
		if r["status"] != "fail" || reportItem(t, r, c[0])["status"] != "fail" {
			t.Errorf("%v host: %v", c, r)
		}
		contains(t, mapStr(reportItem(t, r, c[0]), "remedy"), "迁移")
		m := report(t, data, "macvlan")
		if m["status"] != "pass" || reportItem(t, m, c[0])["status"] != "pass" {
			t.Errorf("%v macvlan: %v", c, m)
		}
	}
}

func TestUnrecognizedUDPOutputIsUnknown(t *testing.T) {
	for _, line := range []string{"ss: not supported", "UNCONN 0 0 127.0.0.53:53", "LISTEN 0 1 *:53 *:*"} {
		if s := reportItem(t, report(t, preflightBlock(preflightEvidence(evidenceField{"udp53", 0, line}))), "udp53")["status"]; s != "unknown" {
			t.Errorf("%q: %v", line, s)
		}
	}
}

func TestComposeIsOptionalForRunInstallation(t *testing.T) {
	for _, v := range []evidenceField{{"compose", 127, ""}, {"compose", 0, "2.29.9"}, {"compose", 0, "nonsense"}} {
		r := report(t, preflightBlock(preflightEvidence(v)))
		if r["status"] != "warning" {
			t.Errorf("%v: %v", v, r)
		}
		contains(t, mapStr(reportItem(t, r, "compose"), "detail"), "Docker Run")
	}
}

func TestMissingDefaultIdentityAndLXCAreWarningsNotNativeSuccessOrFailure(t *testing.T) {
	r := report(t, preflightBlock(preflightEvidence(evidenceField{"machine_fingerprint", 1, ""}, evidenceField{"nesting", 0, "container"})))
	if r["status"] != "warning" || mapHas(r, "host_fingerprint") {
		t.Fatal(r)
	}
	contains(t, mapStr(reportItem(t, r, "host_identity"), "detail"), "/etc/machine-id")
	if reportItem(t, r, "nesting")["status"] != "warning" {
		t.Fatal("nesting")
	}
}

func TestPastedShellAndHTMLAreNeverExecuted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	attack := "$(touch " + marker + ")<script>alert(1)</script>"
	r := report(t, preflightBlock(preflightEvidence(evidenceField{"ipv4", 0, attack})))
	if r["status"] != "unknown" {
		t.Fatal(r)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("executed")
	}
	data, _ := json.Marshal(r)
	notContains(t, string(data), "<script>")
}

// collectorEnv is collector_env(tmp_path): environment entries for the stub
// commands (added to os.Environ()).
func collectorEnv(t *testing.T, tmp string) []string {
	t.Helper()
	writeFile(t, filepath.Join(tmp, "docker"), stubScript(t, "preflight-docker", ""), 0o755)
	writeFile(t, filepath.Join(tmp, "ip"), "#!/bin/sh\nprintf \"%s\\n\" \"2: eth0    inet 192.168.10.20/24 scope global eth0\"\n", 0o755)
	writeFile(t, filepath.Join(tmp, "ss"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(tmp, "systemd-detect-virt"), "#!/bin/sh\nexit 1\n", 0o755)
	return []string{"PATH=" + tmp + ":" + os.Getenv("PATH"), "DOCKER_HOST=", "DOCKER_CONTEXT="}
}

// runCollector runs bash with the script on stdin.
func runCollector(t *testing.T, script string, env []string, replace bool) runResult {
	t.Helper()
	cmd := exec.Command(bashPath(t))
	cmd.Stdin = strings.NewReader(script)
	if replace {
		cmd.Env = env
	} else {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	return runResult{code, stdout.String(), stderr.String()}
}

func TestCollectorContinuesAfterDaemonFailureAndPreservesCallersOptions(t *testing.T) {
	tmp := t.TempDir()
	env := append(collectorEnv(t, tmp), "CF_TEST_DAEMON_FAIL=1")
	command := "set -eu\n" + CollectorScript() + "\ncase $- in *e*u*|*u*e*) printf \"OPTIONS_RETAINED\\n\" ;; *) exit 9 ;; esac\n"
	result := runCollector(t, command, env, false)
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	contains(t, result.stdout, PreflightEnd)
	contains(t, result.stdout, "OPTIONS_RETAINED")
	r := report(t, result.stdout)
	if r["status"] != "fail" || reportItem(t, r, "docker_daemon")["status"] != "fail" ||
		reportItem(t, r, "ipv4")["status"] != "pass" || reportItem(t, r, "udp53")["status"] != "pass" {
		t.Fatal(r)
	}
}

func TestCollectorDockerContextOverridesHostWithoutDisclosingEndpoint(t *testing.T) {
	tmp := t.TempDir()
	events := filepath.Join(tmp, "docker-events")
	env := append(collectorEnv(t, tmp), "DOCKER_CONTEXT=remote-context", "DOCKER_HOST=unix:///var/run/docker.sock", "CF_TEST_DOCKER_EVENTS="+events)
	result := runCollector(t, CollectorScript(), env, false)
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	r := report(t, result.stdout)
	if reportItem(t, r, "docker_endpoint")["status"] != "fail" {
		t.Fatal(r)
	}
	notContains(t, result.stdout, "private-user")
	notContains(t, result.stdout, "secret")
	if anyCall(eventCalls(t, events), "version", "info") {
		t.Fatal("daemon queried")
	}
	daemon := reportItem(t, r, "docker_daemon")
	if daemon["status"] != "unknown" {
		t.Fatal(daemon)
	}
	contains(t, mapStr(daemon, "detail"), "已跳过")
	env = append(env, "DOCKER_CONTEXT=default", "DOCKER_HOST=ssh://elsewhere")
	result = runCollector(t, CollectorScript(), env, false)
	if reportItem(t, report(t, result.stdout), "docker_endpoint")["status"] != "pass" {
		t.Fatal(result.stdout)
	}
}

func TestCollectorWithoutEncodingToolsNeverEmitsFalseCompleteBlock(t *testing.T) {
	tmp := t.TempDir()
	env := append([]string{}, os.Environ()...)
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + tmp
		}
	}
	cmd := exec.Command("/bin/bash")
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Fatalf("/bin/bash is required on the test server: %v", err)
	}
	cmd.Stdin = strings.NewReader(CollectorScript())
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	notContains(t, string(out), PreflightEnd)
	contains(t, string(out), "base64")
	if report(t, string(out))["status"] != "unknown" {
		t.Fatal(string(out))
	}
}

func TestFingerprintCollectionNeverOutputsRawMachineID(t *testing.T) {
	script := CollectorScript()
	contains(t, script, "sha256sum")
	notContains(t, script, "/var/lib/dbus/machine-id")
	contains(t, script, `printf '%s' "$cf_identity" | sha256sum`)
	for _, s := range []string{"docker inspect", "docker stop", "docker run"} {
		notContains(t, script, s)
	}
	if mapStr(mustGuide(t, map[string]any{"action": "inspect"}, nil), "inspection_command") != script {
		t.Fatal("inspection command is not the collector")
	}
}

func TestCollectorIdentitySymlinkIsNotClaimedCompatibleWithInstallation(t *testing.T) {
	tmp := t.TempDir()
	env := collectorEnv(t, tmp)
	target := writeFile(t, filepath.Join(tmp, "real-id"), "0123456789abcdef0123456789abcdef\n", 0o644)
	link := filepath.Join(tmp, "identity-link")
	os.Symlink(target, link)
	source := strings.ReplaceAll(CollectorScript(), "/etc/machine-id", q(link))
	r := report(t, runCollector(t, source, env, false).stdout)
	if reportItem(t, r, "host_identity")["status"] != "warning" || mapHas(r, "host_fingerprint") {
		t.Fatal(r)
	}
	contains(t, mapStr(reportItem(t, r, "host_identity"), "detail"), "软链接")
	source = strings.ReplaceAll(CollectorScript(), "/etc/machine-id", q(target))
	result := runCollector(t, source, env, false)
	r = report(t, result.stdout)
	sum := sha256.Sum256([]byte("0123456789abcdef0123456789abcdef"))
	if reportItem(t, r, "host_identity")["status"] != "pass" || r["host_fingerprint"] != hex.EncodeToString(sum[:]) {
		t.Fatal(r)
	}
	notContains(t, result.stdout, "0123456789abcdef0123456789abcdef")
}

func TestIPv4ReportShowsActualInterfacesAndPrefixesWithoutSelectingLAN(t *testing.T) {
	addresses := "2: enp3s0 inet 192.168.10.20/24 scope global enp3s0\n3: docker0 inet 172.17.0.1/16 scope global docker0\n"
	detail := mapStr(reportItem(t, report(t, preflightBlock(preflightEvidence(evidenceField{"ipv4", 0, addresses}))), "ipv4"), "detail")
	for _, s := range []string{"2 个候选", "enp3s0 · 192.168.10.20/24", "docker0 · 172.17.0.1/16", "Docker bridge", "尚未验证局域网", "不会自动选择"} {
		contains(t, detail, s)
	}
}

func TestIPv4ReportBoundsVisibleListButKeepsTotalCount(t *testing.T) {
	var addresses strings.Builder
	for n := 0; n < 10; n++ {
		addresses.WriteString(itoa(n+1) + ": eth" + itoa(n) + " inet 192.168.10." + itoa(n+1) + "/24 scope global eth" + itoa(n) + "\n")
	}
	detail := mapStr(reportItem(t, report(t, preflightBlock(preflightEvidence(evidenceField{"ipv4", 0, addresses.String()}))), "ipv4"), "detail")
	contains(t, detail, "10 个候选")
	contains(t, detail, "仅显示前 8 条")
	contains(t, detail, "eth7 · 192.168.10.8/24")
	notContains(t, detail, "eth8 ·")
	if strings.Count(detail, " · ") != 8 {
		t.Fatal(detail)
	}
}

func TestIPv4ReportCleansControlAndMarkupAndLimitsInterfaceName(t *testing.T) {
	name := "eth<script>" + "\x1b[31m" + strings.Repeat("x", 100)
	detail := mapStr(reportItem(t, report(t, preflightBlock(preflightEvidence(evidenceField{"ipv4", 0, "2: " + name + " inet 192.168.10.20/24 scope global interface\n"}))), "ipv4"), "detail")
	for _, s := range []string{"<", ">", "\x1b", strings.Repeat("x", 49)} {
		notContains(t, detail, s)
	}
	contains(t, detail, "名称已截短或清理")
	contains(t, detail, " · 192.168.10.20/24")
}

func TestSynologySanitizedReconstructedCopyPreservesActionableChecks(t *testing.T) {
	data, err := os.ReadFile("testdata/docker_ha_preflight_synology_reconstructed.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, markdown := range []bool{false, true} {
		output := string(data)
		if markdown {
			lines := pySplitLines(output)
			for i := range lines {
				lines[i] += "\\"
			}
			output = strings.Join(lines, "\n") + "\n"
		}
		r := report(t, output)
		if r["status"] != "unknown" || reportItem(t, r, "udp53")["status"] != "unknown" || reportItem(t, r, "tcp53")["status"] != "unknown" ||
			reportItem(t, r, "compose")["status"] != "warning" || reportItem(t, r, "docker_daemon")["status"] != "pass" {
			t.Fatal(r)
		}
		contains(t, mapStr(reportItem(t, r, "ipv4"), "detail"), "ovs_eth0 · 192.168.0.105/24")
		contains(t, mapStr(reportItem(t, r, "ipv4"), "detail"), "12 个候选")
		if r["host_fingerprint"] != strings.Repeat("a", 64) {
			t.Fatal("fingerprint")
		}
		m := report(t, output, "macvlan")
		if m["status"] != "warning" || reportItem(t, m, "udp53")["status"] != "pass" || reportItem(t, m, "tcp53")["status"] != "pass" {
			t.Fatal(m)
		}
		contains(t, mapStr(reportItem(t, m, "udp53"), "detail"), "尚未验证")
	}
}

func TestExplicitEmptySentinelSurvivesHorizontalWhitespaceCopy(t *testing.T) {
	for _, separator := range []string{" ", "   ", "\t ", " \t"} {
		output := strings.ReplaceAll(strings.ReplaceAll(preflightBlock(preflightEvidence()), "\t0\t\n", "\t0\t-\n"), "\t", separator)
		if report(t, output)["status"] != "pass" {
			t.Errorf("%q: not pass", separator)
		}
		output = strings.Replace(output, "CFHA1"+separator+"udp53"+separator+"0"+separator+"-", "CFHA1"+separator+"udp53"+separator+"127", 1)
		if reportItem(t, report(t, output), "udp53")["status"] != "unknown" {
			t.Errorf("%q: 127 not unknown", separator)
		}
	}
}

func TestCopyNormalizationNeverRepairsAmbiguousOrInvalidSuccess(t *testing.T) {
	for _, row := range []string{"CFHA1 udp53 0", "CFHA1 udp53 0   ", "CFHA1\tudp53\t0", "CFHA1 udp53 0 -", "CFHA1 udp53 0 --",
		"CFHA1 udp53 0 -\\\\", "CFHA1 udp53 0 \\", "CFHA1 udp53 0 c291\\\ncmNl", "CFHA1 udp53 0 !!!!\\"} {
		output := strings.Replace(preflightBlock(preflightEvidence()), "CFHA1\tudp53\t0\t", row, 1)
		if report(t, output)["status"] != "unknown" || report(t, output, "macvlan")["status"] != "unknown" {
			t.Errorf("%q repaired", row)
		}
	}
}

func TestCopyNormalizationStillRejectsDuplicateAndUnknownFields(t *testing.T) {
	output := strings.ReplaceAll(strings.ReplaceAll(preflightBlock(preflightEvidence()), "\t0\t\n", "\t0\t-\n"), "\t", "  ")
	for _, changed := range []string{strings.ReplaceAll(output, "tcp53", "udp53"), strings.ReplaceAll(output, "tcp53", "unexpected"), output + "CFHA1 udp53 127\n"} {
		if report(t, changed)["status"] != "unknown" {
			t.Errorf("%q accepted", changed)
		}
	}
}

func TestExplicitProcSourceRecordsOccupiedAndEmptyTables(t *testing.T) {
	for _, c := range []struct {
		proto, rows string
		busy        bool
	}{{"udp", "", false}, {"tcp", "", false}, {"udp", "udp 4 0100007F:0035 07\n", true},
		{"udp", "udp 6 00000000000000000000000000000000:0035 01\n", true}, {"tcp", "tcp 4 00000000:0035 0A\n", true}} {
		payload := "source=proc-net-v1\ntables=" + c.proto + "," + c.proto + "6\n" + c.rows
		block := preflightBlock(preflightEvidence(evidenceField{c.proto + "53", 0, payload}))
		result := reportItem(t, report(t, block), c.proto+"53")
		want := "pass"
		if c.busy {
			want = "fail"
		}
		if result["status"] != want {
			t.Errorf("%v: %v", c, result)
		}
		contains(t, mapStr(result, "detail"), "/proc/net")
		if reportItem(t, report(t, block, "macvlan"), c.proto+"53")["status"] != "pass" {
			t.Errorf("%v macvlan", c)
		}
	}
}

func TestMalformedProcEvidenceNeverImpliesHostPortIsFree(t *testing.T) {
	for _, payload := range []string{"source=proc-net-v1", "source=proc-net-v1\ntables=udp", "source=proc-net-v1\ntables=tcp,tcp6",
		"source=proc-net-v1\ntables=udp,udp6\nudp 4 00000000:0035 FF", "source=proc-net-v1\ntables=udp,udp6\nudp 6 00000000:0035 07",
		"source=proc-net-v1\ntables=udp,udp6\nudp 4 00000000:0036 07", "source=unknown"} {
		if reportItem(t, report(t, preflightBlock(preflightEvidence(evidenceField{"udp53", 0, payload}))), "udp53")["status"] != "unknown" {
			t.Errorf("%q", payload)
		}
	}
}

const procHeader = "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"

func procRow(family int, state, port string) string {
	address := "0100007F"
	if family != 4 {
		address = strings.Repeat("0", 32)
	}
	return "0: " + address + ":" + port + " " + strings.Repeat("0", len(address)) + ":0000 " + state + " 00000000:00000000 00:00000000 00000000 0 0 123 1 00000000\n"
}

// procCollector is proc_collector(tmp_path, tables).
func procCollector(t *testing.T, tables map[string]string) (map[string]preflightValue, map[string]any) {
	t.Helper()
	tmp := t.TempDir()
	env := collectorEnv(t, tmp)
	writeFile(t, filepath.Join(tmp, "ss"), "#!/bin/sh\nexit 127\n", 0o755)
	proc := filepath.Join(tmp, "proc-net")
	os.MkdirAll(proc, 0o755)
	for name, contents := range tables {
		writeFile(t, filepath.Join(proc, name), contents, 0o644)
	}
	source := strings.ReplaceAll(CollectorScript(), "/proc/net/", proc+"/")
	result := runCollector(t, source, env, false)
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	values, reason, err := preflightDecode(result.stdout)
	if err != nil || reason != "" {
		t.Fatal(err, reason)
	}
	return values, report(t, result.stdout)
}

func TestCollectorProcFallbackAcceptsValidDualTablesAndConservativeUDPBindings(t *testing.T) {
	values, r := procCollector(t, map[string]string{
		"tcp":  procHeader + procRow(4, "01", "0035"), // Established TCP is not a listener.
		"tcp6": procHeader,
		"udp":  procHeader + procRow(4, "01", "0035"), // Connected UDP still owns its local port.
		"udp6": procHeader + procRow(6, "07", "0035"),
	})
	if values["tcp53"] != (preflightValue{0, "source=proc-net-v1\ntables=tcp,tcp6"}) {
		t.Fatalf("%#v", values["tcp53"])
	}
	if reportItem(t, r, "tcp53")["status"] != "pass" || reportItem(t, r, "udp53")["status"] != "fail" {
		t.Fatal(r)
	}
	contains(t, mapStr(reportItem(t, r, "udp53"), "detail"), "2 项")
	if !strings.HasPrefix(values["udp53"].text, "source=proc-net-v1\ntables=udp,udp6\n") {
		t.Fatalf("%q", values["udp53"].text)
	}
}

func TestCollectorProcFallbackRequiresEveryTableAndValidNonmatchingRows(t *testing.T) {
	for _, fault := range []string{"missing-v6", "empty-v6", "bad-header", "bad-row", "bad-state", "bad-address"} {
		t.Run(fault, func(t *testing.T) {
			tables := map[string]string{"udp": procHeader, "udp6": procHeader, "tcp": procHeader, "tcp6": procHeader}
			switch fault {
			case "missing-v6":
				delete(tables, "udp6")
			case "empty-v6":
				tables["udp6"] = ""
			case "bad-header":
				tables["udp6"] = "unrecognized table\n"
			case "bad-row":
				tables["udp"] += "not a proc socket row\n"
			case "bad-state":
				tables["udp"] += procRow(4, "FF", "1234")
			default:
				tables["udp"] += strings.Replace(procRow(4, "07", "1234"), "0100007F", "invalid", 1)
			}
			values, r := procCollector(t, tables)
			if values["udp53"].code == 0 || reportItem(t, r, "udp53")["status"] != "unknown" || reportItem(t, r, "tcp53")["status"] != "pass" {
				t.Fatal(values["udp53"], r)
			}
		})
	}
}

func TestCollectorSerializesEmptyFailedFieldsWithExplicitSentinel(t *testing.T) {
	tmp := t.TempDir()
	env := collectorEnv(t, tmp)
	writeFile(t, filepath.Join(tmp, "ip"), "#!/bin/sh\nexit 127\n", 0o755)
	result := runCollector(t, CollectorScript(), env, false)
	contains(t, result.stdout, "CFHA1\tipv4\t127\t-\n")
	if reportItem(t, report(t, result.stdout), "ipv4")["status"] != "unknown" {
		t.Fatal(result.stdout)
	}
}
