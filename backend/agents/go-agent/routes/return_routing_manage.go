package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agent/internal/atomicfile"
	"gopkg.in/yaml.v3"
)

// Managed return routing: ConfigFlow publishes the desired state, the Agent
// renders it into its own nft table plus a policy routing table and keeps it
// applied across restarts. Existing objects need a versioned ownership journal;
// manual/legacy setups are preserved and never automatically adopted.

const (
	managedReturnTable = "configflow_return"
	returnStateFile    = "return-routing.json"
	// Protocol 1 marks only connections to proxy_dst. Protocol 2 marks every
	// new connection the router forwards, so routing changes never touch it.
	returnProtocolByDestination = 1
	returnProtocolByRouter      = 2
)

// returnProtocols lists the spec protocols this Agent can apply.
var returnProtocols = []int{returnProtocolByDestination, returnProtocolByRouter}

// runCommandInput runs a command with stdin; replaceable in tests.
var runCommandInput = func(input string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type ReturnRoutingSpec struct {
	SafetyVersion int      `json:"safety_version"`
	RouterIP      string   `json:"router_ip"`
	ProxyDst      []string `json:"proxy_dst"`
	Mark          string   `json:"mark"`
	Table         int      `json:"table"`
	Priority      int      `json:"priority"`
	LanInterface  string   `json:"lan_interface,omitempty"`
	TunDevice     string   `json:"tun_device,omitempty"`
	// Kept only to explicitly reject legacy adoption requests from old clients.
	AdoptLegacy []string `json:"adopt_legacy,omitempty"`
}

type legacyBackup struct {
	Tables  []string `json:"tables"`
	Dump    string   `json:"dump,omitempty"`
	Scripts []string `json:"scripts,omitempty"`
}

type returnRoutingState struct {
	Active    bool              `json:"active"`
	Spec      ReturnRoutingSpec `json:"spec"`
	Legacy    *legacyBackup     `json:"legacy,omitempty"`
	AppliedAt string            `json:"applied_at"`
	Ownership *returnOwnership  `json:"ownership,omitempty"`
	// Retain audit metadata written by Agent 1.8.6. New versions never infer
	// ownership from legacy names/comments or migrate unowned resources.
	MigratedAt     string        `json:"migrated_at,omitempty"`
	MigratedLegacy *legacyBackup `json:"migrated_legacy,omitempty"`
	// send_redirects values ("all", LAN interface) before return routing
	// disabled them; restored when it is removed.
	SendRedirects map[string]string `json:"send_redirects,omitempty"`
}

// A versioned journal is the only evidence that a resource belongs to us.
// Names, marks and table numbers by themselves never confer ownership.
type returnOwnership struct {
	Version int              `json:"version"`
	Token   string           `json:"token"`
	Metric  int              `json:"metric"`
	Routes  []map[string]any `json:"routes"`
	Rules   []map[string]any `json:"rules"`
	NFT     []map[string]any `json:"nft"`
	Pending bool             `json:"pending"`
}

type resolvedSpec struct {
	ReturnRoutingSpec
	mark      uint32
	lanCIDR   string
	routerMAC string
}

var tableNamePattern = regexp.MustCompile(`^(ip|ip6|inet) [A-Za-z0-9_-]+$`)

func parseMark(text string) (uint32, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(text), 0, 32)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("mark 无效：%q", text)
	}
	return uint32(value), nil
}

