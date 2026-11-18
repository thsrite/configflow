package agents

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/installer"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/utilx"
)

// Installer routes of backend/routes/agents.py (A08): install script and
// install-command links, the core installer, the gateway network probe and
// the Docker Agent snippets. GET /api/agents/download/<filename> stays with A10.

func registerInstaller(r *httpx.Router) {
	r.GET("/api/agents/core-install-script", "agents.get_core_install_script", getCoreInstallScript)
	r.GET("/api/agents/gateway-network-probe.sh", "agents.get_gateway_network_probe", getGatewayNetworkProbe)
	r.POST("/api/agents/install-command", "agents.create_install_command", auth.Require(createInstallCommand))
	r.POST("/api/agents/core-install-command", "agents.create_core_install_command", auth.Require(createCoreInstallCommand))
	r.GET("/api/agents/i/<token>", "agents.download_install_command", downloadInstallCommand).PreservePlainErrors = true
	r.GET("/api/agents/install-script", "agents.get_install_script", getInstallScript)
	r.GET("/api/agents/docker-agent-compose", "agents.get_docker_agent_compose", auth.Require(getDockerAgentCompose))
	r.GET("/api/agents/docker-agent-run", "agents.get_docker_agent_run", auth.Require(getDockerAgentRun))
}

// scriptDownload is a script body with the download headers of the routes.
func scriptDownload(status int, script, filename string) *httpx.Response {
	return httpx.Text(status, script).
		WithHeader("Content-Disposition", `attachment; filename="`+filename+`"`).
		WithHeader("Cache-Control", "no-store").
		WithHeader("X-Content-Type-Options", "nosniff")
}

// coreInstallScript is get_core_install_script's body: the script, or the
// 500 response.
func coreInstallScript() (string, *httpx.Response) {
	script, err := installer.GenerateCoreInstallScript(false)
	if err != nil {
		logger.Exception("生成双核心安装脚本失败", err)
		return "", failure(500, "核心安装脚本不可用，请更新 ConfigFlow 后重试。")
	}
	return script, nil
}

func getCoreInstallScript(c *httpx.Ctx) (*httpx.Response, error) {
	script, failed := coreInstallScript()
	if failed != nil {
		return failed, nil
	}
	return scriptDownload(200, script, "install-mihomo-oxidns.sh"), nil
}

func getGatewayNetworkProbe(c *httpx.Ctx) (*httpx.Response, error) {
	return scriptDownload(200, installer.GatewayNetworkProbe, "detect-gateway-network.sh"), nil
}

var installCommandFields = map[string]bool{
	"name": true, "type": true, "port": true, "agent_ip": true, "config_path": true, "restart_command": true, "server_url": true,
	"agent_type": true, "install_core": true, "service_manager": true, "service_unit": true, "service_binary": true,
	"stop_command": true, "start_command": true, "status_command": true, "health_url": true,
	"health_dns_address": true, "health_dns_name": true, "deployment_health_timeout": true,
	"mihomo_agent_port": true, "oxidns_agent_port": true, "mihomo_config_path": true, "oxidns_config_path": true,
	"mihomo_service_unit": true, "oxidns_service_unit": true, "mihomo_service_binary": true, "oxidns_service_binary": true,
	"ipv4_forwarding": true, "prepare_dns_ha": true,
}
var installCommandBooleans = map[string]bool{"install_core": true, "ipv4_forwarding": true, "prepare_dns_ha": true}
var installCommandNumbers = map[string]bool{"port": true, "mihomo_agent_port": true, "oxidns_agent_port": true, "deployment_health_timeout": true}

// LegacyShellAgentRemoved is LEGACY_SHELL_AGENT_REMOVED.
const LegacyShellAgentRemoved = "旧版 Shell Agent（agent_type=shell）已停止提供。请去掉该参数使用 Go Agent；" +
	"已安装旧版的设备直接运行 Go Agent 安装命令即可原地升级。"

func requireGoAgent(agentType string) error {
	if agentType == "shell" {
		return &installer.ValueError{Message: LegacyShellAgentRemoved}
	}
	if agentType != "go" {
		return &installer.ValueError{Message: "agent_type 只支持 go"}
	}
	return nil
}

