package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"

	"unicode"
)

// Port of backend/agents/docker_ha_guide.py: copyable Docker HA setup steps;
// no Docker host is contacted by this module.

// DockerHAImage is IMAGE.
const DockerHAImage = "thsrite/config-flow-agent:latest"

func enrollmentURL(e *Capability) string {
	if e == nil {
		return ""
	}
	return e.URL
}

// canonicalInstallPath accepts the published double-leading-slash spelling,
// while delegating component normalization to the native Linux path library.
func canonicalInstallPath(value string) string {
	cleaned := path.Clean(value)
	if strings.HasPrefix(value, "//") && !strings.HasPrefix(value, "///") {
		return "/" + cleaned
	}
	return cleaned
}

// installPathParent retains the reviewed raw spelling in generated commands.
func installPathParent(value string) string {
	parent, _ := path.Split(value)
	if strings.Trim(parent, "/") == "" {
		return parent
	}
	return strings.TrimRight(parent, "/")
}

func installPathsOverlap(left, right string) bool {
	left, right = path.Clean(left), path.Clean(right)
	return left == right || strings.HasPrefix(left, strings.TrimSuffix(right, "/")+"/") || strings.HasPrefix(right, strings.TrimSuffix(left, "/")+"/")
}

// guidePath is _path(value, label, allow_colon).
func guidePath(text string, label string, allowColon bool) (string, error) {
	bad := ",:\x00\r\n"
	if allowColon {
		bad = ",\x00\r\n"
	}
	// "//" is the published double-slash spelling of the root itself; compare
	// the cleaned path so it cannot bypass the root refusal.
	if !strings.HasPrefix(text, "/") || path.Clean(text) == "/" || canonicalInstallPath(text) != text || strings.ContainsAny(text, bad) {
		if allowColon {
			return "", valueError(label + "须为规范的绝对路径，不能包含逗号或换行")
		}
		return "", valueError(label + "须为规范的绝对路径，不能包含逗号、冒号或换行")
	}
	return text, nil
}

// isGuideName is re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', value).
func isGuideName(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && matchesClass(value[:1], "") && (len(value) == 1 || matchesClass(value[1:], "_.-"))
}

// guideName is _name(value, label).
func guideName(text string, label string) (string, error) {
	if !isGuideName(text) {
		return "", valueError(label + "只能包含字母、数字、点、下划线或短横线")
	}
	return text, nil
}

func guideVolume(mount composeMount, target string, readonly bool) composeMount {
	mount.Target = target
	mount.ReadOnly = readonly
	if mount.Type == "bind" {
		mount.Bind = &composeBind{false}
	}
	return mount
}
func mountArg(mount composeMount) (string, error) {
	text := "type=" + mount.Type + ",src=" + mount.Source + ",dst=" + mount.Target
	if mount.ReadOnly {
		text += ",readonly"
	}
	return text, nil
}

// Q is shlex.quote.
func q(value string) string { return shlexQuote(value) }

func haHeader() string { return "#!/bin/bash\nset -euo pipefail\numask 077\n" }

func haFail(message string) string { return `{ printf "%s\n" ` + q(message) + " >&2; exit 1; }" }

// haIdentityCheck is _identity_check(path).
func haIdentityCheck(path string) string {
	p := q(path)
	return "[ ! -L " + p + " ] && [ -f " + p + " ] && [ -r " + p + " ] || " +
		haFail("宿主身份文件不可读、不是普通文件或为软链接；请先完成身份文件步骤") + "\n" +
		"cf_identity=$(cat -- " + p + ")\n" +
		`[ "${#cf_identity}" -ge 16 ] && [ "${#cf_identity}" -le 128 ] && [[ "$cf_identity" != *$'\n'* ]] || ` +
		haFail("宿主身份文件须包含一行 16–128 字符的稳定标识") + "\n"
}

// IdentityCommand is identity_command(path). Bash noclobber creates with
// O_EXCL, including for dangling symlinks.
func identityCommand(value string) (string, error) {
	path, err := guidePath(value, "宿主身份文件", false)
	if err != nil {
		return "", err
	}
	return haHeader() + "cf_identity_path=" + q(path) + "\n" +
		`[ ! -e "$cf_identity_path" ] && [ ! -L "$cf_identity_path" ] || ` +
		haFail("目标已存在（包括软链接）；未改写，请沿用有效身份文件或另选路径") + "\n" +
		"mkdir -p -- " + q(installPathParent(path)) + "\n" +
		`cf_identity=$(tr -cd "0123456789abcdefABCDEF" < /proc/sys/kernel/random/uuid)` + "\n" +
		`[[ "$cf_identity" =~ ^[0-9a-fA-F]{32}$ ]] || ` + haFail("无法读取内核随机 UUID") + "\n" +
		`(set -o noclobber; printf "%s\n" "$cf_identity" > "$cf_identity_path") || ` +
		haFail("身份文件被并发创建或无法写入；未覆盖任何现有文件") + "\n" +
		`printf "%s\n" "宿主身份文件已创建；同一宿主所有实例沿用此文件，不要复制到第二台宿主。"` + "\n", nil
}

