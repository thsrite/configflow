package installer

import (
	"bufio"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"configflow/internal/installer/installertest"
	"configflow/internal/jsonx"
)

// kwargs reads the Python keyword arguments of one matrix case.
type kwargs struct{ *jsonx.Object }

func (k kwargs) str(name, def string) string {
	v, ok := k.Get(name)
	if !ok || v == nil {
		return def
	}
	return v.(string)
}

func (k kwargs) boolean(name string, def bool) bool {
	v, ok := k.Get(name)
	if !ok {
		return def
	}
	return v.(bool)
}

// value is an int (*big.Int), str or bool argument; def when absent.
func (k kwargs) value(name string, def any) any {
	v, ok := k.Get(name)
	if !ok {
		return def
	}
	switch x := v.(type) {
	case jsonx.Number:
		n, _ := new(big.Int).SetString(string(x), 10)
		return n
	default:
		return x
	}
}

func renderCase(fn string, k kwargs) (string, error) {
	switch fn {
	case "with_install_output":
		return WithInstallOutput(k.str("script", "")), nil
	case "with_root_privileges":
		return WithRootPrivileges(k.str("script", "")), nil
	case "core":
		return GenerateCoreInstallScript(k.boolean("installer_forwarding", false))
	case "go_agent":
		forwarding := ForwardingOff
		if v, ok := k.Get("manage_ipv4_forwarding"); ok {
			switch v {
			case nil:
				forwarding = ForwardingAuto
			case true:
				forwarding = ForwardingOn
			}
		}
		return GenerateGoAgentInstallScript(GoAgentOptions{
			ServerURL: k.str("server_url", ""), AgentName: k.str("agent_name", ""), ServiceType: k.str("service_type", ""),
			AgentPort: k.value("agent_port", nil), AgentIP: k.str("agent_ip", ""), ConfigPath: k.str("config_path", ""),
			RestartCommand: k.str("restart_command", ""), BinaryDownloadURL: k.str("binary_download_url", ""),
			ServiceManager: k.str("service_manager", ""), ServiceUnit: k.str("service_unit", ""),
			ServiceBinary: k.str("service_binary", ""), StopCommand: k.str("stop_command", ""),
			StartCommand: k.str("start_command", ""), StatusCommand: k.str("status_command", ""),
			HealthURL: k.str("health_url", ""), HealthDNSAddress: k.str("health_dns_address", ""),
			HealthDNSName: k.str("health_dns_name", ""), DeploymentHealthTimeout: k.value("deployment_health_timeout", nil),
			InstallCore: k.boolean("install_core", false), ManageIPv4Forwarding: forwarding,
			PrepareDNSHA: k.boolean("prepare_dns_ha", false),
		})
	case "aio":
		o := NewAIOOptions(k.str("server_url", ""), k.str("agent_name", ""))
		o.AgentIP = k.str("agent_ip", "")
		o.BinaryDownloadURL = k.str("binary_download_url", "")
		o.MihomoAgentPort = k.value("mihomo_agent_port", o.MihomoAgentPort)
		o.OxidnsAgentPort = k.value("oxidns_agent_port", o.OxidnsAgentPort)
		o.MihomoConfigPath = k.str("mihomo_config_path", o.MihomoConfigPath)
		o.OxidnsConfigPath = k.str("oxidns_config_path", o.OxidnsConfigPath)
		o.MihomoServiceUnit = k.str("mihomo_service_unit", o.MihomoServiceUnit)
		o.OxidnsServiceUnit = k.str("oxidns_service_unit", o.OxidnsServiceUnit)
		o.MihomoServiceBinary = k.str("mihomo_service_binary", o.MihomoServiceBinary)
		o.OxidnsServiceBinary = k.str("oxidns_service_binary", o.OxidnsServiceBinary)
		o.InstallCore = k.boolean("install_core", false)
		o.NonInteractive = k.boolean("non_interactive", false)
		o.VerifiedAgentPath = k.str("verified_agent_path", "")
		o.InstallationID = k.str("installation_id", "")
		o.InstallationToken = k.str("installation_token", "")
		o.ManageIPv4Forwarding = k.boolean("manage_ipv4_forwarding", true)
		o.ManageTun = k.boolean("manage_tun", true)
		o.InstallationDescription = k.str("installation_description", o.InstallationDescription)
		o.TunPrepareAssumeEnabled = k.boolean("tun_prepare_assume_enabled", false)
		return GenerateGoAIOInstallScript(o)
	case "gateway":
		o := GatewayOptions{
			ServerURL: k.str("server_url", ""), AgentName: k.str("agent_name", ""), AgentIP: k.str("agent_ip", ""),
			BinaryDownloadURL: k.str("binary_download_url", ""), MihomoAgentPort: k.value("mihomo_agent_port", nil),
			OxidnsAgentPort: k.value("oxidns_agent_port", nil), EnrollmentURL: k.str("enrollment_url", ""),
			EnrollmentToken: k.str("enrollment_token", ""), InstallationID: k.str("installation_id", ""),
			FullTun: k.boolean("full_tun", false),
		}
		if checksums := k.GetObject("agent_checksums"); checksums != nil {
			o.AgentChecksums = map[string]string{}
			for key, value := range checksums.All() {
				o.AgentChecksums[key] = value.(string)
			}
		}
		return GenerateGatewayInstallScript(o)
	case "compose", "run":
		o := NewDockerOptions(k.str("server_url", ""))
		o.AgentName = k.str("agent_name", o.AgentName)
		o.AgentIP = k.str("agent_ip", "")
		o.DataDir = k.str("data_dir", o.DataDir)
		o.NetworkMode = k.str("network_mode", o.NetworkMode)
		o.EnableMihomo = k.boolean("enable_mihomo", true)
		o.EnableMosdns = k.boolean("enable_mosdns", false)
		o.EnableOxidns = k.boolean("enable_oxidns", false)
		o.MihomoPort = k.value("mihomo_port", o.MihomoPort)
		o.MosdnsPort = k.value("mosdns_port", o.MosdnsPort)
		o.OxidnsPort = k.value("oxidns_port", o.OxidnsPort)
		o.EnableDNSHA = k.boolean("enable_dns_ha", false)
		o.HAParent = k.str("ha_parent", "")
		o.HASubnet = k.str("ha_subnet", "")
		o.HAGateway = k.str("ha_gateway", "")
		o.HAHostIDPath = k.str("ha_host_id_path", o.HAHostIDPath)
		if fn == "compose" {
			return GenerateDockerAgentCompose(o)
		}
		return GenerateDockerAgentRun(o)
	}
	panic("unknown generator " + fn)
}

