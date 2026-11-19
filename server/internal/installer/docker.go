package installer

import (
	"strings"

	"configflow/internal/yamlx"
)

// Port of backend/agents/install_script.py: the Docker Compose file and the
// Docker run installer of the Docker Agent.

// DockerInstallOptions contains the native Docker renderer input.
// Port strings preserve the selected machine values in generated commands.
type DockerInstallOptions struct {
	RegistrationTokens map[string]string
	ServerURL          string
	AgentName          string
	AgentIP            string
	DataDir            string
	NetworkMode        string
	EnableMihomo       bool
	EnableMosdns       bool
	MihomoPort         string
	MosdnsPort         string
	EnableOxidns       bool
	OxidnsPort         string
	EnableDNSHA        bool
	HAParent           string
	HASubnet           string
	HAGateway          string
	HAHostIDPath       string
}

// DefaultDockerOptions returns the standard Docker settings.
func DefaultDockerOptions(serverURL string) DockerInstallOptions {
	return DockerInstallOptions{
		ServerURL: serverURL, AgentName: "agent", DataDir: "./agent_data", NetworkMode: "host",
		EnableMihomo: true, MihomoPort: "8080", MosdnsPort: "8081", OxidnsPort: "8082",
		HAHostIDPath: "/etc/machine-id",
	}
}

// ValidateAgentName is validate_agent_name with re.fullmatch semantics.
// Deliberate fix of a Python validation bug (coordinator decision 6,
// 2026-11-16): Python's re.match(r'^[a-zA-Z0-9_-]+$') lets one trailing
// "\n" through into the Docker snippets; Go refuses it with the same error
// as any other invalid name.
func ValidateAgentName(name string) error {
	if !matchesClass(name, "_-") {
		return valueError("Invalid agent name: only alphanumeric characters, hyphens, and underscores are allowed.")
	}
	return nil
}

// haNetwork is the macvlan network of _docker_ha_network (nil for host).
type haNetwork struct {
	name, parent, subnet, gateway string
}

func (n *haNetwork) object() *composeNetwork {
	return &composeNetwork{Name: n.name, Driver: "macvlan", DriverOpts: composeDriverOpts{n.parent},
		IPAM: composeIPAM{[]composeSubnet{{Subnet: n.subnet, Gateway: n.gateway}}}}
}

func isHAParent(parent string) bool {
	// re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}", ha_parent)
	return len(parent) >= 1 && len(parent) <= 15 && matchesClass(parent[:1], "_") &&
		(len(parent) == 1 || matchesClass(parent[1:], "_.-"))
}

// dockerHANetwork is _docker_ha_network.
func dockerHANetwork(o DockerInstallOptions) (*haNetwork, error) {
	if !o.EnableOxidns || o.EnableMihomo || o.EnableMosdns {
		return nil, valueError(msgDockerHAOxidnsOnly)
	}
	if o.NetworkMode != "host" && o.NetworkMode != "macvlan" {
		return nil, valueError(msgDockerHANetwork)
	}
	address, err := parseIPv4(o.AgentIP)
	if err != nil || isLoopback(address) || isMulticast(address) || isLinkLocal(address) || isUnspecified(address) || isReserved(address) {
		return nil, valueError(msgFixedIPv4)
	}
	if o.NetworkMode == "host" {
		return nil, nil
	}
	if !isHAParent(o.HAParent) {
		return nil, valueError(msgHAParent)
	}
	invalid := valueError(msgHASubnet)
	subnet, err := parseIPv4Network(o.HASubnet)
	if err != nil {
		return nil, invalid
	}
	var gateway *uint32
	if o.HAGateway != "" {
		g, err := parseIPv4(o.HAGateway)
		if err != nil {
			return nil, invalid
		}
		gateway = &g
	}
	if subnet.prefix > 30 {
		return nil, invalid
	}
	for _, ip := range []uint32{subnet.address, address} {
		if isLoopback(ip) || isMulticast(ip) || isLinkLocal(ip) || isUnspecified(ip) {
			return nil, invalid
		}
	}
	for _, ip := range []*uint32{&address, gateway} {
		if ip != nil && (!subnet.contains(*ip) || *ip == subnet.address || *ip == subnet.broadcast()) {
			return nil, invalid
		}
	}
	if gateway != nil && address == *gateway {
		return nil, invalid
	}
	network := &haNetwork{name: o.AgentName + "-oxidns-ha-net", parent: o.HAParent, subnet: subnet.String()}
	if gateway != nil {
		network.gateway = ipv4Text(*gateway)
	}
	return network, nil
}