// InspectionCommand is inspection_command(container_name).
func inspectionCommand(containerName string) (string, error) {
	command := CollectorScript()
	if containerName != "" {
		name, err := guideName(containerName, "原容器名")
		if err != nil {
			return "", err
		}
		command += "docker inspect --format 'Name={{.Name}} Image={{.Config.Image}} Network={{.HostConfig.NetworkMode}}' " + q(name) + "\n" +
			`docker inspect --format '{{range .Mounts}}{{printf "%s | %s | %s | %s | rw=%t\n" .Type .Name .Source .Destination .RW}}{{end}}' ` + q(name) + "\n" +
			"docker exec " + q(name) + " python3 -c " + q(
			`import json; c=json.load(open("/opt/configflow-agent/config-oxidns.json")); `+
				`print(json.dumps({k:c.get(k) for k in ("agent_name","agent_port","service_type","config_path")},ensure_ascii=False))`) + "\n"
	}
	return command, nil
}

func haDaemonCheck() string {
	return `[ "$(uname -s)" = Linux ] || ` + haFail("仅支持 Linux Docker 宿主机") + "\n" +
		`[ "$(docker info --format '{{.OSType}}')" = linux ] || ` + haFail("Docker daemon 不是 Linux") + "\n" +
		`case "$(docker info --format '{{json .SecurityOptions}}')" in *rootless*) ` +
		haFail("DNS 高可用需要 rootful Docker，当前是 rootless") + " ;; esac\n"
}

// shellArg is one word of _run: quoted, or raw (the ('raw', …) tuple).
type shellArg struct {
	text string
	raw  bool
}

func haRun(service *composeService, cidFile, envFile, cidVariable string) (string, error) {
	var args []shellArg
	add := func(values ...string) {
		for _, v := range values {
			args = append(args, shellArg{text: v})
		}
	}
	add("docker", "run", "-d", "--name", service.ContainerName, "--restart", "unless-stopped", "--stop-timeout", "30")
	if cidFile != "" {
		add("--cidfile", cidFile)
	}
	if cidVariable != "" {
		add("--cidfile")
		args = append(args, shellArg{text: `"$` + cidVariable + `"`, raw: true})
	}
	if envFile != "" {
		add("--env-file", envFile)
	}
	if service.owner != "" {
		add("--label", ComposeLabel+"="+service.owner)
	}
	if service.NetworkMode == "" && service.Networks["dns-ha"].IPv4Address.Tag != "!!str" {
		return "", typeError(msgRendererText)
	}
	if service.NetworkMode != "" {
		add("--network", service.NetworkMode)
	} else {
		add("--network", service.networkName, "--ip", nodeText(service.Networks["dns-ha"].IPv4Address))
	}
	for _, capability := range service.CapAdd {
		add("--cap-add", capability)
	}
	for _, mount := range service.Volumes {
		if mount.Short != "" {
			return "", typeError(msgRendererMount)
		}
		text, err := mountArg(mount)
		if err != nil {
			return "", err
		}
		add("--mount", text)
	}
	for _, env := range service.Environment.Entries {
		add("-e", env)
	}
	add(service.Image.Value)
	words := make([]string, len(args))
	for i, arg := range args {
		if arg.raw {
			words[i] = arg.text
		} else {
			words[i] = q(arg.text)
		}
	}
	return strings.Join(words, " \\\n  ") + "\n", nil
}

func usableAddress(v uint32) bool {
	return !(isLoopback(v) || isMulticast(v) || isLinkLocal(v) || isUnspecified(v) || isReserved(v))
}

