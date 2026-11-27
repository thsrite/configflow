package routes

// Docker HA is explicitly opted in. Host mode delegates only keepalived to
// an independent companion; macvlan owns it in the isolated LAN namespace.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent/svc"

	"golang.org/x/sys/unix"
)

const oxidnsHAContainerProtocol = 1
const haDockerDirectory = "/var/lib/configflow-ha"
const haHostIDPath = "/run/configflow-host-machine-id"
const haHostIdentityDirectory = "/run/configflow-host-identity"
const haHostBootIDPath = "/run/configflow-host-boot-id"
const haNativeBootIDPath = "/proc/sys/kernel/random/boot_id"
const haHostNetNSPath = "/run/configflow-host-netns"
const haInstallationIdentityFile = ".configflow-ha-identity"
const haInstallationMarkerFile = ".configflow-compose-compact.json"

var haEnvironmentRead = os.ReadFile
var haEnvironmentStat = os.Stat
var haEnvironmentLstat = os.Lstat
var haBootIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var haInstallationIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var haCompanionPeerCheck = checkHACompanionPeer

type haNamespace struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type haCompanionEvidence struct {
	Protocol int         `json:"protocol"`
	Mode     string      `json:"mode"`
	HostID   string      `json:"host_id"`
	CapEff   string      `json:"cap_eff"`
	Network  haNamespace `json:"network_namespace"`
	PID      haNamespace `json:"pid_namespace"`
}

type haEnvironmentInfo struct {
	MissingTools *[]string `json:"missing_tools,omitempty"`
	Kind         string    `json:"kind"`
	Network      string    `json:"network,omitempty"`
	Ready        bool      `json:"ready"`
	HostID       string    `json:"host_id"`
	HostIDSource string    `json:"host_id_source"`
	HostBootID   string    `json:"host_boot_id,omitempty"`
	Interface    string    `json:"interface,omitempty"`
	Blockers     []string  `json:"blockers"`
}

func haDockerOptIn() bool     { return os.Getenv("CONFIGFLOW_DNS_HA") == "1" }
func haDockerNetwork() string { return os.Getenv("CONFIGFLOW_DNS_HA_NETWORK") }

func defaultHAKeepalivedDirectory() string {
	if haDockerOptIn() {
		return haDockerDirectory
	}
	return "/etc/keepalived"
}

// haCompanionSocket is the host-mode companion's control socket.
func haCompanionSocket() string { return filepath.Join(haKeepalivedDir, "run", "control.sock") }

// haContainerManager controls the managed keepalived program of Docker HA:
// the host-mode companion's through its control socket, otherwise this
// container's own.
var haContainerManager = func() svc.Manager {
	if haDockerNetwork() == "host" {
		return svc.Remote{Socket: haCompanionSocket()}
	}
	return containerServices
}

// haContainerState is the managed keepalived program state ("RUNNING",
// "STOPPED", ...), or "" when the program cannot be reached.
func haContainerState() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := haContainerManager().Status(ctx, svc.Keepalived)
	if err != nil {
		return ""
	}
	return string(status.State)
}

