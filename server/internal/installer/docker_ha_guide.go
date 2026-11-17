package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/weburl"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

// Port of backend/agents/docker_ha_guide.py: copyable Docker HA setup steps;
// no Docker host is contacted by this module.

// DockerHAImage is IMAGE.
const DockerHAImage = "thsrite/config-flow-agent:latest"

// TypeError is a Python exception other than ValueError (TypeError,
// KeyError, AttributeError …) raised by the guide or enrollment code: the
// route answers it with the 500 page. Error() is only logged.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

func typeError(message string) error { return &TypeError{Message: message} }

// Enrollment is the capability dict docker_ha_enrollment.issue returns
// ({'url', 'token', 'expires_at'}); nil is None.
type Enrollment struct {
	URL       string
	Token     string
	ExpiresAt any
}

func enrollmentURL(e *Enrollment) string {
	if e == nil {
		return ""
	}
	return e.URL
}

// posixNormpath is posixpath.normpath for a str.
func posixNormpath(path string) string {
	if path == "" {
		return "."
	}
	initial := 0
	if strings.HasPrefix(path, "/") {
		initial = 1
		if strings.HasPrefix(path, "//") && !strings.HasPrefix(path, "///") {
			initial = 2
		}
	}
	var comps []string
	for _, comp := range strings.Split(path, "/") {
		if comp == "" || comp == "." {
			continue
		}
		if comp != ".." || (initial == 0 && len(comps) == 0) || (len(comps) > 0 && comps[len(comps)-1] == "..") {
			comps = append(comps, comp)
		} else if len(comps) > 0 {
			comps = comps[:len(comps)-1]
		}
	}
	result := strings.Repeat("/", initial) + strings.Join(comps, "/")
	if result == "" {
		return "."
	}
	return result
}

// posixDirname is posixpath.dirname.
func posixDirname(path string) string {
	head := path[:strings.LastIndex(path, "/")+1]
	if head != "" && head != strings.Repeat("/", len(head)) {
		head = strings.TrimRight(head, "/")
	}
	return head
}

// posixCommonpath is posixpath.commonpath((left, right)) for two absolute paths.
func posixCommonpath(left, right string) string {
	parts := func(path string) []string {
		var out []string
		for _, c := range strings.Split(path, "/") {
			if c != "" && c != "." {
				out = append(out, c)
			}
		}
		return out
	}
	a, b := parts(left), parts(right)
	var common []string
	for i := 0; i < len(a) && i < len(b) && a[i] == b[i]; i++ {
		common = append(common, a[i])
	}
	return "/" + strings.Join(common, "/")
}

