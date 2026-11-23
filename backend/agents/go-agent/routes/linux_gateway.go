package routes

// Linux gateway is an opt-in host operation, independent of configuration
// publishing. Its journal is the only authority for changing host resources.
import (
	"agent/netfilter"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const gatewayTable = "configflow_gateway"

var gatewayMu sync.Mutex
var gatewayOS = runtime.GOOS
var gatewayGeteuid = os.Geteuid
var gatewaySysctlRoot = "/proc/sys"
var gatewayRun = func(name string, args ...string) (string, error) { return runCommand(name, args...) }
var gatewayRunInput = func(input, name string, args ...string) (string, error) { return runCommandInput(input, name, args...) }

type LinuxGatewaySpec struct {
	Interface       string `json:"interface"`
	Address         string `json:"address"`
	LANCIDR         string `json:"lan_cidr"`
	UpstreamGateway string `json:"upstream_gateway"`
}

type gatewayState struct {
	Version   int               `json:"version"`
	Token     string            `json:"token"`
	Active    bool              `json:"active"`
	Pending   bool              `json:"pending"`
	Spec      LinuxGatewaySpec  `json:"spec"`
	Original  map[string]string `json:"original_sysctls"`
	NFT       []map[string]any  `json:"nft"`
	AppliedAt string            `json:"applied_at"`
	ProfileID string            `json:"profile_id"`
	TunDevice string            `json:"tun_device"`
}

type gatewayInterface struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}

type gatewayDefaultRoute struct {
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
}

type gatewayDetection struct {
	Interfaces    []gatewayInterface    `json:"interfaces"`
	DefaultRoutes []gatewayDefaultRoute `json:"default_routes"`
	Suggested     *LinuxGatewaySpec     `json:"suggested,omitempty"`
}

func validateLinuxGatewaySpec(spec LinuxGatewaySpec) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`).MatchString(spec.Interface) || spec.Interface == "lo" {
		return fmt.Errorf("interface 必须是有效的 LAN 网卡名（最多 15 字符，不能是 lo）")
	}
	ip, network, err := net.ParseCIDR(spec.LANCIDR)
	if err != nil || ip.To4() == nil || !ip.Equal(network.IP) {
		return fmt.Errorf("lan_cidr 必须是 IPv4 网络地址，例如 192.168.1.0/24")
	}
	ones, _ := network.Mask.Size()
	if ones < 8 || ones > 30 {
		return fmt.Errorf("lan_cidr 掩码必须在 /8 到 /30 之间")
	}
	for name, value := range map[string]string{"address": spec.Address, "upstream_gateway": spec.UpstreamGateway} {
		addr := net.ParseIP(value)
		if addr == nil || addr.To4() == nil || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || !network.Contains(addr) || addr.Equal(network.IP) {
			return fmt.Errorf("%s 必须是 lan_cidr 中可用的 IPv4 主机地址", name)
		}
		v, n := addr.To4(), network.IP.To4()
		broadcast := true
		for i := 0; i < 4; i++ {
			broadcast = broadcast && v[i] == n[i]|^network.Mask[i]
		}
		if broadcast {
			return fmt.Errorf("%s 不能是广播地址", name)
		}
	}
	if spec.Address == spec.UpstreamGateway {
		return fmt.Errorf("Linux IP 与主路由 IP 不能相同")
	}
	return nil
}

func gatewayPath(cfg *Config) (string, error) {
	if cfg == nil || cfg.AgentConfigFile == "" {
		return "", fmt.Errorf("Agent 配置路径未知，无法保存网关状态")
	}
	return filepath.Join(filepath.Dir(cfg.AgentConfigFile), "linux-gateway.json"), nil
}

func loadGatewayState(cfg *Config) (*gatewayState, error) {
	path, err := gatewayPath(cfg)
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
	var state gatewayState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("网关状态文件损坏：%w", err)
	}
	if state.Version != 1 || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(state.Token) {
		return nil, fmt.Errorf("网关状态没有有效的归属信息，保留现有网络配置")
	}
	if err := validateLinuxGatewaySpec(state.Spec); err != nil {
		return nil, fmt.Errorf("网关状态参数无效：%w", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`).MatchString(state.TunDevice) {
		return nil, fmt.Errorf("网关状态 TUN 设备无效")
	}
	if len(state.NFT) != len(gatewayDesiredNFT(&state)) {
		return nil, fmt.Errorf("网关 journal 的 nftables 资源清单不完整")
	}
	if !gatewayJournalNFTValid(&state) {
		return nil, fmt.Errorf("网关 journal 资源或规则顺序与参数不一致，保留现有配置")
	}
	for key, value := range state.Original {
		if _, exists := gatewayStateSysctls(&state)[key]; !exists || (value != "0" && value != "1") {
			return nil, fmt.Errorf("网关 journal 包含无效 sysctl 记录")
		}
	}
	if len(state.Original) != len(gatewayStateSysctls(&state)) {
		return nil, fmt.Errorf("网关 journal 缺少原始 sysctl 值，保留现有配置")
	}
	return &state, nil
}

