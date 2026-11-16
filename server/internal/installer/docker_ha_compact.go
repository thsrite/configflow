package installer

// Port of backend/agents/docker_ha_compact.py (module A10): readable
// Compose whose initialization and enrollment live in the image entrypoint,
// with maintenance notes above the YAML entries.

import (
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/pyyaml"
	"configflow/internal/yamlx"
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

// pyStrOf is str(value) for the jsonx values a request carries.
func pyStrOf(value any) string { return jsonx.Str(value) }

// GenerateCompact is generate_compact(params, request, enrollment).
func GenerateCompact(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error) {
	data := params.GetString("data_dir")
	if composeDataDirRefused(data) {
		return "", nil, valueError(composeDataDirMessage)
	}
	baseParams := params.Clone()
	baseParams.Set("ha_host_id_path", "/etc/machine-id")
	base, err := guideCompose(baseParams)
	if err != nil {
		return "", nil, err
	}
	imageText, _ := requestImage(request).(string)
	image := &yamlx.SharedString{Value: imageText, Anchor: "agent_image"}
	agentName, err := pyConcat(params.Value("agent_name"))
	if err != nil {
		return "", nil, err
	}
	name := agentName + "-oxidns"
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
	identity := jsonx.NewObject()
	for k, v := range params.All() {
		if k != "ha_host_id_path" {
			identity.Set(k, v)
		}
	}
	identity.Set("session", session)
	digest, err := pyJSONHash(identity, false)
	if err != nil {
		return "", nil, err
	}
	suffix := digest[:8]
	lowered := []rune(pystr.Lower(agentName))
	if len(lowered) > 32 {
		lowered = lowered[:32]
	}
	project := "cf-oxidns-" + string(lowered) + "-" + suffix
	host := params.Value("network_mode") == "host"

	kernelMount := func(source, target string) *jsonx.Object {
		return jsonx.Obj("type", "bind", "source", source, "target", target, "read_only", true,
			"bind", yamlx.FlowMap{Object: jsonx.Obj("create_host_path", false)})
	}
	directories := func(readonly bool) []any {
		targets := [][2]string{{"agent", "/opt/configflow-agent"}, {"oxidns", "/etc/oxidns"}, {"ha", "/var/lib/configflow-ha"}}
		var out []any
		if strings.Contains(data, ":") {
			for _, item := range targets {
				out = append(out, jsonx.Obj("type", "bind", "source", data+"/"+item[0], "target", item[1],
					"read_only", readonly && item[0] != "ha", "bind", yamlx.FlowMap{Object: jsonx.Obj("create_host_path", true)}))
			}
			return out
		}
		ro := ""
		if readonly {
			ro = ":ro"
		}
		return []any{data + "/agent:/opt/configflow-agent" + ro, data + "/oxidns:/etc/oxidns" + ro, data + "/ha:/var/lib/configflow-ha"}
	}
	portText := pyStrOf(params.Value("oxidns_port"))
	main := jsonx.Obj("image", image, "container_name", name, "restart", "unless-stopped")
	if host {
		main.Set("network_mode", "host")
	} else {
		main.Set("networks", jsonx.Path(base, "services", "agent", "networks"))
	}
	networkMode, _ := params.Value("network_mode").(string)
	main.Set("environment", jsonx.Obj("SERVICE_TYPE", "oxidns-ha-"+networkMode,
		"SERVER_URL", params.Value("server_url"), "AGENT_NAME", name,
		"AGENT_IP", params.Value("agent_ip"), "AGENT_PORT", portText, "INSTALL_KEY", key))
	main.Set("volumes", append(directories(false), kernelMount("/proc/sys/kernel/random/boot_id", "/run/configflow-host-boot-id")))
	main.Set("stop_grace_period", "30s")
	main.Set("logging", jsonx.Obj("driver", "json-file", "options", yamlx.FlowMap{Object: jsonx.Obj("max-size", "10m", "max-file", "3")}))
	services := jsonx.Obj("agent", main)
	if host {
		main.Set("volumes", append(main.GetList("volumes"), kernelMount("/proc/1/ns/net", "/run/configflow-host-netns")))
		services.Set("ha-companion", jsonx.Obj("image", image, "container_name", name+"-ha", "restart", "unless-stopped",
			"network_mode", "host", "environment", jsonx.Obj("SERVICE_TYPE", "ha-companion-compact", "INSTALL_SCOPE", session),
			"cap_add", yamlx.FlowList{"NET_ADMIN", "NET_RAW"}, "volumes", directories(true),
			"stop_grace_period", "30s",
			"logging", jsonx.Obj("driver", "json-file", "options", yamlx.FlowMap{Object: jsonx.Obj("max-size", "5m", "max-file", "2")})))
	} else {
		main.Set("cap_add", yamlx.FlowList{"NET_ADMIN", "NET_RAW"})
	}
	compose := jsonx.Obj("name", project, "services", services)
	if !host {
		networks := base.GetObject("networks")
		compose.Set("networks", networks)
		for _, value := range networks.All() {
			value.(*jsonx.Object).Set("name", project+"-dns-ha")
		}
	}
	text, err := yamlx.Dump(compose, yamlx.DumpOptions{AllowUnicode: true, Width: 1000})
	if err != nil {
		return "", nil, err
	}
	text = strings.ReplaceAll(text, "$", "$$")
	annotated, err := annotateCompose(text, compose)
	if err != nil {
		return "", nil, err
	}
	return annotated, jsonx.Obj("filename", "compose.yaml", "project_name", project, "command", "docker compose up -d",
		"identity_mode", "installation", "service_count", services.Len()), nil
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
func annotateCompose(text string, compose *jsonx.Object) (string, error) {
	notes := map[string]string{notePath("name"): "保持默认：项目名，重建时沿用"}
	for role, value := range compose.GetObject("services").All() {
		service := value.(*jsonx.Object)
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
			if service.Has(field[0]) {
				notes[at(field[0])] = field[1]
			}
		}
		agentIP := "核对：容器独立 IPv4"
		if service.GetOr("network_mode", nil) == "host" {
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
		for _, key := range service.GetObject("environment").Keys() {
			notes[at("environment", key)] = environments[key]
		}
		notes[at("logging", "driver")] = "保持默认：容器日志格式"
		notes[at("logging", "options")] = pick("可改：每份 5 MB，最多 2 份", "可改：每份 10 MB，最多 3 份")
		for index, item := range service.GetList("volumes") {
			if _, isString := item.(string); isString {
				notes[at("volumes", index)] = [3]string{"Agent 身份", "配置与规则", "高可用状态"}[index]
				continue
			}
			mount := item.(*jsonx.Object)
			if index < 3 {
				notes[at("volumes", index, "source")] = "核对：宿主机持久目录"
			} else if strings.HasSuffix(mount.GetString("source"), "boot_id") {
				notes[at("volumes", index, "source")] = "保持默认：识别宿主，防止同机配对"
			} else {
				notes[at("volumes", index, "source")] = "保持默认：核对 Host 网络；不是文本文件"
			}
			notes[at("volumes", index, "type")] = "保持默认：宿主路径直接挂载"
			notes[at("volumes", index, "target")] = "保持默认：容器内路径，宿主机无需创建"
			access := "允许保存数据"
			if jsonx.Truthy(mount.GetOr("read_only", nil)) {
				access = "只读访问"
			}
			notes[at("volumes", index, "read_only")] = "保持默认：" + access
			if index >= 3 {
				notes[at("volumes", index, "bind")] = "保持默认：源路径缺失即报错"
			} else {
				notes[at("volumes", index, "bind")] = "保持默认：自动创建数据目录"
			}
		}
		if service.Has("networks") {
			notes[at("networks", "dns-ha", "ipv4_address")] = "核对：与 AGENT_IP 一致；需调整请重新生成"
		}
	}
	if compose.Has("networks") {
		notes[notePath("networks", "dns-ha", "name")] = "保持默认：专用网络名称"
		notes[notePath("networks", "dns-ha", "driver")] = "保持默认：容器使用独立局域网地址"
		notes[notePath("networks", "dns-ha", "driver_opts", "parent")] = "核对：宿主机实际网卡"
		notes[notePath("networks", "dns-ha", "ipam", "config", 0, "subnet")] = "核对：局域网网段"
		notes[notePath("networks", "dns-ha", "ipam", "config", 0, "gateway")] = "核对：局域网网关"
	}
	annotations := map[int]string{}
	var visit func(node *pyyaml.Node, path []any, key *pyyaml.Node)
	visit = func(node *pyyaml.Node, path []any, key *pyyaml.Node) {
		if note, found := notes[notePath(path...)]; found && len(path) > 0 {
			marked := node
			if key != nil {
				marked = key
			}
			annotations[marked.Start.Line] = note
		}
		switch node.Kind {
		case pyyaml.MappingNode:
			for _, pair := range node.Pairs {
				visit(pair[1], append(append([]any{}, path...), pair[0].Value), pair[0])
			}
		case pyyaml.SequenceNode:
			for index, item := range node.Items {
				visit(item, append(append([]any{}, path...), index), nil)
			}
		}
	}
	root, err := pyyaml.Compose(text)
	if err != nil {
		return "", err
	}
	if root != nil {
		visit(root, nil, nil)
	}
	lines := []string{"# 已按页面填写生成；地址、名称、端口或网络要调整，请回页面重新生成。",
		"# 已安装请保留原数据目录和身份；变更设备信息请使用迁移入口。"}
	for index, line := range pySplitLines(text) {
		if note := annotations[index]; note != "" {
			trimmed := strings.TrimLeftFunc(line, pystr.IsSpace)
			lines = append(lines, line[:len(line)-len(trimmed)]+"# "+note)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n", nil
}
