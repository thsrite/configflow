package installer

import (
	"configflow/internal/installer/installertest"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Installer parts of backend/test_router_split_regression.py: the
// single-service installers equal the 0ef9e89 release after undoing the
// reviewed changes. The delta tables are generated from the Python test
// module by testdata/gen_reviewed_deltas.py (never copied by hand).

type reviewedDeltas struct {
	NativeHADependencyCases         string            `json:"native_ha_dependency_cases"`
	NativeHAPackageCase             string            `json:"native_ha_package_case"`
	GatewayDependencyCases          string            `json:"gateway_dependency_cases"`
	ManualInstallCommand            string            `json:"manual_install_command"`
	GatewayDependencyRewords        [][2]string       `json:"gateway_dependency_rewords"`
	CoreInstallPath                 string            `json:"core_install_path"`
	CoreAgentFollows                string            `json:"core_agent_follows"`
	OpenRCStopRetryDeltas           [][2]string       `json:"openrc_stop_retry_deltas"`
	ResolvedStubPromptDeltas        [][2]string       `json:"resolved_stub_prompt_deltas"`
	InstallerHardeningDeltas        [][2]string       `json:"installer_hardening_deltas"`
	UnifiedCleanupPattern           string            `json:"unified_cleanup_pattern"`
	StagedAgent                     string            `json:"staged_agent"`
	PreviousCoreCleanup             string            `json:"previous_core_cleanup"`
	SingleServiceScriptSHA256       map[string]string `json:"single_service_script_sha256"`
	PythonSingleServiceScriptSHA256 map[string]string `json:"python_single_service_script_sha256"`
	GatewayOldNeed                  string            `json:"gateway_old_need"`
	GatewayDependencyCall           string            `json:"gateway_dependency_call"`
	OrdinaryAIOForwardingDeltas     [][2]string       `json:"ordinary_aio_forwarding_deltas"`
}

var loadedDeltas *reviewedDeltas

func deltas(t *testing.T) *reviewedDeltas {
	t.Helper()
	if loadedDeltas == nil {
		var d reviewedDeltas
		if err := json.Unmarshal([]byte(readFile(t, "testdata/reviewed_deltas.json")), &d); err != nil {
			t.Fatal(err)
		}
		loadedDeltas = &d
	}
	return loadedDeltas
}

// backendTestdata is backend/testdata/<name> (the Python baselines).
func backendTestdata(t *testing.T, name string) string {
	t.Helper()
	return readFile(t, filepath.Join("..", "..", "..", "backend", "testdata", name))
}

const unifiedCleanupPattern = `BINARY_TMP=""\nRESTART_PREVIOUS_AGENT=0\n.*?\ntrap \'exit 143\' TERM\n(?=\n# Shared by the native single-Agent)`

// unifiedCleanupBlocks is _UNIFIED_CLEANUP.findall(script) (RE2 has no
// look-ahead, so the lazy match and the look-ahead are spelled out).
func unifiedCleanupBlocks(script string) []string {
	const head = "BINARY_TMP=\"\"\nRESTART_PREVIOUS_AGENT=0\n"
	const tail = "\ntrap 'exit 143' TERM\n"
	const ahead = "\n# Shared by the native single-Agent"
	var blocks []string
	for offset := 0; ; {
		start := strings.Index(script[offset:], head)
		if start < 0 {
			return blocks
		}
		start += offset
		end := strings.Index(script[start+len(head):], tail+ahead)
		if end < 0 {
			return blocks
		}
		end += start + len(head) + len(tail)
		blocks = append(blocks, script[start:end])
		offset = end
	}
}

func count(s, sub string) int { return strings.Count(s, sub) }

func require(t *testing.T, cond bool, msg string, args ...any) {
	t.Helper()
	if !cond {
		t.Fatalf(msg, args...)
	}
}

// withoutUnifiedAgentCleanup is without_unified_agent_cleanup(script, core_path).
func withoutUnifiedAgentCleanup(t *testing.T, script string, corePath bool) string {
	t.Helper()
	d := deltas(t)
	blocks := unifiedCleanupBlocks(script)
	if len(blocks) == 0 {
		return script
	}
	require(t, len(blocks) == 1 && count(script, d.StagedAgent) == 1, "unified cleanup blocks %d", len(blocks))
	script = strings.Replace(script, blocks[0], "", 1)
	if !corePath {
		require(t, !strings.Contains(blocks[0], "CORE_PHASE_COMPLETED"), "core phase state without a core path")
		return strings.Replace(script, d.StagedAgent, d.StagedAgent+"trap 'rm -f \"$BINARY_TMP\"' EXIT HUP INT TERM\n", 1)
	}
	m := regexp.MustCompile(`Agent 安装未完成；([a-z]+) 核心安装阶段已结束`).FindStringSubmatch(blocks[0])
	require(t, m != nil, "no core service in the cleanup block")
	marker := "\n)\nCORE_PHASE_COMPLETED=true\nCORE_BINARY="
	require(t, count(script, marker) == 1, "core phase marker count %d", count(script, marker))
	script = strings.Replace(script, marker, "\n)\n"+strings.Replace(d.PreviousCoreCleanup, "%s", m[1], 1)+"CORE_BINARY=", 1)
	return strings.Replace(script, d.StagedAgent,
		d.StagedAgent+"# The Agent cleanup trap above preserves the completed core phase.\n", 1)
}

// withoutReviewedInstallerDeltas is without_reviewed_installer_deltas(script, core_path).
func withoutReviewedInstallerDeltas(t *testing.T, script string, corePath bool) string {
	t.Helper()
	var err error
	script, err = installertest.PythonBaseline(script)
	if err != nil {
		t.Fatal(err)
	}
	d := deltas(t)
	n := count(script, d.NativeHADependencyCases)
	require(t, n >= 1, "the reviewed native HA dependency addition changed")
	require(t, count(script, d.NativeHADependencyCases+"        if ! command -v \"$agent_tool\"") == n, "HA case position")
	require(t, count(script, d.NativeHAPackageCase) == n, "HA package case")
	script = strings.ReplaceAll(strings.ReplaceAll(script, d.NativeHADependencyCases, ""), d.NativeHAPackageCase, "")
	require(t, count(script, "                cmp) agent_add_package diffutils ;;\n"+d.GatewayDependencyCases) == n, "gateway cases")
	require(t, count(script, d.ManualInstallCommand+"agent_install_packages() {") == n, "manual command")
	script = strings.ReplaceAll(strings.ReplaceAll(script, d.GatewayDependencyCases, ""), d.ManualInstallCommand, "")
	for _, p := range d.GatewayDependencyRewords {
		require(t, count(script, p[0]) == n, "reword %q", p[0])
		script = strings.ReplaceAll(script, p[0], p[1])
	}
	if corePath {
		require(t, count(script, "set -eu\n"+d.CoreInstallPath+"for cf_argument do\n") == 1, "core PATH position")
		script = strings.Replace(script, d.CoreInstallPath, "", 1)
		embedded := regexp.MustCompile(`\(\nset -- --core [a-z]+\n`+regexp.QuoteMeta(d.CoreAgentFollows)).FindAllString(script, -1)
		require(t, len(embedded) == 1, "the embedded core phase must not ask for a separate Agent")
		script = strings.Replace(script, embedded[0], strings.TrimSuffix(embedded[0], d.CoreAgentFollows), 1)
	}
	for _, table := range [][][2]string{d.OpenRCStopRetryDeltas, d.ResolvedStubPromptDeltas, d.InstallerHardeningDeltas} {
		for _, p := range table {
			require(t, count(script, p[0]) <= 1, "delta %q", p[0])
			script = strings.ReplaceAll(script, p[0], p[1])
		}
	}
	require(t, count(script, `readlink rm sed sleep wc "$@"; do`) == n, "sleep dependency")
	script = strings.ReplaceAll(script, `readlink rm sed sleep wc "$@"; do`, `readlink rm sed wc "$@"; do`)
	return withoutUnifiedAgentCleanup(t, script, corePath)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func singleServiceScript(t *testing.T, service string, installCore bool, forwarding Forwarding) string {
	return ok(GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "https://config.test", AgentName: "single",
		ServiceType: service, AgentIP: "192.0.2.20", BinaryDownloadURL: "https://config.test/api/agents/download",
		InstallCore: installCore, ManageIPv4Forwarding: forwarding}))
}