func validateSpec(spec ReturnRoutingSpec) (resolvedSpec, error) {
	resolved := resolvedSpec{ReturnRoutingSpec: spec}
	if spec.SafetyVersion != returnProtocolByDestination && spec.SafetyVersion != returnProtocolByRouter {
		return resolved, fmt.Errorf("需要回流安全协议 1 或 2；请更新 ConfigFlow 后重新预览")
	}
	if len(spec.AdoptLegacy) != 0 {
		return resolved, fmt.Errorf("不允许接管其他程序或手工回流规则，请保留原配置并先处理冲突")
	}
	if ip := net.ParseIP(spec.RouterIP); ip == nil || ip.To4() == nil {
		return resolved, fmt.Errorf("router_ip 必须是 IPv4 地址")
	}
	mark, err := parseMark(spec.Mark)
	if err != nil {
		return resolved, err
	}
	resolved.mark = mark
	if spec.Table < 1 || spec.Table > 252 {
		return resolved, fmt.Errorf("table 必须在 1-252 之间")
	}
	if spec.Priority < 1 || spec.Priority > 32765 {
		return resolved, fmt.Errorf("priority 必须在 1-32765 之间")
	}
	if spec.SafetyVersion == returnProtocolByRouter && len(spec.ProxyDst) != 0 {
		return resolved, fmt.Errorf("回流安全协议 2 按来源标记，不接受 proxy_dst")
	}
	if spec.SafetyVersion == returnProtocolByDestination && len(spec.ProxyDst) == 0 {
		return resolved, fmt.Errorf("proxy_dst 不能为空")
	}
	for _, cidr := range spec.ProxyDst {
		if _, network, err := net.ParseCIDR(cidr); err != nil || network.IP.To4() == nil {
			return resolved, fmt.Errorf("proxy_dst 只接受 IPv4 CIDR：%q", cidr)
		}
	}
	for _, name := range []string{spec.LanInterface, spec.TunDevice} {
		if name != "" && !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`).MatchString(name) {
			return resolved, fmt.Errorf("网卡名无效：%q", name)
		}
	}
	for _, table := range spec.AdoptLegacy {
		if !tableNamePattern.MatchString(table) || strings.HasSuffix(table, " "+managedReturnTable) {
			return resolved, fmt.Errorf("adopt_legacy 表名无效：%q", table)
		}
	}
	return resolved, nil
}

// mihomoMarks reads marks and routing indexes from the mihomo config that must not collide.
type mihomoRouting struct {
	marks      map[string]uint32
	tunEnabled bool
	tunDevice  string
	tableIndex int
	ruleIndex  int
}

func readMihomoRouting(configPath string) mihomoRouting {
	result, _ := readMihomoRoutingStrict(configPath)
	return result
}

func readMihomoRoutingStrict(configPath string) (mihomoRouting, error) {
	result := mihomoRouting{marks: map[string]uint32{}, tunDevice: "Meta", tableIndex: 2022, ruleIndex: 9000}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return result, fmt.Errorf("无法读取 mihomo 配置以检查冲突：%w", err)
	}
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil || doc == nil {
		return result, fmt.Errorf("mihomo 配置格式无效，无法确认标记和路由表占用")
	}
	if value, exists := doc["routing-mark"]; exists {
		var mark uint64
		var parseErr error
		switch v := value.(type) {
		case int:
			if v < 0 {
				parseErr = fmt.Errorf("negative mark")
			} else {
				mark = uint64(v)
			}
		case string:
			mark, parseErr = strconv.ParseUint(strings.TrimSpace(v), 0, 32)
		default:
			parseErr = fmt.Errorf("invalid mark type")
		}
		if parseErr != nil || mark > uint64(^uint32(0)) {
			return result, fmt.Errorf("mihomo routing-mark 无法解析")
		}
		if mark != 0 {
			result.marks["routing-mark"] = uint32(mark)
		}
	}
	tun, _ := doc["tun"].(map[string]any)
	if tun == nil {
		if value, exists := doc["tun"]; exists && value != nil {
			return result, fmt.Errorf("mihomo tun 配置无效")
		}
		return result, nil
	}
	for _, key := range []string{"auto-redirect-input-mark", "auto-redirect-output-mark", "iproute2-table-index", "iproute2-rule-index"} {
		if value, exists := tun[key]; exists {
			if _, ok := yamlUint(value); !ok {
				return result, fmt.Errorf("mihomo tun.%s 无法解析", key)
			}
		}
	}
	for _, key := range []string{"enable", "auto-redirect"} {
		if value, exists := tun[key]; exists {
			if _, ok := value.(bool); !ok {
				return result, fmt.Errorf("mihomo tun.%s 无法解析", key)
			}
		}
	}
	result.tunEnabled, _ = tun["enable"].(bool)
	if device, ok := tun["device"].(string); ok && device != "" {
		result.tunDevice = device
	}
	if value, ok := yamlUint(tun["iproute2-table-index"]); ok {
		result.tableIndex = int(value)
	}
	if value, ok := yamlUint(tun["iproute2-rule-index"]); ok {
		result.ruleIndex = int(value)
	}
	if redirect, _ := tun["auto-redirect"].(bool); redirect {
		result.marks["auto-redirect-input-mark"] = 0x2023
		result.marks["auto-redirect-output-mark"] = 0x2024
		for _, key := range []string{"auto-redirect-input-mark", "auto-redirect-output-mark"} {
			if mark, ok := yamlUint(tun[key]); ok {
				result.marks[key] = mark
			}
		}
	}
	return result, nil
}

func yamlUint(value any) (uint32, bool) {
	switch v := value.(type) {
	case int:
		return uint32(v), v > 0 && uint64(v) <= uint64(^uint32(0))
	case string:
		mark, err := parseMark(v)
		return mark, err == nil
	}
	return 0, false
}

func checkMihomoConflicts(spec resolvedSpec, routing mihomoRouting) error {
	for key, mark := range routing.marks {
		if mark&spec.mark != 0 {
			return fmt.Errorf("mark 0x%x 与 mihomo %s 0x%x 有位重叠", spec.mark, key, mark)
		}
	}
	if spec.Table == routing.tableIndex {
		return fmt.Errorf("路由表 %d 已被 mihomo TUN 使用", spec.Table)
	}
	if spec.Priority >= routing.ruleIndex {
		return fmt.Errorf("策略路由优先级 %d 必须小于 mihomo 的 iproute2-rule-index %d", spec.Priority, routing.ruleIndex)
	}
	return nil
}

var (
	defaultRouteDev = regexp.MustCompile(`\bdev\s+(\S+)`)
	inetAddr        = regexp.MustCompile(`\binet\s+(\d+\.\d+\.\d+\.\d+/\d+)`)
	neighLLAddr     = regexp.MustCompile(`\blladdr\s+([0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5})\b`)
)

// resolveRouterMAC reads the router's MAC from the neighbor table, pinging once
// to populate it. Only connections forwarded by the router are marked: clients
// that reach the gateway directly (static gateway, or after an ICMP redirect
// from the router) must get replies directly, or the router drops them.
func resolveRouterMAC(spec *resolvedSpec) error {
	for attempt := 0; attempt < 2; attempt++ {
		out, err := runCommand("ip", "neigh", "show", spec.RouterIP, "dev", spec.LanInterface)
		if err == nil {
			if m := neighLLAddr.FindStringSubmatch(out); m != nil {
				spec.routerMAC = strings.ToLower(m[1])
				return nil
			}
		}
		if attempt == 0 {
			runCommand("ping", "-c", "1", "-W", "1", spec.RouterIP)
		}
	}
	return fmt.Errorf("无法获取主路由 %s 的 MAC 地址", spec.RouterIP)
}

// resolveInterfaces fills the LAN interface (the default route's device) and its CIDR.
func resolveInterfaces(spec *resolvedSpec, routing mihomoRouting) error {
	if spec.LanInterface == "" {
		out, err := runCommand("ip", "-4", "route", "show", "default")
		if err != nil {
			return fmt.Errorf("读取默认路由失败：%v", err)
		}
		for _, line := range strings.Split(out, "\n") {
			if m := defaultRouteDev.FindStringSubmatch(line); m != nil && !tunDeviceName.MatchString(m[1]) {
				spec.LanInterface = m[1]
				break
			}
		}
		if spec.LanInterface == "" {
			return fmt.Errorf("无法确定 LAN 网卡，请在期望状态中指定 lan_interface")
		}
	}
	out, err := runCommand("ip", "-4", "-o", "addr", "show", "dev", spec.LanInterface)
	if err != nil {
		return fmt.Errorf("读取网卡 %s 地址失败：%v", spec.LanInterface, err)
	}
	m := inetAddr.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("网卡 %s 没有 IPv4 地址", spec.LanInterface)
	}
	_, network, _ := net.ParseCIDR(m[1])
	spec.lanCIDR = network.String()
	if !network.Contains(net.ParseIP(spec.RouterIP)) {
		return fmt.Errorf("主路由 %s 不在 %s 的网段 %s 内", spec.RouterIP, spec.LanInterface, spec.lanCIDR)
	}
	if spec.TunDevice == "" && routing.tunEnabled {
		spec.TunDevice = routing.tunDevice
	}
	return resolveRouterMAC(spec)
}

func returnStatePath(cfg *Config) (string, error) {
	if cfg == nil || cfg.AgentConfigFile == "" {
		return "", fmt.Errorf("Agent 配置文件路径未知，无法保存回流规则")
	}
	return filepath.Join(filepath.Dir(cfg.AgentConfigFile), returnStateFile), nil
}

func loadReturnState(cfg *Config) (*returnRoutingState, error) {
	path, err := returnStatePath(cfg)
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
	var state returnRoutingState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("回流规则状态文件损坏：%v", err)
	}
	return &state, nil
}

func saveReturnState(cfg *Config, state *returnRoutingState) error {
	path, err := returnStatePath(cfg)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteWith(path, data, 0o600, atomicfile.Options{
		Pattern: ".return-state-*",
	})
}

// returnRoutingTools names every missing host tool at once, with install commands.
// BusyBox ip lacks -j/-N, so it fails "ip -V" like a missing ip and is called out.
func returnRoutingTools() error {
	missing := []string{}
	if _, err := runCommand("ip", "-V"); err != nil {
		if strings.Contains(err.Error(), "BusyBox") {
			missing = append(missing, "iproute2（当前 ip 为 BusyBox 版本）")
		} else {
			missing = append(missing, "iproute2")
		}
	}
	if _, err := runCommand("nft", "--version"); err != nil {
		missing = append(missing, "nftables")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("mihomo 主机缺少 %s，无法管理回流规则。请安装后重启 Agent："+
		"Alpine 执行 apk add iproute2 nftables；Debian/Ubuntu 执行 apt install iproute2 nftables；"+
		"OpenWrt 执行 opkg install ip-full nftables", strings.Join(missing, "、"))
}

func prepareSpec(cfg *Config, spec ReturnRoutingSpec) (resolvedSpec, error) {
	resolved, err := validateSpec(spec)
	if err != nil {
		return resolved, err
	}
	if err := returnRoutingTools(); err != nil {
		return resolved, err
	}
	routing, err := readMihomoRoutingStrict(cfg.ConfigPath)
	if err != nil {
		return resolved, err
	}
	if err := checkMihomoConflicts(resolved, routing); err != nil {
		return resolved, err
	}
	if err := resolveInterfaces(&resolved, routing); err != nil {
		return resolved, err
	}
	return resolved, nil
}

func managedReturnStatus(cfg *Config) any {
	state, err := loadReturnState(cfg)
	if err != nil {
		return &ReturnRoutingManagedError{Error: err.Error()}
	}
	if state == nil {
		return nil
	}
	complete := returnResourcesComplete(state)
	return &ReturnRoutingManagedDetail{
		Active:        state.Active,
		AppliedAt:     state.AppliedAt,
		Legacy:        state.Legacy,
		SafetyVersion: state.Spec.SafetyVersion,
		Spec:          state.Spec,
		TablePresent:  complete,
	}
}

func returnRoutingWrite(cfg *Config, w http.ResponseWriter, r *http.Request) {
	var (
		result any
		err    error
	)
	if r.Method == http.MethodPut {
		var spec ReturnRoutingSpec
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&spec); err != nil {
			JsonResponse(w, http.StatusBadRequest, MessageResponse{Success: false, Message: "请求格式错误：" + err.Error()})
			return
		}
		result, err = ApplyReturnRouting(cfg, spec)
	} else {
		result, err = RemoveReturnRouting(cfg, r.URL.Query().Get("restore_legacy") == "1")
	}
	if err != nil {
		JsonResponse(w, http.StatusUnprocessableEntity, MessageResponse{Success: false, Message: err.Error()})
		return
	}
	JsonResponse(w, http.StatusOK, result)
}