func guideComposeNative(p GuideOptions) (*composeDocument, error) {
	if raw, present := p.identity.Get("agent_name"); present && len(raw) > 0 && raw[0] != '"' {
		return nil, typeError(msgRendererText)
	}

	options := DefaultDockerOptions(p.ServerURL)
	options.AgentName = p.AgentName
	options.DataDir = p.DataDir
	options.AgentIP = p.AgentIP
	options.NetworkMode = p.NetworkMode
	options.EnableMihomo = false
	options.EnableOxidns = true
	options.EnableDNSHA = true
	options.OxidnsPort = p.OxidnsPort.String()
	options.HAParent = p.HAParent
	options.HASubnet = p.HASubnet
	options.HAGateway = p.HAGateway
	options.HAHostIDPath = p.HAHostIDPath
	document, err := dockerCompose(options)
	if err != nil {
		if raw, present := p.identity.Get("ha_parent"); present && len(raw) > 0 && raw[0] != '"' && err.Error() == msgHAParent {
			return nil, typeError(msgRendererText)
		}
		return nil, err
	}
	if p.addressNode != nil {
		if p.addressDisplay != p.AgentIP {
			for i, entry := range document.Services.Agent.Environment.Entries {
				if entry == "AGENT_IP="+p.AgentIP {
					document.Services.Agent.Environment.Entries[i] = "AGENT_IP=" + p.addressDisplay
				}
			}
		}
		if document.Services.Agent.Networks != nil {
			document.Services.Agent.Networks["dns-ha"] = composeAddress{IPv4Address: p.addressNode}
		}
	}
	return document, nil
}

func haSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// haDumpCompose renders the compose graph and escapes Compose interpolation.
func haDumpCompose(compose *composeDocument) (string, error) { return composeYAML(compose, true) }

// bootstrapCommand is docker_ha_enrollment.bootstrap_command (replaceable in tests).
var bootstrapCommand = bootstrapNative

const ownedContainerFunction = `
# Only a recorded exact ID plus the immutable setup label permits reuse.
cf_owned_container() {
  local cf_role=$1 cf_name=$2 cf_id cf_record cf_saved cf_found=false
  cf_id=$(docker container inspect --format '{{.Id}}' "$cf_name" 2>/dev/null || true)
  for cf_record in "$cf_state/$cf_role-id."*; do
    [ -e "$cf_record" ] || [ -L "$cf_record" ] || continue
    [ ! -L "$cf_record" ] && [ -f "$cf_record" ] || { printf '%s\n' '安装 CID 记录异常，拒绝继续' >&2; return 1; }
    [ -s "$cf_record" ] || continue
    cf_saved=$(cat "$cf_record")
    [[ "$cf_saved" =~ ^[0-9a-f]{64}$ ]] || { printf '%s\n' '安装 CID 记录无效，拒绝继续' >&2; return 1; }
    if [ "$cf_saved" = "$cf_id" ]; then
      cf_found=true
    elif docker container inspect "$cf_saved" >/dev/null 2>&1; then
      printf '%s\n' '本次安装的容器已被改名，请先核对，拒绝创建重复服务' >&2; return 1
    fi
  done
  if [ -n "$cf_id" ]; then
    [ "$cf_found" = true ] && [ "$(docker inspect --format '{{index .Config.Labels "io.configflow.dns-ha.installation"}}' "$cf_id")" = "$cf_owner" ] || {
      printf '%s\n' '同名容器不是本次安装记录的容器，拒绝接管' >&2; return 1;
    }
    printf '%s' "$cf_id"
  fi
}
`

const enrollmentProof = `import json, sys
from pathlib import Path
root=Path('/opt/configflow-agent')
path=root/'config-oxidns.json'; marker=root/'.docker-ha-installation.json'
if path.is_symlink() or marker.is_symlink() or not path.is_file() or not marker.is_file():
    sys.exit('本次安装的身份文件或标记缺失；请保留数据并检查，拒绝覆盖')
try:
    config=json.loads(path.read_text()); setup=json.loads(marker.read_text())
except Exception:
    sys.exit('本次安装的身份文件不可读，拒绝覆盖')
if setup.get('url') != sys.argv[1] or any(not isinstance(config.get(k),str) or not config[k] or config[k]=='[REDACTED]' for k in ('agent_id','token')):
    sys.exit('本次安装的身份与标记不匹配，拒绝继续')
print('已核对本次安装的本地身份；保留原凭据并继续启动。')
`

func joinQuoted(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = q(a)
	}
	return strings.Join(words, " ")
}

