package installer

import (
	"bytes"
	"configflow/internal/yamlx"
	"encoding/json"
	"strconv"
	"strings"

	"configflow/internal/model"
	"configflow/internal/weburl"
)

// GuideOptions is the validated renderer plan. The raw identity members are
// kept privately only for the published installation hash format.
type GuideOptions struct {
	ServerURL, AgentName, AgentIP, DataDir, NetworkMode string
	OxidnsPort                                          json.Number
	HAParent, HASubnet, HAGateway, HAHostIDPath         string
	identity                                            model.Fields
	addressNode                                         *yamlx.Node
	addressDisplay                                      string
	imageRaw                                            json.RawMessage
}

// GuideRequest contains inputs consumed by the selected installation mode.
// All protocol keys are selected exactly before constructing this DTO.
type GuideRequest struct {
	Action, Mode, InstallMethod, Image                     string
	CreateHostIdentity                                     bool
	ContainerName, ExistingAgentName, HADataDir, BackupDir string
	AgentMount, CoreMount                                  composeMount
	Options                                                GuideOptions
	Output, VIP                                            string
	imageRaw                                               json.RawMessage
}

// GuideBlock is one copyable step in the installation guide.
type GuideBlock struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Command string `json:"command"`
}

// ComposeDeployment describes a generated single-file installation.
type ComposeDeployment struct {
	Filename     string `json:"filename"`
	ProjectName  string `json:"project_name"`
	Command      string `json:"command"`
	IdentityMode string `json:"identity_mode"`
	ServiceCount *int   `json:"service_count,omitempty"`
}

// ComposeBundle describes the downloadable installation archive.
type ComposeBundle struct {
	Filename       string `json:"filename"`
	ProjectName    string `json:"project_name"`
	ContentBase64  string `json:"content_base64"`
	PrepareCommand string `json:"prepare_command"`
	StartCommand   string `json:"start_command"`
}

// GuideResult is the installation HTTP response. Read-only actions omit the
// generation fields rather than materializing an empty installation.
type GuideResult struct {
	EnrollmentExpiresAt *json.Number       `json:"enrollment_expires_at,omitempty"`
	Success             bool               `json:"success"`
	PreflightReport     *PreflightReport   `json:"preflight_report,omitempty"`
	InspectionCommand   *string            `json:"inspection_command,omitempty"`
	VerificationCommand *string            `json:"verification_command,omitempty"`
	IdentityCommand     *string            `json:"identity_command,omitempty"`
	Compose             *string            `json:"compose,omitempty"`
	InstallCommand      *string            `json:"install_command,omitempty"`
	RollbackCommand     *string            `json:"rollback_command,omitempty"`
	CommandBlocks       []GuideBlock       `json:"command_blocks,omitempty"`
	Warnings            []string           `json:"warnings,omitempty"`
	ComposeBundle       *ComposeBundle     `json:"compose_bundle,omitempty"`
	ComposeDeployment   *ComposeDeployment `json:"compose_deployment,omitempty"`
}

func textOr(fields model.Fields, key, fallback string) (string, bool) {
	if !fields.Has(key) {
		return fallback, true
	}
	return fieldText(fields, key)
}
func validateGuidePath(text string, valid bool, label string, allowColon bool) (string, error) {
	if !valid {
		text = ""
	}
	return guidePath(text, label, allowColon)
}
func decodeMount(raw json.RawMessage, label string) (composeMount, error) {
	var fields model.Fields
	if json.Unmarshal(raw, &fields) != nil {
		return composeMount{}, valueError(label + "请选择 bind 目录或 Docker 命名卷")
	}
	kind, _ := fieldText(fields, "type")
	if kind != "bind" && kind != "volume" {
		return composeMount{}, valueError(label + "请选择 bind 目录或 Docker 命名卷")
	}
	source, ok := fieldText(fields, "source")
	var err error
	if kind == "bind" {
		source, err = validateGuidePath(source, ok, label, false)
	} else {
		if !ok {
			source = ""
		}
		source, err = guideName(source, label)
	}
	return composeMount{Type: kind, Source: source}, err
}

