package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Port of backend/test_oxidns_docker_install.py (generator cases; the API
// case is in internal/api/agents). Cases testing docker/ entrypoint,
// supervisor and keepalived files stay with the image (finish domain, #40):
// test_entrypoint_rejects_ha_for_other_cores_before_creating_service_config,
// test_keepalived_program_has_separate_shutdown_and_no_automatic_activation,
// test_companion_entrypoint_writes_private_evidence_without_starting_core,
// test_companion_supervisor_socket_cannot_control_core_or_agent.

func haParams(mode string) DockerOptions {
	o := NewDockerOptions("https://config.example")
	o.AgentName, o.AgentIP, o.EnableMihomo, o.EnableOxidns, o.EnableDNSHA, o.NetworkMode = "dns-a", "192.168.50.20", false, true, true, mode
	if mode == "macvlan" {
		o.HAParent, o.HASubnet, o.HAGateway = "eth0", "192.168.50.0/24", "192.168.50.1"
	}
	return o
}

func loadYAML(t *testing.T, text string) *jsonx.Object {
	v, err := yamlx.SafeLoad(text)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object)
}

func strs(v any) []string {
	var out []string
	list, _ := jsonx.AsList(v)
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestHostComposeSeparatesCoreAndHAAuthority(t *testing.T) {
	compose := loadYAML(t, ok(GenerateDockerAgentCompose(haParams("host"))))
	services := compose.GetObject("services")
	main, companion := services.GetObject("agent"), services.GetObject("ha-companion")
	if main.GetString("network_mode") != "host" || companion.GetString("network_mode") != "host" || main.Has("cap_add") {
		t.Fatal(main)
	}
	caps := strs(companion.Value("cap_add"))
	slices.Sort(caps)
	if strings.Join(caps, ",") != "NET_ADMIN,NET_RAW" || strings.Join(strs(main.Value("depends_on")), ",") != "ha-companion" {
		t.Error("caps/depends")
	}
	if !slices.Contains(strs(companion.Value("environment")), "SERVICE_TYPE=ha-companion") {
		t.Error("companion env")
	}
	for _, service := range []*jsonx.Object{main, companion} {
		env := strs(service.Value("environment"))
		if service.Has("ports") || service.Has("privileged") || !slices.Contains(env, "CONFIGFLOW_DNS_HA=1") ||
			!slices.Contains(env, "CONFIGFLOW_DNS_HA_NETWORK=host") || !slices.Contains(strs(service.Value("volumes")), "./agent_data/ha:/var/lib/configflow-ha") {
			t.Error("service")
		}
		found := false
		for _, item := range service.GetList("volumes") {
			if o, isObj := item.(*jsonx.Object); isObj && strings.HasSuffix(o.GetString("target"), "machine-id") {
				found = o.Value("read_only") == true && o.GetObject("bind").Value("create_host_path") == false
			}
			text := jsonx.Repr(item)
			if strings.Contains(text, "docker.sock") || strings.Contains(text, "/etc:/etc") {
				t.Error(text)
			}
		}
		if !found {
			t.Error("identity")
		}
	}
	cv, mv := strs(companion.Value("volumes")), strs(main.Value("volumes"))
	if !slices.Contains(cv, "./agent_data/agent:/opt/configflow-agent:ro") || !slices.Contains(cv, "./agent_data/oxidns:/etc/oxidns:ro") ||
		!slices.Contains(mv, "./agent_data/agent:/opt/configflow-agent") {
		t.Error("volumes")
	}
	netns := false
	for _, item := range main.GetList("volumes") {
		if o, isObj := item.(*jsonx.Object); isObj && o.GetString("source") == "/proc/1/ns/net" && o.Value("read_only") == true {
			netns = true
		}
	}
	if !netns || strings.Contains(jsonx.Repr(companion.Value("volumes")), "host-netns") {
		t.Error("netns")
	}
}

func TestMacvlanComposeHasOneIsolatedStaticIPAndNoCompanion(t *testing.T) {
	compose := loadYAML(t, ok(GenerateDockerAgentCompose(haParams("macvlan"))))
	if strings.Join(compose.GetObject("services").Keys(), ",") != "agent" {
		t.Fatal("services")
	}
	main := compose.GetObject("services").GetObject("agent")
	if main.Has("network_mode") || main.Has("ports") || !jsonx.Equal(main.Value("networks"), jsonx.Obj("dns-ha", jsonx.Obj("ipv4_address", "192.168.50.20"))) {
		t.Error("main")
	}
	if !slices.Contains(strs(main.Value("environment")), "CONFIGFLOW_DNS_HA_NETWORK=macvlan") {
		t.Error("env")
	}
	network := compose.GetObject("networks").GetObject("dns-ha")
	if network.GetString("driver") != "macvlan" || !jsonx.Equal(network.Value("driver_opts"), jsonx.Obj("parent", "eth0")) ||
		!jsonx.Equal(network.GetObject("ipam").Value("config"), []any{jsonx.Obj("subnet", "192.168.50.0/24", "gateway", "192.168.50.1")}) {
		t.Error("network")
	}
	if strings.Contains(jsonx.Repr(main.Value("volumes")), "host-netns") {
		t.Error("netns")
	}
}

func TestHAGeneratorRejectsIncompatibleOrUnsafeInputs(t *testing.T) {
	changes := []func(*DockerOptions){
		func(o *DockerOptions) { o.NetworkMode = "bridge" }, func(o *DockerOptions) { o.EnableMihomo = true },
		func(o *DockerOptions) { o.EnableMosdns = true }, func(o *DockerOptions) { o.EnableOxidns = false },
		func(o *DockerOptions) { o.AgentIP = "" }, func(o *DockerOptions) { o.AgentIP = "127.0.0.1" },
		func(o *DockerOptions) { o.AgentIP = "224.0.0.1" }, func(o *DockerOptions) { o.AgentIP = "0.0.0.0" },
		func(o *DockerOptions) { o.HAHostIDPath = "relative" }, func(o *DockerOptions) { o.HAHostIDPath = "/tmp/id,readonly" },
	}
	for i, change := range changes {
		for _, generate := range []func(DockerOptions) (string, error){GenerateDockerAgentCompose, GenerateDockerAgentRun} {
			o := haParams("host")
			change(&o)
			if _, err := generate(o); err == nil {
				t.Errorf("change %d accepted", i)
			}
		}
	}
}

func TestMacvlanGeneratorRejectsMismatchedAddressing(t *testing.T) {
	for i, change := range []func(*DockerOptions){
		func(o *DockerOptions) { o.HAParent = "eth0;bad" }, func(o *DockerOptions) { o.HASubnet = "192.168.50.3/24" },
		func(o *DockerOptions) { o.HASubnet = "192.168.50.20/32" }, func(o *DockerOptions) { o.HAGateway = "192.168.60.1" },
		func(o *DockerOptions) { o.AgentIP = "192.168.50.0" }, func(o *DockerOptions) { o.AgentIP = "192.168.50.255" },
		func(o *DockerOptions) { o.AgentIP = "192.168.60.20" }, func(o *DockerOptions) { o.AgentIP = "192.168.50.1" },
	} {
		o := haParams("macvlan")
		change(&o)
		if _, err := GenerateDockerAgentCompose(o); err == nil {
			t.Errorf("change %d accepted", i)
		}
	}
}

func TestGeneratedRunExecutesLiteralArgumentsAndStartsOnlyExpectedServices(t *testing.T) {
	python := pythonExe(t)
	for _, mode := range []string{"host", "macvlan"} {
		tmp := t.TempDir()
		identity := writeFile(t, filepath.Join(tmp, "host identity"), "unique-test-host\n", 0o644)
		writeFile(t, filepath.Join(tmp, "docker"), "#!"+python+"\nimport json, os, sys\n"+
			"with open(os.environ[\"DOCKER_EVENTS\"], \"a\") as output: output.write(json.dumps(sys.argv[1:]) + \"\\n\")\n", 0o755)
		events := filepath.Join(tmp, "events")
		marker := filepath.Join(tmp, "must-not-execute")
		url := `https://config.test/"$(touch ` + marker + `)`
		o := haParams(mode)
		o.ServerURL, o.DataDir, o.HAHostIDPath = url, filepath.Join(tmp, "data ' folder"), identity
		script := writeFile(t, filepath.Join(tmp, "install.sh"), ok(GenerateDockerAgentRun(o)), 0o644)
		r := runShell(t, "bash", script, runOptions{env: []string{"PATH=" + tmp + ":" + os.Getenv("PATH"), "DOCKER_EVENTS=" + events}})
		if r.code != 0 {
			t.Fatal(r.stderr + r.stdout)
		}
		var calls [][]string
		for _, line := range strings.Split(strings.TrimSpace(readFile(t, events)), "\n") {
			var call []string
			json.Unmarshal([]byte(line), &call)
			calls = append(calls, call)
		}
		if exists(marker) || len(calls) != 2 {
			t.Fatalf("%s: calls %v", mode, calls)
		}
		main := calls[1]
		if !slices.Contains(main, "SERVER_URL="+url) || !slices.Contains(main, "type=bind,src="+identity+",dst=/run/configflow-host-machine-id,readonly") {
			t.Error("main args")
		}
		if mode == "host" {
			oxidnsRO := false
			for _, v := range calls[0] {
				oxidnsRO = oxidnsRO || strings.HasSuffix(v, ":/etc/oxidns:ro")
			}
			if !slices.Contains(calls[0], "SERVICE_TYPE=ha-companion") || !slices.Contains(calls[0], "--cap-add") || slices.Contains(main, "--cap-add") || !oxidnsRO {
				t.Error("companion")
			}
		} else if strings.Join(calls[0][:3], " ") != "network create --driver" || slices.Contains(calls[0], "SERVICE_TYPE=ha-companion") ||
			main[slices.Index(main, "--ip")+1] != "192.168.50.20" {
			t.Error("macvlan")
		}
	}
}

func TestOrdinaryDockerKeepsOriginalNetworkAndPrivileges(t *testing.T) {
	for _, mode := range []string{"host", "bridge", "existing-private-network"} {
		for _, services := range [][3]bool{{true, false, false}, {false, true, false}, {false, false, true}, {true, false, true}} {
			o := NewDockerOptions("https://config.example")
			o.NetworkMode, o.EnableMihomo, o.EnableMosdns, o.EnableOxidns = mode, services[0], services[1], services[2]
			implicit := ok(GenerateDockerAgentCompose(o))
			compose := loadYAML(t, implicit)
			if strings.Join(compose.GetObject("services").Keys(), ",") != "agent" || compose.Has("networks") {
				t.Fatal("services")
			}
			service := compose.GetObject("services").GetObject("agent")
			if service.GetString("network_mode") != mode || service.Has("ports") != (mode == "bridge") || service.Has("cap_add") || service.Has("privileged") {
				t.Error("service")
			}
			for _, item := range strs(service.Value("environment")) {
				if strings.Contains(item, "CONFIGFLOW_DNS_HA") {
					t.Error(item)
				}
			}
			volumes := jsonx.Repr(service.Value("volumes"))
			if strings.Contains(volumes, "configflow-ha") || strings.Contains(volumes, "machine-id") {
				t.Error(volumes)
			}
			command := ok(GenerateDockerAgentRun(o))
			contains(t, command, "--network "+mode)
			notContains(t, command, "--cap-add")
			notContains(t, command, "CONFIGFLOW_DNS_HA")
		}
	}
}

func TestGeneratedGoInstallerDoesNotPrintBinaryDownloadURL(t *testing.T) {
	// Deferred case from test_safe_url_queries.py (C02).
	script := ok(GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "https://server.test/?token=server-secret", AgentName: "probe",
		ServiceType: "mihomo", BinaryDownloadURL: "https://download.test/bin?token=installer-secret#key=fragment-secret"}))
	notContains(t, script, "URL: $BINARY_URL")
	contains(t, script, `wget -q -O "$BINARY_TMP" "$BINARY_URL"`)
	contains(t, script, `curl -fsL -o "$BINARY_TMP" "$BINARY_URL"`)
}
