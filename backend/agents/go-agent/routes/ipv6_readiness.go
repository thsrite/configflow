package routes

// IPv6 readiness keeps host configuration, DNS behaviour and end-to-end
// connectivity separate. Inspection never changes host or Mihomo settings.
import (
	"agent/preflight"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var ipv6Run = func(name string, args ...string) (string, error) { return runCommand(name, args...) }
var ipv6Assignments = preflight.IPv6SysctlAssignments
var ipv6SysctlRoot = "/proc/sys"
var ipv6SysctlDirectory = "/etc/sysctl.d"
var ipv6ReadFile = os.ReadFile
var ipv6WriteValue = func(path, value string) error { return os.WriteFile(path, []byte(value+"\n"), 0644) }
var ipv6Lookup = net.DefaultResolver.LookupIPAddr
var ipv6Dial = dialIPv6OnInterface

var ipv6InterfacePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`)

type ipv6Address struct {
	Address      string `json:"address"`
	PrefixLength int    `json:"prefix_length"`
	Scope        string `json:"scope"`
	Usable       bool   `json:"usable"`
	Public       bool   `json:"public"`
	Tentative    bool   `json:"tentative,omitempty"`
	Deprecated   bool   `json:"deprecated,omitempty"`
	DadFailed    bool   `json:"dad_failed,omitempty"`
	Temporary    bool   `json:"temporary,omitempty"`
}
type ipv6Interface struct {
	Name      string        `json:"name"`
	Index     int           `json:"index"`
	MAC       string        `json:"mac"`
	Addresses []ipv6Address `json:"addresses"`
}
type ipv6Route struct {
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
	Protocol  string `json:"protocol"`
	Expires   any    `json:"expires,omitempty"`
	Metric    int    `json:"metric"`
}
type ipv6Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type ipv6Evidence struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	SourceKey string `json:"source_key,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}
type ipv6Persistence struct {
	Persisted bool           `json:"persisted"`
	State     string         `json:"state"`
	Evidence  []ipv6Evidence `json:"evidence"`
	Missing   []string       `json:"missing"`
	Conflicts []string       `json:"conflicts"`
}
type ipv6Connectivity struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Protocol  string `json:"protocol,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Address   string `json:"address,omitempty"`
	Port      int    `json:"port,omitempty"`
	Interface string `json:"interface,omitempty"`
}
type ipv6Report struct {
	Success            bool             `json:"success"`
	Supported          bool             `json:"supported"`
	CanPrepare         bool             `json:"can_prepare"`
	Reason             string           `json:"reason,omitempty"`
	Interface          string           `json:"interface"`
	InterfaceIndex     int              `json:"interface_index"`
	Interfaces         []ipv6Interface  `json:"interfaces"`
	Addresses          []ipv6Address    `json:"addresses"`
	DefaultRoutes      []ipv6Route      `json:"default_routes"`
	Runtime            map[string]int   `json:"runtime"`
	Persistence        ipv6Persistence  `json:"persistence"`
	Checks             []ipv6Check      `json:"checks"`
	ConfigurationReady bool             `json:"configuration_ready"`
	Connectivity       ipv6Connectivity `json:"connectivity"`
	RebootVerified     bool             `json:"reboot_verified"`
	RARequired         bool             `json:"ra_required"`
	Core               map[string]any   `json:"core"`
	TUNIPv6            ipv6TUNReport    `json:"tun_ipv6"`
	Changed            bool             `json:"changed,omitempty"`
	Message            string           `json:"message,omitempty"`
}

type ipv6Network struct {
	Interfaces   []ipv6Interface
	Routes       []ipv6Route
	Upstream     string
	Reason       string
	Excluded     []*net.IPNet
	TUNAddresses map[string][]ipv6Address
}

func ipv6Key(iface, field string) string { return "net/ipv6/conf/" + iface + "/" + field }
func ipv6ShortKey(key string) string {
	return strings.ReplaceAll(strings.TrimPrefix(key, "net/ipv6/conf/"), "/", ".")
}
func ipv6ReadValue(key string) (string, error) {
	data, err := ipv6ReadFile(filepath.Join(ipv6SysctlRoot, key))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value != "0" && value != "1" && value != "2" {
		return "", fmt.Errorf("%s 的当前值无效", key)
	}
	return value, nil
}

func detectIPv6Network() (ipv6Network, error) {
	result := ipv6Network{Interfaces: []ipv6Interface{}, Routes: []ipv6Route{}, TUNAddresses: map[string][]ipv6Address{}}
	raw, err := ipv6Run("ip", "-j", "addr", "show")
	if err != nil {
		return result, fmt.Errorf("无法读取 IPv6 网卡信息，需要 iproute2：%w", err)
	}
	var links []struct {
		Name  string `json:"ifname"`
		Index int    `json:"ifindex"`
		MAC   string `json:"address"`
		Type  string `json:"link_type"`
		Info  []struct {
			Local      string   `json:"local"`
			Prefix     int      `json:"prefixlen"`
			Scope      string   `json:"scope"`
			Flags      []string `json:"flags"`
			Tentative  bool     `json:"tentative"`
			DadFailed  bool     `json:"dadfailed"`
			Deprecated bool     `json:"deprecated"`
			Temporary  bool     `json:"temporary"`
			Valid      any      `json:"valid_life_time"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return result, fmt.Errorf("IPv6 网卡信息格式无效：%w", err)
	}
	for _, link := range links {
		if link.Name == "lo" || link.Type != "ether" || !ipv6InterfacePattern.MatchString(link.Name) {
			for _, addr := range link.Info {
				if ip := net.ParseIP(addr.Local); ip != nil && ip.To4() == nil {
					if _, prefix, e := net.ParseCIDR(fmt.Sprintf("%s/%d", addr.Local, addr.Prefix)); e == nil {
						result.Excluded = append(result.Excluded, prefix)
					}
					invalid := addr.Tentative || addr.DadFailed || addr.Deprecated
					for _, flag := range addr.Flags {
						invalid = invalid || flag == "tentative" || flag == "dadfailed" || flag == "deprecated"
					}
					if life, ok := addr.Valid.(float64); ok && life == 0 {
						invalid = true
					}
					result.TUNAddresses[link.Name] = append(result.TUNAddresses[link.Name], ipv6Address{Address: ip.String(), PrefixLength: addr.Prefix, Usable: ip.IsGlobalUnicast() && !invalid})
				}
			}
			continue
		}
		item := ipv6Interface{Name: link.Name, Index: link.Index, MAC: link.MAC, Addresses: []ipv6Address{}}
		for _, addr := range link.Info {
			ip := net.ParseIP(addr.Local)
			if ip == nil || ip.To4() != nil {
				continue
			}
			invalid := addr.Tentative || addr.DadFailed || addr.Deprecated
			for _, flag := range addr.Flags {
				addr.Tentative = addr.Tentative || flag == "tentative"
				addr.DadFailed = addr.DadFailed || flag == "dadfailed"
				addr.Deprecated = addr.Deprecated || flag == "deprecated"
				addr.Temporary = addr.Temporary || flag == "temporary"
				invalid = invalid || flag == "tentative" || flag == "dadfailed" || flag == "deprecated"
			}
			if life, ok := addr.Valid.(float64); ok && life == 0 {
				invalid = true
			}
			usable := ip.IsGlobalUnicast() && !invalid
			item.Addresses = append(item.Addresses, ipv6Address{Address: ip.String(), PrefixLength: addr.Prefix, Scope: addr.Scope, Usable: usable, Public: usable && !ip.IsPrivate(), Tentative: addr.Tentative, Deprecated: addr.Deprecated, DadFailed: addr.DadFailed, Temporary: addr.Temporary})
		}
		result.Interfaces = append(result.Interfaces, item)
	}
	raw, err = ipv6Run("ip", "-j", "-6", "route", "show", "table", "main", "default")
	if err != nil {
		return result, fmt.Errorf("无法读取 IPv6 默认路由：%w", err)
	}
	var routes []struct {
		Dev       string          `json:"dev"`
		Gateway   string          `json:"gateway"`
		Protocol  any             `json:"protocol"`
		Expires   any             `json:"expires"`
		Metric    int             `json:"metric"`
		Type      string          `json:"type"`
		Multipath json.RawMessage `json:"multipath"`
	}
	if err = json.Unmarshal([]byte(raw), &routes); err != nil {
		return result, fmt.Errorf("IPv6 默认路由格式无效：%w", err)
	}
	candidates := map[string]bool{}
	for _, route := range routes {
		if route.Type != "" && route.Type != "unicast" {
			continue
		}
		if len(route.Multipath) > 0 {
			result.Reason = "IPv6 使用多路径默认路由，请手动核对上联网卡"
			continue
		}
		result.Routes = append(result.Routes, ipv6Route{Interface: route.Dev, Gateway: route.Gateway, Protocol: fmt.Sprint(route.Protocol), Expires: route.Expires, Metric: route.Metric})
		candidates[route.Dev] = true
	}
	// RA may currently be disabled by forwarding, so use the existing IPv4
	// upstream only when there is no IPv6 default route to select from.
	if len(candidates) == 0 && result.Reason == "" {
		raw, err = ipv6Run("ip", "-j", "-4", "route", "show", "table", "main", "default")
		if err != nil {
			return result, fmt.Errorf("无法确认上联网卡：%w", err)
		}
		routes = nil
		if err = json.Unmarshal([]byte(raw), &routes); err != nil {
			return result, fmt.Errorf("IPv4 默认路由格式无效：%w", err)
		}
		for _, route := range routes {
			if route.Type != "" && route.Type != "unicast" {
				continue
			}
			if len(route.Multipath) > 0 {
				result.Reason = "IPv4 使用多路径默认路由，请手动核对上联网卡"
				continue
			}
			candidates[route.Dev] = true
		}
	}
	if result.Reason == "" && len(candidates) == 1 {
		for _, iface := range result.Interfaces {
			if candidates[iface.Name] {
				result.Upstream = iface.Name
			}
		}
	}
	if result.Reason == "" && result.Upstream == "" {
		result.Reason = "未找到唯一的以太网上联网卡，请核对默认路由（不自动选择 TUN 或多出口）"
	}
	return result, nil
}