func saveGatewayState(cfg *Config, state *gatewayState) error {
	path, err := gatewayPath(cfg)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return durableWrite(path, data, 0600)
}

func gatewayPlatform(cfg *Config) error {
	if gatewayOS != "linux" {
		return fmt.Errorf("自动配置网关仅支持 Linux")
	}
	if cfg == nil || cfg.ServiceType != "mihomo" {
		return fmt.Errorf("请选择 Mihomo Agent；OxiDNS Agent 不管理主机网关")
	}
	if cfg.ServiceManager != "systemd" && cfg.ServiceManager != "openrc" {
		return fmt.Errorf("仅支持 Shell 原生安装（systemd 或 OpenRC），容器及其他服务管理方式请手动配置")
	}
	if gatewayGeteuid() != 0 {
		return fmt.Errorf("Agent 需要 root 权限才能配置主机网络")
	}
	return nil
}

func detectGatewayNetwork() (gatewayDetection, error) {
	result := gatewayDetection{Interfaces: []gatewayInterface{}, DefaultRoutes: []gatewayDefaultRoute{}}
	addresses, err := gatewayRun("ip", "-j", "-4", "addr", "show")
	if err != nil {
		return result, fmt.Errorf("无法读取网卡 IPv4 地址（需要 iproute2）：%w", err)
	}
	var links []struct {
		Name string `json:"ifname"`
		Info []struct {
			Local  string `json:"local"`
			Prefix int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(addresses), &links); err != nil {
		return result, fmt.Errorf("网卡探测返回格式无效：%w", err)
	}
	for _, link := range links {
		if link.Name == "lo" {
			continue
		}
		item := gatewayInterface{Name: link.Name, Addresses: []string{}}
		for _, addr := range link.Info {
			if ip := net.ParseIP(addr.Local); ip != nil && ip.To4() != nil {
				item.Addresses = append(item.Addresses, fmt.Sprintf("%s/%d", addr.Local, addr.Prefix))
			}
		}
		result.Interfaces = append(result.Interfaces, item)
	}
	raw, err := gatewayRun("ip", "-j", "-4", "route", "show", "default")
	if err != nil {
		return result, fmt.Errorf("无法读取 IPv4 默认路由：%w", err)
	}
	var routes []struct {
		Dev       string          `json:"dev"`
		Gateway   string          `json:"gateway"`
		Multipath json.RawMessage `json:"multipath"`
	}
	if err := json.Unmarshal([]byte(raw), &routes); err != nil {
		return result, fmt.Errorf("默认路由探测返回格式无效：%w", err)
	}
	for _, route := range routes {
		result.DefaultRoutes = append(result.DefaultRoutes, gatewayDefaultRoute{Interface: route.Dev, Gateway: route.Gateway})
		if len(route.Multipath) > 0 {
			return result, fmt.Errorf("当前有多路径默认路由，自动配置仅支持单出口")
		}
	}
	if len(result.DefaultRoutes) == 1 {
		route := result.DefaultRoutes[0]
		for _, iface := range result.Interfaces {
			if iface.Name != route.Interface || len(iface.Addresses) != 1 {
				continue
			}
			addr, subnet, _ := net.ParseCIDR(iface.Addresses[0])
			if addr != nil && subnet.Contains(net.ParseIP(route.Gateway)) {
				result.Suggested = &LinuxGatewaySpec{Interface: iface.Name, Address: addr.String(), LANCIDR: subnet.String(), UpstreamGateway: route.Gateway}
			}
		}
	}
	return result, nil
}

func checkGatewayNetwork(spec LinuxGatewaySpec, detection gatewayDetection) error {
	if len(detection.DefaultRoutes) == 0 {
		return gatewayNotReady{fmt.Errorf("主机 IPv4 默认路由尚未就绪")}
	}
	if len(detection.DefaultRoutes) != 1 || detection.DefaultRoutes[0].Interface != spec.Interface || detection.DefaultRoutes[0].Gateway != spec.UpstreamGateway {
		return fmt.Errorf("实际默认路由与 interface/upstream_gateway 不一致；Linux 自身默认网关必须是填写的主路由")
	}
	for _, iface := range detection.Interfaces {
		if iface.Name != spec.Interface {
			continue
		}
		for _, cidr := range iface.Addresses {
			addr, subnet, _ := net.ParseCIDR(cidr)
			if addr != nil && addr.String() == spec.Address && subnet.String() == spec.LANCIDR {
				return nil
			}
		}
	}
	return gatewayNotReady{fmt.Errorf("address/lan_cidr 与所选网卡实际 IPv4 地址或掩码不一致或尚未就绪；本功能不会修改网卡 IP")}
}

func gatewayConfigEnabled(cfg *Config) (bool, map[string]any, error) {
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return false, nil, fmt.Errorf("无法读取已部署 Mihomo 配置：%w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, nil, err
	}
	marker, _ := doc["x-configflow-linux-gateway"].(map[string]any)
	enabled, _ := marker["enabled"].(bool)
	version, _ := yamlUint(marker["version"])
	return enabled && version == 1, doc, nil
}

func checkGatewayConfig(cfg *Config, spec LinuxGatewaySpec, profileID string) error {
	enabled, doc, err := gatewayConfigEnabled(cfg)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("尚未部署启用 Linux 网关的 Mihomo 配置，请先保存并发布配置")
	}
	if err := checkLinuxGatewayCore(cfg, doc); err != nil {
		return err
	}
	if doc["mode"] != "rule" {
		return fmt.Errorf("已部署 Mihomo 必须使用 mode: rule，避免全局或直连模式改变规则选择")
	}
	marker, _ := doc["x-configflow-linux-gateway"].(map[string]any)
	if profileID == "" || marker["profile_id"] != profileID {
		return fmt.Errorf("已部署网关配置不属于当前配置空间；请重新发布当前空间的 Mihomo 配置")
	}
	tun, _ := doc["tun"].(map[string]any)
	for _, key := range []string{"include-interface", "exclude-interface", "include-uid", "include-uid-range", "exclude-uid", "exclude-uid-range", "include-mac-address", "exclude-mac-address", "include-android-user", "include-package", "exclude-package"} {
		value := tun[key]
		if value == nil {
			continue
		}
		if entries, ok := value.([]any); ok && len(entries) == 0 {
			continue
		}
		return fmt.Errorf("已部署 tun.%s 保留旧流量过滤，可能绕过网关接管；请移除后重新发布", key)
	}
	for _, key := range []string{"enable", "auto-route", "auto-redirect"} {
		if tun[key] != true {
			return fmt.Errorf("已部署配置 tun.%s 必须为 true", key)
		}
	}
	excluded, _ := tun["route-exclude-address"].([]any)
	protectedDNS := false
	for _, value := range excluded {
		if value == spec.Address+"/32" {
			protectedDNS = true
		}
	}
	if !protectedDNS {
		return fmt.Errorf("已部署配置尚未排除网关自身 DNS 地址，Mihomo 可能绕过 OxiDNS；请更新 Agent 并重新发布 Mihomo 配置")
	}
	dns, _ := doc["dns"].(map[string]any)
	if dns["enable"] != true || dns["enhanced-mode"] != "fake-ip" {
		return fmt.Errorf("已部署 Mihomo DNS 必须启用 fake-ip")
	}
	listen, _ := dns["listen"].(string)
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port != "1053" {
		return fmt.Errorf("Mihomo DNS 必须监听 1053 端口，53 端口留给 OxiDNS")
	}
	fakeRange, _ := dns["fake-ip-range"].(string)
	_, fake, err := net.ParseCIDR(fakeRange)
	if err != nil || fake.IP.To4() == nil {
		return fmt.Errorf("Mihomo fake-ip-range 必须是 IPv4 CIDR")
	}
	_, lan, _ := net.ParseCIDR(spec.LANCIDR)
	if fake.Contains(lan.IP) || lan.Contains(fake.IP) {
		return fmt.Errorf("fake-IP 网段不能与 LAN 网段重叠")
	}
	providers, _ := doc["rule-providers"].(map[string]any)
	refs, _ := tun["route-address-set"].([]any)
	foundFake := false
	for _, value := range refs {
		name, ok := value.(string)
		provider, present := providers[name].(map[string]any)
		if !ok || !strings.HasPrefix(name, "cf_gateway_") || !present || provider["behavior"] != "ipcidr" {
			return fmt.Errorf("route-address-set 必须只引用已生成的 cf_gateway_ IP 规则集")
		}
		if name == "cf_gateway_fake_ip" {
			payload, _ := provider["payload"].([]any)
			if len(payload) != 1 || payload[0] != fake.String() {
				return fmt.Errorf("自动 fake-IP 规则集与 DNS fake-ip-range 不一致")
			}
			foundFake = true
		}
	}
	if !foundFake {
		return fmt.Errorf("route-address-set 缺少 cf_gateway_fake_ip 自动规则集")
	}
	return nil
}

