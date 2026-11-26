package routes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

var gatewayLookPath = exec.LookPath

func gatewayOwner(state *gatewayState) string { return "configflow-gateway:" + state.Token }

func gatewayDesiredNFT(state *gatewayState) []map[string]any {
	tag, spec := gatewayOwner(state), state.Spec
	objects := []map[string]any{
		nftObject("table", map[string]any{"family": "ip", "name": gatewayTable, "comment": tag}),
		nftObject("chain", map[string]any{"family": "ip", "table": gatewayTable, "name": "forward", "type": "filter", "hook": "forward", "prio": 0, "policy": "accept", "comment": tag}),
		nftObject("chain", map[string]any{"family": "ip", "table": gatewayTable, "name": "postrouting", "type": "nat", "hook": "postrouting", "prio": 100, "policy": "accept", "comment": tag}),
	}
	meta := func(key string) any {
		return map[string]any{"meta": map[string]any{"key": key}}
	}
	ip := func(key string) any {
		return map[string]any{"payload": map[string]any{"protocol": "ip", "field": key}}
	}
	parts := strings.Split(spec.LANCIDR, "/")
	var bits int
	_, _ = fmt.Sscanf(parts[1], "%d", &bits)
	subnet := map[string]any{"prefix": map[string]any{"addr": parts[0], "len": bits}}
	addRule := func(chain, name string, expr []any) {
		objects = append(objects, nftObject("rule", map[string]any{"family": "ip", "table": gatewayTable, "chain": chain, "expr": expr, "comment": tag + ":" + name}))
	}
	addRule("forward", "lan", []any{nftMatch(meta("iifname"), spec.Interface, "=="), nftMatch(ip("saddr"), subnet, "=="), map[string]any{"accept": nil}})
	addRule("forward", "reply", []any{nftMatch(meta("oifname"), spec.Interface, "=="), nftMatch(ip("daddr"), subnet, "=="), nftMatch(map[string]any{"ct": map[string]any{"key": "state"}}, map[string]any{"set": []any{"established", "related"}}, "=="), map[string]any{"accept": nil}})
	addRule("postrouting", "snat", []any{nftMatch(meta("iifname"), spec.Interface, "=="), nftMatch(meta("oifname"), spec.Interface, "=="), nftMatch(ip("saddr"), subnet, "=="), nftMatch(ip("daddr"), subnet, "!="), map[string]any{"snat": map[string]any{"addr": spec.Address}}})
	return objects
}

// nft compiles the old ct-state set membership spelling "in" to "==".
// Accept that one equivalent spelling in preexisting journals, without relaxing
// object identity, comments, expression ordering, or any other rule content.
func canonicalGatewayNFT(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			result[key] = canonicalGatewayNFT(child)
		}
		if match, ok := result["match"].(map[string]any); ok && len(result) == 1 && len(match) == 3 && match["op"] == "in" {
			left := map[string]any{"left": match["left"]}
			right := map[string]any{"right": match["right"]}
			state := map[string]any{"left": map[string]any{"ct": map[string]any{"key": "state"}}}
			states := map[string]any{"right": map[string]any{"set": []any{"established", "related"}}}
			if normalizedResourceEqual(left, state) && normalizedResourceEqual(right, states) {
				match["op"] = "=="
			}
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, child := range value {
			result[index] = canonicalGatewayNFT(child)
		}
		return result
	default:
		return value
	}
}

func gatewayNormalizedResourceEqual(expected, actual map[string]any) bool {
	return normalizedResourceEqual(canonicalGatewayNFT(expected).(map[string]any), canonicalGatewayNFT(actual).(map[string]any))
}

func gatewayOwnedResourceEqual(expected, actual map[string]any) bool {
	return ownedResourceEqual(canonicalGatewayNFT(expected).(map[string]any), canonicalGatewayNFT(actual).(map[string]any))
}

func readGatewayNFT() ([]map[string]any, error) {
	raw, err := gatewayRun("nft", "-j", "list", "ruleset")
	if err != nil {
		return nil, fmt.Errorf("无法检查 nftables（需要 nft 和网络管理权限）：%w", err)
	}
	value, err := decodeResourceJSON(raw)
	if err != nil {
		return nil, err
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("nft ruleset 返回格式无效")
	}
	return resourceObjects(doc["nftables"], "nft ruleset")
}

