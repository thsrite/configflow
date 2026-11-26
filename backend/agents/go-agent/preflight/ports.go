package preflight

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type portBinding struct {
	Protocol string
	Address  netip.Addr
	Port     int
}

type portRequirement struct {
	portBinding
	Config int
	Agent  bool
	Name   string
}

type portProcess struct {
	PID       int
	Exe       string
	Args      []string
	Cwd       string
	Cgroups   string
	Uncertain bool
}

type portListener struct {
	portBinding
	Inode          string
	Owners         []portProcess
	Uncertain      bool
	IncompleteView bool
}

// The probe never binds a socket or changes a process. Tests provide snapshots
// so they exercise conflicts and ownership without depending on the host ports.
type portProbe interface {
	Listeners() ([]portListener, error)
	Executable(string) (string, error)
	AgentConfig(string) (Config, error)
}

type procPortProbe struct{ Root string }

func portChecks(configs []Config) []Check {
	return portChecksWithProbe(configs, procPortProbe{Root: "/proc"})
}

func portChecksWithProbe(configs []Config, probe portProbe) []Check {
	var checks []Check
	var requirements []portRequirement
	for index, cfg := range configs {
		host := cfg.AgentHost
		if host == "" {
			host = "0.0.0.0"
		}
		bindings, err := parsePortAddress(net.JoinHostPort(host, strconv.Itoa(cfg.AgentPort)), "tcp")
		if err != nil {
			checks = append(checks, Check{Name: cfg.ServiceType + " Agent 端口", Level: "error", Message: "Agent 监听地址或端口无效，无法检查占用", Fix: "填写有效的监听 IP 和 1–65535 的端口后重新生成脚本。"})
		} else {
			for _, binding := range bindings {
				requirements = append(requirements, portRequirement{portBinding: binding, Config: index, Agent: true, Name: cfg.ServiceType + " Agent"})
			}
		}
		doc, err := loadCoreConfig(cfg)
		if err != nil {
			// Run reports the sanitized configuration error independently.
			continue
		}
		core, coreChecks := corePortRequirements(cfg, doc, index)
		requirements = append(requirements, core...)
		checks = append(checks, coreChecks...)
	}
	for i, requirement := range requirements {
		for _, other := range requirements[:i] {
			if portBindingsOverlap(requirement.portBinding, other.portBinding) {
				checks = append(checks, Check{Name: "安装参数端口冲突", Level: "error", Message: fmt.Sprintf("%s 与 %s 将同时占用 %s %d 的重叠地址", requirement.Name, other.Name, requirement.Protocol, requirement.Port), Fix: "为两个 Agent 选择不同端口，或修改核心 DNS 监听地址/端口，使监听范围不重叠。IPv6 通配监听可能双栈占用，带接口范围的 IPv6 地址也按可能重叠检查。"})
			}
		}
	}
	listeners, err := probe.Listeners()
	if err != nil {
		return append(checks, Check{Name: "端口占用检查", Level: "error", Message: "无法读取 /proc 的网络监听或进程信息，不能确认端口是否可用", Fix: "在目标 Linux 系统内以 root 运行，检查 /proc 是否正确挂载及可读，并用 ss -lntup 核对监听进程。"})
	}
	for _, listener := range listeners {
		if listener.IncompleteView {
			checks = append(checks, Check{Name: "进程可见性", Level: "warn", Message: "部分 /proc 进程信息不可读；已找到的监听进程仍逐一核实，未找到归属的占用将阻止安装", Fix: "用 ss -lntup 复核目标端口；LXC 内可能受进程可见性限制，不要因此放宽整个容器的安全权限。"})
			break
		}
	}
	executables := make([]string, len(configs))
	for i, cfg := range configs {
		binaryName := cfg.ServiceBinary
		if binaryName == "" {
			binaryName = cfg.ServiceType
		}
		executables[i], _ = probe.Executable(binaryName)
	}
	for _, requirement := range requirements {
		result := Check{Name: fmt.Sprintf("%s %s/%d", requirement.Name, requirement.Protocol, requirement.Port), Level: "pass", Message: "当前没有重叠的监听占用"}
		owned, unknown, external := false, false, false
		var pids []string
		for _, listener := range listeners {
			if !portBindingsOverlap(requirement.portBinding, listener.portBinding) {
				continue
			}
			if listener.Uncertain || len(listener.Owners) == 0 {
				unknown = true
			}
			for _, owner := range listener.Owners {
				pids = append(pids, strconv.Itoa(owner.PID))
				if owner.Uncertain {
					unknown = true
					continue
				}
				cfg := configs[requirement.Config]
				if requirement.Agent && portOwnerIsAgent(owner, cfg, probe) || !requirement.Agent && portOwnerIsCore(owner, cfg, executables[requirement.Config]) {
					owned = true
					continue
				}
				external = true
			}
		}
		switch {
		case unknown || external:
			result.Level = "error"
			result.Message = "监听端口被其他进程占用"
			if unknown {
				result.Message = "监听端口已有占用，且无法核实全部进程归属"
			}
			if len(pids) > 0 {
				result.Message += "（PID " + strings.Join(pids, ", ") + "）"
			}
			result.Fix = fmt.Sprintf("运行 ss -lntup 检查 %d 端口；确认占用服务用途后调整 Agent 端口或核心监听地址/端口。IPv6 通配监听可能双栈占用，带接口范围的 IPv6 地址也按可能重叠检查。不会自动终止任何进程。", requirement.Port)
		case owned:
			result.Message = "监听属于当前受管服务，可继续重装 Agent"
		}
		checks = append(checks, result)
	}
	return checks
}