func digestKey(service string, installCore bool) string {
	if installCore {
		return service + ":True"
	}
	return service + ":False"
}

// singleServiceScriptDigests is single_service_script_digests().
func singleServiceScriptDigests(t *testing.T) map[string]string {
	out := map[string]string{}
	for _, c := range []struct {
		service string
		cores   []bool
	}{{"mihomo", []bool{false, true}}, {"oxidns", []bool{false, true}}, {"mosdns", []bool{false}}} {
		for _, core := range c.cores {
			out[digestKey(c.service, core)] = sha256Hex(withoutReviewedInstallerDeltas(t, singleServiceScript(t, c.service, core, ForwardingOff), core))
		}
	}
	return out
}

func TestReviewedDeltaPatternIsThePythonOne(t *testing.T) {
	// The Go matcher above spells out this pattern; fail if Python's changes.
	if deltas(t).UnifiedCleanupPattern != unifiedCleanupPattern {
		t.Fatalf("_UNIFIED_CLEANUP changed: %q", deltas(t).UnifiedCleanupPattern)
	}
}

func TestSingleServiceScriptsMatchThePreviousRelease(t *testing.T) {
	got := singleServiceScriptDigests(t)
	want := deltas(t).SingleServiceScriptSHA256
	if len(got) != len(want) {
		t.Fatalf("%v != %v", got, want)
	}
	for key, digest := range want {
		if got[key] != digest {
			t.Errorf("%s: %s != %s", key, got[key], digest)
		}
	}
}

func writeTemp(t *testing.T, name, text string) string {
	return writeFile(t, filepath.Join(t.TempDir(), name), text, 0o644)
}