// dockerHAIdentity is _docker_ha_identity(path).
func dockerHAIdentity(path string) (composeMount, error) {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, ",:\x00\r\n") {
		return composeMount{}, valueError(msgDockerIdentityPath)
	}
	return bindMount(path, "/run/configflow-host-machine-id", true, false), nil
}

func checkHAPort(port string) error {
	n, err := parseInstallInteger(port)
	if err != nil || !inRange(n, 1024, 65535) {
		return valueError(msgDockerHAPort)
	}
	return nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// RenderDockerCompose renders the selected services as native Compose YAML.
func RenderDockerCompose(o DockerInstallOptions) (string, error) {
	document, err := dockerCompose(o)
	if err != nil {
		return "", err
	}
	return composeYAML(document, false)
}

func dockerCompose(o DockerInstallOptions) (*composeDocument, error) {
	if err := ValidateAgentName(o.AgentName); err != nil {
		return nil, err
	}
	var network *haNetwork
	var identity composeMount
	if o.EnableDNSHA {
		var err error
		if network, err = dockerHANetwork(o); err != nil {
			return nil, err
		}
		if err := checkHAPort(o.OxidnsPort); err != nil {
			return nil, err
		}
		if identity, err = dockerHAIdentity(o.HAHostIDPath); err != nil {
			return nil, err
		}
	}
	if o.EnableMosdns && o.EnableOxidns {
		return nil, valueError(msgDNSServiceConflict)
	}
	var services []string
	if o.EnableMihomo {
		services = append(services, "mihomo")
	}
	if o.EnableMosdns {
		services = append(services, "mosdns")
	}
	if o.EnableOxidns {
		services = append(services, "oxidns")
	}
	suffix := "agent"
	if len(services) > 0 {
		suffix = strings.Join(services, "-")
	}
	containerName := o.AgentName + "-" + suffix
	mihomoPort, mosdnsPort, oxidnsPort := o.MihomoPort, o.MosdnsPort, o.OxidnsPort
	environment := []string{
		"SERVER_URL=" + o.ServerURL,
		"TZ=Asia/Shanghai",
		"ENABLE_MIHOMO=" + boolText(o.EnableMihomo),
		"ENABLE_MOSDNS=" + boolText(o.EnableMosdns),
		"ENABLE_OXIDNS=" + boolText(o.EnableOxidns),
	}
	for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
		if token := o.RegistrationTokens[service]; token != "" {
			environment = append(environment, "AGENT_"+strings.ToUpper(service)+"_REGISTRATION_TOKEN="+token)
		}
	}
	if o.AgentIP != "" {
		environment = append(environment, "AGENT_IP="+o.AgentIP)
	}
	if o.EnableMihomo {
		environment = append(environment, "AGENT_MIHOMO_NAME="+o.AgentName+"-mihomo", "AGENT_MIHOMO_PORT="+mihomoPort)
	}
	if o.EnableMosdns {
		environment = append(environment, "AGENT_MOSDNS_NAME="+o.AgentName+"-mosdns", "AGENT_MOSDNS_PORT="+mosdnsPort)
	}
	if o.EnableOxidns {
		environment = append(environment, "AGENT_OXIDNS_NAME="+o.AgentName+"-oxidns", "AGENT_OXIDNS_PORT="+oxidnsPort)
	}
	volumes := []composeMount{{Short: o.DataDir + "/agent:/opt/configflow-agent"}}
	if o.EnableMihomo {
		volumes = append(volumes, composeMount{Short: o.DataDir + "/mihomo:/etc/mihomo"})
	}
	if o.EnableMosdns {
		volumes = append(volumes, composeMount{Short: o.DataDir + "/mosdns:/etc/mosdns"})
	}
	if o.EnableOxidns {
		volumes = append(volumes, composeMount{Short: o.DataDir + "/oxidns:/etc/oxidns"})
	}
	if o.EnableDNSHA {
		environment = append(environment, "CONFIGFLOW_DNS_HA=1", "CONFIGFLOW_DNS_HA_NETWORK="+o.NetworkMode)
		volumes = append(volumes, composeMount{Short: o.DataDir + "/ha:/var/lib/configflow-ha"}, identity)
		if o.NetworkMode == "host" {
			volumes = append(volumes, bindMount("/proc/1/ns/net", "/run/configflow-host-netns", true, false))
		}
	}
	logging := &composeLogging{Driver: "json-file", Options: composeLogOptions{"10m", "3"}}
	service := &composeService{Image: yamlx.Str(DockerHAImage), ContainerName: containerName, Restart: "unless-stopped",
		NetworkMode: o.NetworkMode, Environment: composeEnvironment{Entries: environment}, Volumes: volumes, Logging: logging}
	if network != nil {
		service.NetworkMode = ""
		service.CapAdd = []string{"NET_ADMIN", "NET_RAW"}
		service.StopGracePeriod = "30s"
		service.Networks = map[string]composeAddress{"dns-ha": {IPv4Address: yamlx.Str(o.AgentIP)}}
	} else if o.EnableDNSHA {
		service.StopGracePeriod = "30s"
		service.DependsOn.Names = []string{"ha-companion"}
	}

	if o.NetworkMode == "bridge" {
		var mappings []string
		has := func(v string) bool {
			for _, m := range mappings {
				if m == v {
					return true
				}
			}
			return false
		}
		if o.EnableMihomo {
			mappings = append(mappings, mihomoPort+":"+mihomoPort, "53:53/udp", "7890:7890", "9090:9090")
		}
		if o.EnableMosdns {
			mappings = append(mappings, mosdnsPort+":"+mosdnsPort)
			if !has("53:53/udp") {
				mappings = append(mappings, "53:53/udp")
			}
		}
		if o.EnableOxidns {
			mappings = append(mappings, oxidnsPort+":"+oxidnsPort, "53:53/tcp")
			if !has("53:53/udp") {
				mappings = append(mappings, "53:53/udp")
			}
		}
		if len(mappings) > 0 {
			service.Ports = mappings
		}
	}
	document := &composeDocument{Version: "3.8", Services: composeServices{Agent: service}}
	if network != nil {
		document.Networks = map[string]*composeNetwork{"dns-ha": network.object()}
	} else if o.EnableDNSHA {
		document.Services.Companion = &composeService{
			Image: yamlx.Str(DockerHAImage), ContainerName: containerName + "-ha", Restart: "unless-stopped", NetworkMode: "host",
			CapAdd: []string{"NET_ADMIN", "NET_RAW"}, StopGracePeriod: "30s",
			Environment: composeEnvironment{Entries: []string{"SERVICE_TYPE=ha-companion", "CONFIGFLOW_DNS_HA=1", "CONFIGFLOW_DNS_HA_NETWORK=host", "TZ=Asia/Shanghai"}},
			Volumes:     []composeMount{{Short: o.DataDir + "/agent:/opt/configflow-agent:ro"}, {Short: o.DataDir + "/oxidns:/etc/oxidns:ro"}, {Short: o.DataDir + "/ha:/var/lib/configflow-ha"}, identity}, Logging: logging}
	}
	return document, nil
}