// installParams is the MultiDict the script renderer reads: request.args or
// the converted install-command payload.
type installParams interface {
	get(name string) (string, bool)
}

type queryParams struct{ c *httpx.Ctx }

func (q queryParams) get(name string) (string, bool) { return q.c.Arg(name) }

type credentialParams struct {
	base   installParams
	tokens map[string]string
}

func (p credentialParams) get(name string) (string, bool) {
	if service, ok := strings.CutPrefix(name, "registration_token_"); ok {
		if token, present := p.tokens[service]; present {
			return token, true
		}
	}
	return p.base.get(name)
}

type mapParams map[string]string

func (m mapParams) get(name string) (string, bool) {
	value, ok := m[name]
	return value, ok
}

func paramDefault(p installParams, name, def string) string {
	if value, ok := p.get(name); ok {
		return value
	}
	return def
}

// paramInt is params.get(name, def, type=int): int() of the value, the
// default when int() raises ValueError (including the digit limit).
func paramInt(p installParams, name string, def int64) *big.Int {
	if value, ok := p.get(name); ok {
		if n, err := installer.PyInt(value); err == nil {
			return n
		}
	}
	return big.NewInt(def)
}

// installCommandParams is _install_command_params(payload).
func installCommandParams(payload any) (mapParams, error) {
	invalid := &installer.ValueError{Message: "安装参数无效，请重新生成命令。"}
	object, ok := payload.(*jsonx.Object)
	if !ok {
		return nil, invalid
	}
	for key := range object.All() {
		if !installCommandFields[key] {
			return nil, invalid
		}
	}
	params := mapParams{}
	for key, value := range object.All() {
		switch {
		case installCommandBooleans[key]:
			b, ok := value.(bool)
			if !ok {
				return nil, &installer.ValueError{Message: key + " 必须为 true 或 false"}
			}
			params[key] = "false"
			if b {
				params[key] = "true"
			}
		case installCommandNumbers[key]:
			n, ok := value.(jsonx.Number)
			if !ok || !n.IsInt() {
				return nil, &installer.ValueError{Message: key + " 必须是 1 到 65535 之间的整数"}
			}
			v, ok := new(big.Int).SetString(string(n), 10)
			if !ok || v.Cmp(big.NewInt(1)) < 0 || v.Cmp(big.NewInt(65535)) > 0 {
				return nil, &installer.ValueError{Message: key + " 必须是 1 到 65535 之间的整数"}
			}
			params[key] = v.String()
		default:
			text, ok := value.(string)
			if !ok || utf8.RuneCountInString(text) > 4096 || strings.ContainsRune(text, 0) {
				return nil, &installer.ValueError{Message: key + " 参数无效"}
			}
			params[key] = text
		}
	}
	agentType, ok := params["agent_type"]
	if !ok {
		agentType = "go"
	}
	if err := requireGoAgent(agentType); err != nil {
		return nil, err
	}
	return params, nil
}

// pyLowerText is str.lower() (CPython 3.11 full case mapping).
func pyLowerText(text string) string { return pystr.Lower(text) }

func valueErrorResponse(err error) *httpx.Response {
	return failure(400, err.Error())
}

// installScriptResponse is _install_script_response(params): the native
// Agent installer (single service or Mihomo + OxiDNS on one host).
func installScriptResponse(c *httpx.Ctx, params installParams) (string, *httpx.Response) {
	script, err := renderInstallScript(c, params)
	if err != nil {
		var valueErr *installer.ValueError
		if errors.As(err, &valueErr) {
			return "", valueErrorResponse(err)
		}
		logger.Errorf("生成安装脚本失败: %s", err.Error())
		logger.Errorf("错误详情:\n%s", err.Error())
		name, _ := params.get("name")
		serviceType, _ := params.get("type")
		port, _ := params.get("port")
		agentType, _ := params.get("agent_type")
		logger.Errorf("请求参数: name=%s, type=%s, port=%s, agent_type=%s", pyOptional(params, "name", name),
			pyOptional(params, "type", serviceType), pyOptional(params, "port", port), pyOptional(params, "agent_type", agentType))
		return "", failure(500, err.Error())
	}
	return script, nil
}

