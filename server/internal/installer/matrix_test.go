package installer

import (
	"bufio"
	"configflow/internal/textx"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"configflow/internal/installer/installertest"
)

// kwargs reads the Python keyword arguments of one matrix case.
type kwargs struct{ m map[string]any }

func (k kwargs) str(name, def string) string {
	v, ok := k.m[name]
	if !ok || v == nil {
		return def
	}
	return v.(string)
}

func (k kwargs) boolean(name string, def bool) bool {
	v, ok := k.m[name]
	if !ok {
		return def
	}
	return v.(bool)
}

// value is an int (*big.Int), str or bool argument; def when absent.
func (k kwargs) value(name string, def any) any {
	v, ok := k.m[name]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case json.Number:
		n, _ := new(big.Int).SetString(string(x), 10)
		return n
	default:
		return x
	}
}

func testCompatInteger(value any, fallback int64) (*big.Int, error) {
	if value == nil {
		return big.NewInt(fallback), nil
	}
	switch n := value.(type) {
	case int:
		return big.NewInt(int64(n)), nil
	case int64:
		return big.NewInt(n), nil
	case *big.Int:
		if n != nil {
			return new(big.Int).Set(n), nil
		}
	case bool:
		if n {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case string:
		return parseInstallInteger(n)
	}
	return nil, invalidInput("Invalid installer integer")
}

func testCompatPort(value any, fallback int64) (int, error) {
	n, err := testCompatInteger(value, fallback)
	if err != nil {
		return 0, err
	}
	if !inRange(n, 1, 65535) {
		return 0, invalidInput("Invalid Agent port")
	}
	return int(n.Int64()), nil
}

func testCompatIntegerText(value any, fallback int64) (string, error) {
	if boolean, ok := value.(bool); ok {
		if boolean {
			return "True", nil
		}
		return "False", nil
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	n, err := testCompatInteger(value, fallback)
	if err != nil {
		return "", err
	}
	return n.String(), nil
}

func testCompatDecimalPort(value any) bool {
	switch n := value.(type) {
	case string:
		return textx.ASCIIDigits(n)
	case bool:
		return false
	case int:
		return n >= 0
	case int64:
		return n >= 0
	case *big.Int:
		return n != nil && n.Sign() >= 0
	}
	return false
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
		if v, ok := k.m["manage_ipv4_forwarding"]; ok {
			switch v {
			case nil:
				forwarding = ForwardingAuto
			case true:
				forwarding = ForwardingOn
			}
		}
		rawPort := k.value("agent_port", nil)
		port, portErr := testCompatPort(rawPort, 8080)
		rawTimeout := k.value("deployment_health_timeout", nil)
		timeout, timeoutErr := testCompatInteger(rawTimeout, 30)
		opts := AgentInstallOptions{
			ServerURL: k.str("server_url", ""), AgentName: k.str("agent_name", ""), ServiceType: k.str("service_type", ""),
			AgentPort: port, AgentIP: k.str("agent_ip", ""), ConfigPath: k.str("config_path", ""),
			RestartCommand: k.str("restart_command", ""), BinaryDownloadURL: k.str("binary_download_url", ""),
			ServiceManager: k.str("service_manager", ""), ServiceUnit: k.str("service_unit", ""),
			ServiceBinary: k.str("service_binary", ""), StopCommand: k.str("stop_command", ""),
			StartCommand: k.str("start_command", ""), StatusCommand: k.str("status_command", ""),
			HealthURL: k.str("health_url", ""), HealthDNSAddress: k.str("health_dns_address", ""),
			HealthDNSName: k.str("health_dns_name", ""), DeploymentHealthTimeout: timeout,
			InstallCore: k.boolean("install_core", false), ManageIPv4Forwarding: forwarding,
			PrepareDNSHA: k.boolean("prepare_dns_ha", false),
		}
		if portErr != nil {
			opts.AgentPort = 8080
			if _, err := RenderAgentInstall(opts); err != nil && (opts.ServiceType != "mihomo" && opts.ServiceType != "mosdns" && opts.ServiceType != "oxidns" ||
				(opts.PrepareDNSHA && opts.ServiceType != "oxidns") || !matchesClass(opts.AgentName, "_-")) {
				return "", err
			}
			return "", portErr
		}
		if opts.InstallCore && rawPort != nil && !testCompatDecimalPort(rawPort) {
			if _, err := RenderAgentInstall(opts); err != nil && err.Error() != msgCoreDNSPortConflict {
				return "", err
			}
			return "", invalidInput("Invalid Agent port")
		}
		if timeoutErr != nil {
			opts.DeploymentHealthTimeout = big.NewInt(30)
			if _, err := RenderAgentInstall(opts); err != nil {
				return "", err
			}
			return "", timeoutErr
		}
		return RenderAgentInstall(opts)
	case "aio":
		o := DefaultAIOOptions(k.str("server_url", ""), k.str("agent_name", ""))
		o.AgentIP = k.str("agent_ip", "")
		o.BinaryDownloadURL = k.str("binary_download_url", "")
		rawMihomo := k.value("mihomo_agent_port", o.MihomoAgentPort)
		rawOxidns := k.value("oxidns_agent_port", o.OxidnsAgentPort)
		mihomoPort, portErr := testCompatPort(rawMihomo, 8080)
		oxidnsPort, secondErr := testCompatPort(rawOxidns, 8082)
		if rawMihomo != nil && !testCompatDecimalPort(rawMihomo) {
			portErr = invalidInput("Invalid Agent port")
		}
		if rawOxidns != nil && !testCompatDecimalPort(rawOxidns) {
			secondErr = invalidInput("Invalid Agent port")
		}
		if portErr == nil {
			portErr = secondErr
		}
		o.MihomoAgentPort = mihomoPort
		o.OxidnsAgentPort = oxidnsPort
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
		if portErr != nil {
			o.MihomoAgentPort, o.OxidnsAgentPort = 8080, 8082
			if _, err := RenderAIOInstall(o); err != nil &&
				(o.TunPrepareAssumeEnabled && !(o.ManageTun && o.VerifiedAgentPath != "") ||
					o.VerifiedAgentPath != "" && o.VerifiedAgentPath != "$GATEWAY_STAGE/configflow-agent" ||
					o.VerifiedAgentPath != "" && !o.NonInteractive ||
					(o.InstallationID != "") != (o.InstallationToken != "") ||
					o.InstallationID != "" && !isInstallationID(o.InstallationID) ||
					o.InstallationToken != "" && !IsSessionToken(o.InstallationToken) ||
					!matchesClass(o.AgentName, "_-")) {
				return "", err
			}
			return "", portErr
		}
		return RenderAIOInstall(o)
	case "gateway":
		rawMihomo := k.value("mihomo_agent_port", nil)
		rawOxidns := k.value("oxidns_agent_port", nil)
		mihomoPort, portErr := testCompatPort(rawMihomo, 8080)
		oxidnsPort, secondErr := testCompatPort(rawOxidns, 8082)
		if rawMihomo != nil && !testCompatDecimalPort(rawMihomo) {
			portErr = invalidInput("Invalid Agent port")
		}
		if rawOxidns != nil && !testCompatDecimalPort(rawOxidns) {
			secondErr = invalidInput("Invalid Agent port")
		}
		if portErr == nil {
			portErr = secondErr
		}
		o := GatewayInstallOptions{
			ServerURL: k.str("server_url", ""), AgentName: k.str("agent_name", ""), AgentIP: k.str("agent_ip", ""),
			BinaryDownloadURL: k.str("binary_download_url", ""), MihomoAgentPort: mihomoPort,
			OxidnsAgentPort: oxidnsPort, EnrollmentURL: k.str("enrollment_url", ""),
			EnrollmentToken: k.str("enrollment_token", ""), InstallationID: k.str("installation_id", ""),
			FullTun: k.boolean("full_tun", false),
		}
		if checksums := mapObj(k.m, "agent_checksums"); checksums != nil {
			o.AgentChecksums = map[string]string{}
			for key, value := range checksums {
				o.AgentChecksums[key] = value.(string)
			}
		}
		if portErr != nil {
			o.MihomoAgentPort, o.OxidnsAgentPort = 8080, 8082
			if _, err := RenderGatewayInstall(o); err != nil {
				return "", err
			}
			return "", portErr
		}
		return RenderGatewayInstall(o)
	case "compose", "run":
		o := DefaultDockerOptions(k.str("server_url", ""))
		o.AgentName = k.str("agent_name", o.AgentName)
		o.AgentIP = k.str("agent_ip", "")
		o.DataDir = k.str("data_dir", o.DataDir)
		o.NetworkMode = k.str("network_mode", o.NetworkMode)
		o.EnableMihomo = k.boolean("enable_mihomo", true)
		o.EnableMosdns = k.boolean("enable_mosdns", false)
		o.EnableOxidns = k.boolean("enable_oxidns", false)
		var err error
		if o.MihomoPort, err = testCompatIntegerText(k.value("mihomo_port", o.MihomoPort), 8080); err != nil {
			return "", err
		}
		if o.MosdnsPort, err = testCompatIntegerText(k.value("mosdns_port", o.MosdnsPort), 8081); err != nil {
			return "", err
		}
		if o.OxidnsPort, err = testCompatIntegerText(k.value("oxidns_port", o.OxidnsPort), 8082); err != nil {
			return "", err
		}
		o.EnableDNSHA = k.boolean("enable_dns_ha", false)
		o.HAParent = k.str("ha_parent", "")
		o.HASubnet = k.str("ha_subnet", "")
		o.HAGateway = k.str("ha_gateway", "")
		o.HAHostIDPath = k.str("ha_host_id_path", o.HAHostIDPath)
		if fn == "compose" {
			return RenderDockerCompose(o)
		}
		return RenderDockerRun(o)
	}
	panic("unknown generator " + fn)
}

func errorText(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return "ValueError: " + v.Message
	}
	return "error: " + err.Error()
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<20), 64<<20)
	var out []map[string]any
	for scanner.Scan() {
		value, err := parseJSONMap(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, value)
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
			id := mapInt(record, "id")
			full[id] = mapStr(record, "output")
		}
	}
	rendered, refused, deliberate := 0, 0, 0
	for _, record := range cases {
		id := mapInt(record, "id")
		fn := mapStr(record, "fn")
		k := kwargs{mapObj(record, "kwargs")}
		output, err := renderCase(fn, k)
		if handled, problem := n810ScalarMatch(record, output, err); handled {
			if problem != "" {
				t.Errorf("case %d approved integer policy: %s", id, problem)
			}
			continue
		}
		if err != nil && deliberateAgentNameFix(k.m, err) {
			deliberate++ // decision 6: one trailing "\n" in agent_name is refused in Go
			continue
		}
		if !mapBool(record, "ok") {
			refused++
			if err == nil && urllibOnlyRefusal(mapStr(record, "error")) {
				// accepted N2 class: urllib-only host refusal
			} else if err == nil {
				t.Errorf("case %d %s %v: python refused (%s), go rendered", id, fn, k.m, mapStr(record, "error"))
			} else if got := errorText(err); !sameRefusal(mapStr(record, "error"), got, k.m) {
				t.Errorf("case %d %s %v:\n  python %s\n  go     %s", id, fn, k.m, mapStr(record, "error"), got)
			}
			continue
		}
		rendered++
		if err != nil && refusedByNetURL(k.m) {
			continue // N2: a URL net/url refuses
		}
		if err != nil {
			t.Errorf("case %d %s %v: python rendered, go: %v", id, fn, k.m, err)
			continue
		}
		output, err = installertest.PythonBaseline(output)
		if err != nil {
			t.Fatal(err)
		}
		matches, oracleErr := writerHashCaseMatches(fn, k.m, output, mapStr(record, "sha256"), mapInt(record, "length"))
		if oracleErr != nil {
			t.Fatal(oracleErr)
		}
		if !matches {
			detail := "(set INSTALLER_MATRIX_FULL for the first differing line)"
			if py, ok := full[id]; ok {
				detail = firstDifference(py, output)
			}
			t.Errorf("case %d %s %v: output differs %s", id, fn, k.m, detail)
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
	write := func(name string, records []map[string]any, step int) {
		var picked []map[string]any
		for _, r := range records {
			if mapBool(r, "ok") && shell[mapStr(r, "fn")] {
				picked = append(picked, r)
			}
		}
		for i := 0; i < len(picked); i += step {
			r := picked[i]
			out, err := renderCase(mapStr(r, "fn"), kwargs{mapObj(r, "kwargs")})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fmt.Sprintf("%s/go/%s-%d.sh", dir, name, mapInt(r, "id")), []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("matrix", readJSONLines(t, "testdata/matrix.jsonl"), 4)
	write("fuzz", readGzipJSONLines(t, "testdata/fuzz.jsonl.gz"), 40)
}