// shellWord is _shell_word(value, plain): ordinary values keep their form.
func shellWord(value string, plain bool) string {
	if matchesClass(value, "@%+=:,./_-") {
		if plain {
			return value
		}
		return `"` + value + `"`
	}
	return shlexQuote(value)
}

// RenderDockerRun renders the selected services as Docker shell commands.
func RenderDockerRun(o DockerInstallOptions) (string, error) {
	if err := ValidateAgentName(o.AgentName); err != nil {
		return "", err
	}
	if o.EnableDNSHA {
		network, err := dockerHANetwork(o)
		if err != nil {
			return "", err
		}
		if err := checkHAPort(o.OxidnsPort); err != nil {
			return "", err
		}
		if _, err := dockerHAIdentity(o.HAHostIDPath); err != nil {
			return "", err
		}
		return generateDockerHARun(o, o.OxidnsPort, network), nil
	}
	if o.EnableMosdns && o.EnableOxidns {
		return "", valueError(msgDNSServiceConflict)
	}
	var services []string
	if o.EnableMihomo {
		services = append(services, "mihomo")
	}
	if o.EnableMosdns {
		services = append(services, "mosdns")
	}
	if o.EnableOxidns {
		services = append(services, "oxidns")
	}
	suffix := "agent"
	if len(services) > 0 {
		suffix = strings.Join(services, "-")
	}
	containerName := o.AgentName + "-" + suffix
	var desc string
	switch {
	case o.EnableMihomo && o.EnableMosdns:
		desc = "Mihomo + MosDNS"
	case o.EnableMihomo:
		desc = "Mihomo"
	case o.EnableMosdns:
		desc = "MosDNS"
	default:
		desc = "Agent"
	}
	if o.EnableOxidns {
		if o.EnableMihomo {
			desc = "Mihomo + OxiDNS"
		} else {
			desc = "OxiDNS"
		}
	}
	mihomoPort, mosdnsPort, oxidnsPort := o.MihomoPort, o.MosdnsPort, o.OxidnsPort
	portsArgs := ""
	if o.NetworkMode == "bridge" {
		var ports []string
		if o.EnableMihomo {
			ports = append(ports, "-p "+mihomoPort+":"+mihomoPort, "-p 7890:7890", "-p 9090:9090", "-p 53:53/udp")
		}
		if o.EnableMosdns {
			ports = append(ports, "-p "+mosdnsPort+":"+mosdnsPort)
			if !o.EnableMihomo {
				ports = append(ports, "-p 53:53/udp")
			}
		}
		if o.EnableOxidns {
			ports = append(ports, "-p "+oxidnsPort+":"+oxidnsPort, "-p 53:53/tcp")
			found := false
			for _, p := range ports {
				if p == "-p 53:53/udp" {
					found = true
				}
			}
			if !found {
				ports = append(ports, "-p 53:53/udp")
			}
		}
		if len(ports) > 0 {
			portsArgs = strings.Join(ports, " ") + " \\"
		}
	}
	volumes := []string{"-v " + shellWord(o.DataDir+"/agent:/opt/configflow-agent", true)}
	if o.EnableMihomo {
		volumes = append(volumes, "-v "+shellWord(o.DataDir+"/mihomo:/etc/mihomo", true))
	}
	if o.EnableMosdns {
		volumes = append(volumes, "-v "+shellWord(o.DataDir+"/mosdns:/etc/mosdns", true))
	}
	if o.EnableOxidns {
		volumes = append(volumes, "-v "+shellWord(o.DataDir+"/oxidns:/etc/oxidns", true))
	}
	volumesArgs := strings.Join(volumes, " ") + " \\"
	cmd := "#!/bin/bash\n# 运行 Agent + " + desc + "\ndocker run -d \\\n  --name " + containerName +
		" \\\n  --restart unless-stopped \\\n  --network " + shellWord(o.NetworkMode, true) + " \\"
	if portsArgs != "" {
		cmd += "\n  " + portsArgs
	}
	cmd += "\n  -e SERVER_URL=" + shellWord(o.ServerURL, false) + " \\\n" +
		"  -e ENABLE_MIHOMO=\"" + boolText(o.EnableMihomo) + "\" \\\n" +
		"  -e ENABLE_MOSDNS=\"" + boolText(o.EnableMosdns) + "\" \\"
	for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
		if token := o.RegistrationTokens[service]; token != "" {
			cmd += "\n  -e " + shellWord("AGENT_"+strings.ToUpper(service)+"_REGISTRATION_TOKEN="+token, true) + " \\"
		}
	}
	if o.AgentIP != "" {
		cmd += "\n  -e AGENT_IP=" + shellWord(o.AgentIP, true) + " \\"
	}
	if o.EnableMihomo {
		cmd += "\n  -e AGENT_MIHOMO_NAME=" + o.AgentName + "-mihomo \\\n  -e AGENT_MIHOMO_PORT=" + mihomoPort + " \\"
	}
	if o.EnableMosdns {
		cmd += "\n  -e AGENT_MOSDNS_NAME=" + o.AgentName + "-mosdns \\\n  -e AGENT_MOSDNS_PORT=" + mosdnsPort + " \\"
	}
	if o.EnableOxidns {
		cmd += "\n  -e ENABLE_OXIDNS=\"true\" -e AGENT_OXIDNS_NAME=" + o.AgentName + "-oxidns -e AGENT_OXIDNS_PORT=" + oxidnsPort + " \\"
	}
	cmd += "\n  " + volumesArgs
	cmd += "\n  thsrite/config-flow-agent:latest\n"
	cmd = replaceOnce(cmd, "docker run -d", `cf_run "启动 Docker Agent" docker run -d`)
	header, body, _ := strings.Cut(cmd, "\n")
	return header + "\n" + cliOutputHelpers +
		"\ncf_verbose_options \"$@\"\ncf_init \"Docker Agent 安装\" || exit $?\nset -e\n" +
		body +
		"cf_notice \"容器：" + containerName + "\"\n" +
		"cf_result \"Docker Agent 安装完成\"\n", nil
}