func ipv6Desired(iface string, ra bool) map[string]string {
	desired := map[string]string{ipv6Key("all", "forwarding"): "1", ipv6Key(iface, "forwarding"): "1"}
	if ra {
		desired[ipv6Key(iface, "accept_ra")] = "2"
		desired[ipv6Key(iface, "accept_ra_defrtr")] = "1"
	}
	return desired
}

func ipv6PersistenceFor(desired map[string]string, pending map[string][]byte) ipv6Persistence {
	result := ipv6Persistence{State: "missing", Evidence: []ipv6Evidence{}, Missing: []string{}, Conflicts: []string{}}
	assignments, unknown := ipv6Assignments(pending)
	effective := map[string]preflight.SysctlAssignment{}
	for _, a := range assignments {
		if strings.ContainsAny(a.Key, "*?[") {
			for key := range desired {
				if match, _ := filepath.Match(a.Key, key); match {
					unknown = true
				}
			}
			continue
		}
		if _, ok := desired[a.Key]; ok {
			effective[a.Key] = a
		}
		// all.forwarding writes every current interface's forwarding flag. A
		// later per-interface entry can override it, so respect assignment order.
		if a.Key == ipv6Key("all", "forwarding") {
			for key := range desired {
				if strings.HasSuffix(key, "/forwarding") {
					effective[key] = a
				}
			}
		}
	}
	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	managed := false
	for _, key := range keys {
		a, ok := effective[key]
		if !ok || !a.Persistent {
			result.Missing = append(result.Missing, ipv6ShortKey(key))
			continue
		}
		result.Evidence = append(result.Evidence, ipv6Evidence{Key: ipv6ShortKey(key), Value: a.Value, SourceKey: ipv6ShortKey(a.Key), File: a.File, Line: a.Line})
		if a.Value != desired[key] {
			result.Conflicts = append(result.Conflicts, fmt.Sprintf("%s:%d 设置 %s=%s", a.File, a.Line, ipv6ShortKey(a.Key), a.Value))
		}
		managed = managed || a.File == ipv6ManagedPath()
	}
	switch {
	case unknown:
		result.State = "unknown"
	case len(result.Conflicts) > 0:
		result.State = "conflict"
	case len(result.Missing) > 0:
		result.State = "missing"
	default:
		result.Persisted = true
		result.State = "system"
		if managed {
			result.State = "managed"
		}
	}
	return result
}