func corePortRequirements(cfg Config, doc map[string]any, index int) ([]portRequirement, []Check) {
	var requirements []portRequirement
	var checks []Check
	unrecognized := func(message string) {
		checks = append(checks, Check{Name: cfg.ServiceType + " DNS 端口", Level: "error", Message: message, Fix: "检查核心实际配置并用 ss -lntup 核对监听；将外部文件中的 DNS 监听配置合并到主配置后重新检查。"})
	}
	add := func(address, protocol string) {
		bindings, err := parsePortAddress(address, protocol)
		if err != nil {
			checks = append(checks, Check{Name: cfg.ServiceType + " DNS 端口", Level: "error", Message: "DNS 监听地址缺失或无法解析，不能确认实际占用", Fix: "检查核心配置中的 DNS listen，填写明确的 IP:端口（IPv6 使用 [地址]:端口），再重新检查。"})
			return
		}
		for _, binding := range bindings {
			requirements = append(requirements, portRequirement{portBinding: binding, Config: index, Name: cfg.ServiceType + " DNS"})
		}
	}
	if cfg.ServiceType == "mihomo" {
		dns, isMap := doc["dns"].(map[string]any)
		if value, present := doc["dns"]; present && value != nil && !isMap {
			unrecognized("DNS 配置结构无法识别，不能判断是否启用监听")
		}
		enabled, isBool := dns["enable"].(bool)
		if _, present := dns["enable"]; present && !isBool {
			unrecognized("dns.enable 不是布尔值，不能判断是否启用监听")
		}
		if enabled {
			address, isString := dns["listen"].(string)
			if value, present := dns["listen"]; present && value != nil && !isString {
				unrecognized("dns.listen 不是字符串，无法判断外部 DNS 监听")
			}
			// Mihomo keeps its internal resolver enabled without opening a
			// TCP/UDP server when listen is absent or empty (ReCreateServer).
			if address != "" {
				add(address, "tcp")
				add(address, "udp")
			} else if len(checks) == 0 {
				checks = append(checks, Check{Name: "mihomo DNS 端口", Level: "pass", Message: "不适用：DNS 已启用内部解析，listen 为空，未配置外部 TCP/UDP 监听"})
			}
		}
	} else {
		if include, present := doc["include"]; present && include != nil {
			if items, ok := include.([]any); !ok || len(items) > 0 {
				unrecognized("配置含外部 include，DNS 监听检查未覆盖外部文件")
			}
		}
		plugins, isList := doc["plugins"].([]any)
		if !isList {
			unrecognized("plugins 缺失或不是列表，无法完整识别 DNS 监听")
		}
		for _, item := range plugins {
			plugin, isMap := item.(map[string]any)
			kind, isString := plugin["type"].(string)
			if !isMap || !isString || kind == "" {
				unrecognized("plugins 中存在无法识别的插件，不能完整判断 DNS 监听")
				continue
			}
			if kind != "udp_server" && kind != "tcp_server" {
				continue
			}
			args, _ := plugin["args"].(map[string]any)
			protocol := strings.TrimSuffix(kind, "_server")
			switch listen := args["listen"].(type) {
			case string:
				add(listen, protocol)
			case []any:
				if len(listen) == 0 {
					add("", protocol)
				}
				for _, item := range listen {
					address, _ := item.(string)
					add(address, protocol)
				}
			default:
				// Missing listen is not inferred as port 53: a malformed or
				// version-specific core config must not produce a false pass.
				add("", protocol)
			}
		}
	}
	if len(requirements) == 0 && len(checks) == 0 {
		checks = append(checks, Check{Name: cfg.ServiceType + " DNS 端口", Level: "pass", Message: "不适用：主配置未启用 UDP/TCP DNS 监听"})
	}
	return requirements, checks
}