// newInstall is _new_install(params, request, enrollment): replay only this
// setup's immutable plan and captured container IDs.
func newInstallNative(params GuideOptions, request GuideRequest, enrollment *Capability) (string, string, error) {
	compose, err := guideComposeNative(params)
	if err != nil {
		return "", "", err
	}
	image := request.Image
	owner, err := planHash(params, map[string]string{"image": image, "enrollment_url": enrollmentURL(enrollment)}, true)
	if err != nil {
		return "", "", err
	}
	data := params.DataDir
	state := data + "/.ha-install"
	services := &compose.Services
	for key, service := range services.all() {
		service.Image = yamlString(image)
		service.Labels = map[string]string{ComposeLabel: owner}
		service.owner = owner
		readonly := key == "ha-companion"
		volumes := []composeMount{bindMount(data+"/agent", "/opt/configflow-agent", readonly, false),
			bindMount(data+"/oxidns", "/etc/oxidns", readonly, false), bindMount(data+"/ha", "/var/lib/configflow-ha", false, false),
			bindMount(params.HAHostIDPath, "/run/configflow-host-machine-id", true, false)}
		if key == "agent" && params.NetworkMode == "host" {
			volumes = append(volumes, bindMount("/proc/1/ns/net", "/run/configflow-host-netns", true, false))
		}
		service.Volumes = volumes
	}
	hostIDPath := params.HAHostIDPath
	script := haHeader() + haDaemonCheck() + haIdentityCheck(hostIDPath)
	script += "cf_data=" + q(data) + "\ncf_state=" + q(state) + "\ncf_owner=" + q(owner) + "\n"
	script += `[ ! -L "$cf_data" ] && [ ! -L "$cf_state" ] && [ ! -L "$cf_state/setup" ] || ` + haFail("安装目录或标记为软链接；拒绝继续") + "\n"
	script += `if [ -e "$cf_state/setup" ]; then` + "\n" + `  [ -f "$cf_state/setup" ] && [ "$(cat "$cf_state/setup")" = "$cf_owner" ] || ` + haFail("数据目录属于另一份安装设置，请保留原命令续装或使用迁移") + "\nelse\n"
	script += `  [ ! -e "$cf_state" ] && [ ! -e "$cf_data/agent/config-oxidns.json" ] && [ ! -L "$cf_data/agent/config-oxidns.json" ] || ` + haFail("目录已有身份或未识别的安装状态；请使用原命令或迁移，不能接管") + "\n"
	agentName := params.AgentName
	for _, name := range []string{agentName + "-oxidns", agentName + "-oxidns-ha"} {
		script += "  ! docker container inspect " + q(name) + " >/dev/null 2>&1 || " + haFail("同名容器已存在且不属于本次安装："+name) + "\n"
	}
	script += `  mkdir -p -- "$cf_data"` + "\n" + `  mkdir -- "$cf_state"` + "\n" + `  (set -o noclobber; printf "%s\n" "$cf_owner" > "$cf_state/setup")` + "\nfi\n"
	script += ownedContainerFunction + haStartupChecks
	main := services.Agent
	mainName := main.ContainerName
	script += "cf_main_id=$(cf_owned_container main " + q(mainName) + ")\n"
	companion := services.Companion
	if companion != nil {
		name := companion.ContainerName
		script += "cf_companion_id=$(cf_owned_container companion " + q(name) + ")\n"
	}
	if network := compose.Networks["dns-ha"]; network != nil {
		network.Labels = map[string]string{ComposeLabel: owner}
		name := network.Name
		main.networkName = name
		ipam := network.IPAM.Config[0]
		parent := network.DriverOpts.Parent
		subnet := ipam.Subnet
		gateway := ipam.Gateway
		script += "if docker network inspect " + q(name) + " >/dev/null 2>&1; then\n"
		checks := [][2]string{{"{{.Driver}}", "macvlan"}, {`{{index .Options "parent"}}`, parent},
			{`{{index .Labels "io.configflow.dns-ha.installation"}}`, owner},
			{"{{len .IPAM.Config}}", "1"}, {"{{(index .IPAM.Config 0).Subnet}}", subnet}}
		if gateway != "" {
			checks = append(checks, [2]string{"{{(index .IPAM.Config 0).Gateway}}", gateway})
		}
		for _, c := range checks {
			script += `  [ "$(docker network inspect --format ` + q(c[0]) + " " + q(name) + `)" = ` + q(c[1]) + " ] || " + haFail("同名 Macvlan 网络不属于本次安装或参数已改变；拒绝复用") + "\n"
		}
		script += "else\n"
		args := []string{"docker", "network", "create", "--driver", "macvlan", "--subnet", subnet, "--opt", "parent=" + parent,
			"--label", "io.configflow.dns-ha.installation=" + owner}
		if gateway != "" {
			args = append(args, "--gateway", gateway)
		}
		script += "  " + joinQuoted(append(args, name)) + "\nfi\n"
	}
	script += "docker image inspect " + q(image) + " >/dev/null 2>&1 || docker pull " + q(image) + "\n"
	if params.NetworkMode == "host" {
		script += haPortCheck(image)
	}
	for _, dirname := range []string{"agent", "oxidns", "ha"} {
		script += `[ ! -L "$cf_data/` + dirname + `" ] || ` + haFail("数据子目录是软链接，拒绝继续") + "\n"
	}
	script += `mkdir -p "$cf_data/agent" "$cf_data/oxidns" "$cf_data/ha"` + "\n"
	if enrollment != nil {
		// A created Macvlan container retains its fixed IP even while stopped.
		// Re-enrolling on that IP would collide; verify the durable seed locally.
		bootstrap, err := bootstrapCommand(enrollment, request)
		if err != nil {
			return "", "", err
		}
		script += `if [ -z "$cf_main_id" ]; then` + "\n" + bootstrap + "\nelse\n"
		proofMount, _ := mountArg(bindMount(data+"/agent", "/opt/configflow-agent", true, false))
		args := []string{"docker", "run", "--rm", "--network", "none", "--entrypoint", "python3", "--mount", proofMount,
			image, "-c", enrollmentProof, enrollment.URL}
		script += "  " + joinQuoted(args) + "\nfi\n"
	} else {
		script += `[ ! -e "$cf_data/agent/config-oxidns.json" ] || ` + haFail("缺少本次注册凭据；请在页面重新生成安装命令") + "\n"
	}
	for _, role := range []struct {
		name    string
		service *composeService
	}{{"companion", companion}, {"main", main}} {
		if role.service == nil {
			continue
		}
		r := role.name
		script += `if [ -n "$cf_` + r + `_id" ]; then` + "\n"
		script += `  if [ "$(docker inspect --format '{{.State.Running}}' "$cf_` + r + `_id")" != true ]; then docker start "$cf_` + r + `_id"; fi` + "\nelse\n"
		script += `  cf_attempt=$(tr -cd "0123456789abcdefABCDEF" < /proc/sys/kernel/random/uuid)` + "\n"
		script += `  [[ "$cf_attempt" =~ ^[0-9a-fA-F]{32}$ ]] || exit 1` + "\n"
		script += `  cf_cid_path="$cf_state/` + r + `-id.$cf_attempt"` + "\n"
		run, err := haRun(role.service, "", "", "cf_cid_path")
		if err != nil {
			return "", "", err
		}
		script += run + "fi\n"
	}
	script += haNewReadiness(mainName)
	script += `printf "%s\n" "本次安装的容器已启动或恢复；请回到页面检查真实心跳和高可用环境，再配对。相同命令可安全续装，不会接管同名其他容器。"` + "\n"

	text, err := haDumpCompose(compose)
	if err != nil {
		return "", "", err
	}
	return text, script, nil
}

