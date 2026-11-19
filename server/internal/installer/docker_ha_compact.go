package installer

// Port of backend/agents/docker_ha_compact.py (module A10): readable
// Compose whose initialization and enrollment live in the image entrypoint,
// with maintenance notes above the YAML entries.

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

var (
	composeSystemRoots  = []string{"/etc", "/usr", "/bin", "/sbin", "/proc", "/sys", "/dev", "/run", "/boot"}
	composeSharedRoots  = []string{"/var", "/var/lib", "/opt", "/srv", "/home", "/root", "/tmp"}
	composeSynologyRoot = regexp.MustCompile(`\A/volume[0-9]+(?:/docker)?\z`)
	composeSessionRe    = regexp.MustCompile(`\A[0-9a-f]{32}\z`)
	composeTokenRe      = regexp.MustCompile(`\A[A-Za-z0-9_-]{43}\z`)
)

// composeDataDirRefused is the system-directory check shared by
// generate_compact and generate_direct. [0-9] is ASCII in Python too.
func composeDataDirRefused(data string) bool {
	for _, root := range composeSystemRoots {
		if data == root || strings.HasPrefix(data, root+"/") {
			return true
		}
	}
	for _, root := range composeSharedRoots {
		if data == root {
			return true
		}
	}
	return composeSynologyRoot.MatchString(data)
}

const composeDataDirMessage = "请为应用选择专用持久子目录，不能读写挂载系统目录或共享根目录"

func generateCompactNative(params GuideOptions, request GuideRequest, enrollment *Enrollment) (string, *ComposeDeployment, error) {
	data, err := guidePath(params.DataDir, "持久数据根目录", false)
	if err != nil {
		return "", nil, err
	}
	if composeDataDirRefused(data) {
		return "", nil, valueError(composeDataDirMessage)
	}
	baseParams := params
	baseParams.HAHostIDPath = "/etc/machine-id"
	base, err := guideComposeNative(baseParams)
	if err != nil {
		return "", nil, err
	}
	image := yamlx.Str(request.Image)
	image.Anchor = "agent_image"
	name := params.AgentName + "-oxidns"
	key, session := "", ""
	if enrollment != nil {
		session = enrollment.URL
		if i := strings.LastIndexByte(session, '/'); i >= 0 {
			session = session[i+1:]
		}
		if !composeSessionRe.MatchString(session) || !composeTokenRe.MatchString(enrollment.Token) {
			return "", nil, valueError("安装凭据格式无效")
		}
		key = session + "." + enrollment.Token
	}
	identity := params
	identity.identity.Delete("ha_host_id_path")
	digest, err := planHash(identity, map[string]string{"session": session}, false)
	if err != nil {
		return "", nil, err
	}
	lowered := []rune(strings.ToLower(params.AgentName))
	if len(lowered) > 32 {
		lowered = lowered[:32]
	}
	project := "cf-oxidns-" + string(lowered) + "-" + digest[:8]
	host := params.NetworkMode == "host"
	kernelMount := func(source, target string) composeMount {
		m := bindMount(source, target, true, false)
		m.flowBind = true
		return m
	}
	directories := func(readonly bool) []composeMount {
		ro := ""
		if readonly {
			ro = ":ro"
		}
		return []composeMount{{Short: data + "/agent:/opt/configflow-agent" + ro}, {Short: data + "/oxidns:/etc/oxidns" + ro}, {Short: data + "/ha:/var/lib/configflow-ha"}}
	}
	main := &composeService{Image: image, ContainerName: name, Restart: "unless-stopped",
		Environment: composeEnvironment{Mapping: true, Entries: []string{"SERVICE_TYPE=oxidns-ha-" + params.NetworkMode, "SERVER_URL=" + params.ServerURL, "AGENT_NAME=" + name, "AGENT_IP=" + params.AgentIP, "AGENT_PORT=" + params.OxidnsPort.String(), "INSTALL_KEY=" + key}},
		Volumes:     append(directories(false), kernelMount("/proc/sys/kernel/random/boot_id", "/run/configflow-host-boot-id")), StopGracePeriod: "30s", Logging: &composeLogging{Driver: "json-file", Options: composeLogOptions{"10m", "3"}}}
	if host {
		main.NetworkMode = "host"
	} else {
		main.Networks = base.Services.Agent.Networks
		main.CapAdd = []string{"NET_ADMIN", "NET_RAW"}
	}
	compose := &composeDocument{Name: project, Services: composeServices{Agent: main}}
	if host {
		main.Volumes = append(main.Volumes, kernelMount("/proc/1/ns/net", "/run/configflow-host-netns"))
		compose.Services.Companion = &composeService{Image: image, ContainerName: name + "-ha", Restart: "unless-stopped", NetworkMode: "host",
			Environment: composeEnvironment{Mapping: true, Entries: []string{"SERVICE_TYPE=ha-companion-compact", "INSTALL_SCOPE=" + session}},
			CapAdd:      []string{"NET_ADMIN", "NET_RAW"}, Volumes: directories(true), StopGracePeriod: "30s", Logging: &composeLogging{Driver: "json-file", Options: composeLogOptions{"5m", "2"}}}
	} else {
		compose.Networks = base.Networks
		for _, network := range compose.Networks {
			network.Name = project + "-dns-ha"
		}
	}
	var node yamlx.Node
	if err := node.Encode(compose); err != nil {
		return "", nil, err
	}
	encoded, err := yamlx.Marshal(&node)
	if err != nil {
		return "", nil, err
	}
	text := strings.ReplaceAll(string(encoded), "$", "$$")
	annotated, err := annotateComposeNative(text, &node)
	if err != nil {
		return "", nil, err
	}
	count := 1
	if host {
		count = 2
	}
	return annotated, &ComposeDeployment{Filename: "compose.yaml", ProjectName: project, Command: "docker compose up -d", IdentityMode: "installation", ServiceCount: &count}, nil
}