// DecodeGuideRequest parses exact JSON members and validates only the inputs
// consumed by the selected action, preserving the existing validation order.
func DecodeGuideRequest(data json.RawMessage) (GuideRequest, error) {
	request := GuideRequest{}
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return request, valueError(msgGuideObject)
	}
	request.Action, _ = textOr(fields, "action", "generate")
	request.Options.NetworkMode, _ = textOr(fields, "network_mode", "host")
	switch request.Action {
	case "check":
		text, ok := fieldText(fields, "output")
		if !ok {
			return request, valueError(msgPreflightOutputType)
		}
		request.Output = text
		return request, nil
	case "inspect":
		text, ok := textOr(fields, "container_name", "")
		if !ok {
			raw, _ := fields.Get("container_name")
			if !emptyInspectionName(raw) {
				return request, valueError(msgOriginalContainerName)
			}
		}
		request.ContainerName = text
		return request, nil
	case "verify":
		raw, _ := fields.Get("vip")
		ip, _, _, ok := decodeIPv4Value(raw)
		if !ok {
			return request, valueError(msgVirtualIPv4)
		}
		request.VIP = ip
		return request, nil
	}
	request.Mode, _ = textOr(fields, "mode", "new")
	if request.Action != "generate" || (request.Mode != "new" && request.Mode != "migrate") {
		return request, valueError(msgGuideAction)
	}
	request.InstallMethod, _ = textOr(fields, "install_method", "script")
	switch request.InstallMethod {
	case "script", "compose", "compose_direct", "compose_compact":
	default:
		return request, valueError(msgGuideMethod)
	}
	if request.InstallMethod != "script" && request.Mode != "new" {
		return request, valueError(msgComposeNewOnly)
	}
	request.Image, _ = textOr(fields, "image", DockerHAImage)
	if !isImageName(request.Image) {
		return request, valueError(msgDockerImage)
	}
	p := &request.Options
	p.ServerURL, _ = textOr(fields, "server_url", "")
	if !isServerURL(p.ServerURL) {
		return request, valueError(msgServerURL)
	}
	parsed, err := weburl.Parse(p.ServerURL)
	if err != nil {
		return request, valueError(err.Error())
	}
	if weburl.HasCredentials(parsed) || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return request, valueError(msgServerURLComponents)
	}
	p.ServerURL = strings.TrimRight(p.ServerURL, "/")
	port, ok := fieldNumber(fields, "oxidns_port")
	if !fields.Has("oxidns_port") {
		port, ok = "8082", true
	}
	if !ok || strings.ContainsAny(port.String(), ".eE") {
		return request, valueError(msgAgentPortInteger)
	}
	p.OxidnsPort = port
	// Preserve the exact historical plan members for identity hashing, including
	// unused macvlan fields on host installs. They are never ownership proof.
	for _, field := range []struct{ key, fallback string }{
		{"server_url", `""`}, {"agent_name", `"agent"`}, {"agent_ip", `""`}, {"data_dir", `""`}, {"network_mode", `"host"`},
		{"enable_mihomo", `false`}, {"enable_oxidns", `true`}, {"enable_dns_ha", `true`}, {"oxidns_port", `8082`},
		{"ha_parent", `""`}, {"ha_subnet", `""`}, {"ha_gateway", `""`}, {"ha_host_id_path", `"/etc/machine-id"`},
	} {
		raw, present := fields.Get(field.key)
		if strings.HasPrefix(field.key, "enable_") {
			raw = []byte(field.fallback)
			present = true
		}
		if !present {
			raw = []byte(field.fallback)
		}
		if err := p.identity.Set(field.key, raw); err != nil {
			return request, err
		}
	}
	if err := setSessionField(&p.identity, "server_url", p.ServerURL); err != nil {
		return request, err
	}
	for _, field := range []struct {
		key      string
		target   *string
		fallback string
	}{
		{"agent_name", &p.AgentName, "agent"}, {"agent_ip", &p.AgentIP, ""}, {"data_dir", &p.DataDir, ""},
		{"ha_parent", &p.HAParent, ""}, {"ha_subnet", &p.HASubnet, ""}, {"ha_gateway", &p.HAGateway, ""}, {"ha_host_id_path", &p.HAHostIDPath, "/etc/machine-id"},
	} {
		*field.target, _ = textOr(fields, field.key, field.fallback)
	}
	if _, ok := fieldText(fields, "agent_ip"); !ok {
		return request, valueError(msgFixedIPv4)
	}
	gatewayRaw, _ := p.identity.Get("ha_gateway")
	if emptyInspectionName(gatewayRaw) {
		p.HAGateway = ""
	} else if gateway, _, _, ok := decodeIPv4Value(gatewayRaw); ok {
		p.HAGateway = gateway
	} else {
		p.HAGateway = "invalid-gateway"
	}
	if request.InstallMethod != "compose_compact" {
		raw, present := fields.Get("create_host_identity")
		if present && (!bytesBoolean(raw) || json.Unmarshal(raw, &request.CreateHostIdentity) != nil) {
			return request, valueError(msgCreateIdentityBoolean)
		}
	}
	if request.InstallMethod != "compose_compact" && !(request.InstallMethod == "compose_direct" && request.CreateHostIdentity) {
		if _, err := guidePath(p.HAHostIDPath, "宿主身份文件", false); err != nil {
			return request, err
		}
	}
	if request.Mode == "migrate" {
		request.ContainerName, _ = textOr(fields, "container_name", "")
		if _, err := guideName(request.ContainerName, "原容器名"); err != nil {
			return request, err
		}
		request.ExistingAgentName, ok = fieldText(fields, "existing_agent_name")
		if !ok {
			return request, valueError(msgOriginalAgentName)
		}
		if err := ValidateAgentName(request.ExistingAgentName); err != nil {
			return request, err
		}
		raw, _ := fields.Get("agent_mount")
		request.AgentMount, err = decodeMount(raw, "原 Agent 挂载")
		if err != nil {
			return request, err
		}
		raw, _ = fields.Get("core_mount")
		request.CoreMount, err = decodeMount(raw, "原 OxiDNS 挂载")
		if err != nil {
			return request, err
		}
		request.HADataDir, _ = fieldText(fields, "ha_data_dir")
		if _, err := guidePath(request.HADataDir, "新增 HA 目录", false); err != nil {
			return request, err
		}
		request.BackupDir, _ = fieldText(fields, "backup_dir")
		if _, err := guidePath(request.BackupDir, "备份目录", false); err != nil {
			return request, err
		}
	} else {
		if _, err := guidePath(p.DataDir, "持久数据根目录", false); err != nil {
			return request, err
		}
	}
	if raw, _ := p.identity.Get("agent_name"); len(raw) > 0 && raw[0] != '"' {
		return request, &RenderError{Message: msgRendererText}
	}
	if p.NetworkMode == "macvlan" {
		if raw, _ := p.identity.Get("ha_parent"); len(raw) > 0 && raw[0] != '"' {
			return request, &RenderError{Message: msgRendererText}
		}
	}
	return request, nil
}
func bytesBoolean(raw []byte) bool { return string(raw) == "true" || string(raw) == "false" }