// pyOptional is f'{params.get(name)}': None when absent.
func pyOptional(params installParams, name, value string) string {
	if _, ok := params.get(name); !ok {
		return "None"
	}
	return value
}

func renderInstallScript(c *httpx.Ctx, params installParams) (string, error) {
	agentName := paramDefault(params, "name", "My-Agent")
	serviceType := paramDefault(params, "type", "mihomo")
	var agentPort any = paramInt(params, "port", 8080)
	agentIP := installer.PyStrip(paramDefault(params, "agent_ip", ""))
	configPath := paramDefault(params, "config_path", "/etc/"+serviceType+"/config.yaml")
	restartCommand := paramDefault(params, "restart_command", "systemctl restart "+serviceType)

	agentType := installer.PyStrip(pyLowerText(paramDefault(params, "agent_type", "go")))
	if err := requireGoAgent(agentType); err != nil {
		return "", err
	}
	installCoreValue := paramDefault(params, "install_core", "false")
	if installCoreValue != "true" && installCoreValue != "false" {
		return "", &installer.ValueError{Message: "install_core 必须为 true 或 false"}
	}
	installCore := installCoreValue == "true"
	prepareHAValue, prepareHASet := params.get("prepare_dns_ha")
	if prepareHASet && prepareHAValue != "true" && prepareHAValue != "false" {
		return "", &installer.ValueError{Message: "prepare_dns_ha 必须为 true 或 false"}
	}
	if prepareHASet && (serviceType != "oxidns" || agentType != "go") {
		return "", &installer.ValueError{Message: "仅 OxiDNS 单服务 Go Agent 支持准备 DNS 高可用环境"}
	}
	prepareDNSHA := prepareHAValue == "true"
	if installCore {
		if (serviceType != "mihomo" && serviceType != "oxidns" && serviceType != "aio") || agentType != "go" {
			return "", &installer.ValueError{Message: "仅 Mihomo、OxiDNS 或同机双服务 Go Agent 支持同时安装核心"}
		}
		// Strict validation instead of Flask's integer converter silently
		// replacing an invalid explicit value with the default.
		agentPort = paramDefault(params, "port", "8080")
	}
	forwardingValue, forwardingSet := params.get("ipv4_forwarding")
	if forwardingSet && forwardingValue != "true" && forwardingValue != "false" {
		return "", &installer.ValueError{Message: "ipv4_forwarding 必须为 true 或 false"}
	}
	if forwardingSet && (serviceType != "mihomo" || agentType != "go") {
		return "", &installer.ValueError{Message: "仅 Mihomo 单独安装（Go Agent）可设置 IPv4 转发"}
	}
	manageForwarding := installer.ForwardingAuto
	if forwardingSet {
		manageForwarding = installer.ForwardingOff
		if forwardingValue == "true" {
			manageForwarding = installer.ForwardingOn
		}
	}

	logger.Infof("生成安装脚本请求 - 名称: %s, 类型: %s, 端口: %s, Agent类型: %s", agentName, serviceType, installer.PyStr(agentPort), agentType)

	serverURL := installer.PyStrip(paramDefault(params, "server_url", ""))
	if serverURL == "" {
		serverURL = strings.TrimRight(latin1(c.URLRoot()), "/")
		proto, _ := c.Header("X-Forwarded-Proto")
		host, _ := c.Header("X-Forwarded-Host")
		if proto != "" && host != "" {
			serverURL = latin1(proto) + "://" + latin1(host)
		}
	}

	var script string
	var err error
	if serviceType == "aio" {
		o := installer.NewAIOOptions(serverURL, agentName)
		o.AgentIP = agentIP
		o.RegistrationTokens = map[string]string{"mihomo": paramDefault(params, "registration_token_mihomo", ""), "oxidns": paramDefault(params, "registration_token_oxidns", "")}
		o.RegistrationKey = paramDefault(params, "registration_key", "")
		o.BinaryDownloadURL = serverURL + "/api/agents/download"
		o.MihomoAgentPort = paramDefault(params, "mihomo_agent_port", "8080")
		o.OxidnsAgentPort = paramDefault(params, "oxidns_agent_port", "8082")
		o.InstallCore = installCore
		o.MihomoConfigPath = installer.PyStrip(paramDefault(params, "mihomo_config_path", "/etc/mihomo/config.yaml"))
		o.MihomoServiceUnit = installer.PyStrip(paramDefault(params, "mihomo_service_unit", "mihomo"))
		o.MihomoServiceBinary = installer.PyStrip(paramDefault(params, "mihomo_service_binary", "mihomo"))
		o.OxidnsConfigPath = installer.PyStrip(paramDefault(params, "oxidns_config_path", "/etc/oxidns/config.yaml"))
		o.OxidnsServiceUnit = installer.PyStrip(paramDefault(params, "oxidns_service_unit", "oxidns"))
		o.OxidnsServiceBinary = installer.PyStrip(paramDefault(params, "oxidns_service_binary", "oxidns"))
		script, err = installer.GenerateGoAIOInstallScript(o)
	} else {
		strip := func(name string) string { return installer.PyStrip(paramDefault(params, name, "")) }
		forwarding := installer.ForwardingOff
		if serviceType == "mihomo" {
			forwarding = manageForwarding
		}
		script, err = installer.GenerateGoAgentInstallScript(installer.GoAgentOptions{
			ServerURL: serverURL, AgentName: agentName, ServiceType: serviceType, AgentPort: agentPort,
			AgentIP: agentIP, ConfigPath: configPath, RestartCommand: restartCommand,
			BinaryDownloadURL: serverURL + "/api/agents/download",
			ServiceManager:    strip("service_manager"), ServiceUnit: strip("service_unit"), ServiceBinary: strip("service_binary"),
			StopCommand: strip("stop_command"), StartCommand: strip("start_command"), StatusCommand: strip("status_command"),
			HealthURL: strip("health_url"), HealthDNSAddress: strip("health_dns_address"), HealthDNSName: strip("health_dns_name"),
			DeploymentHealthTimeout: paramInt(params, "deployment_health_timeout", 30),
			RegistrationToken:       paramDefault(params, "registration_token_"+serviceType, paramDefault(params, "registration_token", "")), RegistrationKey: paramDefault(params, "registration_key", ""),
			InstallCore: installCore, ManageIPv4Forwarding: forwarding, PrepareDNSHA: prepareDNSHA,
		})
	}
	if err != nil {
		return "", err
	}
	logger.Infof("安装脚本生成成功，长度: %d 字符", utf8.RuneCountInString(script))
	return script, nil
}