// guidePath is _path(value, label, allow_colon).
func guidePath(value any, label string, allowColon bool) (string, error) {
	text, isString := value.(string)
	bad := ",:\x00\r\n"
	if allowColon {
		bad = ",\x00\r\n"
	}
	if !isString || !strings.HasPrefix(text, "/") || text == "/" || posixNormpath(text) != text || strings.ContainsAny(text, bad) {
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
func guideName(value any, label string) (string, error) {
	text, isString := value.(string)
	if !isString || !isGuideName(text) {
		return "", valueError(label + "只能包含字母、数字、点、下划线或短横线")
	}
	return text, nil
}

// guideMount is _mount(value, label): {'type', 'source'}.
func guideMount(value any, label string) (*jsonx.Object, error) {
	o, isObject := value.(*jsonx.Object)
	kind, _ := o.GetOr("type", nil).(string)
	if !isObject || (kind != "bind" && kind != "volume") {
		return nil, valueError(label + "请选择 bind 目录或 Docker 命名卷")
	}
	var source string
	var err error
	if kind == "bind" {
		source, err = guidePath(o.GetOr("source", nil), label, false)
	} else {
		source, err = guideName(o.GetOr("source", nil), label)
	}
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("type", kind, "source", source), nil
}

// guideVolume is _volume(mount, target, readonly).
func guideVolume(mount *jsonx.Object, target string, readonly bool) *jsonx.Object {
	result := mount.Clone()
	result.Set("target", target)
	result.Set("read_only", readonly)
	if mount.GetString("type") == "bind" {
		result.Set("bind", jsonx.Obj("create_host_path", false))
	}
	return result
}

// mountArg is _mount_arg(mount).
func mountArg(mount *jsonx.Object) (string, error) {
	var parts [3]string
	for i, key := range []string{"type", "source", "target"} {
		text, isString := mount.GetOr(key, nil).(string)
		if !isString {
			return "", typeError("can only concatenate str (not \"" + pyTypeName(mount.GetOr(key, nil)) + "\") to str")
		}
		parts[i] = text
	}
	text := "type=" + parts[0] + ",src=" + parts[1] + ",dst=" + parts[2]
	if jsonx.Truthy(mount.GetOr("read_only", nil)) {
		text += ",readonly"
	}
	return text, nil
}

func pyTypeName(v any) string {
	switch x := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case jsonx.Number:
		if x.IsInt() {
			return "int"
		}
		return "float"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	}
	return "object"
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
func IdentityCommand(value any) (string, error) {
	path, err := guidePath(value, "宿主身份文件", false)
	if err != nil {
		return "", err
	}
	return haHeader() + "cf_identity_path=" + q(path) + "\n" +
		`[ ! -e "$cf_identity_path" ] && [ ! -L "$cf_identity_path" ] || ` +
		haFail("目标已存在（包括软链接）；未改写，请沿用有效身份文件或另选路径") + "\n" +
		"mkdir -p -- " + q(posixDirname(path)) + "\n" +
		`cf_identity=$(tr -cd "0123456789abcdefABCDEF" < /proc/sys/kernel/random/uuid)` + "\n" +
		`[[ "$cf_identity" =~ ^[0-9a-fA-F]{32}$ ]] || ` + haFail("无法读取内核随机 UUID") + "\n" +
		`(set -o noclobber; printf "%s\n" "$cf_identity" > "$cf_identity_path") || ` +
		haFail("身份文件被并发创建或无法写入；未覆盖任何现有文件") + "\n" +
		`printf "%s\n" "宿主身份文件已创建；同一宿主所有实例沿用此文件，不要复制到第二台宿主。"` + "\n", nil
}

// InspectionCommand is inspection_command(container_name).
func InspectionCommand(containerName any) (string, error) {
	command := CollectorScript()
	if jsonx.Truthy(containerName) {
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

func haString(v any) (string, error) {
	text, isString := v.(string)
	if !isString {
		return "", typeError("expected string or bytes-like object, got '" + pyTypeName(v) + "'")
	}
	return text, nil
}

// haRun is _run(service, cid_file, env_file, cid_variable).
func haRun(service *jsonx.Object, cidFile, envFile, cidVariable string) (string, error) {
	var args []shellArg
	add := func(values ...string) {
		for _, v := range values {
			args = append(args, shellArg{text: v})
		}
	}
	name, err := haString(service.GetOr("container_name", nil))
	if err != nil {
		return "", err
	}
	add("docker", "run", "-d", "--name", name, "--restart", "unless-stopped", "--stop-timeout", "30")
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
	if owner := service.GetOr("_owner", nil); jsonx.Truthy(owner) {
		add("--label", "io.configflow.dns-ha.installation="+owner.(string))
	}
	if mode := service.GetOr("network_mode", nil); jsonx.Truthy(mode) {
		text, err := haString(mode)
		if err != nil {
			return "", err
		}
		add("--network", text)
	} else {
		network, err := haString(service.GetOr("_network_name", nil))
		if err != nil {
			return "", err
		}
		ip, err := haString(jsonx.Path(service, "networks", "dns-ha", "ipv4_address"))
		if err != nil {
			return "", err
		}
		add("--network", network, "--ip", ip)
	}
	for _, capability := range service.GetList("cap_add") {
		text, err := haString(capability)
		if err != nil {
			return "", err
		}
		add("--cap-add", text)
	}
	for _, mount := range service.GetList("volumes") {
		o, _ := mount.(*jsonx.Object)
		if o == nil {
			return "", typeError("string indices must be integers, not 'str'")
		}
		text, err := mountArg(o)
		if err != nil {
			return "", err
		}
		add("--mount", text)
	}
	for _, env := range service.GetList("environment") {
		text, err := haString(env)
		if err != nil {
			return "", err
		}
		add("-e", text)
	}
	image, err := haString(service.GetOr("image", nil))
	if err != nil {
		return "", err
	}
	add(image)
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

// pyIPv4FromInt is ipaddress.IPv4Address(int) (bool is an int): the address
// when 0 <= value < 2**32.
func pyIPv4FromInt(value any) (uint32, bool, bool) {
	var n *big.Int
	switch x := value.(type) {
	case bool:
		n = big.NewInt(0)
		if x {
			n = big.NewInt(1)
		}
	case jsonx.Number:
		if !x.IsInt() {
			return 0, false, false
		}
		n, _ = x.BigInt()
	default:
		return 0, false, false
	}
	if n.Sign() < 0 || n.BitLen() > 32 {
		return 0, true, false
	}
	return uint32(n.Uint64()), true, true
}

func usableAddress(v uint32) bool {
	return !(isLoopback(v) || isMulticast(v) || isLinkLocal(v) || isUnspecified(v) || isReserved(v))
}

// guideCompose is yaml.safe_load(generate_docker_agent_compose(**params))
// for the guide's params dict. Values of a JSON body that are not strings
// follow Python: ipaddress accepts ints for the addresses, re raises a
// TypeError for the name and parent, other values are invalid addresses.
func guideCompose(params *jsonx.Object) (*jsonx.Object, error) {
	agentName, isString := params.GetOr("agent_name", nil).(string)
	if !isString {
		return nil, typeError("expected string or bytes-like object, got '" + pyTypeName(params.GetOr("agent_name", nil)) + "'")
	}
	options := NewDockerOptions(params.GetString("server_url"))
	options.AgentName = agentName
	options.DataDir = params.GetString("data_dir")
	options.EnableMihomo, options.EnableOxidns, options.EnableDNSHA = false, true, true
	if port, ok := params.GetNumber("oxidns_port"); ok {
		options.OxidnsPort, _ = port.BigInt()
	}
	if mode, isString := params.GetOr("network_mode", nil).(string); isString {
		options.NetworkMode = mode
	} else {
		options.NetworkMode = "\x00" // not in ("host", "macvlan")
	}
	rawIP := params.GetOr("agent_ip", nil)
	intIP := false
	switch x := rawIP.(type) {
	case string:
		options.AgentIP = x
	default:
		options.AgentIP = "x" // str() of the value is no address
		if v, isInt, inRange := pyIPv4FromInt(x); isInt && inRange && usableAddress(v) {
			options.AgentIP, intIP = ipv4Text(v), true
		}
	}
	rawParent := params.GetOr("ha_parent", nil)
	if text, isString := rawParent.(string); isString {
		options.HAParent = text
	} else {
		options.HAParent = "\x00"
	}
	if text, isString := params.GetOr("ha_subnet", nil).(string); isString {
		options.HASubnet = text
	} else {
		options.HASubnet = "x" // IPv4Network(int) is a /32: refused like any non-address
	}
	switch x := params.GetOr("ha_gateway", nil).(type) {
	case string:
		options.HAGateway = x
	default:
		if jsonx.Truthy(x) {
			options.HAGateway = "x"
			if v, isInt, inRange := pyIPv4FromInt(x); isInt && inRange {
				options.HAGateway = ipv4Text(v)
			}
		}
	}
	if text, isString := params.GetOr("ha_host_id_path", nil).(string); isString {
		options.HAHostIDPath = text
	} else {
		options.HAHostIDPath = "" // not isinstance(path, str)
	}
	text, err := GenerateDockerAgentCompose(options)
	if err != nil {
		var ve *ValueError
		if _, isString := rawParent.(string); !isString && asValueError(err, &ve) && ve.Message == "请填写有效的宿主机网卡名称，如 eth0 或 enp1s0" {
			return nil, typeError("expected string or bytes-like object, got '" + pyTypeName(rawParent) + "'")
		}
		return nil, err
	}
	compose, err := readCompose(text)
	if err != nil {
		return nil, err
	}
	if intIP {
		// f"AGENT_IP={agent_ip}" and ipv4_address keep the int itself.
		agent := jsonx.Path(compose, "services", "agent").(*jsonx.Object)
		environment := agent.GetList("environment")
		for i, item := range environment {
			if item == "AGENT_IP="+options.AgentIP {
				environment[i] = "AGENT_IP=" + jsonx.Str(rawIP)
			}
		}
		if network, _ := jsonx.Path(agent, "networks", "dns-ha").(*jsonx.Object); network != nil {
			network.Set("ipv4_address", rawIP)
		}
	}
	return compose, nil
}

func asValueError(err error, target **ValueError) bool {
	ve, ok := err.(*ValueError)
	if ok {
		*target = ve
	}
	return ok
}

func haSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// haDumpCompose renders the compose graph and escapes Compose interpolation.
func haDumpCompose(compose *jsonx.Object) (string, error) {
	node, err := yamlbridge.Encode(compose)
	if err != nil {
		return "", err
	}
	data, err := yamlx.Marshal(node)
	text := string(data)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(text, "$", "$$"), nil
}

// bootstrapCommand is docker_ha_enrollment.bootstrap_command (replaceable in tests).
var bootstrapCommand = BootstrapCommand

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
func newInstall(params, request *jsonx.Object, enrollment *Enrollment) (string, string, error) {
	compose, err := guideCompose(params)
	if err != nil {
		return "", "", err
	}
	image := request.GetOr("image", DockerHAImage).(string)
	plan := params.Clone()
	plan.Set("image", image)
	plan.Set("enrollment_url", enrollmentURL(enrollment))
	planJSON, err := jsonx.Encode(plan, jsonx.Options{EnsureASCII: true, SortKeys: true, Compact: true})
	if err != nil {
		return "", "", err
	}
	owner := haSHA256Hex(planJSON)
	data := params.GetString("data_dir")
	state := data + "/.ha-install"
	services := compose.GetObject("services")
	for key, value := range services.All() {
		service := value.(*jsonx.Object)
		service.Set("image", image)
		service.Set("labels", jsonx.Obj("io.configflow.dns-ha.installation", owner))
		service.Set("_owner", owner)
		readonly := key == "ha-companion"
		volumes := []any{
			guideVolume(jsonx.Obj("type", "bind", "source", data+"/agent"), "/opt/configflow-agent", readonly),
			guideVolume(jsonx.Obj("type", "bind", "source", data+"/oxidns"), "/etc/oxidns", readonly),
			guideVolume(jsonx.Obj("type", "bind", "source", data+"/ha"), "/var/lib/configflow-ha", false),
			jsonx.Obj("type", "bind", "source", params.GetOr("ha_host_id_path", nil),
				"target", "/run/configflow-host-machine-id", "read_only", true,
				"bind", jsonx.Obj("create_host_path", false)),
		}
		if key == "agent" && params.GetOr("network_mode", nil) == "host" {
			volumes = append(volumes, jsonx.Obj("type", "bind", "source", "/proc/1/ns/net", "target", "/run/configflow-host-netns",
				"read_only", true, "bind", jsonx.Obj("create_host_path", false)))
		}
		service.Set("volumes", volumes)
	}
	hostIDPath, _ := params.GetOr("ha_host_id_path", nil).(string)
	script := haHeader() + haDaemonCheck() + haIdentityCheck(hostIDPath)
	script += "cf_data=" + q(data) + "\ncf_state=" + q(state) + "\ncf_owner=" + q(owner) + "\n"
	script += `[ ! -L "$cf_data" ] && [ ! -L "$cf_state" ] && [ ! -L "$cf_state/setup" ] || ` + haFail("安装目录或标记为软链接；拒绝继续") + "\n"
	script += `if [ -e "$cf_state/setup" ]; then` + "\n" + `  [ -f "$cf_state/setup" ] && [ "$(cat "$cf_state/setup")" = "$cf_owner" ] || ` + haFail("数据目录属于另一份安装设置，请保留原命令续装或使用迁移") + "\nelse\n"
	script += `  [ ! -e "$cf_state" ] && [ ! -e "$cf_data/agent/config-oxidns.json" ] && [ ! -L "$cf_data/agent/config-oxidns.json" ] || ` + haFail("目录已有身份或未识别的安装状态；请使用原命令或迁移，不能接管") + "\n"
	agentName := params.GetString("agent_name")
	for _, name := range []string{agentName + "-oxidns", agentName + "-oxidns-ha"} {
		script += "  ! docker container inspect " + q(name) + " >/dev/null 2>&1 || " + haFail("同名容器已存在且不属于本次安装："+name) + "\n"
	}
	script += `  mkdir -p -- "$cf_data"` + "\n" + `  mkdir -- "$cf_state"` + "\n" + `  (set -o noclobber; printf "%s\n" "$cf_owner" > "$cf_state/setup")` + "\nfi\n"
	script += ownedContainerFunction + haStartupChecks
	main := services.GetObject("agent")
	mainName, err := haString(main.GetOr("container_name", nil))
	if err != nil {
		return "", "", err
	}
	script += "cf_main_id=$(cf_owned_container main " + q(mainName) + ")\n"
	companion := services.GetObject("ha-companion")
	if companion != nil {
		name, err := haString(companion.GetOr("container_name", nil))
		if err != nil {
			return "", "", err
		}
		script += "cf_companion_id=$(cf_owned_container companion " + q(name) + ")\n"
	}
	if networks := compose.GetOr("networks", nil); jsonx.Truthy(networks) {
		network := jsonx.Path(networks, "dns-ha").(*jsonx.Object)
		network.Set("labels", jsonx.Obj("io.configflow.dns-ha.installation", owner))
		name := network.GetString("name")
		main.Set("_network_name", name)
		ipam := jsonx.Path(network, "ipam", "config").([]any)[0].(*jsonx.Object)
		parent := jsonx.Path(network, "driver_opts", "parent").(string)
		subnet := ipam.GetString("subnet")
		gateway := ipam.GetString("gateway")
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
	if params.GetString("network_mode") == "host" {
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
		proofMount, _ := mountArg(guideVolume(jsonx.Obj("type", "bind", "source", data+"/agent"), "/opt/configflow-agent", true))
		args := []string{"docker", "run", "--rm", "--network", "none", "--entrypoint", "python3", "--mount", proofMount,
			image, "-c", enrollmentProof, enrollment.URL}
		script += "  " + joinQuoted(args) + "\nfi\n"
	} else {
		script += `[ ! -e "$cf_data/agent/config-oxidns.json" ] || ` + haFail("缺少本次注册凭据；请在页面重新生成安装命令") + "\n"
	}
	for _, role := range []struct {
		name    string
		service *jsonx.Object
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
	for _, value := range services.All() {
		service := value.(*jsonx.Object)
		service.Delete("_owner")
		service.Delete("_network_name")
	}
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
func migration(params, request *jsonx.Object) (string, string, string, error) {
	old, err := guideName(request.GetOr("container_name", nil), "原容器名")
	if err != nil {
		return "", "", "", err
	}
	oldAgent, isString := request.GetOr("existing_agent_name", nil).(string)
	if !isString {
		return "", "", "", valueError("请填写原 Agent 完整名称")
	}
	if err := ValidateAgentName(oldAgent); err != nil {
		return "", "", "", err
	}
	agent, err := guideMount(request.GetOr("agent_mount", nil), "原 Agent 挂载")
	if err != nil {
		return "", "", "", err
	}
	core, err := guideMount(request.GetOr("core_mount", nil), "原 OxiDNS 挂载")
	if err != nil {
		return "", "", "", err
	}
	haDir, err := guidePath(request.GetOr("ha_data_dir", nil), "新增 HA 目录", false)
	if err != nil {
		return "", "", "", err
	}
	backup, err := guidePath(request.GetOr("backup_dir", nil), "备份目录", false)
	if err != nil {
		return "", "", "", err
	}
	directories := []string{haDir, backup}
	for _, m := range []*jsonx.Object{agent, core} {
		if m.GetString("type") == "bind" {
			directories = append(directories, m.GetString("source"))
		}
	}
	for i, left := range directories {
		for _, right := range directories[i+1:] {
			if common := posixCommonpath(left, right); common == left || common == right {
				return "", "", "", valueError("Agent、OxiDNS、HA 和备份目录须彼此独立，不能相同或互相包含")
			}
		}
	}
	if jsonx.Equal(agent, core) {
		return "", "", "", valueError("Agent 和 OxiDNS 必须使用不同的数据挂载")
	}
	params = params.Clone()
	params.Set("data_dir", "/unused-migration-path")
	compose, err := guideCompose(params)
	if err != nil {
		return "", "", "", err
	}
	services := compose.GetObject("services")
	main := services.GetObject("agent")
	main.Set("container_name", old)
	var environment []any
	for _, item := range main.GetList("environment") {
		v, _ := item.(string)
		if strings.HasPrefix(v, "TZ=") {
			continue
		}
		if strings.HasPrefix(v, "AGENT_OXIDNS_NAME=") {
			v = "AGENT_OXIDNS_NAME=" + oldAgent
		}
		environment = append(environment, v)
	}
	main.Set("environment", environment)
	main.Set("env_file", []any{jsonx.Obj("path", backup+"/runtime.env", "format", "raw")})
	private := jsonx.Obj("type", "bind", "source", haDir)
	image := request.GetOr("image", DockerHAImage).(string)
	for key, value := range services.All() {
		service := value.(*jsonx.Object)
		readonly := key == "ha-companion"
		service.Set("image", image)
		volumes := []any{guideVolume(agent, "/opt/configflow-agent", readonly), guideVolume(core, "/etc/oxidns", readonly),
			guideVolume(private, "/var/lib/configflow-ha", false)}
		for _, v := range service.GetList("volumes") {
			if _, isObject := v.(*jsonx.Object); isObject {
				volumes = append(volumes, v)
			}
		}
		service.Set("volumes", volumes)
		if readonly {
			service.Set("container_name", old+"-ha")
		}
	}
	networks := compose.GetOr("networks", nil)
	if jsonx.Truthy(networks) {
		main.Set("_network_name", jsonx.Path(networks, "dns-ha", "name"))
	}
	external := jsonx.NewObject()
	for _, m := range []*jsonx.Object{agent, core} {
		if m.GetString("type") == "volume" {
			external.Set(m.GetString("source"), jsonx.Obj("external", true, "name", m.GetString("source")))
		}
	}
	if external.Len() > 0 {
		compose.Set("volumes", external)
	}
	previous, failed, companion := old+"-pre-ha", old+"-failed-ha", old+"-ha"
	hostIDPath, _ := params.GetOr("ha_host_id_path", nil).(string)
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
		mount  *jsonx.Object
		target string
	}{{agent, "/opt/configflow-agent"}, {core, "/etc/oxidns"}} {
		targetJSON, _ := jsonx.Default(pair.target)
		format := "{{range .Mounts}}{{if eq .Destination " + string(targetJSON) + `}}{{.Type}}|{{if eq .Type "volume"}}{{.Name}}{{else}}{{.Source}}{{end}}|{{.RW}}{{end}}{{end}}`
		kind, source := pair.mount.GetString("type"), pair.mount.GetString("source")
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
	port, _ := params.GetNumber("oxidns_port")
	checkCommand := `docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker exec -i "$cf_old_id" python3 -c ` + q(migrationCheck) + " " +
		joinQuoted([]string{oldAgent, params.GetString("server_url"), port.String()})
	script += "cf_runtime_env=$(" + checkCommand + ")\n"
	script += "docker image inspect " + q(image) + " >/dev/null 2>&1 || docker pull " + q(image) + "\n"
	script += "[ ! -e " + q(haDir) + " ] && [ ! -L " + q(haDir) + " ] || " + haFail("请选择全新 HA 目录，不能复用已有 HA 状态") + "\n"
	script += "mkdir -p -- " + q(posixDirname(backup)) + "\nmkdir -- \"$cf_backup\"\n"
	script += `printf "%s\n" "$cf_old_id" > "$cf_backup/old-id"` + "\n" + `printf "%s\n" "$cf_runtime_env" > "$cf_backup/runtime.env"` + "\n"
	script += `docker inspect --format '{{.HostConfig.RestartPolicy.Name}}:{{.HostConfig.RestartPolicy.MaximumRetryCount}}' "$cf_old_id" > "$cf_backup/restart-policy"` + "\n"
	if jsonx.Truthy(networks) {
		network := jsonx.Path(networks, "dns-ha").(*jsonx.Object)
		ipam := jsonx.Path(network, "ipam", "config").([]any)[0].(*jsonx.Object)
		args := []string{"docker", "network", "create", "--driver", "macvlan", "--subnet", ipam.GetString("subnet"), "--opt",
			"parent=" + jsonx.Path(network, "driver_opts", "parent").(string)}
		if gateway := ipam.GetString("gateway"); gateway != "" {
			args = append(args, "--gateway", gateway)
		}
		script += joinQuoted(append(args, network.GetString("name"))) + "\n"
	}
	script += "mkdir -p -- " + q(posixDirname(haDir)) + "\nmkdir -- " + q(haDir) + "\n"
	script += "docker update --restart=no \"$cf_old_id\" >/dev/null\ndocker stop \"$cf_old_id\" >/dev/null\n"
	script += "mkdir \"$cf_backup/agent\" \"$cf_backup/oxidns\"\n"
	script += "docker cp \"$cf_old_id:/opt/configflow-agent/.\" \"$cf_backup/agent/\"\ndocker cp \"$cf_old_id:/etc/oxidns/.\" \"$cf_backup/oxidns/\"\n"
	script += "docker rename \"$cf_old_id\" " + q(previous) + "\n"
	if helper := services.GetObject("ha-companion"); helper != nil {
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
	main.Delete("_network_name")
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
	return rest != "" && !strings.ContainsFunc(rest, pystr.IsSpace)
}

// A10's generators (docker_ha_compact.generate_compact,
// docker_ha_compose_direct.generate_direct, docker_ha_compose.generate_bundle).
var (
	generateCompact func(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error)
	generateDirect  func(params, request *jsonx.Object, enrollment *Enrollment) (string, *jsonx.Object, error)
	generateBundle  func(params, request *jsonx.Object, enrollment *Enrollment) (string, any, error)
)

// ErrComposeVariantNotPorted is returned for the Compose install methods
// until module A10 provides their generators.
var ErrComposeVariantNotPorted = typeError("Docker HA Compose variants are not ported yet (A10)")

// GenerateGuide is generate_guide(request, enrollment): the guide answer, or
// a *ValueError (400) or *TypeError (500).
func GenerateGuide(value any, enrollment *Enrollment) (*jsonx.Object, error) {
	request, isObject := value.(*jsonx.Object)
	if !isObject {
		return nil, valueError("请提交 JSON 对象")
	}
	action := request.GetOr("action", "generate")
	switch action {
	case "check":
		report, err := ParsePreflightReport(request.GetOr("output", nil), request.GetOr("network_mode", "host"))
		if err != nil {
			return nil, err
		}
		return jsonx.Obj("success", true, "preflight_report", report), nil
	case "inspect":
		command, err := InspectionCommand(request.GetOr("container_name", ""))
		if err != nil {
			return nil, err
		}
		return jsonx.Obj("success", true, "inspection_command", command), nil
	case "verify":
		raw := request.GetOr("vip", nil)
		var vip uint32
		valid := false
		if text, isString := raw.(string); isString {
			v, err := parseIPv4(text)
			vip, valid = v, err == nil
		} else if v, isInt, inRange := pyIPv4FromInt(raw); isInt && inRange {
			vip, valid = v, true
		}
		if !valid || !usableAddress(vip) {
			return nil, valueError("请填写有效的虚拟 IPv4 地址")
		}
		return jsonx.Obj("success", true, "verification_command", strings.ReplaceAll(haVerification, "{{VIP}}", ipv4Text(vip))), nil
	}
	mode := request.GetOr("mode", "new")
	if action != "generate" || (mode != "new" && mode != "migrate") {
		return nil, valueError("安装步骤或模式无效")
	}
	method := request.GetOr("install_method", "script")
	if method != "script" && method != "compose" && method != "compose_direct" && method != "compose_compact" {
		return nil, valueError("install_method 须为 script、compose、compose_direct 或 compose_compact")
	}
	if method != "script" && mode != "new" {
		return nil, valueError("Compose 安装包仅支持新安装；已有容器请使用迁移脚本")
	}
	image, isString := request.GetOr("image", DockerHAImage).(string)
	if !isString || !isImageName(image) {
		return nil, valueError("Docker 镜像名称无效")
	}
	server, isString := request.GetOr("server_url", "").(string)
	if !isString || !isServerURL(server) {
		return nil, valueError("请填写完整的 ConfigFlow HTTP(S) 地址")
	}
	parsed, err := weburl.Parse(server)
	if err != nil {
		return nil, valueError(err.Error())
	}
	if weburl.HasCredentials(parsed) || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return nil, valueError("ConfigFlow 地址不能包含登录凭据、查询参数或片段")
	}
	server = strings.TrimRight(server, "/")
	port := request.GetOr("oxidns_port", jsonx.Int(8082))
	if number, isNumber := port.(jsonx.Number); !isNumber || !number.IsInt() {
		return nil, valueError("Agent 端口须为整数")
	}
	params := jsonx.Obj("server_url", server, "agent_name", request.GetOr("agent_name", "agent"),
		"agent_ip", request.GetOr("agent_ip", ""), "data_dir", request.GetOr("data_dir", ""),
		"network_mode", request.GetOr("network_mode", "host"),
		"enable_mihomo", false, "enable_oxidns", true, "enable_dns_ha", true, "oxidns_port", port,
		"ha_parent", request.GetOr("ha_parent", ""), "ha_subnet", request.GetOr("ha_subnet", ""),
		"ha_gateway", request.GetOr("ha_gateway", ""), "ha_host_id_path", request.GetOr("ha_host_id_path", "/etc/machine-id"))
	var createIdentity any = false
	if method != "compose_compact" {
		createIdentity = request.GetOr("create_host_identity", false)
	}
	create, isBool := createIdentity.(bool)
	if !isBool {
		return nil, valueError("create_host_identity 须为布尔值")
	}
	if method != "compose_compact" && !(method == "compose_direct" && create) {
		if _, err := guidePath(params.Value("ha_host_id_path"), "宿主身份文件", false); err != nil {
			return nil, err
		}
	}
	identity := ""
	if create && method != "compose_direct" && method != "compose_compact" {
		if identity, err = IdentityCommand(params.Value("ha_host_id_path")); err != nil {
			return nil, err
		}
	}
	var compose, install, rollback string
	var direct *jsonx.Object
	if mode == "migrate" {
		if compose, install, rollback, err = migration(params, request); err != nil {
			return nil, err
		}
	} else {
		dataDir, err := guidePath(params.Value("data_dir"), "持久数据根目录", false)
		if err != nil {
			return nil, err
		}
		params.Set("data_dir", dataDir)
		switch method {
		case "compose_compact", "compose_direct":
			generator := generateCompact
			if method == "compose_direct" {
				generator = generateDirect
			}
			if generator == nil {
				return nil, ErrComposeVariantNotPorted
			}
			if compose, direct, err = generator(params, request, enrollment); err != nil {
				return nil, err
			}
			install, _ = direct.GetOr("command", nil).(string)
		default:
			if compose, install, err = newInstall(params, request, enrollment); err != nil {
				return nil, err
			}
		}
	}
	var containerName any = ""
	if mode == "migrate" {
		containerName = request.GetOr("container_name", "")
	}
	inspection, err := InspectionCommand(containerName)
	if err != nil {
		return nil, err
	}
	logs := "docker compose logs --tail 80\n"
	if method != "compose_direct" && method != "compose_compact" {
		logsName := params.GetString("agent_name") + "-oxidns"
		if mode == "migrate" {
			logsName = request.GetString("container_name")
		}
		logs = "docker logs --tail 80 " + q(logsName) + "\n"
	}
	blocks := []any{jsonx.Obj("id", "inspect", "title", "只读检查宿主机与原容器", "command", inspection)}
	if identity != "" {
		blocks = append(blocks, jsonx.Obj("id", "identity", "title", "仅在身份文件缺失时创建一次", "command", identity))
	}
	installTitle := "安装当前主机"
	if mode == "migrate" {
		installTitle = "备份并迁移当前主机"
	}
	blocks = append(blocks, jsonx.Obj("id", "install", "title", installTitle, "command", install),
		jsonx.Obj("id", "logs", "title", "查看启动日志", "command", logs))
	if rollback != "" {
		blocks = append(blocks, jsonx.Obj("id", "rollback", "title", "需要时恢复原容器", "command", rollback))
	}
	warnings := []any{"首次安装请先执行安装命令完成安全注册；Compose 仅供查看或后续维护，直接 Compose up 不会执行注册步骤。"}
	if mode == "migrate" {
		warnings = []any{"迁移仅适用于 ConfigFlow 单 OxiDNS 默认启动容器；先执行迁移脚本，不要直接 Compose up 跳过原挂载核对和备份。",
			"已有高可用组请先在页面处理虚拟 IP 与停用；脚本拒绝迁移仍有 HA 记录的容器。",
			"备份包含 Agent 身份与配置，请保护备份目录；旧容器和数据不会被删除。",
			"迁移 Compose 的 raw 环境文件格式需要 Docker Compose 2.30+；安装命令使用 Docker Run，不依赖 Compose 版本。"}
	}
	result := jsonx.Obj("success", true, "inspection_command", inspection, "identity_command", identity, "compose", compose,
		"install_command", install, "rollback_command", rollback, "command_blocks", blocks, "warnings", warnings)
	switch method {
	case "compose":
		if generateBundle == nil {
			return nil, ErrComposeVariantNotPorted
		}
		bundleCompose, bundle, err := generateBundle(params, request, enrollment)
		if err != nil {
			return nil, err
		}
		result.Set("compose", bundleCompose)
		result.Set("compose_bundle", bundle)
		result.Set("warnings", []any{"安装包仅支持新安装：解压后先执行 sudo bash prepare.sh，再运行 docker compose up -d；需要 Compose 2.20+。",
			"安装包包含限定本机的首次登记凭据，请勿公开分享；已登记并持久保存身份后，重启不依赖该凭据或登记 API。",
			"请在 Compose 安装包和备用安装脚本中选择一种方式，不要对同一数据目录混用。"})
	case "compose_direct":
		result.Set("compose_deployment", direct)
		result.Set("warnings", []any{"只需保存 compose.yaml 并运行 docker compose up -d；需要包含内置安装器的新镜像和 Compose 2.20+。",
			"YAML 包含仅用于本设备的24小时登记凭据，也可被 Docker inspect 读取；请勿公开分享。已保存 Agent 身份后重启不依赖该凭据。",
			"初始化仅写应用数据目录；选用自动身份时还使用固定 Docker 身份卷。不要复制或删除该共享身份卷，不要运行 down -v。"})
	case "compose_compact":
		result.Set("compose_deployment", direct)
		result.Set("warnings", []any{"保存一份 compose.yaml 后启动即可；需要支持精简安装的新镜像与 Compose 2.20+。",
			"INSTALL_KEY 是本设备的限时安装凭据，请勿分享。首次登记完成后可移除，重建继续复用持久身份。",
			"安装身份保存在 Agent 数据目录中；两端需提供只读宿主校验信息，旧安装请沿用原 Compose 或使用迁移入口。"})
	}
	return result, nil
}