func planHash(options GuideOptions, extra map[string]string, compact bool) (string, error) {
	fields := options.identity
	for key, value := range extra {
		if key == "image" && len(options.imageRaw) > 0 {
			if err := fields.Set(key, options.imageRaw); err != nil {
				return "", err
			}
			continue
		}

		if err := setSessionField(&fields, key, value); err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	data, err := identityJSON(raw, compact)
	if err != nil {
		return "", err
	}
	return haSHA256Hex(data), nil
}

// GenerateGuideJSON renders an installation guide from an exact-key DTO.
func GenerateGuideJSON(data json.RawMessage, enrollment *Capability) (*GuideResult, error) {
	request, err := DecodeGuideRequest(data)
	if err != nil {
		return nil, err
	}
	return renderGuide(request, enrollment)
}

func renderGuide(request GuideRequest, enrollment *Capability) (*GuideResult, error) {
	result := &GuideResult{Success: true}
	p := request.Options
	switch request.Action {
	case "check":
		report, err := EvaluatePreflight(request.Output, p.NetworkMode)
		result.PreflightReport = report
		return result, err
	case "inspect":
		command, err := inspectionCommand(request.ContainerName)
		result.InspectionCommand = &command
		return result, err
	case "verify":
		vip, err := parseIPv4(request.VIP)
		if err != nil || !usableAddress(vip) {
			return nil, valueError(msgVirtualIPv4)
		}
		command := strings.ReplaceAll(haVerification, "{{VIP}}", ipv4Text(vip))
		result.VerificationCommand = &command
		return result, nil
	}
	identity := ""
	if request.CreateHostIdentity && request.InstallMethod != "compose_direct" && request.InstallMethod != "compose_compact" {
		var err error
		identity, err = identityCommand(p.HAHostIDPath)
		if err != nil {
			return nil, err
		}
	}
	var compose, install, rollback string
	var direct *ComposeDeployment
	var err error
	if request.Mode == "migrate" {
		compose, install, rollback, err = migrationNative(p, request)
	} else {
		switch request.InstallMethod {
		case "compose_compact":
			compose, direct, err = generateCompactNative(p, request, enrollment)
		case "compose_direct":
			compose, direct, err = generateDirectNative(p, request, enrollment)
		default:
			compose, install, err = newInstallNative(p, request, enrollment)
		}
		if direct != nil {
			install = direct.Command
		}
	}
	if err != nil {
		return nil, err
	}
	name := ""
	if request.Mode == "migrate" {
		name = request.ContainerName
	}
	inspection, err := inspectionCommand(name)
	if err != nil {
		return nil, err
	}
	logs := "docker compose logs --tail 80\n"
	if request.InstallMethod != "compose_direct" && request.InstallMethod != "compose_compact" {
		name = p.AgentName + "-oxidns"
		if request.Mode == "migrate" {
			name = request.ContainerName
		}
		logs = "docker logs --tail 80 " + q(name) + "\n"
	}
	result.InspectionCommand = &inspection
	result.IdentityCommand = &identity
	result.Compose = &compose
	result.InstallCommand = &install
	result.RollbackCommand = &rollback
	result.CommandBlocks = []GuideBlock{{"inspect", "只读检查宿主机与原容器", inspection}}
	if identity != "" {
		result.CommandBlocks = append(result.CommandBlocks, GuideBlock{"identity", "仅在身份文件缺失时创建一次", identity})
	}
	title := "安装当前主机"
	if request.Mode == "migrate" {
		title = "备份并迁移当前主机"
	}
	result.CommandBlocks = append(result.CommandBlocks, GuideBlock{"install", title, install}, GuideBlock{"logs", "查看启动日志", logs})
	if rollback != "" {
		result.CommandBlocks = append(result.CommandBlocks, GuideBlock{"rollback", "需要时恢复原容器", rollback})
	}
	result.Warnings = []string{"首次安装请先执行安装命令完成安全注册；Compose 仅供查看或后续维护，直接 Compose up 不会执行注册步骤。"}
	if request.Mode == "migrate" {
		result.Warnings = []string{"迁移仅适用于 ConfigFlow 单 OxiDNS 默认启动容器；先执行迁移脚本，不要直接 Compose up 跳过原挂载核对和备份。", "已有高可用组请先在页面处理虚拟 IP 与停用；脚本拒绝迁移仍有 HA 记录的容器。", "备份包含 Agent 身份与配置，请保护备份目录；旧容器和数据不会被删除。", "迁移 Compose 的 raw 环境文件格式需要 Docker Compose 2.30+；安装命令使用 Docker Run，不依赖 Compose 版本。"}
	}
	switch request.InstallMethod {
	case "compose":
		text, bundle, err := generateBundleNative(p, request, enrollment)
		if err != nil {
			return nil, err
		}
		result.Compose = &text
		result.ComposeBundle = bundle
		result.Warnings = []string{"安装包仅支持新安装：解压后先执行 sudo bash prepare.sh，再运行 docker compose up -d；需要 Compose 2.20+。", "安装包包含限定本机的首次登记凭据，请勿公开分享；已登记并持久保存身份后，重启不依赖该凭据或登记 API。", "请在 Compose 安装包和备用安装脚本中选择一种方式，不要对同一数据目录混用。"}
	case "compose_direct":
		result.ComposeDeployment = direct
		result.Warnings = []string{"只需保存 compose.yaml 并运行 docker compose up -d；需要包含内置安装器的新镜像和 Compose 2.20+。", "YAML 包含仅用于本设备的24小时登记凭据，也可被 Docker inspect 读取；请勿公开分享。已保存 Agent 身份后重启不依赖该凭据。", "初始化仅写应用数据目录；选用自动身份时还使用固定 Docker 身份卷。不要复制或删除该共享身份卷，不要运行 down -v。"}
	case "compose_compact":
		result.ComposeDeployment = direct
		result.Warnings = []string{"保存一份 compose.yaml 后启动即可；需要支持精简安装的新镜像与 Compose 2.20+。", "INSTALL_KEY 是本设备的限时安装凭据，请勿分享。首次登记完成后可移除，重建继续复用持久身份。", "安装身份保存在 Agent 数据目录中；两端需提供只读宿主校验信息，旧安装请沿用原 Compose 或使用迁移入口。"}
	}
	return result, nil
}

// decodeIPv4Value supports the existing public renderer's textual and numeric
// address forms; each is validated as IPv4 before it reaches a command.
func decodeIPv4Value(raw json.RawMessage) (string, *yamlx.Node, string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", nil, "", false
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return "", nil, "", false
		}
		return text, yamlx.Str(text), text, true
	}
	if bytes.Equal(raw, []byte("true")) {
		return "0.0.0.1", yamlx.Bool(true), "True", true
	}
	if bytes.Equal(raw, []byte("false")) {
		return "0.0.0.0", yamlx.Bool(false), "False", true
	}
	value, err := strconv.ParseUint(string(raw), 10, 32)
	if err != nil {
		return "", nil, "", false
	}
	return ipv4Text(uint32(value)), yamlx.Int(int64(value)), strconv.FormatUint(value, 10), true
}
func emptyInspectionName(raw json.RawMessage) bool {
	switch string(bytes.TrimSpace(raw)) {
	case "", "null", "false", "0", "0.0", "[]", "{}":
		return true
	}
	return false
}

func imageNode(request GuideRequest) *yamlx.Node {
	if len(request.imageRaw) > 0 {
		if node, err := yamlx.FromJSON(request.imageRaw); err == nil {
			return node
		}
	}
	return yamlx.Str(request.Image)
}