func gatewayNFTObjects(objects []map[string]any) []map[string]any {
	result := []map[string]any{}
	for _, object := range objects {
		for kind, value := range object {
			body, _ := value.(map[string]any)
			if body["family"] == "ip" && ((kind == "table" && body["name"] == gatewayTable) || body["table"] == gatewayTable) {
				result = append(result, object)
			}
		}
	}
	return result
}

func checkGatewayOwnership(state *gatewayState, all []map[string]any, require bool) error {
	actual := gatewayNFTObjects(all)
	if len(actual) == 0 {
		if require {
			return fmt.Errorf("受管 nftables 表不存在，需要重新应用")
		}
		return nil
	}
	if state == nil {
		return fmt.Errorf("发现同名 nftables 表但没有归属 journal，拒绝接管")
	}
	if len(actual) != len(state.NFT) {
		return fmt.Errorf("受管 nftables 表已被外部修改，保留现有规则")
	}
	expected := state.NFT
	if gatewayInitialNFTReadback(state, actual) {
		expected = gatewayNFTWithoutObjectComments(expected)
	}
	if !gatewayNFTListsEqual(expected, actual, gatewayOwnedResourceEqual) {
		return fmt.Errorf("受管 nftables 资源归属、内容或规则顺序变化，保留现有规则")
	}
	return nil
}

func gatewayPreflightResources(cfg *Config, state *gatewayState) ([]map[string]any, error) {
	if prior, err := loadReturnState(cfg); err != nil {
		return nil, err
	} else if prior != nil && (prior.Active || (prior.Ownership != nil && prior.Ownership.Pending)) {
		return nil, fmt.Errorf("自动回流已启用或尚未完成，请先撤销回流再配置 Linux 网关")
	}
	for _, command := range []struct {
		name string
		args []string
	}{{"firewall-cmd", []string{"--state"}}, {"ufw", []string{"status"}}} {
		if _, err := gatewayLookPath(command.name); err != nil {
			continue
		}
		out, err := gatewayRun(command.name, command.args...)
		if command.name == "firewall-cmd" {
			if strings.TrimSpace(out) == "running" {
				return nil, fmt.Errorf("firewalld 正在管理防火墙；请先规划兼容规则，自动配置已停止")
			}
			if err != nil && strings.TrimSpace(out) != "not running" {
				return nil, fmt.Errorf("无法确认 firewalld 状态，拒绝修改网络")
			}
		} else if strings.Contains(out, "Status: active") {
			return nil, fmt.Errorf("UFW 已启用；请先规划兼容规则，自动配置已停止")
		} else if err != nil || !strings.Contains(out, "Status: inactive") {
			return nil, fmt.Errorf("无法确认 UFW 状态，拒绝修改网络")
		}
	}
	// nft cannot observe iptables-legacy filter rules. Read both backends when present.
	for _, binary := range []string{"iptables-save", "iptables-legacy-save"} {
		if _, err := gatewayLookPath(binary); err != nil {
			continue
		}
		out, err := gatewayRun(binary, "-t", "filter")
		if err != nil {
			return nil, fmt.Errorf("无法检查 %s FORWARD 规则：%w", binary, err)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, ":FORWARD ") && !strings.HasPrefix(line, ":FORWARD ACCEPT ") || strings.HasPrefix(line, "-A FORWARD ") {
				return nil, fmt.Errorf("已有 iptables FORWARD 规则或非 ACCEPT 策略，无法保证独立网关表放行；已停止自动配置")
			}
		}
	}
	objects, err := readGatewayNFT()
	if err != nil {
		return nil, err
	}
	if err := checkGatewayOwnership(state, objects, false); err != nil {
		return nil, err
	}
	forward := map[string]bool{}
	for _, object := range objects {
		if table, ok := object["table"].(map[string]any); ok && table["name"] == managedReturnTable {
			return nil, fmt.Errorf("检测到自动回流表，不能混用 Linux 网关")
		}
		if chain, ok := object["chain"].(map[string]any); ok && chain["hook"] == "forward" && chain["table"] != gatewayTable && (chain["family"] == "ip" || chain["family"] == "inet") {
			if chain["policy"] != "accept" {
				return nil, fmt.Errorf("现有 nftables FORWARD 链策略不是 accept，不能保证网关转发")
			}
			forward[fmt.Sprint(chain["family"], "/", chain["table"], "/", chain["name"])] = true
		}
	}
	for _, object := range objects {
		if rule, ok := object["rule"].(map[string]any); ok && forward[fmt.Sprint(rule["family"], "/", rule["table"], "/", rule["chain"])] && !gatewayPureAcceptRule(rule) {
			return nil, fmt.Errorf("现有 nftables FORWARD 链包含自定义规则，自动配置不会覆盖；请先核对防火墙")
		}
	}
	return objects, nil
}