func ipv6CoreSettings(cfg *Config) map[string]any {
	result := map[string]any{}
	if cfg == nil {
		return result
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	doc := map[string]any{}
	if err == nil {
		err = yaml.Unmarshal(data, &doc)
	}
	if err != nil {
		result["error"] = "无法读取当前 Mihomo 配置"
		return result
	}
	// Never include proxy credentials, DNS URLs or the full YAML.
	for _, key := range []string{"ipv6"} {
		if v, ok := doc[key].(bool); ok {
			result[key] = v
		}
	}
	if dns, ok := doc["dns"].(map[string]any); ok {
		if v, ok := dns["ipv6"].(bool); ok {
			result["dns.ipv6"] = v
		}
		if v, ok := dns["fake-ip-range6"].(string); ok {
			result["dns.fake-ip-range6"] = v
		}
	}
	if tun, ok := doc["tun"].(map[string]any); ok {
		for _, key := range []string{"enable", "auto-route"} {
			if v, ok := tun[key].(bool); ok {
				result["tun."+key] = v
			}
		}
	}
	return result
}

func inspectIPv6(cfg *Config, selected string) ipv6Report {
	r := ipv6Report{Success: true, Interfaces: []ipv6Interface{}, Addresses: []ipv6Address{}, DefaultRoutes: []ipv6Route{}, Runtime: map[string]int{}, Checks: []ipv6Check{}, Persistence: ipv6Persistence{State: "unknown", Evidence: []ipv6Evidence{}, Missing: []string{}, Conflicts: []string{}}, Connectivity: ipv6Connectivity{Status: "not_tested", Message: "尚未测试节点连接；地址、路由和持久化配置通过不代表实际连接成功"}, Core: ipv6CoreSettings(cfg)}
	check := func(id, status, message string) { r.Checks = append(r.Checks, ipv6Check{id, status, message}) }
	if gatewayOS != "linux" || cfg == nil || cfg.ServiceType != "mihomo" {
		r.Reason = "IPv6 主机检查仅支持 Linux 上的 Mihomo Agent"
		check("platform", "info", r.Reason)
		return r
	}
	platformErr := gatewayPlatform(cfg)
	r.Supported = platformErr == nil
	if platformErr != nil {
		r.Reason = platformErr.Error()
		check("platform", "info", r.Reason)
	}
	network, err := detectIPv6Network()
	r.Interfaces, r.DefaultRoutes = network.Interfaces, network.Routes
	if err != nil {
		r.Reason = err.Error()
		check("network", "fail", r.Reason)
		return r
	}
	if network.Reason != "" {
		r.Reason = network.Reason
		check("interface", "warn", r.Reason)
		return r
	}
	if selected != "" && selected != network.Upstream {
		r.Reason = "所选网卡与当前上联网卡不一致，请重新检查"
		check("interface", "fail", r.Reason)
		return r
	}
	r.Interface = network.Upstream
	for _, iface := range network.Interfaces {
		if iface.Name == r.Interface {
			r.Addresses = iface.Addresses
			r.InterfaceIndex = iface.Index
		}
	}
	check("interface", "pass", "当前上联网卡为 "+r.Interface)
	r.RARequired = len(network.Routes) == 0
	for _, route := range network.Routes {
		if route.Protocol == "ra" || route.Protocol == "9" {
			r.RARequired = true
		}
	}
	hasPublic := false
	for _, addr := range r.Addresses {
		hasPublic = hasPublic || addr.Public
	}
	if hasPublic {
		check("address", "pass", "上联网卡已有可用公网 IPv6 地址")
	} else {
		check("address", "warn", "未发现可用公网 IPv6 地址；仅有 ULA 或链路本地地址不能证明公网可达")
	}
	if len(network.Routes) > 0 {
		check("default_route", "pass", "已有 IPv6 默认路由；通过 RA 学到且带有效期的路由正常，无需改为静态")
	} else {
		check("default_route", "warn", "尚未获得 IPv6 默认路由；准备主机后仍需上游发送 RA 或另行配置路由")
	}
	desired := ipv6Desired(r.Interface, r.RARequired)
	readOK := true
	runtimeOK := true
	for _, iface := range []string{"all", "default", r.Interface} {
		for _, field := range []string{"forwarding", "accept_ra", "accept_ra_defrtr", "autoconf", "disable_ipv6"} {
			key := ipv6Key(iface, field)
			value, e := ipv6ReadValue(key)
			if e != nil {
				if _, required := desired[key]; required {
					readOK = false
					runtimeOK = false
				}
				continue
			}
			n, _ := strconv.Atoi(value)
			r.Runtime[ipv6ShortKey(key)] = n
			if want, required := desired[key]; required && value != want {
				runtimeOK = false
			}
		}
	}
	if r.Runtime[r.Interface+".disable_ipv6"] == 1 {
		readOK = false
		runtimeOK = false
		check("ipv6_enabled", "fail", "上联网卡禁用了 IPv6，请先核对主机网络设置")
	}
	if runtimeOK {
		check("forwarding_ra", "pass", "转发与当前上网方式的 RA 设置相容")
	} else {
		check("forwarding_ra", "warn", "转发或实际网卡的 RA 参数尚未满足网关准备要求")
	}
	r.Persistence = ipv6PersistenceFor(desired, nil)
	if r.Persistence.Persisted {
		check("persistence", "pass", "已找到开机加载的参数配置；仍需实际重启验证，启动服务可能覆盖设置")
	} else {
		check("persistence", "warn", "部分参数未持久化或存在冲突，详见配置来源")
	}
	r.CanPrepare = r.Supported && readOK
	if _, e := loadIPv6Managed(cfg); e != nil {
		r.CanPrepare = false
		r.Reason = e.Error()
		check("ownership", "fail", r.Reason)
	}
	if r.CanPrepare {
		// Refuse a preparation that a later file or unknown wildcard would undo.
		candidate := ipv6PersistenceFor(desired, map[string][]byte{ipv6ManagedPath(): []byte(ipv6ManagedContent(r.Interface, desired))})
		if !candidate.Persisted {
			r.CanPrepare = false
			r.Reason = "存在后加载或无法确认的 sysctl 配置，请先处理冲突"
			check("prepare", "warn", r.Reason)
		}
	}
	if !readOK && r.Reason == "" {
		r.Reason = "无法完整读取或安全修改当前网卡的 IPv6 参数"
	}
	r.ConfigurationReady = hasPublic && len(network.Routes) > 0 && runtimeOK && r.Persistence.Persisted
	ipv6TUNCheck(&r, network, cfg)
	return r
}

func IPv6ReadinessHandler(cfg *Config, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := http.MethodGet
		if action != "status" {
			expected = http.MethodPost
		}
		if r.Method != expected {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if action == "probe" {
			ipv6ProbeHandler(cfg, w, r)
			return
		}
		if action == "verify" {
			ipv6VerifyHandler(cfg, w, r)
			return
		}
		if action == "verify-client" {
			ipv6VerifyClientHandler(cfg, w, r)
			return
		}
		if action == "verify-connections" {
			ipv6VerifyConnectionsHandler(cfg, w, r)
			return
		}
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		if action == "status" {
			JsonResponse(w, 200, inspectIPv6(cfg, r.URL.Query().Get("interface")))
			return
		}
		returnRoutingMu.Lock()
		defer returnRoutingMu.Unlock()
		var request struct {
			Interface      string `json:"interface"`
			InterfaceIndex int    `json:"interface_index"`
		}
		if err := decodeIPv6Request(w, r, &request); err != nil {
			JsonResponse(w, 400, MessageResponse{Success: false, Message: err.Error()})
			return
		}
		if !ipv6InterfacePattern.MatchString(request.Interface) || request.Interface == "lo" || request.InterfaceIndex <= 0 {
			JsonResponse(w, 400, MessageResponse{Success: false, Message: "请先检查并选择有效上联网卡"})
			return
		}
		// Unsupported platforms must not even create a deployment lock file.
		if err := gatewayPlatform(cfg); err != nil {
			JsonResponse(w, 409, MessageResponse{Success: false, Message: err.Error()})
			return
		}
		release, err := AcquireServiceOperation(cfg)
		if err != nil {
			JsonResponse(w, 409, MessageResponse{Success: false, Message: err.Error()})
			return
		}
		defer release()
		changed, err := prepareIPv6(cfg, request.Interface, request.InterfaceIndex)
		var coreErr error
		if err == nil {
			if network, e := detectIPv6Network(); e == nil {
				coreChanged, e := recoverIPv6TUN(cfg, network)
				changed = changed || coreChanged
				coreErr = e
			} else {
				coreErr = e
			}
		}
		report := inspectIPv6(cfg, request.Interface)
		if err != nil {
			report.Success = false
			report.Message = err.Error()
			JsonResponse(w, 409, report)
			return
		}
		report.Changed = changed
		report.Message = ipv6TUNRecoveryMessage(coreErr)
		if coreErr != nil {
			report.Checks = append(report.Checks, ipv6Check{ID: "tun_recovery", Status: "warn", Message: coreErr.Error()})
		}
		JsonResponse(w, 200, report)
	}
}
func decodeIPv6Request(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("IPv6 请求参数无效：%w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("请求只能包含一个 JSON 对象")
	}
	return nil
}

func ipv6ProbeHandler(cfg *Config, w http.ResponseWriter, r *http.Request) {
	var request struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := decodeIPv6Request(w, r, &request); err != nil {
		JsonResponse(w, 400, MessageResponse{Success: false, Message: err.Error()})
		return
	}
	if cfg == nil || cfg.ServiceType != "mihomo" || gatewayOS != "linux" {
		JsonResponse(w, 400, MessageResponse{Success: false, Message: "仅支持 Linux Mihomo 主机连接检查"})
		return
	}
	host := strings.TrimSpace(request.Host)
	ip := net.ParseIP(host)
	domain := regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.?$`)
	if request.Port < 1 || request.Port > 65535 || len(host) > 253 || (ip == nil && !domain.MatchString(host)) || (ip != nil && (ip.To4() != nil || !ip.IsGlobalUnicast())) {
		JsonResponse(w, 400, MessageResponse{Success: false, Message: "需要有效的 IPv6 节点地址或域名及端口（1–65535）"})
		return
	}
	result := ipv6Connectivity{Status: "error", Message: "节点 IPv6 TCP 连接未成功，请核对真实节点地址、端口和上联网卡绑定权限", Protocol: "tcp", Scope: "node_transport", Port: request.Port}
	finish := func() { JsonResponse(w, 200, IPv6ConnectivityResponse{Connectivity: result, Success: true}) }
	network, err := detectIPv6Network()
	if err != nil || network.Upstream == "" {
		result.Message = "无法确认实际 IPv6 上联网卡，未进行可能被 TUN 接管的连接测试"
		finish()
		return
	}
	result.Interface = network.Upstream
	excluded := network.Excluded
	configData, err := os.ReadFile(cfg.ConfigPath)
	doc := map[string]any{}
	if err == nil {
		err = yaml.Unmarshal(configData, &doc)
	}
	if err != nil {
		result.Message = "无法检查当前 Mihomo Fake-IP 范围，未执行节点连接测试"
		finish()
		return
	}
	if dns, ok := doc["dns"].(map[string]any); ok {
		if raw, ok := dns["fake-ip-range6"].(string); ok && raw != "" {
			_, prefix, e := net.ParseCIDR(raw)
			if e != nil || prefix.IP.To4() != nil {
				result.Message = "当前 Mihomo IPv6 Fake-IP 范围无效，未执行连接测试"
				finish()
				return
			}
			excluded = append(excluded, prefix)
		}
	}
	blocked := func(address net.IP) bool {
		for _, prefix := range excluded {
			if prefix.Contains(address) {
				return true
			}
		}
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	addresses := []net.IP{}
	if ip != nil {
		if blocked(ip) {
			result.Message = "节点地址落在 Mihomo Fake-IP 或 TUN 网段，需使用真实节点 IPv6 地址"
		} else {
			addresses = append(addresses, ip)
		}
	} else {
		found, err := ipv6Lookup(ctx, host)
		if err != nil {
			result.Message = "节点域名解析失败"
		} else {
			for _, a := range found {
				if a.IP.To4() == nil && a.IP.IsGlobalUnicast() {
					if blocked(a.IP) {
						result.Message = "节点域名解析到了 Mihomo Fake-IP 或 TUN 地址，请核对节点专用 DNS 或使用真实节点 IPv6 地址"
					} else {
						addresses = append(addresses, a.IP)
					}
				}
			}
			if len(addresses) == 0 && !strings.Contains(result.Message, "Fake-IP") {
				result.Message = "节点域名未返回可用 IPv6 地址"
			}
		}
	}
	// A failing first AAAA must not consume the entire deadline for all others.
	for i, address := range addresses {
		if i >= 4 || ctx.Err() != nil {
			break
		}
		attempt, done := context.WithTimeout(ctx, 2*time.Second)
		connection, err := ipv6Dial(attempt, network.Upstream, net.JoinHostPort(address.String(), strconv.Itoa(request.Port)))
		done()
		if err == nil {
			connection.Close()
			result.Status = "ok"
			result.Address = address.String()
			result.Message = "主机到节点的 IPv6 TCP 连接成功；未验证代理协议握手或客户端分流"
			break
		}
	}
	finish()
}
