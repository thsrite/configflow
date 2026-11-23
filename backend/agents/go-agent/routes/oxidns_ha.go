package routes

// OxiDNS high availability is an opt-in host operation: keepalived moves one
// virtual IP between two OxiDNS hosts. Its journal is the only authority for
// changing keepalived files or the keepalived service. Without a journal the
// Agent never reads, writes or restarts anything keepalived related.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"agent/svc"
)

const oxidnsHAProtocol = 1

var haMu sync.Mutex
var haOS = runtime.GOOS
var haGeteuid = os.Geteuid
var haLookPath = exec.LookPath
var haExecutable = os.Executable
var haSleep = time.Sleep

// haRun is replaceable in tests. Service commands can take far longer than the
// 3 second read-only probes of runCommand.
var haRun = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
	}
	return string(out), err
}

var haInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`)
var haTokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var haDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type OxiDNSHASpec struct {
	SafetyVersion int    `json:"safety_version"`
	VIP           string `json:"vip"`
	VRID          int    `json:"vrid"`
	Priority      int    `json:"priority"`
	SelfIP        string `json:"self_ip"`
	PeerIP        string `json:"peer_ip"`
}

type haFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Created bool   `json:"created"`
}

type haService struct {
	Manager    string `json:"manager"`
	Unit       string `json:"unit"`
	WasEnabled bool   `json:"was_enabled"`
	WasRunning bool   `json:"was_running"`
}

type haState struct {
	Version         int          `json:"version"`
	Token           string       `json:"token"`
	Active          bool         `json:"active"`
	Pending         bool         `json:"pending"`
	Spec            OxiDNSHASpec `json:"spec"`
	Interface       string       `json:"interface"`
	CIDR            string       `json:"cidr"`
	Files           []haFile     `json:"files"`
	Service         haService    `json:"service"`
	AppliedAt       string       `json:"applied_at"`
	ContainerHostID string       `json:"container_host_id,omitempty"`
}

// haUnicastIPv4 accepts only canonical dotted IPv4 host addresses, so the same
// address cannot be spelled two ways in keepalived and in the journal.
func haUnicastIPv4(value string) net.IP {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil || ip.To4().String() != value || !ip.IsGlobalUnicast() {
		return nil
	}
	return ip.To4()
}

func validateOxiDNSHASpec(spec OxiDNSHASpec) error {
	if spec.SafetyVersion != oxidnsHAProtocol {
		return fmt.Errorf("safety_version 必须为 %d，请升级 ConfigFlow 或 Agent", oxidnsHAProtocol)
	}
	if spec.VRID < 1 || spec.VRID > 255 {
		return fmt.Errorf("vrid 必须在 1 到 255 之间")
	}
	if spec.Priority != 150 && spec.Priority != 100 {
		return fmt.Errorf("priority 只能是 150（主）或 100（备）")
	}
	for name, value := range map[string]string{"vip": spec.VIP, "self_ip": spec.SelfIP, "peer_ip": spec.PeerIP} {
		if haUnicastIPv4(value) == nil {
			return fmt.Errorf("%s 必须是单播 IPv4 地址", name)
		}
	}
	if spec.VIP == spec.SelfIP || spec.VIP == spec.PeerIP || spec.SelfIP == spec.PeerIP {
		return fmt.Errorf("虚拟 IP、本机地址与对端地址必须互不相同")
	}
	return nil
}

func haJournalPath(cfg *Config) (string, error) {
	if cfg == nil || cfg.AgentConfigFile == "" {
		return "", fmt.Errorf("Agent 配置路径未知，无法保存 DNS 高可用状态")
	}
	return filepath.Join(filepath.Dir(cfg.AgentConfigFile), "oxidns-ha.json"), nil
}

func loadHAState(cfg *Config) (*haState, error) {
	path, err := haJournalPath(cfg)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state haState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("DNS 高可用状态文件损坏，保留现有 keepalived 配置：%w", err)
	}
	if state.Version != 1 || !haTokenPattern.MatchString(state.Token) {
		return nil, fmt.Errorf("DNS 高可用状态没有有效的归属信息，保留现有 keepalived 配置")
	}
	if err := validateOxiDNSHASpec(state.Spec); err != nil {
		return nil, fmt.Errorf("DNS 高可用状态参数无效：%w", err)
	}
	if !haInterfacePattern.MatchString(state.Interface) || state.Interface == "lo" {
		return nil, fmt.Errorf("DNS 高可用状态网卡无效")
	}
	if _, network, err := net.ParseCIDR(state.CIDR); err != nil || network.String() != state.CIDR || !haSubnetUsable(network, state.Spec) {
		return nil, fmt.Errorf("DNS 高可用状态网段无效")
	}
	if len(state.Files) != 2 || state.Files[0].Path != haConfigPath() || state.Files[1].Path != haMainConfigPath() {
		return nil, fmt.Errorf("DNS 高可用状态的文件清单不完整，保留现有 keepalived 配置")
	}
	if !state.Files[0].Created {
		return nil, fmt.Errorf("DNS 高可用状态的文件归属无效，保留现有 keepalived 配置")
	}
	for _, file := range state.Files {
		if !haDigestPattern.MatchString(file.SHA256) {
			return nil, fmt.Errorf("DNS 高可用状态的文件摘要无效，保留现有 keepalived 配置")
		}
	}
	// Journals written before 1.9.3-go record the container manager as
	// "supervisor"; the next save records "s6".
	state.Service.Manager = svc.NormalizeManager(state.Service.Manager)
	if (state.Service.Manager != "systemd" && state.Service.Manager != "openrc" && state.Service.Manager != svc.ContainerManager) || state.Service.Unit != haServiceUnit {
		return nil, fmt.Errorf("DNS 高可用状态的服务记录无效，保留现有 keepalived 配置")
	}
	if state.Service.Manager == svc.ContainerManager && !haDigestPattern.MatchString(state.ContainerHostID) {
		return nil, fmt.Errorf("容器 HA 状态缺少有效的宿主归属信息")
	}
	return &state, nil
}

func saveHAState(cfg *Config, state *haState) error {
	path, err := haJournalPath(cfg)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return durableWrite(path, data, 0600)
}

func deleteHAState(cfg *Config) error {
	path, err := haJournalPath(cfg)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func lockHAJournal(cfg *Config) (func(), error) {
	path, err := haJournalPath(cfg)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("另一 DNS 高可用操作正在运行：%w", err)
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func haPlatform(cfg *Config) error {
	if haOS != "linux" {
		return fmt.Errorf("DNS 高可用仅支持 Linux")
	}
	if cfg == nil || cfg.ServiceType != "oxidns" {
		return fmt.Errorf("DNS 高可用只能在 OxiDNS Agent 上配置")
	}
	if cfg.ServiceManager == svc.ContainerManager {
		environment := haEnvironment(cfg)
		if !environment.Ready {
			return fmt.Errorf("%s", strings.Join(environment.Blockers, "；"))
		}
	} else if cfg.ServiceManager != "systemd" && cfg.ServiceManager != "openrc" {
		return fmt.Errorf("DNS 高可用需要原生 systemd/OpenRC 或受管 Docker HA 环境")
	}
	if haGeteuid() != 0 {
		return fmt.Errorf("Agent 需要 root 权限才能管理 keepalived")
	}
	return nil
}

// haTools names every missing host tool at once. The Agent never installs packages.
func haTools() error {
	missing := []string{}
	for _, name := range []string{"keepalived", "ip"} {
		if _, err := haLookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("本机缺少 %s，请先安装后重试："+
		"Debian/Ubuntu 执行 apt install keepalived iproute2；Alpine 执行 apk add keepalived iproute2；"+
		"RHEL/Fedora 执行 dnf install keepalived iproute", strings.Join(missing, "、"))
}

type haAddress struct {
	Interface string
	IP        string
	Prefix    int
}

func readHAAddresses() ([]haAddress, error) {
	raw, err := haRun("ip", "-j", "-4", "addr", "show")
	if err != nil {
		return nil, fmt.Errorf("无法读取网卡 IPv4 地址（需要 iproute2）：%w", err)
	}
	var links []struct {
		Name string `json:"ifname"`
		Info []struct {
			Local  string `json:"local"`
			Prefix int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return nil, fmt.Errorf("网卡探测返回格式无效：%w", err)
	}
	result := []haAddress{}
	for _, link := range links {
		for _, addr := range link.Info {
			if ip := net.ParseIP(addr.Local); ip != nil && ip.To4() != nil {
				result = append(result, haAddress{Interface: link.Name, IP: ip.To4().String(), Prefix: addr.Prefix})
			}
		}
	}
	return result, nil
}

func haSubnetUsable(network *net.IPNet, spec OxiDNSHASpec) bool {
	ones, bits := network.Mask.Size()
	if bits != 32 || ones < 8 || ones > 30 {
		return false
	}
	broadcast := make(net.IP, 4)
	for i := range broadcast {
		broadcast[i] = network.IP.To4()[i] | ^network.Mask[i]
	}
	for _, value := range []string{spec.VIP, spec.SelfIP, spec.PeerIP} {
		ip := net.ParseIP(value)
		if ip == nil || !network.Contains(ip) || ip.Equal(network.IP) || ip.Equal(broadcast) {
			return false
		}
	}
	return true
}

// haResolveNetwork finds the interface that holds self_ip and its prefix; VIP
// and peer must be ordinary hosts in that same prefix.
func haResolveNetwork(spec OxiDNSHASpec, addresses []haAddress) (string, string, error) {
	for _, addr := range addresses {
		if addr.IP != spec.SelfIP {
			continue
		}
		if addr.Interface == "lo" || !haInterfacePattern.MatchString(addr.Interface) {
			return "", "", fmt.Errorf("本机地址 %s 所在网卡 %s 不能用于虚拟 IP", spec.SelfIP, addr.Interface)
		}
		_, network, err := net.ParseCIDR(fmt.Sprintf("%s/%d", addr.IP, addr.Prefix))
		if err != nil {
			return "", "", fmt.Errorf("无法确定本机地址 %s 的网段", spec.SelfIP)
		}
		if !haSubnetUsable(network, spec) {
			return addr.Interface, network.String(), fmt.Errorf("虚拟 IP %s 和对端 %s 必须是本机网段 %s 内的可用主机地址", spec.VIP, spec.PeerIP, network.String())
		}
		return addr.Interface, network.String(), nil
	}
	return "", "", fmt.Errorf("本机网卡上没有地址 %s，请确认 Agent 地址", spec.SelfIP)
}

func haVIPPresent(spec OxiDNSHASpec, addresses []haAddress) bool {
	for _, addr := range addresses {
		if addr.IP == spec.VIP {
			return true
		}
	}
	return false
}

type haListener struct {
	Host     string
	Port     string
	Wildcard bool
	Entry    string
}

// haListeners reads OxiDNS UDP servers from the active configuration.
func haListeners(doc map[string]any) []haListener {
	plugins, _ := doc["plugins"].([]any)
	result := []haListener{}
	for _, value := range plugins {
		plugin, _ := value.(map[string]any)
		if plugin["type"] != "udp_server" {
			continue
		}
		args, _ := plugin["args"].(map[string]any)
		listen, _ := args["listen"].(string)
		entry, _ := args["entry"].(string)
		host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
		if err != nil {
			continue
		}
		wildcard := host == "" || host == "*"
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			wildcard = true
		}
		result = append(result, haListener{Host: host, Port: port, Wildcard: wildcard, Entry: entry})
	}
	return result
}

func readOxiDNSDocument(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func haListenerBlocker(cfg *Config) error {
	doc, err := readOxiDNSDocument(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("无法读取已发布的 OxiDNS 配置，请先发布配置：%w", err)
	}
	specific := []string{}
	for _, listener := range haListeners(doc) {
		if listener.Port != "53" {
			continue
		}
		if listener.Wildcard {
			return nil
		}
		specific = append(specific, net.JoinHostPort(listener.Host, listener.Port))
	}
	if len(specific) > 0 {
		return fmt.Errorf("OxiDNS 只监听了具体地址（%s），无法应答虚拟 IP；请把 udp_server 的 listen 改为 0.0.0.0:53 后重新发布", strings.Join(specific, "、"))
	}
	return fmt.Errorf("OxiDNS 没有在 53 端口监听 UDP，客户端无法经虚拟 IP 查询；请把 udp_server 的 listen 改为 0.0.0.0:53 后重新发布")
}

// haPreflight is shared by preview and apply. It never changes the host.
func haPreflight(cfg *Config, spec OxiDNSHASpec, state *haState) ([]string, string, string) {
	blockers := []string{}
	add := func(err error) {
		if err != nil {
			blockers = append(blockers, err.Error())
		}
	}
	add(haPlatform(cfg))
	if cfg != nil && cfg.ServiceManager == svc.ContainerManager {
		environment := haEnvironment(cfg)
		if spec.SelfIP != cfg.AgentIP {
			add(fmt.Errorf("HA 本机地址必须与容器固定 AGENT_IP 一致"))
		}
		if state != nil && (state.Service.Manager != svc.ContainerManager || state.ContainerHostID != environment.HostID) {
			add(fmt.Errorf("HA 记录属于其他宿主或安装方式，已保留原记录，请先确认并停用"))
		}
	}
	validErr := validateOxiDNSHASpec(spec)
	add(validErr)
	toolsErr := haTools()
	add(toolsErr)
	if cfg != nil && cfg.ServiceType == "oxidns" {
		add(haListenerBlocker(cfg))
	}
	iface, cidr := "", ""
	if validErr == nil && toolsErr == nil {
		addresses, err := readHAAddresses()
		add(err)
		if err == nil {
			iface, cidr, err = haResolveNetwork(spec, addresses)
			add(err)
			if haVIPPresent(spec, addresses) && (state == nil || state.Spec.VIP != spec.VIP) {
				add(fmt.Errorf("虚拟 IP %s 已在本机网卡上，但不是由 DNS 高可用持有；请先移除该地址或换一个虚拟 IP", spec.VIP))
			}
		}
	}
	if validErr == nil {
		add(haCheckKeepalivedFiles(cfg, spec, state))
	}
	if _, err := haScriptCommand(cfg); err != nil {
		add(err)
	}
	return blockers, iface, cidr
}

func haStatus(cfg *Config) map[string]any {
	environment := haEnvironment(cfg)
	result := map[string]any{"success": true, "protocol": oxidnsHAProtocol, "managed": nil, "handoff": nil, "error": "", "environment": environment}
	haMu.Lock()
	state, err := loadHAState(cfg)
	haMu.Unlock()
	if err != nil {
		result["error"] = err.Error()
	} else if ownershipErr := haContainerOwnership(cfg, state, environment); ownershipErr != nil {
		result["error"] = ownershipErr.Error()
	}
	running := false
	if _, lookErr := haLookPath("keepalived"); lookErr == nil && cfg != nil {
		running = haServiceRunning(cfg.ServiceManager)
	}
	vipPresent, role := false, "unknown"
	if state != nil {
		result["managed"] = map[string]any{"spec": state.Spec, "active": state.Active, "pending": state.Pending, "applied_at": state.AppliedAt, "interface": state.Interface, "cidr": state.CIDR}
		if addresses, addrErr := readHAAddresses(); addrErr == nil {
			vipPresent = haVIPPresent(state.Spec, addresses)
		} else if err == nil {
			result["error"] = addrErr.Error()
		}
		if vipPresent {
			role = "master"
		} else if running {
			role = "backup"
		}
		if err == nil && result["error"] == "" {
			if driftErr := haFilesDrift(cfg, state); driftErr != nil {
				result["error"] = driftErr.Error()
			}
		}
	}
	localHealthy := haCheckLocal(cfg) == nil
	vipHealthy := false
	if state != nil && state.Active && !state.Pending {
		_, name := haCheckTarget(cfg.ConfigPath)
		vipHealthy = haHandoffProbe(net.JoinHostPort(state.Spec.VIP, "53"), name, haCheckTimeout) == nil
	}
	if handoff, handoffErr := haHandoffPublic(cfg, state, localHealthy); handoffErr != nil {
		if result["error"] == "" {
			result["error"] = handoffErr.Error()
		}
	} else {
		result["handoff"] = handoff
	}
	result["runtime"] = map[string]any{"keepalived_running": running, "vip_present": vipPresent, "role": role,
		"healthy": localHealthy, "vip_healthy": vipHealthy}
	return result
}

func haFailure(w http.ResponseWriter, code int, err error) {
	JsonResponse(w, code, map[string]any{"success": false, "message": err.Error(), "error": err.Error()})
}

func decodeHASpec(w http.ResponseWriter, r *http.Request) (OxiDNSHASpec, bool) {
	var spec OxiDNSHASpec
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		haFailure(w, http.StatusBadRequest, fmt.Errorf("请求格式错误：%w", err))
		return spec, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		haFailure(w, http.StatusBadRequest, fmt.Errorf("请求只能包含一个 JSON 对象"))
		return spec, false
	}
	return spec, true
}

// OxiDNSHAHandler serves GET/PUT/DELETE /api/oxidns-ha.
func OxiDNSHAHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			JsonResponse(w, http.StatusOK, haStatus(cfg))
		case http.MethodPut:
			spec, ok := decodeHASpec(w, r)
			if !ok {
				return
			}
			release, err := AcquireServiceOperation(cfg)
			if err != nil {
				haFailure(w, http.StatusConflict, err)
				return
			}
			err = applyOxiDNSHA(cfg, spec)
			release()
			if err != nil {
				haFailure(w, http.StatusUnprocessableEntity, err)
				return
			}
			JsonResponse(w, http.StatusOK, haStatus(cfg))
		case http.MethodDelete:
			release, err := AcquireServiceOperation(cfg)
			if err != nil {
				haFailure(w, http.StatusConflict, err)
				return
			}
			removed, err := removeOxiDNSHA(cfg)
			release()
			if err != nil {
				code := http.StatusUnprocessableEntity
				var ownership haOwnershipError
				if errors.As(err, &ownership) {
					code = http.StatusConflict
				}
				haFailure(w, code, err)
				return
			}
			JsonResponse(w, http.StatusOK, map[string]any{"success": true, "removed": removed})
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// OxiDNSHAPreviewHandler serves POST /api/oxidns-ha/preview without changing the host.
func OxiDNSHAPreviewHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		spec, ok := decodeHASpec(w, r)
		if !ok {
			return
		}
		haMu.Lock()
		state, err := loadHAState(cfg)
		haMu.Unlock()
		blockers, iface, cidr := haPreflight(cfg, spec, state)
		if err != nil {
			blockers = append(blockers, err.Error())
		}
		JsonResponse(w, http.StatusOK, map[string]any{"success": true, "ok": len(blockers) == 0, "blockers": blockers, "interface": iface, "cidr": cidr, "environment": haEnvironment(cfg)})
	}
}

// haOwnershipError marks removals refused because ownership cannot be proven.
type haOwnershipError struct{ error }

func (e haOwnershipError) Unwrap() error { return e.error }

// haNotReady marks only transient boot failures that may be retried.
type haNotReady struct{ error }

// ReapplyOxiDNSHA repairs recorded keepalived files and service at boot. With
// no journal it returns without touching keepalived; a consistent host is left
// alone so that an Agent restart never moves the virtual IP.
func ReapplyOxiDNSHA(cfg *Config) {
	if cfg == nil || cfg.ServiceType != "oxidns" || cfg.AgentConfigFile == "" {
		return
	}
	if cfg.ServiceManager == svc.ContainerManager && haDockerOptIn() {
		// A companion can be recreated long after the application started.
		// Only an owned active journal may restart its one managed program.
		go func() {
			lastMessage := ""
			for {
				if err := reconcileOxiDNSHA(cfg); err != nil {
					if message := err.Error(); message != lastMessage {
						log.Printf("oxidns ha: container recovery pending: %s", message)
						lastMessage = message
					}
				} else {
					lastMessage = ""
				}
				time.Sleep(5 * time.Second)
			}
		}()
		return
	}
	if state, err := loadHAState(cfg); err == nil && state == nil {
		return
	}
	go func() {
		if err := reapplyOxiDNSHA(cfg, 10, func() { haSleep(3 * time.Second) }); err != nil {
			log.Printf("oxidns ha: boot replay refused: %v", err)
		}
	}()
}

func reapplyOxiDNSHA(cfg *Config, attempts int, pause func()) error {
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		last = reconcileOxiDNSHA(cfg)
		if last == nil {
			return nil
		}
		if _, transient := last.(haNotReady); !transient {
			return last
		}
		if attempt+1 < attempts {
			pause()
		}
	}
	return fmt.Errorf("keepalived 未能恢复，请检查服务后在 ConfigFlow 重新开启 DNS 高可用：%w", last)
}

func reconcileOxiDNSHA(cfg *Config) error {
	state, err := loadHAState(cfg)
	if err != nil || state == nil || !state.Active || state.Pending {
		return err
	}
	if err := haPlatform(cfg); err != nil {
		return err
	}
	if err := haContainerOwnership(cfg, state, haEnvironment(cfg)); err != nil {
		return err
	}
	if haFilesDrift(cfg, state) == nil && haServiceEnabled(state.Service.Manager) && haServiceRunning(state.Service.Manager) {
		return nil
	}
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		if err == errDeploymentBusy {
			return haNotReady{err}
		}
		return err
	}
	defer release()
	haMu.Lock()
	defer haMu.Unlock()
	unlock, err := lockHAJournal(cfg)
	if err != nil {
		return haNotReady{err}
	}
	defer unlock()
	state, err = loadHAState(cfg)
	if err != nil || state == nil || !state.Active || state.Pending {
		return err
	}
	if err := haContainerOwnership(cfg, state, haEnvironment(cfg)); err != nil {
		return err
	}
	return repairOxiDNSHA(cfg, state)
}

// checkOxiDNSHAUninstall keeps the only ownership record while keepalived still
// carries the virtual IP; disabling DNS HA in ConfigFlow removes it first.
func checkOxiDNSHAUninstall(cfg *Config) error {
	if cfg == nil || cfg.ServiceType != "oxidns" || cfg.AgentConfigFile == "" {
		return nil
	}
	haMu.Lock()
	defer haMu.Unlock()
	state, err := loadHAState(cfg)
	if err != nil {
		return fmt.Errorf("无法核对 DNS 高可用记录，已停止卸载以保留 keepalived 配置：%w", err)
	}
	if state != nil {
		return fmt.Errorf("请先在 ConfigFlow 的 Agent 页停用 DNS 高可用，完成后才能卸载 OxiDNS Agent")
	}
	return nil
}

// OxiDNSHAVirtualIPs lists virtual IPs recorded by DNS HA journals that apply to
// this host, so address detection never reports a floating address as the
// Agent's own. Without a journal it returns nil and detection is unchanged.
func OxiDNSHAVirtualIPs(cfg *Config) []string {
	if cfg == nil || cfg.AgentConfigFile == "" {
		return nil
	}
	paths := []string{filepath.Join(filepath.Dir(cfg.AgentConfigFile), "oxidns-ha.json")}
	if cfg.NativeAIO && cfg.ServiceType == "mihomo" {
		paths = append(paths, filepath.Join(aioAgentRoot, "oxidns", "oxidns-ha.json"))
	}
	var result []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var journal struct {
			Spec struct {
				VIP string `json:"vip"`
			} `json:"spec"`
		}
		if json.Unmarshal(data, &journal) != nil || haUnicastIPv4(journal.Spec.VIP) == nil {
			continue
		}
		result = append(result, journal.Spec.VIP)
	}
	return result
}

func haNow() string { return time.Now().UTC().Format(time.RFC3339) }