func getInstallScript(c *httpx.Ctx) (*httpx.Response, error) {
	params := queryParams{c}
	script, failed := installScriptResponse(c, params)
	if failed != nil {
		return failed, nil
	}
	// Anonymous script downloads remain templates. Only an authenticated panel
	// request may mint a capability; a public URL can never become a proof oracle.
	if auth.IsAuthEnabled() && validPanelJWT(c) {
		tokens, failed := issueRegistration(c, shellRegistrationIdentities(params))
		if failed != nil {
			return failed, nil
		}
		script, failed = installScriptResponse(c, credentialParams{base: params, tokens: tokens})
		if failed != nil {
			return failed, nil
		}
		return httpx.Text(200, script).NoStore(), nil
	}
	return httpx.Text(200, script), nil
}

// saveInstallCommand is _save_install_command(script).
func saveInstallCommand(c *httpx.Ctx, script string) *httpx.Response {
	store := &installer.Store{DataDir: repo.DataDir()}
	token, expiresAt, err := store.SaveInstallScript(c.Context(), script, nil)
	if errors.Is(err, installer.ErrStoreFull) {
		return failure(503, "可用安装链接已达上限，请使用已生成的命令或等待旧链接过期。")
	}
	if err != nil {
		logger.Exception("保存安装命令失败", err)
		return failure(500, "无法保存安装命令，请检查数据目录后重试。")
	}
	return httpx.JSON(jsonx.Obj("success", true, "script", script,
		"download_path", "/api/agents/i/"+token, "expires_at", expiresAt)).NoStore()
}