func gatewayCommands(spec LinuxGatewaySpec) []string {
	return []string{"检查已部署 Mihomo 网关配置、网卡和默认路由；保留主机现有 IP 和默认路由", "保存原值并设置 net.ipv4.ip_forward=1", "保存原值并设置 net.ipv4.conf.all.send_redirects=0、net.ipv4.conf.default.send_redirects=0、net.ipv4.conf." + spec.Interface + ".send_redirects=0", "在独立 nftables 表 ip " + gatewayTable + " 中允许 LAN 转发，并将经 " + spec.Interface + " 转发的 " + spec.LANCIDR + " 出网流量 SNAT 为 " + spec.Address, "保存归属 journal；Agent 重启后恢复，撤销时只清理未被外部修改的受管资源"}
}

func gatewayManaged(cfg *Config) LinuxGatewayManagedStatus {
	result := LinuxGatewayManagedStatus{
		Active:              false,
		Healthy:             false,
		PreservesForwarding: aioForwardingConfigManaged(),
	}
	state, err := loadGatewayState(cfg)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if state == nil {
		return result
	}
	pending := state.Pending
	spec := state.Spec
	result.Active = state.Active
	result.Pending = &pending
	result.Spec = &spec
	result.AppliedAt = state.AppliedAt
	objects, err := readGatewayNFT()
	if err == nil {
		err = checkGatewayOwnership(state, objects, true)
	}
	if err == nil && state.Active && !state.Pending {
		err = checkGatewaySysctls(state)
	}
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Healthy = state.Active && !state.Pending
	}
	return result
}