const migrationCheck = `import json, os, pathlib, re, sys
environment=json.load(sys.stdin)
if not isinstance(environment,list): sys.exit('无法读取原容器环境配置')
original={}
for item in environment:
    if not isinstance(item,str) or '=' not in item: sys.exit('原环境变量格式不支持自动迁移')
    key,value=item.split('=',1)
    if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*',key) or any(c in value for c in ('\n','\r','\x00')):
        sys.exit('原环境变量含多行值或无效名称，请保留原 Compose 手动迁移')
    original[key]=value
c=json.load(open('/opt/configflow-agent/config-oxidns.json'))
expected_name, expected_server, expected_port=sys.argv[1:]
if (c.get('agent_name') != expected_name or c.get('server_url') != expected_server or str(c.get('agent_port')) != expected_port
    or c.get('service_type') != 'oxidns' or c.get('config_path') != '/etc/oxidns/config.yaml'):
    sys.exit('原 Agent 名称、服务器地址、端口或配置路径不匹配；未执行迁移')
if (os.environ.get('CONFIGFLOW_DNS_HA') == '1' or any(pathlib.Path('/opt/configflow-agent', name).exists() for name in ('oxidns-ha.json','oxidns-ha-handoff.json'))):
    sys.exit('原容器已有 HA 配置；请先在现有高可用设置中迁移虚拟 IP、停用并确认清理后再迁移，勿删除记录')
if any(os.environ.get(key,'false').lower() == 'true' for key in ('ENABLE_MIHOMO','ENABLE_MOSDNS')):
    sys.exit('仅支持单独运行 OxiDNS 的容器')
if os.environ.get('SERVICE_TYPE','') not in ('', 'oxidns'):
    sys.exit('原容器不是单 OxiDNS 服务，请沿用原 Compose 手动调整')
if c.get('health_url'):
    sys.exit('原 Agent 使用自定义 HTTP 健康检查，请沿用原 Compose 手动迁移')
settings={'HEARTBEAT_INTERVAL':c.get('heartbeat_interval',30), 'DEPLOYMENT_HEALTH_TIMEOUT':c.get('deployment_health_timeout',30),
          'AGENT_HOST':c.get('agent_host','0.0.0.0'), 'OXIDNS_HEALTH_DNS_ADDRESS':c.get('health_dns_address',''),
          'OXIDNS_HEALTH_DNS_NAME':c.get('health_dns_name',''), 'TZ':os.environ.get('TZ','Asia/Shanghai')}
original.update(settings)
for key,value in original.items():
    value=str(value)
    if '\n' in value or '\r' in value or '\x00' in value: sys.exit('原运行设置含多行值，请沿用原 Compose 手动迁移')
    print(key+'='+value)
`