func parsePortAddress(address, protocol string) ([]portBinding, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port")
	}
	if host == "localhost" {
		return []portBinding{{protocol, netip.MustParseAddr("127.0.0.1"), port}, {protocol, netip.IPv6Loopback(), port}}, nil
	}
	if host == "" || host == "*" {
		return []portBinding{{Protocol: protocol, Port: port}}, nil
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil, err
	}
	return []portBinding{{protocol, ip.WithZone("").Unmap(), port}}, nil
}

func portBindingsOverlap(a, b portBinding) bool {
	if a.Protocol != b.Protocol || a.Port != b.Port {
		return false
	}
	if !a.Address.IsValid() || !b.Address.IsValid() || a.Address == netip.IPv6Unspecified() || b.Address == netip.IPv6Unspecified() {
		// /proc does not expose IPV6_V6ONLY. Treat IPv6 wildcard as dual
		// stack rather than silently accepting a possible IPv4 conflict.
		return true
	}
	if a.Address.Is4() != b.Address.Is4() {
		return false
	}
	return a.Address.IsUnspecified() || b.Address.IsUnspecified() || a.Address == b.Address
}

func portOwnerIsCore(owner portProcess, cfg Config, executable string) bool {
	if executable == "" || owner.Exe != executable {
		return false
	}
	unit := strings.TrimSuffix(cfg.ServiceUnit, ".service")
	if unit == "" {
		unit = cfg.ServiceType
	}
	for _, line := range strings.Split(owner.Cgroups, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 {
			for _, component := range strings.Split(parts[2], "/") {
				if component == unit+".service" {
					return true
				}
			}
		}
	}
	base := owner.Cwd
	if directory, ok := portArgument(owner.Args, "-d"); ok {
		base = portProcessPath(directory, owner.Cwd)
	}
	for _, flag := range []string{"-c", "-f", "-config", "--config"} {
		if value, ok := portArgument(owner.Args, flag); ok {
			return portProcessPath(value, base) == portCanonicalPath(cfg.ConfigPath)
		}
	}
	if directory, ok := portArgument(owner.Args, "-d"); ok {
		return portProcessPath(filepath.Join(directory, "config.yaml"), owner.Cwd) == portCanonicalPath(cfg.ConfigPath)
	}
	return false
}

func portOwnerIsAgent(owner portProcess, cfg Config, probe portProbe) bool {
	if portOwnerIsLegacyShellAgent(owner, cfg, probe) {
		return true
	}
	directory := "/opt/configflow-agent/" + cfg.ServiceType
	installations := map[string][]string{
		"/usr/local/bin/configflow-agent":            {"/opt/configflow-agent/config-" + cfg.ServiceType + ".json", "/opt/configflow-agent/config.json"},
		filepath.Join(directory, "configflow-agent"): {filepath.Join(directory, "config.json")},
	}
	var configPaths []string
	for binaryPath, paths := range installations {
		if owner.Exe == portCanonicalPath(binaryPath) {
			configPaths = append(configPaths, paths...)
		}
	}
	for _, flag := range []string{"-config", "--config"} {
		if value, ok := portArgument(owner.Args, flag); ok {
			for _, expected := range configPaths {
				if portProcessPath(value, owner.Cwd) == portCanonicalPath(expected) {
					installed, err := probe.AgentConfig(expected)
					// Reinstallation may migrate the managed core's configuration
					// directory; it does not change the Agent listener's identity.
					return err == nil && installed.ServiceType == cfg.ServiceType && installed.AgentPort == cfg.AgentPort
				}
			}
		}
	}
	return false
}

func portOwnerIsLegacyShellAgent(owner portProcess, cfg Config, probe portProbe) bool {
	// The legacy installer only supports single-service upgrades. Recognize its
	// precise listener invocation, never arbitrary socat listeners or commands.
	if cfg.NativeAIO || owner.Uncertain || len(owner.Args) != 3 {
		return false
	}
	socat, err := probe.Executable("socat")
	if err != nil || socat == "" || owner.Exe != socat ||
		owner.Args[1] != fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", cfg.AgentPort) ||
		owner.Args[2] != "SYSTEM:/opt/configflow-agent/agent.sh __handle_request__" {
		return false
	}
	installed, err := probe.AgentConfig("/opt/configflow-agent/config.json")
	if err != nil || installed.ServiceType != cfg.ServiceType || installed.AgentPort != cfg.AgentPort {
		return false
	}
	managedUnit, systemdUnit := false, false
	for _, line := range strings.Split(owner.Cgroups, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		for _, component := range strings.Split(parts[2], "/") {
			if component == "configflow-agent.service" {
				managedUnit = true
			}
			if strings.HasSuffix(component, ".service") || strings.HasSuffix(component, ".scope") || component == "system.slice" || component == "user.slice" {
				systemdUnit = true
			}
		}
	}
	if systemdUnit {
		return managedUnit
	}
	// OpenRC has no systemd unit cgroup. Its installer starts the exact script
	// above from this fixed directory; the core's ServiceManager is unrelated.
	return portProcessPath(owner.Cwd, "") == portCanonicalPath("/opt/configflow-agent")
}