func errorText(err error) string {
	var v *ValueError
	if errors.As(err, &v) {
		return "ValueError: " + v.Message
	}
	return "error: " + err.Error()
}

func readJSONLines(t *testing.T, path string) []*jsonx.Object {
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<20), 64<<20)
	var out []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, value.(*jsonx.Object))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func firstDifference(a, b string) string {
	la, lb := strings.SplitAfter(a, "\n"), strings.SplitAfter(b, "\n")
	for i := 0; i < len(la) || i < len(lb); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n  python %q\n  go     %q", i+1, x, y)
		}
	}
	return "no line differs"
}

// TestRenderMatrixMatchesPython replays testdata/matrix.jsonl (made by
// testdata/gen_matrix.py with the Python generators): every rendered script
// must have the same SHA256 and length, every refusal the same ValueError.
func TestRenderMatrixMatchesPython(t *testing.T) {
	t.Parallel()
	cases := readJSONLines(t, "testdata/matrix.jsonl")
	full := map[int64]string{}
	if path := os.Getenv("INSTALLER_MATRIX_FULL"); path != "" {
		for _, record := range readJSONLines(t, path) {
			id := record.GetInt("id")
			full[id] = record.GetString("output")
		}
	}
	rendered, refused, deliberate := 0, 0, 0
	for _, record := range cases {
		id := record.GetInt("id")
		fn := record.GetString("fn")
		k := kwargs{record.GetObject("kwargs")}
		output, err := renderCase(fn, k)
		if handled, problem := n810ScalarMatch(record, output, err); handled {
			if problem != "" {
				t.Errorf("case %d approved integer policy: %s", id, problem)
			}
			continue
		}
		if err != nil && deliberateAgentNameFix(k.Object, err) {
			deliberate++ // decision 6: one trailing "\n" in agent_name is refused in Go
			continue
		}
		if !record.GetBool("ok") {
			refused++
			if err == nil && urllibOnlyRefusal(record.GetString("error")) {
				// accepted N2 class: urllib-only host refusal
			} else if err == nil {
				t.Errorf("case %d %s %s: python refused (%s), go rendered", id, fn, jsonx.Repr(k.Object), record.GetString("error"))
			} else if got := errorText(err); !sameRefusal(record.GetString("error"), got, k.Object) {
				t.Errorf("case %d %s %s:\n  python %s\n  go     %s", id, fn, jsonx.Repr(k.Object), record.GetString("error"), got)
			}
			continue
		}
		rendered++
		if err != nil && refusedByNetURL(k.Object) {
			continue // N2: a URL net/url refuses
		}
		if err != nil {
			t.Errorf("case %d %s %s: python rendered, go: %v", id, fn, jsonx.Repr(k.Object), err)
			continue
		}
		output, err = installertest.PythonBaseline(output)
		if err != nil {
			t.Fatal(err)
		}
		matches, oracleErr := writerHashCaseMatches(fn, k.Object, output, record.GetString("sha256"), record.GetInt("length"))
		if oracleErr != nil {
			t.Fatal(oracleErr)
		}
		if !matches {
			detail := "(set INSTALLER_MATRIX_FULL for the first differing line)"
			if py, ok := full[id]; ok {
				detail = firstDifference(py, output)
			}
			t.Errorf("case %d %s %s: output differs %s", id, fn, jsonx.Repr(k.Object), detail)
		}
	}
	t.Logf("matrix: %d cases, %d rendered, %d refused, %d deliberate (decision 6)", len(cases), rendered, refused, deliberate)
}

// TestWriteShellcheckSample writes the Go renderings of the cases
// testdata/shellcheck_sample.py picks (INSTALLER_SAMPLE_DIR/go/<source>-<id>.sh)
// for the shellcheck comparison of the A08 review.
func TestWriteShellcheckSample(t *testing.T) {
	dir := os.Getenv("INSTALLER_SAMPLE_DIR")
	if dir == "" {
		t.Skip("set INSTALLER_SAMPLE_DIR")
	}
	shell := map[string]bool{"go_agent": true, "aio": true, "gateway": true, "core": true, "run": true, "with_install_output": true}
	if err := os.MkdirAll(dir+"/go", 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, records []*jsonx.Object, step int) {
		var picked []*jsonx.Object
		for _, r := range records {
			if r.GetBool("ok") && shell[r.GetString("fn")] {
				picked = append(picked, r)
			}
		}
		for i := 0; i < len(picked); i += step {
			r := picked[i]
			out, err := renderCase(r.GetString("fn"), kwargs{r.GetObject("kwargs")})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fmt.Sprintf("%s/go/%s-%d.sh", dir, name, r.GetInt("id")), []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("matrix", readJSONLines(t, "testdata/matrix.jsonl"), 4)
	write("fuzz", readGzipJSONLines(t, "testdata/fuzz.jsonl.gz"), 40)
}