func haHostIdentity(path string) string {
	data, err := haEnvironmentRead(path)
	value := strings.TrimSpace(string(data))
	if err != nil || len(value) < 16 || len(value) > 128 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// Boot identity is live evidence only. It must never become journal ownership,
// since a reboot must not invalidate the persistent host identity.
func haBootIdentity(path string) string {
	data, err := haEnvironmentRead(path)
	value := strings.TrimSpace(string(data))
	if err != nil || !haBootIDPattern.MatchString(value) || value == "00000000-0000-0000-0000-000000000000" {
		return ""
	}
	digest := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(digest[:])
}

func haVolumeIdentity() string {
	if !haMounted(haHostIdentityDirectory, false) {
		return ""
	}
	directory, err := haEnvironmentLstat(haHostIdentityDirectory)
	if err != nil || !directory.IsDir() {
		return ""
	}
	path := filepath.Join(haHostIdentityDirectory, "host-id")
	info, err := haEnvironmentLstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return haHostIdentity(path)
}

// An installation identity owns persistent HA state. Only the separately
// mounted boot proof establishes host independence for this identity source.
func haInstallationIdentity(directory string) string {
	if !filepath.IsAbs(directory) || !haMounted(directory, true) {
		return ""
	}
	info, err := haEnvironmentLstat(directory)
	if err != nil || !info.IsDir() {
		return ""
	}
	readRegular := func(path string, limit int64) ([]byte, error) {
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("installation proof is not a regular file")
		}
		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil || int64(len(data)) > limit {
			return nil, fmt.Errorf("installation proof cannot be read safely")
		}
		return data, nil
	}
	data, err := readRegular(filepath.Join(directory, haInstallationIdentityFile), 128)
	identity := strings.TrimSpace(string(data))
	if err != nil || !haInstallationIDPattern.MatchString(identity) {
		return ""
	}
	data, err = readRegular(filepath.Join(directory, haInstallationMarkerFile), 65536)
	var record struct {
		Protocol       int    `json:"protocol"`
		Ready          bool   `json:"ready"`
		InstallationID string `json:"installation_id"`
	}
	if err != nil || json.Unmarshal(data, &record) != nil || record.Protocol != 2 || !record.Ready || record.InstallationID != identity {
		return ""
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func haNamespaceAt(path string) (haNamespace, error) {
	info, err := haEnvironmentStat(path)
	if err != nil {
		return haNamespace{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return haNamespace{}, fmt.Errorf("无法读取命名空间身份")
	}
	return haNamespace{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func haMountPath(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}

func haMounted(path string, writable bool) bool {
	data, err := haEnvironmentRead("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) < 6 || len(right) < 1 || haMountPath(left[4]) != filepath.Clean(path) {
			continue
		}
		options := "," + left[5] + ","
		if writable {
			return strings.Contains(options, ",rw,") && right[0] != "tmpfs" && right[0] != "ramfs"
		}
		return strings.Contains(options, ",ro,")
	}
	return false
}

func haNetworkCaps(value string) bool {
	bits, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
	return err == nil && bits&(uint64(1)<<12|uint64(1)<<13) == uint64(1)<<12|uint64(1)<<13
}

func haCurrentCaps() string {
	data, _ := haEnvironmentRead("/proc/self/status")
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		}
	}
	return ""
}

func checkHACompanionPeer(path string) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return fmt.Errorf("无法连接独立 HA 辅助容器，请先启动 ha-companion")
	}
	defer conn.Close()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return err
	}
	var credentials *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	// A sibling container's PID is not mapped into this PID namespace. A
	// local process supervisor (or shared host PID namespace) must not impersonate
	// the independent component that survives the application's termination.
	if credentialErr != nil || credentials == nil || credentials.Uid != 0 || credentials.Pid != 0 {
		return fmt.Errorf("HA 辅助服务必须位于独立容器 PID 命名空间，不能使用本容器或 host PID 模式")
	}
	return nil
}

func haContainerInterface(cfg *Config, macvlan bool) (string, error) {
	if cfg == nil || haUnicastIPv4(cfg.AgentIP) == nil {
		return "", fmt.Errorf("Docker HA 必须设置固定的 AGENT_IP IPv4 地址")
	}
	addresses, err := readHAAddresses()
	if err != nil {
		return "", err
	}
	iface := ""
	for _, address := range addresses {
		if address.IP == cfg.AgentIP && address.Interface != "lo" {
			iface = address.Interface
		}
	}
	if iface == "" {
		return "", fmt.Errorf("AGENT_IP 不在当前容器的网卡上，请检查 Docker 网络模式与固定地址")
	}
	if !macvlan {
		return iface, nil
	}
	raw, err := haRun("ip", "-j", "-d", "link", "show")
	if err != nil {
		return "", fmt.Errorf("无法确认 macvlan 网络类型")
	}
	var links []struct {
		Name     string `json:"ifname"`
		ParentNS *int   `json:"link_netnsid"`
		Info     struct {
			Kind string `json:"info_kind"`
		} `json:"linkinfo"`
	}
	if json.Unmarshal([]byte(raw), &links) != nil {
		return "", fmt.Errorf("macvlan 网卡探测返回格式无效")
	}
	count, matched := 0, false
	for _, link := range links {
		if link.Name == "lo" {
			continue
		}
		count++
		matched = link.Name == iface && link.Info.Kind == "macvlan" && link.ParentNS != nil && *link.ParentNS >= 0
	}
	if count != 1 || !matched {
		return "", fmt.Errorf("macvlan HA 需要独立命名空间内唯一的 macvlan 网卡，不支持 bridge、host 或混合网络")
	}
	return iface, nil
}