func gatewayPreview(cfg *Config, spec LinuxGatewaySpec, profileID string) LinuxGatewayPreviewResponse {
	blockers := []string{}
	add := func(err error) {
		if err != nil {
			blockers = append(blockers, err.Error())
		}
	}
	add(gatewayPlatform(cfg))
	validErr := validateLinuxGatewaySpec(spec)
	add(validErr)
	detection, err := detectGatewayNetwork()
	add(err)
	if validErr == nil {
		if err == nil {
			add(checkGatewayNetwork(spec, detection))
		}
		add(checkGatewayConfig(cfg, spec, profileID))
	}
	state, err := loadGatewayState(cfg)
	add(err)
	if err == nil {
		if state != nil && (state.Spec != spec || state.ProfileID != profileID) {
			add(fmt.Errorf("已应用网关参数或配置空间不同，请先恢复客户端并撤销旧网关"))
		}
		_, err = gatewayPreflightResources(cfg, state)
		add(err)
	}
	if validErr == nil {
		routing, err := readMihomoRoutingStrict(cfg.ConfigPath)
		add(err)
		if err == nil {
			if routing.tunDevice == spec.Interface {
				add(fmt.Errorf("Mihomo TUN 设备不能与 LAN 网卡同名"))
			}
			if state != nil && state.TunDevice != routing.tunDevice {
				add(fmt.Errorf("Mihomo TUN 设备名称已变化，请先撤销旧网关"))
			}
			probe := &gatewayState{Spec: spec, TunDevice: routing.tunDevice, Original: map[string]string{}}
			add(recordGatewaySysctls(probe))
		}
	}
	if len(blockers) == 0 && state == nil {
		planned := &gatewayState{Token: strings.Repeat("0", 32), Spec: spec}
		commands := []map[string]any{}
		for _, object := range gatewayDesiredNFT(planned) {
			commands = append(commands, map[string]any{"add": object})
		}
		data, err := json.Marshal(map[string]any{"nftables": commands})
		if err == nil {
			_, err = gatewayRunInput(string(data), "nft", "-c", "-j", "-f", "-")
		}
		if err != nil {
			add(fmt.Errorf("nftables 只读语法/能力预检未通过：%w", err))
		}
	}
	commands := []string{}
	if validErr == nil {
		commands = gatewayCommands(spec)
	}
	return LinuxGatewayPreviewResponse{
		Blockers:  blockers,
		Commands:  commands,
		Detection: detection,
		Managed:   gatewayManaged(cfg),
		Ready:     len(blockers) == 0,
		Spec:      spec,
		Success:   true,
		Warnings:  []string{"仅处理 IPv4；客户端 IPv6、DoH 和未选中的真实 IP 不会自动代理。", "先发布 Mihomo 和 OxiDNS，再应用网关，最后只切换一台客户端的网关与 DNS 进行验证。", "只恢复独立受管网络资源；自定义防火墙或策略路由需要先核对。"},
	}
}