// migration is _migration(params, request): compose, install and rollback.
func migrationNative(params GuideOptions, request GuideRequest) (string, string, string, error) {
	old, oldAgent := request.ContainerName, request.ExistingAgentName
	agent, core := request.AgentMount, request.CoreMount
	haDir, backup := request.HADataDir, request.BackupDir
	directories := []string{haDir, backup}
	for _, m := range []composeMount{agent, core} {
		if m.Type == "bind" {
			directories = append(directories, m.Source)
		}
	}
	for i, left := range directories {
		for _, right := range directories[i+1:] {
			leftCheck, rightCheck := path.Clean(left), path.Clean(right)
			if installPathsOverlap(leftCheck, rightCheck) {
				return "", "", "", valueError(msgMigrationDirectories)
			}
		}
	}
	if agent.Type == core.Type && agent.Source == core.Source {
		return "", "", "", valueError(msgMigrationMounts)
	}
	params.DataDir = "/unused-migration-path"
	compose, err := guideComposeNative(params)
	if err != nil {
		return "", "", "", err
	}
	services := &compose.Services
	main := services.Agent
	main.ContainerName = old
	var environment []string
	for _, v := range main.Environment.Entries {
		if strings.HasPrefix(v, "TZ=") {
			continue
		}
		if strings.HasPrefix(v, "AGENT_OXIDNS_NAME=") {
			v = "AGENT_OXIDNS_NAME=" + oldAgent
		}
		environment = append(environment, v)
	}
	main.Environment.Entries = environment
	main.EnvFile = []composeEnvFile{{backup + "/runtime.env", "raw"}}
	private := composeMount{Type: "bind", Source: haDir}
	image := request.Image
	for key, service := range services.all() {
		readonly := key == "ha-companion"
		service.Image = yamlString(image)
		volumes := []composeMount{guideVolume(agent, "/opt/configflow-agent", readonly), guideVolume(core, "/etc/oxidns", readonly),
			guideVolume(private, "/var/lib/configflow-ha", false)}
		for _, v := range service.Volumes {
			if v.Short == "" {
				volumes = append(volumes, v)
			}
		}
		service.Volumes = volumes
		if readonly {
			service.ContainerName = old + "-ha"
		}
	}
	network := compose.Networks["dns-ha"]
	if network != nil {
		main.networkName = network.Name
	}
	external := map[string]composeNamedVolume{}
	for _, m := range []composeMount{agent, core} {
		if m.Type == "volume" {
			external[m.Source] = composeNamedVolume{External: true, Name: m.Source}
		}
	}
	if len(external) > 0 {
		compose.Volumes = external
	}
	previous, failed, companion := old+"-pre-ha", old+"-failed-ha", old+"-ha"
	hostIDPath := params.HAHostIDPath
	script := haHeader() + haDaemonCheck() + haIdentityCheck(hostIDPath)
	script += haStartupChecks
	script += "cf_old=" + q(old) + "\ncf_backup=" + q(backup) + "\n"
	script += `cf_old_id=$(docker inspect --format '{{.Id}}' "$cf_old")` + "\n"
	script += `[ "$(docker inspect --format '{{.State.Running}}' "$cf_old_id")" = true ] || ` + haFail("原容器须正在运行，以核对配置；未执行迁移") + "\n"
	for _, name := range []string{previous, failed, companion} {
		script += "! docker container inspect " + q(name) + " >/dev/null 2>&1 || " + haFail("保留名称已被占用："+name+"；未执行迁移") + "\n"
	}
	script += `[ "$(docker inspect --format '{{len .Mounts}}' "$cf_old_id")" = 2 ] || ` + haFail("仅支持只有 Agent 与 OxiDNS 两个挂载的单核心容器；请先核对额外挂载或已有 HA 配置") + "\n"
	for _, pair := range []struct {
		mount  composeMount
		target string
	}{{agent, "/opt/configflow-agent"}, {core, "/etc/oxidns"}} {
		targetJSON, _ := json.Marshal(pair.target)
		format := "{{range .Mounts}}{{if eq .Destination " + string(targetJSON) + `}}{{.Type}}|{{if eq .Type "volume"}}{{.Name}}{{else}}{{.Source}}{{end}}|{{.RW}}{{end}}{{end}}`
		kind, source := pair.mount.Type, pair.mount.Source
		script += `[ "$(docker inspect --format ` + q(format) + ` "$cf_old_id")" = ` + q(kind+"|"+source+"|true") + " ] || " + haFail("实际挂载与所填信息不一致："+pair.target+"；未执行迁移") + "\n"
		if kind == "bind" {
			script += "[ -d " + q(source) + " ] && [ ! -L " + q(source) + " ] || " + haFail("原数据目录不存在或是软链接；未执行迁移") + "\n"
		}
	}
	script += `cf_old_image=$(docker inspect --format '{{.Image}}' "$cf_old_id")` + "\n"
	for _, field := range []string{"Entrypoint", "Cmd"} {
		format := "{{json .Config." + field + "}}"
		script += `[ "$(docker inspect --format ` + q(format) + ` "$cf_old_id")" = "$(docker image inspect --format ` + q(format) + ` "$cf_old_image")" ] || ` + haFail("原容器有自定义启动命令，请保留原 Compose 手动调整，不执行自动迁移") + "\n"
	}
	port := params.OxidnsPort
	checkCommand := `docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker exec -i "$cf_old_id" python3 -c ` + q(migrationCheck) + " " +
		joinQuoted([]string{oldAgent, params.ServerURL, port.String()})
	script += "cf_runtime_env=$(" + checkCommand + ")\n"
	script += "docker image inspect " + q(image) + " >/dev/null 2>&1 || docker pull " + q(image) + "\n"
	script += "[ ! -e " + q(haDir) + " ] && [ ! -L " + q(haDir) + " ] || " + haFail("请选择全新 HA 目录，不能复用已有 HA 状态") + "\n"
	script += "mkdir -p -- " + q(installPathParent(backup)) + "\nmkdir -- \"$cf_backup\"\n"
	script += `printf "%s\n" "$cf_old_id" > "$cf_backup/old-id"` + "\n" + `printf "%s\n" "$cf_runtime_env" > "$cf_backup/runtime.env"` + "\n"
	script += `docker inspect --format '{{.HostConfig.RestartPolicy.Name}}:{{.HostConfig.RestartPolicy.MaximumRetryCount}}' "$cf_old_id" > "$cf_backup/restart-policy"` + "\n"
	if network != nil {
		ipam := network.IPAM.Config[0]
		args := []string{"docker", "network", "create", "--driver", "macvlan", "--subnet", ipam.Subnet, "--opt",
			"parent=" + network.DriverOpts.Parent}
		if gateway := ipam.Gateway; gateway != "" {
			args = append(args, "--gateway", gateway)
		}
		script += joinQuoted(append(args, network.Name)) + "\n"
	}
	script += "mkdir -p -- " + q(installPathParent(haDir)) + "\nmkdir -- " + q(haDir) + "\n"
	script += "docker update --restart=no \"$cf_old_id\" >/dev/null\ndocker stop \"$cf_old_id\" >/dev/null\n"
	script += "mkdir \"$cf_backup/agent\" \"$cf_backup/oxidns\"\n"
	script += "docker cp \"$cf_old_id:/opt/configflow-agent/.\" \"$cf_backup/agent/\"\ndocker cp \"$cf_old_id:/etc/oxidns/.\" \"$cf_backup/oxidns/\"\n"
	script += "docker rename \"$cf_old_id\" " + q(previous) + "\n"
	if helper := services.Companion; helper != nil {
		run, err := haRun(helper, backup+"/companion-id", "", "")
		if err != nil {
			return "", "", "", err
		}
		script += run
	}
	run, err := haRun(main, backup+"/new-id", backup+"/runtime.env", "")
	if err != nil {
		return "", "", "", err
	}
	script += run
	script += haMigrationReadiness
	script += `printf "%s\n" "新容器已启动；先检查 Agent 身份、DNS 与高可用环境，再处理第二台并配对。旧容器和数据备份均已保留。"` + "\n"
	rollback := haHeader() + haDaemonCheck() + "cf_backup=" + q(backup) + "\n"
	rollback += "cf_old_id=$(cat \"$cf_backup/old-id\")\n[[ \"$cf_old_id\" =~ ^[0-9a-f]{64}$ ]] || exit 1\n"
	rollback += "docker container inspect \"$cf_old_id\" >/dev/null\n"
	rollback += "cf_name_id=$(docker container inspect --format '{{.Id}}' " + q(old) + " 2>/dev/null || true)\n"
	rollback += "cf_new_id=$(cat \"$cf_backup/new-id\" 2>/dev/null || true)\n"
	rollback += `[ -z "$cf_name_id" ] || [ "$cf_name_id" = "$cf_old_id" ] || [ "$cf_name_id" = "$cf_new_id" ] || ` + haFail("原名称现由其他容器占用；拒绝停止或改名") + "\n"
	rollback += "if [ -n \"$cf_new_id\" ]; then\n  [[ \"$cf_new_id\" =~ ^[0-9a-f]{64}$ ]] || exit 1\n"
	rollbackMount, _ := mountArg(guideVolume(agent, "/opt/configflow-agent", true))
	rollback += "  docker run --rm --network none --mount " + q(rollbackMount) + " --entrypoint /bin/sh " + q(image) + " -c " +
		q("test ! -e /opt/configflow-agent/oxidns-ha.json && test ! -e /opt/configflow-agent/oxidns-ha-handoff.json") + " || " +
		haFail("无法确认新容器未启用 HA；请先在页面停用 HA 并确认记录清理后回滚") + "\n"
	rollback += "  ! docker container inspect " + q(failed) + " >/dev/null 2>&1 || " + haFail("回滚保留名已占用；未执行回滚") + "\n"
	rollback += "  docker update --restart=no \"$cf_new_id\" >/dev/null\n  docker stop \"$cf_new_id\" >/dev/null\n  docker rename \"$cf_new_id\" " + q(failed) + "\nfi\n"
	rollback += "if [ -s \"$cf_backup/companion-id\" ]; then\n  cf_companion_id=$(cat \"$cf_backup/companion-id\")\n  [[ \"$cf_companion_id\" =~ ^[0-9a-f]{64}$ ]] || exit 1\n  docker update --restart=no \"$cf_companion_id\" >/dev/null\n  docker stop \"$cf_companion_id\" >/dev/null\nfi\n"
	rollback += "[ \"$(docker inspect --format '{{.Name}}' \"$cf_old_id\")\" = " + q("/"+old) + " ] || docker rename \"$cf_old_id\" " + q(old) + "\n"
	rollback += "cf_policy=$(cat \"$cf_backup/restart-policy\")\ncase \"$cf_policy\" in no:0|always:0|unless-stopped:0) cf_policy=${cf_policy%:0} ;; on-failure:[0-9]*) ;; *) exit 1 ;; esac\n"
	rollback += "docker update --restart=\"$cf_policy\" \"$cf_old_id\" >/dev/null\ndocker start \"$cf_old_id\"\n"
	rollback += `printf "%s\n" "已恢复原容器与重启策略；数据卷保持原位，原配置备份与失败容器均保留。"` + "\n"
	main.networkName = ""
	text, err := haDumpCompose(compose)
	if err != nil {
		return "", "", "", err
	}
	return text, script, rollback, nil
}

// isImageName is re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:/@-]{0,254}', image).
func isImageName(image string) bool {
	return len(image) >= 1 && len(image) <= 255 && matchesClass(image[:1], "") && (len(image) == 1 || matchesClass(image[1:], "._:/@-"))
}

// isServerURL is re.fullmatch(r'https?://[^\s]+', server). Deliberate fix
// of a Python validation bug (maintainer, 2026-11-16): Python uses
// re.match(r'^https?://[^\s]+$'), whose "$" lets one trailing "\n" through
// into the generated guide and Compose file; Go refuses it with the same
// error as any other invalid server address.
func isServerURL(server string) bool {
	rest, found := strings.CutPrefix(server, "http")
	if !found {
		return false
	}
	rest = strings.TrimPrefix(rest, "s")
	if rest, found = strings.CutPrefix(rest, "://"); !found {
		return false
	}
	return rest != "" && !strings.ContainsFunc(rest, unicode.IsSpace)
}

// A10's generators (docker_ha_compact.generate_compact,
// docker_ha_compose_direct.generate_direct, docker_ha_compose.generate_bundle).
