package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ipv6ManagedMarker = "# Managed by ConfigFlow IPv6 readiness v1"

var ipv6Save = durableWrite

type ipv6Managed struct {
	Version    int               `json:"version"`
	Interface  string            `json:"interface"`
	MAC        string            `json:"mac"`
	Values     map[string]string `json:"values"`
	FileValues map[string]string `json:"file_values"`
	Phase      string            `json:"phase"`
}

func ipv6ManagedPath() string { return filepath.Join(ipv6SysctlDirectory, "99-configflow-ipv6.conf") }
func ipv6StatePath(cfg *Config) (string, error) {
	if cfg == nil || !filepath.IsAbs(cfg.AgentConfigFile) {
		return "", fmt.Errorf("Agent 配置路径未知，无法保存 IPv6 管理记录")
	}
	return filepath.Join(filepath.Dir(cfg.AgentConfigFile), "ipv6-readiness.json"), nil
}
func ipv6ManagedContent(iface string, values map[string]string) string {
	// RA reception must be enabled before turning the host into a router.
	keys := ipv6OrderedKeys(values)
	lines := []string{ipv6ManagedMarker, "# upstream interface: " + iface}
	for _, key := range keys {
		lines = append(lines, key+"="+values[key])
	}
	return strings.Join(lines, "\n") + "\n"
}
func ipv6OrderedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	rank := func(k string) int {
		if strings.HasSuffix(k, "/accept_ra") {
			return 0
		}
		if strings.HasSuffix(k, "/accept_ra_defrtr") {
			return 1
		}
		if k == ipv6Key("all", "forwarding") {
			return 2
		}
		return 3
	}
	sort.Slice(keys, func(i, j int) bool {
		if rank(keys[i]) != rank(keys[j]) {
			return rank(keys[i]) < rank(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}
func ipv6RegularContent(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s 不是普通文件，保留现有内容", path)
	}
	return os.ReadFile(path)
}
func loadIPv6Managed(cfg *Config) (*ipv6Managed, error) {
	path, err := ipv6StatePath(cfg)
	if err != nil {
		return nil, err
	}
	data, err := ipv6RegularContent(path)
	if err != nil {
		return nil, err
	}
	content, err := ipv6RegularContent(ipv6ManagedPath())
	if err != nil {
		return nil, err
	}
	if data == nil {
		if content != nil {
			return nil, fmt.Errorf("IPv6 持久化路径已有未归属本 Agent 的配置，拒绝覆盖：%s", ipv6ManagedPath())
		}
		return nil, nil
	}
	var state ipv6Managed
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("IPv6 管理记录损坏，保留现有配置")
	}
	if state.Version != 1 || !ipv6InterfacePattern.MatchString(state.Interface) || state.Interface == "lo" || state.MAC == "" || (state.Phase != "active" && state.Phase != "pending") {
		return nil, fmt.Errorf("IPv6 管理记录无效，保留现有配置")
	}
	allowed := ipv6Desired(state.Interface, true)
	for _, values := range []map[string]string{state.Values, state.FileValues} {
		for key, value := range values {
			if allowed[key] != value {
				return nil, fmt.Errorf("IPv6 管理记录包含未知参数，保留现有配置")
			}
		}
	}
	// An explicit prepare can authorize delayed-core recovery while keeping
	// complete host-owned sysctl persistence. Empty FileValues owns no sysctl file.
	if len(state.FileValues) == 0 && content != nil {
		return nil, fmt.Errorf("此 IPv6 记录未接管 sysctl 文件，保留现有文件并停止覆盖")
	}
	if state.Values[ipv6Key("all", "forwarding")] != "1" || state.Values[ipv6Key(state.Interface, "forwarding")] != "1" {
		// A file may contain only missing parameters, but the journal always keeps
		// the complete expected gateway configuration for boot replay.
		return nil, fmt.Errorf("IPv6 管理记录缺少转发配置")
	}
	if content != nil && string(content) != ipv6ManagedContent(state.Interface, state.FileValues) {
		return nil, fmt.Errorf("ConfigFlow IPv6 持久化文件已被修改，拒绝覆盖：%s", ipv6ManagedPath())
	}
	if content == nil && state.Phase == "active" && len(state.FileValues) > 0 {
		return nil, fmt.Errorf("受管 IPv6 持久化文件已被移除，请先核对配置")
	}
	return &state, nil
}

// snapshot includes every forwarding flag because writing all.forwarding has
// kernel side effects on current interfaces and conf/default. On failure those
// flags are restored too, rather than restoring only the requested interface.
func ipv6RuntimeSnapshot(values map[string]string) (map[string]string, error) {
	result := map[string]string{}
	for key := range values {
		v, err := ipv6ReadValue(key)
		if err != nil {
			return nil, err
		}
		result[key] = v
	}
	entries, err := os.ReadDir(filepath.Join(ipv6SysctlRoot, "net/ipv6/conf"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := ipv6Key(entry.Name(), "forwarding")
		v, err := ipv6ReadValue(key)
		if err != nil {
			return nil, err
		}
		result[key] = v
	}
	return result, nil
}
func ipv6SetValue(key, value string) error {
	path := filepath.Join(ipv6SysctlRoot, key)
	current, err := ipv6ReadValue(key)
	if err != nil {
		return err
	}
	if current == value {
		return nil
	}
	if err := ipv6WriteValue(path, value); err != nil {
		return err
	}
	actual, err := ipv6ReadValue(key)
	if err != nil || actual != value {
		return fmt.Errorf("%s 写入后读回未达到 %s（%s，%v）", key, value, actual, err)
	}
	return nil
}
func ipv6RestoreRuntime(before map[string]string) error {
	problems := []string{}
	// Restore global first, then every per-interface side effect.
	key := ipv6Key("all", "forwarding")
	if value, ok := before[key]; ok {
		if err := ipv6SetValue(key, value); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, key := range ipv6OrderedKeys(before) {
		if key == ipv6Key("all", "forwarding") {
			continue
		}
		if err := ipv6SetValue(key, before[key]); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}
	return nil
}
func ipv6RestoreFile(path string, before []byte, mode os.FileMode) error {
	if before == nil {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return durableWrite(path, before, mode)
}

func prepareIPv6(cfg *Config, iface string, index int) (changed bool, resultErr error) {
	if err := gatewayPlatform(cfg); err != nil {
		return false, err
	}
	report := inspectIPv6(cfg, iface)
	if !report.CanPrepare {
		return false, fmt.Errorf("无法自动准备 IPv6：%s", report.Reason)
	}
	if report.InterfaceIndex != index {
		return false, fmt.Errorf("上联网卡已变化，请重新检查后再应用")
	}
	state, err := loadIPv6Managed(cfg)
	if err != nil {
		return false, err
	}
	var current ipv6Interface
	for _, candidate := range report.Interfaces {
		if candidate.Name == iface {
			current = candidate
		}
	}
	if current.MAC == "" {
		return false, fmt.Errorf("无法识别上联网卡身份，请手动配置")
	}
	if state != nil && (state.Interface != iface || state.MAC != current.MAC) {
		return false, fmt.Errorf("上联网卡与受管记录不一致，保留原配置，请先核对网卡变更")
	}
	values := ipv6Desired(iface, report.RARequired)
	if state != nil {
		for key, value := range state.Values {
			values[key] = value
		}
	}
	beforeRuntime, err := ipv6RuntimeSnapshot(values)
	if err != nil {
		return false, fmt.Errorf("无法读取 IPv6 原始参数：%w", err)
	}
	runtimeNeedsChange := false
	for key, value := range values {
		runtimeNeedsChange = runtimeNeedsChange || beforeRuntime[key] != value
	}
	// Respect complete user-owned persistence: apply only the missing runtime
	// values and do not claim ownership or rewrite their sysctl files.
	if (state == nil || len(state.FileValues) == 0) && report.Persistence.Persisted {
		statePath, err := ipv6StatePath(cfg)
		if err != nil {
			return false, err
		}
		beforeState, err := ipv6RegularContent(statePath)
		if err != nil {
			return false, err
		}
		defer func() {
			if resultErr != nil {
				if err := ipv6RestoreRuntime(beforeRuntime); err != nil {
					resultErr = fmt.Errorf("%v；恢复原参数失败：%v", resultErr, err)
				}
				if err := ipv6RestoreFile(statePath, beforeState, 0600); err != nil {
					resultErr = fmt.Errorf("%v；恢复授权记录失败：%v", resultErr, err)
				}
			}
		}()
		for _, key := range ipv6OrderedKeys(values) {
			if err := ipv6SetValue(key, values[key]); err != nil {
				return false, fmt.Errorf("无法设置 IPv6 参数（检查 CAP_NET_ADMIN 与 /proc/sys 写权限）：%w", err)
			}
		}
		if state == nil {
			// This records explicit preparation only, without taking ownership of
			// the host's files. Read-only inspection never grants this authorization.
			authorization := ipv6Managed{Version: 1, Interface: iface, MAC: current.MAC, Values: values, FileValues: map[string]string{}, Phase: "active"}
			data, _ := json.MarshalIndent(authorization, "", "  ")
			if err := ipv6Save(statePath, data, 0600); err != nil {
				return false, fmt.Errorf("无法保存 IPv6 启动恢复授权：%w", err)
			}
		}
		return runtimeNeedsChange, nil
	}
	if state != nil && len(state.FileValues) == 0 {
		return false, fmt.Errorf("主机自己的 IPv6 持久化配置已变化，保留其配置，请先核对冲突")
	}

	// Persist only missing or conflicting values. Already correct user-owned
	// assignments remain in their existing files and retain their source evidence.
	fileValues := map[string]string{}
	if state != nil {
		for key, value := range state.FileValues {
			fileValues[key] = value
		}
	}
	persisted := map[string]string{}
	for _, evidence := range report.Persistence.Evidence {
		persisted[evidence.Key] = evidence.Value
	}
	for key, value := range values {
		if persisted[ipv6ShortKey(key)] != value {
			fileValues[key] = value
		}
	}
	content := []byte(ipv6ManagedContent(iface, fileValues))
	if !ipv6PersistenceFor(values, map[string][]byte{ipv6ManagedPath(): content}).Persisted {
		return false, fmt.Errorf("后加载的 sysctl 配置会覆盖 IPv6 参数，请先处理冲突")
	}
	info, err := os.Lstat(ipv6SysctlDirectory)
	if err != nil || !info.IsDir() {
		return false, fmt.Errorf("sysctl.d 目录不可用或不是普通目录：%s", ipv6SysctlDirectory)
	}
	statePath, err := ipv6StatePath(cfg)
	if err != nil {
		return false, err
	}
	dirInfo, err := os.Lstat(filepath.Dir(statePath))
	if err != nil || !dirInfo.IsDir() {
		return false, fmt.Errorf("Agent 配置目录不可用或是符号链接")
	}
	beforeState, err := ipv6RegularContent(statePath)
	if err != nil {
		return false, err
	}
	beforeFile, err := ipv6RegularContent(ipv6ManagedPath())
	if err != nil {
		return false, err
	}
	if state != nil && state.Phase == "active" && string(beforeFile) == string(content) && !runtimeNeedsChange {
		return false, nil
	}
	pending := ipv6Managed{Version: 1, Interface: iface, MAC: current.MAC, Values: values, FileValues: fileValues, Phase: "pending"}
	data, _ := json.MarshalIndent(pending, "", "  ")
	if err := ipv6Save(statePath, data, 0600); err != nil {
		return false, fmt.Errorf("无法保存 IPv6 管理记录：%w", err)
	}
	fileWritten := false
	defer func() {
		if resultErr == nil {
			return
		}
		problems := []string{}
		if err := ipv6RestoreRuntime(beforeRuntime); err != nil {
			problems = append(problems, "恢复参数失败："+err.Error())
		}
		if fileWritten {
			if err := ipv6RestoreFile(ipv6ManagedPath(), beforeFile, 0644); err != nil {
				problems = append(problems, "恢复持久化文件失败："+err.Error())
			}
		}
		if err := ipv6RestoreFile(statePath, beforeState, 0600); err != nil {
			problems = append(problems, "恢复管理记录失败："+err.Error())
		}
		if len(problems) > 0 {
			resultErr = fmt.Errorf("%v；%s", resultErr, strings.Join(problems, "；"))
		}
	}()
	for _, key := range ipv6OrderedKeys(values) {
		if err := ipv6SetValue(key, values[key]); err != nil {
			return false, fmt.Errorf("无法设置 IPv6 参数（检查 CAP_NET_ADMIN 与 /proc/sys 写权限）：%w", err)
		}
	}
	if string(beforeFile) != string(content) {
		// Recheck ownership immediately before replacing our own file.
		currentFile, e := ipv6RegularContent(ipv6ManagedPath())
		if e != nil || string(currentFile) != string(beforeFile) {
			return false, fmt.Errorf("IPv6 持久化文件在应用期间发生变化，停止覆盖")
		}
		if err := ipv6Save(ipv6ManagedPath(), content, 0644); err != nil {
			return false, fmt.Errorf("无法保存 IPv6 持久化参数：%w", err)
		}
		fileWritten = true
	}
	pending.Phase = "active"
	data, _ = json.MarshalIndent(pending, "", "  ")
	if err := ipv6Save(statePath, data, 0600); err != nil {
		return false, fmt.Errorf("无法提交 IPv6 管理记录：%w", err)
	}
	return true, nil
}

// ReapplyIPv6Readiness replays only journaled parameters after the Agent starts.
// The sysctl file handles normal boot; replay also covers late-created LXC
// interfaces. No journal means no action, and changed files/MACs are never taken
// over. Reboot verification is still a separate end-to-end test.
func ReapplyIPv6Readiness(cfg *Config) {
	if gatewayPlatform(cfg) != nil {
		return
	}
	go func() {
		for attempt := 0; attempt < 60; attempt++ {
			waiting, err := replayIPv6Readiness(cfg)
			if !waiting {
				if err != nil {
					logger.Warnf("IPv6 preparation replay needs attention: %v", err)
				}
				return
			}
			time.Sleep(time.Second)
		}
		logger.Warn("IPv6 preparation replay could not confirm IPv6/TUN readiness within 60 checks; existing IPv4 service was kept, inspect IPv6 readiness and republish Mihomo when the network is ready")
	}()
}

func replayIPv6Readiness(cfg *Config) (bool, error) {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	// Use the existing gateway -> return-routing -> service-operation order.
	// Reloading TUN must not race a return-routing rule change.
	returnRoutingMu.Lock()
	defer returnRoutingMu.Unlock()
	state, err := loadIPv6Managed(cfg)
	if err != nil || state == nil {
		return false, err
	}
	network, err := detectIPv6Network()
	if err != nil {
		return true, err
	}
	var iface ipv6Interface
	for _, candidate := range network.Interfaces {
		if candidate.Name == state.Interface {
			iface = candidate
		}
	}
	if iface.Name == "" || network.Upstream == "" {
		return true, fmt.Errorf("受管上联网卡尚未就绪")
	}
	if iface.MAC != state.MAC || network.Upstream != state.Interface {
		return false, fmt.Errorf("上联网卡与 IPv6 恢复记录不一致，未更改配置")
	}
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		return errors.Is(err, errDeploymentBusy), err
	}
	defer release()
	if _, err = prepareIPv6(cfg, iface.Name, iface.Index); err != nil {
		return false, err
	}
	network, err = detectIPv6Network()
	if err != nil {
		return true, err
	}
	_, err = recoverIPv6TUN(cfg, network)
	var waiting ipv6TUNWaiting
	return errors.As(err, &waiting), err
}