func LinuxGatewayHandler(cfg *Config, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		method := http.MethodPost
		if action == "status" {
			method = http.MethodGet
		}
		if r.Method != method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Read-only and bounded; inspected outside the gateway locks so it never
		// delays apply, remove or forwarding requests.
		var forwardFilter netfilter.Result
		if action == "status" {
			forwardFilter = forwardFilterStatus(cfg)
		}
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		// Shares the same order as boot replay; return-routing never takes gatewayMu.
		returnRoutingMu.Lock()
		defer returnRoutingMu.Unlock()
		if action == "status" {
			detection, err := detectGatewayNetwork()
			enabled, _, configErr := gatewayConfigEnabled(cfg)
			result := LinuxGatewayStatusResponse{
				Detection:       detection,
				EnabledInConfig: enabled,
				ForwardFilter:   forwardFilter,
				IPv4Forwarding:  aioForwardingStatus(cfg),
				Managed:         gatewayManaged(cfg),
				Success:         true,
				Supported:       gatewayPlatform(cfg) == nil,
				TUN:             aioTUNStatus(cfg),
			}
			if err != nil {
				result.DetectionError = err.Error()
			}
			if configErr != nil {
				result.ConfigError = configErr.Error()
			}
			if err := gatewayPlatform(cfg); err != nil {
				result.UnsupportedReason = err.Error()
			}
			JsonResponse(w, 200, result)
			return
		}
		if action == "forwarding" {
			linuxGatewayForwarding(cfg, w, r)
			return
		}
		var request struct {
			LinuxGatewaySpec
			ProfileID string `json:"profile_id"`
		}
		if action != "remove" && action != "forwarding" {
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&request); err != nil {
				JsonResponse(w, 400, MessageResponse{Success: false, Message: "网关参数无效：" + err.Error()})
				return
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				JsonResponse(w, 400, MessageResponse{Success: false, Message: "请求只能包含一个 JSON 对象"})
				return
			}
		}
		if action == "preview" {
			JsonResponse(w, 200, gatewayPreview(cfg, request.LinuxGatewaySpec, request.ProfileID))
			return
		}
		release, err := AcquireServiceOperation(cfg)
		if err != nil {
			JsonResponse(w, 409, MessageResponse{Success: false, Message: err.Error()})
			return
		}
		defer release()
		if action == "apply" {
			err = applyLinuxGateway(cfg, request.LinuxGatewaySpec, request.ProfileID)
		} else {
			err = removeLinuxGateway(cfg)
		}
		if err != nil {
			JsonResponse(w, 409, LinuxGatewayActionResponse{Managed: gatewayManaged(cfg), Message: err.Error(), Success: false})
			return
		}
		JsonResponse(w, 200, LinuxGatewayActionResponse{
			Managed: gatewayManaged(cfg),
			Message: map[string]string{"apply": "网关转发已配置；请按引导验证一台客户端", "remove": "受管网关配置已撤销；请恢复客户端原来的网关和 DNS 设置"}[action],
			Success: true,
		})
	}
}