// generateDockerHARun is _generate_docker_ha_run: a new HA installation,
// never an in-place container upgrade.
func generateDockerHARun(o DockerInstallOptions, port string, network *haNetwork) string {
	quote := shlexQuote
	setup := ""
	if network != nil {
		create := []string{"docker", "network", "create", "--driver", "macvlan",
			"--subnet", network.subnet, "--opt", "parent=" + network.parent}
		if network.gateway != "" {
			create = append(create, "--gateway", network.gateway)
		}
		create = append(create, network.name)
		for i, word := range create {
			create[i] = quote(word)
		}
		setup = "# 新安装专用：已有同名网络时停止，请沿用原 Compose 或启动参数维护。\n" +
			"cf_run \"创建专用 macvlan 网络\" " + strings.Join(create, " ") + "\n"
	}
	networkMode := "host"
	if network != nil {
		networkMode = "macvlan"
	}
	env := [][2]string{
		{"SERVER_URL", o.ServerURL}, {"TZ", "Asia/Shanghai"},
		{"ENABLE_MIHOMO", "false"}, {"ENABLE_MOSDNS", "false"}, {"ENABLE_OXIDNS", "true"},
		{"CONFIGFLOW_DNS_HA", "1"}, {"AGENT_IP", o.AgentIP},
		{"CONFIGFLOW_DNS_HA_NETWORK", networkMode},
		{"AGENT_OXIDNS_NAME", o.AgentName + "-oxidns"}, {"AGENT_OXIDNS_PORT", port},
	}
	if token := o.RegistrationTokens["oxidns"]; token != "" {
		env = append(env, [2]string{"AGENT_OXIDNS_REGISTRATION_TOKEN", token})
	}
	identity := "--mount " + quote("type=bind,src="+o.HAHostIDPath+",dst=/run/configflow-host-machine-id,readonly")
	run := []string{
		`cf_run "启动 OxiDNS 高可用容器" docker run -d`,
		"--name " + quote(o.AgentName+"-oxidns"), "--restart unless-stopped", "--stop-timeout 30",
	}
	if network != nil {
		run = append(run, "--network "+quote(network.name), "--ip "+quote(o.AgentIP), "--cap-add NET_ADMIN --cap-add NET_RAW")
	} else {
		run = append(run, "--network host", "--mount type=bind,src=/proc/1/ns/net,dst=/run/configflow-host-netns,readonly")
	}
	run = append(run, identity,
		`-v "$cf_ha_data/agent:/opt/configflow-agent"`,
		`-v "$cf_ha_data/oxidns:/etc/oxidns"`, `-v "$cf_ha_data/ha:/var/lib/configflow-ha"`)
	for _, kv := range env {
		run = append(run, "-e "+quote(kv[0]+"="+kv[1]))
	}
	run = append(run, "thsrite/config-flow-agent:latest")
	companion := ""
	if network == nil {
		companion = strings.Join([]string{
			`cf_run "启动独立高可用容器" docker run -d`,
			"--name " + quote(o.AgentName+"-oxidns-ha"), "--restart unless-stopped", "--stop-timeout 30",
			"--network host", "--cap-add NET_ADMIN --cap-add NET_RAW", identity,
			`-v "$cf_ha_data/agent:/opt/configflow-agent:ro"`,
			`-v "$cf_ha_data/oxidns:/etc/oxidns:ro"`, `-v "$cf_ha_data/ha:/var/lib/configflow-ha"`,
			"-e SERVICE_TYPE=ha-companion -e CONFIGFLOW_DNS_HA=1 -e CONFIGFLOW_DNS_HA_NETWORK=host",
			"thsrite/config-flow-agent:latest",
		}, " \\\n  ") + "\n"
	}
	return "#!/bin/bash\n" + cliOutputHelpers +
		"\ncf_verbose_options \"$@\"\ncf_init \"Docker OxiDNS 高可用安装\" || exit $?\nset -e\n" +
		"[ \"$(uname -s)\" = Linux ] || { cf_error \"仅支持 Linux Docker 宿主机\"; exit 1; }\n" +
		"[ -f " + quote(o.HAHostIDPath) + " ] && [ -r " + quote(o.HAHostIDPath) + " ] || { cf_error \"无法读取宿主机身份文件\"; exit 1; }\n" +
		setup +
		"mkdir -p -- " + quote(o.DataDir) + "\n" +
		"cf_ha_data=$(cd -- " + quote(o.DataDir) + " && pwd -P)\n" +
		"mkdir -p \"$cf_ha_data/agent\" \"$cf_ha_data/oxidns\" \"$cf_ha_data/ha\"\n" +
		companion +
		strings.Join(run, " \\\n  ") + "\n" +
		"cf_notice \"待两台 Agent 上线后，在 DNS 高可用设置中配对；Agent 使用的固定地址不是虚拟 IP\"\n" +
		"cf_result \"Docker OxiDNS 高可用容器安装完成\"\n"
}