// notePath joins a node path (mapping keys and sequence indexes) into a map key.
func notePath(parts ...any) string {
	var b strings.Builder
	for _, part := range parts {
		switch x := part.(type) {
		case string:
			b.WriteString("k")
			b.WriteString(x)
		case int:
			b.WriteString("i")
			b.WriteString(strconv.Itoa(x))
		}
		b.WriteByte(0)
	}
	return b.String()
}

// annotateCompose is _annotate_compose(text, compose).
func annotateComposeNative(text string, compose *yamlx.Node) (string, error) {
	notes := map[string]string{notePath("name"): "保持默认：项目名，重建时沿用"}
	for role, service := range yamlx.Pairs(yamlx.Get(compose, "services")) {
		at := func(parts ...any) string { return notePath(append([]any{"services", role}, parts...)...) }
		helper := role == "ha-companion"
		pick := func(a, b string) string {
			if helper {
				return a
			}
			return b
		}
		fields := [][2]string{
			{"image", pick("保持默认：复用上方镜像", "可改：镜像版本，需支持精简安装；升级只改此处")},
			{"container_name", "保持默认：容器名称"},
			{"restart", "保持默认：异常重启，手动停止后保持停止"},
			{"network_mode", "保持默认：使用宿主机网络"},
			{"cap_add", "保持默认：管理 VIP、发送高可用报文"},
			{"stop_grace_period", "保持默认：正常退出最多等待 30 秒"},
			{"volumes", pick("保持默认：与上方使用同一组目录，前两项只读", "核对：左侧是宿主机目录；安装后保留数据，右侧路径不改")},
		}
		for _, field := range fields {
			if yamlx.Get(service, field[0]) != nil {
				notes[at(field[0])] = field[1]
			}
		}
		agentIP := "核对：容器独立 IPv4"
		if nodeText(yamlx.Get(service, "network_mode")) == "host" {
			agentIP = "核对：宿主机固定 IPv4"
		}
		environments := map[string]string{
			"SERVICE_TYPE":  pick("保持默认：仅运行独立 VIP 服务", "保持默认：OxiDNS 与高可用运行入口"),
			"SERVER_URL":    "核对：容器可访问的 ConfigFlow 地址",
			"AGENT_NAME":    "核对：设备登记名称",
			"AGENT_IP":      agentIP,
			"AGENT_PORT":    "核对：Agent 管理端口，不是 DNS 端口",
			"INSTALL_KEY":   "保持生成值：安装凭据，请勿分享；登记成功后可删",
			"INSTALL_SCOPE": "保持默认：核对安装归属，不是密码",
		}
		for key := range yamlx.Pairs(yamlx.Get(service, "environment")) {
			notes[at("environment", key)] = environments[key]
		}
		notes[at("logging", "driver")] = "保持默认：容器日志格式"
		notes[at("logging", "options")] = pick("可改：每份 5 MB，最多 2 份", "可改：每份 10 MB，最多 3 份")
		for index, item := range yamlx.Get(service, "volumes").Content {
			if item.Kind == yaml.ScalarNode {
				notes[at("volumes", index)] = [3]string{"Agent 身份", "配置与规则", "高可用状态"}[index]
				continue
			}
			mount := item
			if index < 3 {
				notes[at("volumes", index, "source")] = "核对：宿主机持久目录"
			} else if strings.HasSuffix(nodeText(yamlx.Get(mount, "source")), "boot_id") {
				notes[at("volumes", index, "source")] = "保持默认：识别宿主，防止同机配对"
			} else {
				notes[at("volumes", index, "source")] = "保持默认：核对 Host 网络；不是文本文件"
			}
			notes[at("volumes", index, "type")] = "保持默认：宿主路径直接挂载"
			notes[at("volumes", index, "target")] = "保持默认：容器内路径，宿主机无需创建"
			access := "允许保存数据"
			if nodeText(yamlx.Get(mount, "read_only")) == "true" {
				access = "只读访问"
			}
			notes[at("volumes", index, "read_only")] = "保持默认：" + access
			if index >= 3 {
				notes[at("volumes", index, "bind")] = "保持默认：源路径缺失即报错"
			} else {
				notes[at("volumes", index, "bind")] = "保持默认：自动创建数据目录"
			}
		}
		if yamlx.Get(service, "networks") != nil {
			notes[at("networks", "dns-ha", "ipv4_address")] = "核对：与 AGENT_IP 一致；需调整请重新生成"
		}
	}
	if yamlx.Get(compose, "networks") != nil {
		notes[notePath("networks", "dns-ha", "name")] = "保持默认：专用网络名称"
		notes[notePath("networks", "dns-ha", "driver")] = "保持默认：容器使用独立局域网地址"
		notes[notePath("networks", "dns-ha", "driver_opts", "parent")] = "核对：宿主机实际网卡"
		notes[notePath("networks", "dns-ha", "ipam", "config", 0, "subnet")] = "核对：局域网网段"
		notes[notePath("networks", "dns-ha", "ipam", "config", 0, "gateway")] = "核对：局域网网关"
	}
	// Only paths with an actual maintenance note need traversal. In particular,
	// arbitrary nested extension data must not incur a copied path at every level.
	prefixes := map[string]bool{"": true}
	for path := range notes {
		for at := 0; at < len(path); at++ {
			if path[at] == 0 {
				prefixes[path[:at+1]] = true
			}
		}
	}
	annotations := map[int]string{}
	var visit func(*yamlx.Node, string, *yamlx.Node)
	visit = func(node *yamlx.Node, path string, key *yamlx.Node) {
		if !prefixes[path] {
			return
		}
		if note, found := notes[path]; found && path != "" {
			marked := node
			if key != nil {
				marked = key
			}
			annotations[marked.Line-1] = note
		}
		switch node.Kind {
		case yaml.MappingNode:
			for key, value := range yamlx.AllPairs(node) {
				resolved := yamlx.Resolve(key)
				if resolved.Kind == yaml.ScalarNode && resolved.ShortTag() == "!!str" {
					visit(value, path+notePath(resolved.Value), key)
				}
			}
		case yaml.SequenceNode:
			for index, item := range node.Content {
				visit(item, path+notePath(index), nil)
			}
		}
	}

	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		return "", err
	}
	if root != nil {
		visit(root, "", nil)
	}
	lines := []string{"# 已按页面填写生成；地址、名称、端口或网络要调整，请回页面重新生成。",
		"# 已安装请保留原数据目录和身份；变更设备信息请使用迁移入口。"}
	for index, line := range pySplitLines(text) {
		if note := annotations[index]; note != "" {
			trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
			lines = append(lines, line[:len(line)-len(trimmed)]+"# "+note)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n", nil
}