func portArgument(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1], true
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.TrimPrefix(arg, flag+"="), true
		}
	}
	return "", false
}

func portProcessPath(value, cwd string) string {
	if !filepath.IsAbs(value) {
		if cwd == "" {
			return ""
		}
		value = filepath.Join(cwd, value)
	}
	return portCanonicalPath(value)
}

func portCanonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func (probe procPortProbe) Executable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return portCanonicalPath(path), nil
}

func (probe procPortProbe) AgentConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func (probe procPortProbe) Listeners() ([]portListener, error) {
	var listeners []portListener
	for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
		file, err := os.Open(filepath.Join(probe.Root, "net", name))
		if err != nil {
			if strings.HasSuffix(name, "6") && os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		parsed, err := parseProcPorts(file, name)
		file.Close()
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, parsed...)
	}
	entries, err := os.ReadDir(probe.Root)
	if err != nil {
		return nil, err
	}
	byInode := map[string][]int{}
	for i, listener := range listeners {
		byInode[listener.Inode] = append(byInode[listener.Inode], i)
	}
	incomplete := false
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		directory := filepath.Join(probe.Root, entry.Name())
		fds, err := os.ReadDir(filepath.Join(directory, "fd"))
		if err != nil {
			if !os.IsNotExist(err) {
				incomplete = true
			}
			continue
		}
		owned := map[int]bool{}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(directory, "fd", fd.Name()))
			if err != nil {
				if !os.IsNotExist(err) {
					incomplete = true
				}
				continue
			}
			if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
				for _, index := range byInode[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] {
					owned[index] = true
				}
			}
		}
		if len(owned) == 0 {
			continue
		}
		executable, exeErr := os.Readlink(filepath.Join(directory, "exe"))
		args, argsErr := os.ReadFile(filepath.Join(directory, "cmdline"))
		cwd, _ := os.Readlink(filepath.Join(directory, "cwd"))
		cgroups, _ := os.ReadFile(filepath.Join(directory, "cgroup"))
		owner := portProcess{PID: pid, Exe: executable, Args: strings.Split(strings.TrimRight(string(args), "\x00"), "\x00"), Cwd: cwd, Cgroups: string(cgroups), Uncertain: exeErr != nil || argsErr != nil}
		for index := range owned {
			listeners[index].Owners = append(listeners[index].Owners, owner)
		}
	}
	for i := range listeners {
		listeners[i].Uncertain = len(listeners[i].Owners) == 0
		listeners[i].IncompleteView = incomplete
	}
	return listeners, nil
}

func parseProcPorts(file *os.File, table string) ([]portListener, error) {
	var result []portListener
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] == "sl" {
			continue
		}
		if len(fields) < 10 {
			return nil, fmt.Errorf("invalid proc network table")
		}
		if strings.HasPrefix(table, "tcp") && fields[3] != "0A" {
			continue
		}
		parts := strings.SplitN(fields[1], ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid proc address")
		}
		port, err := strconv.ParseUint(parts[1], 16, 16)
		if err != nil {
			return nil, err
		}
		if port == 0 {
			continue
		}
		address := parts[0]
		if len(address) != 8 && len(address) != 32 {
			return nil, fmt.Errorf("invalid proc IP")
		}
		bytes := make([]byte, len(address)/2)
		for i := 0; i < len(address); i += 8 {
			word, err := strconv.ParseUint(address[i:i+8], 16, 32)
			if err != nil {
				return nil, err
			}
			binary.NativeEndian.PutUint32(bytes[i/2:i/2+4], uint32(word))
		}
		ip, ok := netip.AddrFromSlice(bytes)
		if !ok {
			return nil, fmt.Errorf("invalid proc IP")
		}
		result = append(result, portListener{portBinding: portBinding{strings.TrimSuffix(table, "6"), ip.Unmap(), int(port)}, Inode: fields[9]})
	}
	return result, scanner.Err()
}