// readBody is _read_json_body; a json.loads ValueError it does not catch
// (the int digit limit) is the route's uncaught exception.
func readBody(c *httpx.Ctx, maxLength int64, invalid string) (any, *httpx.Response, error) {
	payload, failed := ReadJSONBody(c, maxLength, invalid)
	if failed != nil && failed.Status == 500 {
		return nil, nil, fmt.Errorf("json.loads: %s", "Exceeds the limit for integer string conversion")
	}
	return payload, failed, nil
}

func createInstallCommand(c *httpx.Ctx) (*httpx.Response, error) {
	payload, failed, err := readBody(c, 32*1024, "安装参数无效")
	if err != nil || failed != nil {
		return failed, err
	}
	params, err := installCommandParams(payload)
	if err != nil {
		return valueErrorResponse(err), nil
	}
	script, failed := installScriptResponse(c, params)
	if failed != nil {
		return failed, nil
	}
	if auth.IsAuthEnabled() {
		identities := shellRegistrationIdentities(params)
		tokens, failed := issueRegistration(c, identities)
		if failed != nil {
			return failed, nil
		}
		for service, token := range tokens {
			params["registration_token_"+service] = token
		}
		script, failed = installScriptResponse(c, params)
		if failed != nil {
			return failed, nil
		}
	}
	return saveInstallCommand(c, script), nil
}

func createCoreInstallCommand(c *httpx.Ctx) (*httpx.Response, error) {
	payload, failed, err := readBody(c, 1024, "核心安装参数无效")
	if err != nil || failed != nil {
		return failed, err
	}
	object, ok := payload.(*jsonx.Object)
	target := ""
	if ok && object.Len() == 1 {
		if value, isString := object.Value("target").(string); isString {
			target = value
		}
	}
	if target != "both" && target != "mihomo" && target != "oxidns" {
		return failure(400, "请选择 both、mihomo 或 oxidns 核心。"), nil
	}
	rendered, failed := coreInstallScript()
	if failed != nil {
		return failed, nil
	}
	lines := installer.SplitLines(rendered)
	head := lines
	if len(head) > 2 {
		head = lines[:2]
	}
	// Preserve the shebang and interface marker while fixing the chosen target.
	script := strings.Join(head, "") + "set -- --core " + target + "\n" + strings.Join(lines[len(head):], "")
	return saveInstallCommand(c, script), nil
}

func downloadInstallCommand(c *httpx.Ctx) (*httpx.Response, error) {
	store := &installer.Store{DataDir: repo.DataDir()}
	script, ok, err := store.LoadInstallScript(c.Context(), c.Param("token"))
	if err != nil {
		logger.Exception("读取安装命令失败", err)
		return scriptDownload(503, "安装脚本暂时不可用，请稍后重试。\n", "configflow-install.sh"), nil
	}
	if !ok {
		return scriptDownload(404, "安装链接不存在或已过期，请在面板重新生成。\n", "configflow-install.sh"), nil
	}
	return scriptDownload(200, script, "configflow-install.sh"), nil
}

// dockerOptions reads the query of the Docker Agent routes.
func dockerOptions(c *httpx.Ctx) installer.DockerOptions {
	q := queryParams{c}
	return installer.DockerOptions{
		ServerURL:    paramDefault(q, "server_url", ""),
		AgentName:    paramDefault(q, "agent_name", "agent"),
		AgentIP:      paramDefault(q, "agent_ip", ""),
		DataDir:      paramDefault(q, "data_dir", "./agent_data"),
		NetworkMode:  paramDefault(q, "network_mode", "host"),
		EnableMihomo: utilx.StrToBool(paramDefault(q, "enable_mihomo", "true")),
		EnableMosdns: utilx.StrToBool(paramDefault(q, "enable_mosdns", "false")),
		MihomoPort:   paramInt(q, "mihomo_port", 8080),
		MosdnsPort:   paramInt(q, "mosdns_port", 8081),
		EnableOxidns: utilx.StrToBool(paramDefault(q, "enable_oxidns", "false")),
		OxidnsPort:   paramInt(q, "oxidns_port", 8082),
		EnableDNSHA:  utilx.StrToBool(paramDefault(q, "enable_dns_ha", "false")),
		HAParent:     installer.PyStrip(paramDefault(q, "ha_parent", "")),
		HASubnet:     installer.PyStrip(paramDefault(q, "ha_subnet", "")),
		HAGateway:    installer.PyStrip(paramDefault(q, "ha_gateway", "")),
		HAHostIDPath: installer.PyStrip(paramDefault(q, "ha_host_id_path", "/etc/machine-id")),
	}
}