func haEnvironment(cfg *Config) haEnvironmentInfo {
	result := haEnvironmentInfo{Kind: "native", Ready: true, HostIDSource: "file", Blockers: []string{}}
	if cfg == nil || cfg.ServiceManager != svc.ContainerManager {
		missing := []string{}
		for _, tool := range []string{"keepalived", "ip", "jq"} {
			if _, err := haLookPath(tool); err != nil {
				missing = append(missing, tool)
			}
		}
		result.MissingTools = &missing
		result.HostBootID = haBootIdentity(haNativeBootIDPath)
		result.HostID = haHostIdentity("/etc/machine-id")
		if result.HostID == "" {
			result.HostID = haHostIdentity("/var/lib/dbus/machine-id")
		}
		return result
	}
	result.Kind, result.Network = "container", haDockerNetwork()
	add := func(message string) { result.Blockers = append(result.Blockers, message) }
	if !haDockerOptIn() {
		add("此容器未显式启用 DNS 高可用，请使用 HA 安装配置重新创建")
		result.Ready = false
		return result
	}
	if result.Network != "host" && result.Network != "macvlan" {
		add("Docker HA 仅支持 host 独立辅助容器或 macvlan 模式")
	}
	if haOS != "linux" || haGeteuid() != 0 {
		add("Docker HA 需要 Linux rootful 容器")
	}
	for _, path := range []string{filepath.Dir(cfg.AgentConfigFile), filepath.Dir(cfg.ConfigPath), haKeepalivedDir} {
		if !filepath.IsAbs(path) || !haMounted(path, true) {
			add(path + " 必须独立读写挂载并持久保存，不能只存于容器层或 tmpfs")
		}
	}
	switch os.Getenv("CONFIGFLOW_DNS_HA_IDENTITY") {
	case "", "file":
		if !haMounted(haHostIDPath, false) {
			add("缺少只读挂载的宿主机身份文件")
		} else {
			result.HostID = haHostIdentity(haHostIDPath)
			if result.HostID == "" {
				add("宿主机身份文件无效，无法排除同一 Docker 宿主")
			}
		}
	case "volume":
		result.HostIDSource = "docker-volume"
		result.HostID = haVolumeIdentity()
		if result.HostID == "" {
			add("宿主身份卷必须只读挂载到 /run/configflow-host-identity，并包含有效的普通 host-id 文件，不能使用软链接")
		}
	case "installation":
		result.HostIDSource = "installation"
		result.HostID = haInstallationIdentity(filepath.Dir(cfg.AgentConfigFile))
		if result.HostID == "" {
			add("安装身份或初始化记录缺失、不匹配或不在持久目录中；请保留数据并检查容器启动日志")
		}
	default:
		add("CONFIGFLOW_DNS_HA_IDENTITY 必须是 file、volume 或 installation")
	}
	if haMounted(haHostBootIDPath, false) {
		if info, err := haEnvironmentLstat(haHostBootIDPath); err == nil && info.Mode().IsRegular() {
			result.HostBootID = haBootIdentity(haHostBootIDPath)
		}
	}
	if result.HostIDSource == "installation" && result.HostBootID == "" {
		add("简洁 Compose 需要只读挂载真实宿主 boot_id，才能确认不同宿主机")
	}
	if iface, err := haContainerInterface(cfg, result.Network == "macvlan"); err != nil {
		add(err.Error())
	} else {
		result.Interface = iface
	}
	if result.Network == "macvlan" {
		if !haNetworkCaps(haCurrentCaps()) {
			add("macvlan HA 容器缺少 NET_ADMIN 或 NET_RAW")
		}
	} else if result.Network == "host" {
		selfNet, selfErr := haNamespaceAt("/proc/self/ns/net")
		hostNet, hostErr := haNamespaceAt(haHostNetNSPath)
		if !haMounted(haHostNetNSPath, false) || selfErr != nil || hostErr != nil || selfNet != hostNet {
			add("host HA 需要只读宿主网络命名空间证据，并使用 network_mode: host")
		}
		data, err := haEnvironmentRead(filepath.Join(haKeepalivedDir, "run", "companion.json"))
		var evidence haCompanionEvidence
		selfPID, pidErr := haNamespaceAt("/proc/self/ns/pid")
		if err != nil || len(data) > 4096 || json.Unmarshal(data, &evidence) != nil || evidence.Protocol != 1 || evidence.Mode != "host" ||
			evidence.HostID != result.HostID || !haDigestPattern.MatchString(evidence.HostID) || evidence.Network != selfNet ||
			pidErr != nil || evidence.PID.Inode == 0 || evidence.PID == selfPID || !haNetworkCaps(evidence.CapEff) {
			add("独立 HA 辅助容器的网络、权限或宿主身份证据未就绪")
		}
		if err := haCompanionPeerCheck(haCompanionSocket()); err != nil {
			add(err.Error())
		}
	}
	if haContainerState() == "" {
		add("受管 keepalived 程序尚未就绪")
	}
	result.Ready = len(result.Blockers) == 0
	return result
}

// Both directions matter: a native journal copied into Docker must not invoke
// host service commands, and a container journal cannot run as a native pair.
func haContainerOwnership(cfg *Config, state *haState, environment haEnvironmentInfo) error {
	container := cfg != nil && cfg.ServiceManager == svc.ContainerManager
	if container && !environment.Ready {
		return fmt.Errorf("%s", strings.Join(environment.Blockers, "；"))
	}
	if state != nil && (container || state.Service.Manager == svc.ContainerManager) &&
		(!container || state.Service.Manager != svc.ContainerManager || state.ContainerHostID != environment.HostID) {
		return fmt.Errorf("HA 状态属于其他宿主或安装方式，已保留记录且未自动恢复")
	}
	return nil
}