// Existing pure accept rules (including Mihomo's established-flow allowances)
// cannot deny forwarding. Unknown verdicts, jumps and packet mutations remain
// blockers; table names are never treated as evidence of compatibility.
func gatewayPureAcceptRule(rule map[string]any) bool {
	expressions, ok := rule["expr"].([]any)
	if !ok || len(expressions) == 0 {
		return false
	}
	for index, value := range expressions {
		expression, ok := value.(map[string]any)
		if !ok || len(expression) != 1 {
			return false
		}
		for name := range expression {
			switch name {
			case "match", "counter":
				if index == len(expressions)-1 {
					return false
				}
			case "accept":
				if index != len(expressions)-1 {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func gatewaySysctls(spec LinuxGatewaySpec) map[string]string {
	return map[string]string{"net/ipv4/ip_forward": "1", "net/ipv4/conf/all/send_redirects": "0", "net/ipv4/conf/default/send_redirects": "0", "net/ipv4/conf/" + spec.Interface + "/send_redirects": "0", "net/ipv4/conf/default/forwarding": "1", "net/ipv4/conf/" + spec.Interface + "/forwarding": "1"}
}

func gatewayStateSysctls(state *gatewayState) map[string]string {
	values := gatewaySysctls(state.Spec)
	if state.TunDevice != "" {
		values["net/ipv4/conf/"+state.TunDevice+"/forwarding"] = "1"
	}
	return values
}

var gatewayWriteSysctl = func(key, value string) error {
	return os.WriteFile(filepath.Join(gatewaySysctlRoot, key), []byte(value), 0644)
}

func gatewayReadSysctl(key string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gatewaySysctlRoot, key))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value != "0" && value != "1" {
		return "", fmt.Errorf("%s 当前值无效", key)
	}
	return value, nil
}

func checkGatewaySysctls(state *gatewayState) error {
	for key, expected := range gatewayStateSysctls(state) {
		value, err := gatewayReadSysctl(key)
		if err != nil {
			return fmt.Errorf("无法读取 %s：%w", key, err)
		}
		if value != expected {
			return fmt.Errorf("%s 与已应用值不一致", key)
		}
	}
	return nil
}

func recordGatewaySysctls(state *gatewayState) error {
	for key := range gatewayStateSysctls(state) {
		// Even journaled values must exist again after a reboot, before Pending or
		// firewall changes. In particular, Mihomo may create its TUN a little later.
		value, err := gatewayReadSysctl(key)
		if err != nil {
			failure := fmt.Errorf("无法读取 %s（请先确认 Mihomo TUN 正在运行）：%w", key, err)
			if os.IsNotExist(err) {
				return gatewayNotReady{failure}
			}
			return failure
		}
		if _, exists := state.Original[key]; !exists {
			state.Original[key] = value
		}
	}
	return nil
}

// inet_forward_change also resets all.accept_redirects and every interface's
// forwarding switch. Preserve unrelated settings around that kernel operation.
// LRO is disabled by the kernel while routing; we never force-enable offloads.
func setGatewaySysctls(values map[string]string) (resultErr error) {
	preserved := map[string]string{}
	key := "net/ipv4/conf/all/accept_redirects"
	if value, err := gatewayReadSysctl(key); err != nil {
		return err
	} else {
		preserved[key] = value
	}
	entries, err := os.ReadDir(filepath.Join(gatewaySysctlRoot, "net/ipv4/conf"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "all" {
			continue
		}
		// Changing conf/default can update existing interfaces whose values have
		// never been explicitly set. Preserve those switches too.
		for _, field := range []string{"forwarding", "send_redirects"} {
			key := "net/ipv4/conf/" + entry.Name() + "/" + field
			if _, managed := values[key]; managed {
				continue
			}
			value, err := gatewayReadSysctl(key)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			preserved[key] = value
		}
	}
	write := func(key, value string) error {
		current, err := gatewayReadSysctl(key)
		if err != nil {
			return err
		}
		if current == value {
			return nil
		}
		if err := gatewayWriteSysctl(key, value); err != nil {
			return fmt.Errorf("设置 %s 失败：%w", key, err)
		}
		return nil
	}
	defer func() {
		// Even if a managed write fails halfway through, undo incidental kernel
		// changes to interfaces outside this gateway operation.
		for key, value := range preserved {
			if err := write(key, value); err != nil {
				resultErr = fmt.Errorf("%v；保护原有设置失败：%w", resultErr, err)
			}
		}
	}()
	// Apply global/default switches before interface-specific values because
	// the former may propagate defaults to multiple interfaces in the kernel.
	for _, key := range []string{"net/ipv4/ip_forward", "net/ipv4/conf/default/forwarding", "net/ipv4/conf/default/send_redirects"} {
		if value, exists := values[key]; exists {
			if err := write(key, value); err != nil {
				return err
			}
		}
	}
	for key, value := range values {
		if key == "net/ipv4/ip_forward" || strings.HasPrefix(key, "net/ipv4/conf/default/") {
			continue
		}
		if err := write(key, value); err != nil {
			return err
		}
	}
	return nil
}

func writeGatewaySysctls(state *gatewayState) error {
	if err := setGatewaySysctls(gatewayStateSysctls(state)); err != nil {
		return err
	}
	return checkGatewaySysctls(state)
}

func restoreGatewaySysctls(state *gatewayState) error {
	values := map[string]string{}
	// Persisted AIO forwarding now owns IPv4 forwarding: a router-split host
	// still routes through the full TUN after the gateway record is gone. IPv4
	// checks forwarding per ingress interface, so keep every forwarding switch.
	preserve := aioForwardingConfigManaged()
	for key, expected := range gatewayStateSysctls(state) {
		if preserve && (key == "net/ipv4/ip_forward" || strings.HasSuffix(key, "/forwarding")) {
			continue
		}
		current, err := gatewayReadSysctl(key)
		// The TUN interface may disappear when a disabled configuration is deployed.
		if os.IsNotExist(err) && strings.HasPrefix(key, "net/ipv4/conf/") {
			continue
		}
		if err != nil {
			return err
		}
		original, recorded := state.Original[key]
		if !recorded {
			continue
		}
		if current != expected {
			values[key] = current
		} else {
			values[key] = original
		}
	}
	return setGatewaySysctls(values)
}

func lockGatewayJournal(cfg *Config) (func(), error) {
	path, err := gatewayPath(cfg)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("另一网关操作正在运行：%w", err)
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func gatewayMutateNFT(commands []map[string]any) error {
	data, err := json.Marshal(map[string]any{"nftables": commands})
	if err != nil {
		return err
	}
	if _, err := gatewayRunInput(string(data), "nft", "-c", "-j", "-f", "-"); err != nil {
		return fmt.Errorf("nftables 预校验失败：%w", err)
	}
	_, err = gatewayRunInput(string(data), "nft", "-j", "-f", "-")
	return err
}

func applyLinuxGateway(cfg *Config, spec LinuxGatewaySpec, profileID string) error {
	if err := gatewayPlatform(cfg); err != nil {
		return err
	}
	if err := validateLinuxGatewaySpec(spec); err != nil {
		return err
	}
	if err := checkGatewayConfig(cfg, spec, profileID); err != nil {
		return err
	}
	unlock, err := lockGatewayJournal(cfg)
	if err != nil {
		return err
	}
	defer unlock()
	unlockReturn, err := lockReturnJournal(cfg)
	if err != nil {
		return err
	}
	defer unlockReturn()
	detection, err := detectGatewayNetwork()
	if err != nil {
		return err
	}
	if err := checkGatewayNetwork(spec, detection); err != nil {
		return err
	}
	state, err := loadGatewayState(cfg)
	if err != nil {
		return err
	}
	if state != nil && state.Spec != spec {
		return fmt.Errorf("已应用的网络参数不同；先让客户端恢复主路由并撤销旧网关，再应用新参数")
	}
	if state != nil && state.ProfileID != profileID {
		return fmt.Errorf("已有网关属于另一配置空间，请先撤销旧网关后再应用")
	}
	routing, err := readMihomoRoutingStrict(cfg.ConfigPath)
	if err != nil {
		return err
	}
	if routing.tunDevice == spec.Interface {
		return fmt.Errorf("Mihomo TUN 设备不能与 LAN 网卡同名")
	}
	if state != nil && state.TunDevice != routing.tunDevice {
		return fmt.Errorf("Mihomo TUN 设备名称已变化，请先撤销旧网关后重新应用")
	}
	objects, err := gatewayPreflightResources(cfg, state)
	if err != nil {
		return err
	}
	if state == nil {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		state = &gatewayState{Version: 1, Token: hex.EncodeToString(token[:]), Spec: spec, ProfileID: profileID, TunDevice: routing.tunDevice, Original: map[string]string{}}
		state.NFT = gatewayDesiredNFT(state)
	}
	if state.Original == nil {
		state.Original = map[string]string{}
	}
	if err := recordGatewaySysctls(state); err != nil {
		return err
	}
	state.Pending = true
	if err := saveGatewayState(cfg, state); err != nil {
		return err
	}
	if len(gatewayNFTObjects(objects)) == 0 {
		// A reboot can remove the kernel table while retaining an active
		// journal. This is a fresh creation, not a read-back of pinned objects.
		// Persist that phase before submitting the transaction so an interrupted
		// recreation can use the same narrowly proven pending recovery path.
		state.Active, state.AppliedAt = false, ""
		state.NFT = gatewayDesiredNFT(state)
		if err := saveGatewayState(cfg, state); err != nil {
			return err
		}
		commands := []map[string]any{}
		for _, object := range state.NFT {
			commands = append(commands, map[string]any{"add": object})
		}
		if err := gatewayMutateNFT(commands); err != nil {
			return fmt.Errorf("网关未完成，journal 已保留，可撤销后重试：%w", err)
		}
		objects, err = readGatewayNFT()
		if err != nil {
			return err
		}
	}
	if err := checkGatewayOwnership(state, objects, true); err != nil {
		return err
	}
	// A prior process may have created the table but stopped before recording its
	// kernel handles. Successful retries must persist those identities too.
	state.NFT = gatewayNFTObjects(objects)
	if err := saveGatewayState(cfg, state); err != nil {
		return err
	}
	if err := writeGatewaySysctls(state); err != nil {
		return fmt.Errorf("网关部分应用，journal 已保留，请撤销或重试：%w", err)
	}
	state.Active, state.Pending, state.AppliedAt = true, false, gatewayNow()
	return saveGatewayState(cfg, state)
}

func removeLinuxGateway(cfg *Config) error {
	if err := gatewayPlatform(cfg); err != nil {
		return err
	}
	unlock, err := lockGatewayJournal(cfg)
	if err != nil {
		return err
	}
	defer unlock()
	unlockReturn, err := lockReturnJournal(cfg)
	if err != nil {
		return err
	}
	defer unlockReturn()
	state, err := loadGatewayState(cfg)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}
	objects, err := readGatewayNFT()
	if err != nil {
		return err
	}
	if err := checkGatewayOwnership(state, objects, false); err != nil {
		return err
	}
	state.Pending = true
	if err := saveGatewayState(cfg, state); err != nil {
		return err
	}
	if len(gatewayNFTObjects(objects)) != 0 {
		// Remove by the verified table handle, never merely by a familiar name.
		var table map[string]any
		for _, object := range gatewayNFTObjects(objects) {
			if body, ok := object["table"].(map[string]any); ok {
				table = body
				break
			}
		}
		if table == nil || table["handle"] == nil {
			return fmt.Errorf("受管 nftables 表缺少可验证 handle，拒绝删除")
		}
		if err := gatewayMutateNFT([]map[string]any{{"delete": nftObject("table", map[string]any{"family": "ip", "handle": table["handle"]})}}); err != nil {
			return err
		}
		remaining, err := readGatewayNFT()
		if err != nil {
			return err
		}
		if len(gatewayNFTObjects(remaining)) != 0 {
			return fmt.Errorf("网关 nftables 表删除后仍存在，保留 journal 以便排查")
		}
	}
	if err := restoreGatewaySysctls(state); err != nil {
		return err
	}
	path, _ := gatewayPath(cfg)
	return os.Remove(path)
}