func dockerResponse(text string, err error, contentType string) *httpx.Response {
	if err != nil {
		var valueErr *installer.ValueError
		if errors.As(err, &valueErr) {
			return failure(400, err.Error())
		}
		return failure(500, err.Error())
	}
	return httpx.Bytes(200, contentType, []byte(text))
}

func getDockerAgentCompose(c *httpx.Ctx) (*httpx.Response, error) {
	return dockerAgentResponse(c, installer.GenerateDockerAgentCompose, "text/yaml; charset=utf-8")
}
func getDockerAgentRun(c *httpx.Ctx) (*httpx.Response, error) {
	return dockerAgentResponse(c, installer.GenerateDockerAgentRun, "text/plain; charset=utf-8")
}
func dockerAgentResponse(c *httpx.Ctx, render func(installer.DockerOptions) (string, error), contentType string) (*httpx.Response, error) {
	options := dockerOptions(c)
	text, err := render(options)
	if err != nil {
		return dockerResponse(text, err, contentType), nil
	}
	if auth.IsAuthEnabled() {
		var identities []model.AgentRegistrationSession
		for _, service := range []struct {
			name    string
			enabled bool
			port    any
		}{
			{"mihomo", options.EnableMihomo, options.MihomoPort}, {"mosdns", options.EnableMosdns, options.MosdnsPort}, {"oxidns", options.EnableOxidns, options.OxidnsPort},
		} {
			if !service.enabled {
				continue
			}
			port, _ := strconv.Atoi(installer.PyStr(service.port))
			identities = append(identities, model.AgentRegistrationSession{Name: options.AgentName + "-" + service.name, Host: strings.TrimSpace(options.AgentIP), Port: model.Int(port), ServiceType: service.name, DeploymentMethod: "docker"})
		}
		tokens, failed := issueRegistration(c, identities)
		if failed != nil {
			return failed, nil
		}
		options.RegistrationTokens = tokens
		text, err = render(options)
	}
	if len(options.RegistrationTokens) > 0 {
		return dockerResponse(text, err, contentType).NoStore(), nil
	}
	return dockerResponse(text, err, contentType), nil
}

func shellRegistrationIdentities(params installParams) []model.AgentRegistrationSession {
	service := paramDefault(params, "type", "mihomo")
	name := paramDefault(params, "name", "My-Agent")
	host := installer.PyStrip(paramDefault(params, "agent_ip", ""))
	if service == "aio" {
		return []model.AgentRegistrationSession{
			{Name: name + "-mihomo", Host: host, Port: registrationPort(params, "mihomo_agent_port", 8080), ServiceType: "mihomo", DeploymentMethod: "shell"},
			{Name: name + "-oxidns", Host: host, Port: registrationPort(params, "oxidns_agent_port", 8082), ServiceType: "oxidns", DeploymentMethod: "shell"},
		}
	}
	return []model.AgentRegistrationSession{{Name: name, Host: host, Port: registrationPort(params, "port", 8080), ServiceType: service, DeploymentMethod: "shell"}}
}
func registrationPort(params installParams, name string, fallback int) model.Int {
	// Use the renderer's converter, including accepted surrounding whitespace.
	return model.Int(paramInt(params, name, int64(fallback)).Int64())
}

func issueRegistration(c *httpx.Ctx, identities []model.AgentRegistrationSession) (map[string]string, *httpx.Response) {
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, failure(500, msgRegistrationStore)
	}
	tokens, err := manager.CreateRegistrationCredentials(c.Context(), identities)
	if errors.Is(err, agents.ErrRegistrationSessionsFull) {
		return nil, failure(503, msgRegistrationCapacity)
	}
	if err != nil {
		return nil, failure(500, msgRegistrationStore)
	}
	return tokens, nil
}