// linuxGatewayForwarding persists native AIO IPv4 forwarding, so the installation
// wizard can repair a full-TUN router split whose forwarding is not persisted.
func linuxGatewayForwarding(cfg *Config, w http.ResponseWriter, r *http.Request) {
	var request struct {
		PrepareTUN bool `json:"prepare_tun"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	if err == nil {
		if err := decoder.Decode(new(any)); err != io.EOF {
			JsonResponse(w, 400, MessageResponse{Success: false, Message: "请求只能包含一个 JSON 对象"})
			return
		}
	} else if err != io.EOF {
		JsonResponse(w, 400, MessageResponse{Success: false, Message: "转发参数无效：" + err.Error()})
		return
	}
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		JsonResponse(w, 409, LinuxGatewayForwardingErrorResponse{IPv4Forwarding: aioForwardingStatus(cfg), Message: err.Error(), Success: false})
		return
	}
	defer release()
	result, err := persistAIOForwarding(cfg, request.PrepareTUN)
	if err != nil {
		JsonResponse(w, 409, LinuxGatewayForwardingErrorResponse{IPv4Forwarding: aioForwardingStatus(cfg), Message: err.Error(), Success: false})
		return
	}
	JsonResponse(w, 200, LinuxGatewayForwardingSuccessResponse{
		IPv4Forwarding: aioForwardingStatus(cfg),
		Managed:        gatewayManaged(cfg),
		Message:        "IPv4 转发已开启并持久化",
		Success:        true,
		TUNPrepare:     result["tun_prepare"],
	})
}

// gatewayNotReady marks only transient host readiness failures. Ownership,
// configuration and firewall conflicts must never trigger automatic retries.
type gatewayNotReady struct{ error }

// ReapplyLinuxGateway waits a bounded interval for networking/Mihomo at boot.
func ReapplyLinuxGateway(cfg *Config) {
	if cfg == nil || cfg.ServiceType != "mihomo" {
		return
	}
	go func() {
		if err := reapplyLinuxGateway(cfg, 60, func() { time.Sleep(2 * time.Second) }); err != nil {
			logger.Warnf("linux gateway: boot replay refused: %v", err)
		}
	}()
}

func reapplyLinuxGateway(cfg *Config, attempts int, pause func()) error {
	deadline := time.Now().Add(2 * time.Minute)
	var last error
	for attempt := 0; attempt < attempts && time.Now().Before(deadline); attempt++ {
		state, err := loadGatewayState(cfg)
		if err != nil {
			return err
		}
		if state == nil || !state.Active || state.Pending {
			return nil
		}
		last = reconcileLinuxGateway(cfg)
		if last == nil {
			return nil
		}
		if _, transient := last.(gatewayNotReady); !transient {
			return last
		}
		if attempt+1 < attempts && time.Now().Before(deadline) {
			pause()
		}
	}
	return fmt.Errorf("等待网络或 Mihomo TUN 就绪超时，请检查服务后重新应用网关：%w", last)
}

func ReconcileLinuxGateway(cfg *Config) {
	if cfg == nil || cfg.ServiceType != "mihomo" {
		return
	}
	go func() {
		if err := reconcileLinuxGateway(cfg); err != nil {
			logger.Warnf("linux gateway: safe reconciliation refused: %v", err)
		}
	}()
}

func reconcileLinuxGateway(cfg *Config) error {
	if cfg == nil || cfg.ServiceType != "mihomo" {
		return nil
	}
	state, err := loadGatewayState(cfg)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}
	// Successful deployment still holds its service lock while scheduling
	// reconciliation. Wait without holding either network-operation mutex.
	var release func()
	for attempt := 0; attempt < 100; attempt++ {
		release, err = AcquireServiceOperation(cfg)
		if err == nil {
			break
		}
		if err != errDeploymentBusy {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		if err == errDeploymentBusy {
			return gatewayNotReady{err}
		}
		return err
	}
	defer release()
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	returnRoutingMu.Lock()
	defer returnRoutingMu.Unlock()
	state, err = loadGatewayState(cfg)
	if err != nil || state == nil {
		return err
	}
	enabled, _, err := gatewayConfigEnabled(cfg)
	if err != nil {
		return fmt.Errorf("配置检查失败，保留现有状态：%w", err)
	}
	if !enabled {
		return removeLinuxGateway(cfg)
	}
	if state.Active && !state.Pending {
		return applyLinuxGateway(cfg, state.Spec, state.ProfileID)
	}
	return nil
}

func gatewayNow() string { return time.Now().UTC().Format(time.RFC3339) }
