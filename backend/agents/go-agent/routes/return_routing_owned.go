package routes

// Safe return routing deliberately has no table flush, route replace, legacy
// adoption, or priority-only deletion. An unprovable resource is left alone.
import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const returnProtocol = 242

func validReturnOwner(state *returnRoutingState) bool {
	if state == nil || state.Ownership == nil || state.Ownership.Version != 1 ||
		len(state.Ownership.Token) != 32 || state.Ownership.Metric <= 0 {
		return false
	}
	owner := state.Ownership
	if _, err := hex.DecodeString(owner.Token); err != nil {
		return false
	}
	for _, route := range owner.Routes {
		proto, pOK := resourceNumber(route["protocol"])
		metric, mOK := resourceNumber(route["metric"])
		if !pOK || !mOK || proto != returnProtocol || metric != uint64(owner.Metric) {
			return false
		}
	}
	for _, rule := range owner.Rules {
		proto, ok := resourceNumber(rule["protocol"])
		if !ok || proto != returnProtocol {
			return false
		}
	}
	for _, object := range owner.NFT {
		if len(object) != 1 || !nftObjectIsManaged(object) {
			return false
		}
		for kind, raw := range object {
			body, ok := raw.(map[string]interface{})
			if !ok {
				return false
			}
			comment, hasComment := body["comment"].(string)
			if kind == "table" || kind == "chain" {
				// Agents before 1.9.1 tagged tables and chains; nft 1.0.6 never
				// lists those comments, so they are optional evidence only.
				if hasComment && comment != ownerTag(owner) {
					return false
				}
			} else if kind == "rule" {
				if !strings.HasPrefix(comment, ownerTag(owner)+":") {
					return false
				}
			} else {
				return false
			}
			if kind == "chain" {
				name, _ := body["name"].(string)
				if !strings.HasPrefix(name, "cf_"+owner.Token+"_") {
					return false
				}
			}
			if kind == "rule" {
				chain, _ := body["chain"].(string)
				if !strings.HasPrefix(chain, "cf_"+owner.Token+"_") {
					return false
				}
			}
		}
	}
	return true
}

// Agent restarts and multiple service processes must not race the same journal.
func lockReturnJournal(cfg *Config) (func(), error) {
	path, err := returnStatePath(cfg)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("另一回流操作正在运行，拒绝并发修改：%w", err)
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func returnSafetyStatus(cfg *Config) map[string]interface{} {
	blockers := []string{}
	state, err := loadReturnState(cfg)
	var allocation returnAllocation
	if err == nil {
		var resources *returnResources
		resources, err = readReturnResources()
		if err == nil {
			var routing mihomoRouting
			routing, err = readMihomoRoutingStrict(cfg.ConfigPath)
			if err == nil {
				allocation, err = selectReturnAllocation(state, resources, routing)
			}
		}
	}
	if err != nil {
		blockers = append(blockers, err.Error())
	}
	status := map[string]interface{}{"version": 1, "protocols": returnProtocols, "ok": len(blockers) == 0, "blockers": blockers}
	if err == nil {
		status["allocation"] = allocation
	}
	return status
}

func ownerTag(owner *returnOwnership) string { return "configflow-return:" + owner.Token }

func newReturnOwner() (*returnOwnership, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	return &returnOwnership{Version: 1, Token: hex.EncodeToString(token[:]),
		Metric: int(binary.BigEndian.Uint32(token[:4])&0x3fffffff) + 100000,
		Routes: []map[string]interface{}{}, Rules: []map[string]interface{}{},
		NFT: []map[string]interface{}{}, Pending: true}, nil
}

func nftObject(kind string, body map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{kind: body}
}

func markExpr(kind string) map[string]interface{} {
	return map[string]interface{}{kind: map[string]interface{}{"key": "mark"}}
}

func nftMatch(left, right interface{}, op string) map[string]interface{} {
	return map[string]interface{}{"match": map[string]interface{}{"left": left, "right": right, "op": op}}
}

// Anonymous address sets live inside the owned rule. No shared named set is
// flushed or rewritten, and removing a rule releases only its anonymous set.
//
// Ownership evidence must survive every nft release's JSON listing: chains
// carry the journal token in their names and rules carry it in their comments.
// Tables and chains get no comment, because nft 1.0.6 (Debian 12) omits
// table/chain comments from "nft -j list" and the readback could never match.
func desiredReturnNFT(spec resolvedSpec, owner *returnOwnership) []map[string]interface{} {
	tag := ownerTag(owner)
	pre, output := "cf_"+owner.Token+"_pre", "cf_"+owner.Token+"_out"
	objects := []map[string]interface{}{
		nftObject("table", map[string]interface{}{"family": "inet", "name": managedReturnTable}),
		nftObject("chain", map[string]interface{}{"family": "inet", "table": managedReturnTable, "name": pre,
			"type": "filter", "hook": "prerouting", "prio": -150, "policy": "accept"}),
		nftObject("chain", map[string]interface{}{"family": "inet", "table": managedReturnTable, "name": output,
			"type": "route", "hook": "output", "prio": -150, "policy": "accept"}),
	}
	addresses := []interface{}{}
	for _, cidr := range spec.ProxyDst {
		ip, network, _ := net.ParseCIDR(cidr)
		ones, _ := network.Mask.Size()
		if ones == 32 {
			addresses = append(addresses, ip.String())
		} else {
			addresses = append(addresses, map[string]interface{}{"prefix": map[string]interface{}{"addr": network.IP.String(), "len": ones}})
		}
	}
	meta := func(key string) interface{} {
		return map[string]interface{}{"meta": map[string]interface{}{"key": key}}
	}
	payload := func(protocol, field string) interface{} {
		return map[string]interface{}{"payload": map[string]interface{}{"protocol": protocol, "field": field}}
	}
	setMark := func(kind string) interface{} {
		return map[string]interface{}{"mangle": map[string]interface{}{"key": markExpr(kind),
			"value": map[string]interface{}{"|": []interface{}{markExpr(kind), spec.mark}}}}
	}
	addRule := func(chain, suffix string, expr []interface{}) {
		objects = append(objects, nftObject("rule", map[string]interface{}{"family": "inet", "table": managedReturnTable,
			"chain": chain, "expr": expr, "comment": tag + ":" + suffix}))
	}
	incoming := []interface{}{
		nftMatch(meta("iifname"), spec.LanInterface, "=="),
		nftMatch(payload("ether", "saddr"), spec.routerMAC, "=="),
	}
	if spec.SafetyVersion == returnProtocolByDestination {
		incoming = append(incoming, nftMatch(payload("ip", "daddr"), map[string]interface{}{"set": addresses}, "=="))
	} else {
		// Every connection the router forwards here gets its replies back through
		// the router, whatever the destination; IPv4 only, like the routes.
		incoming = append(incoming, nftMatch(meta("nfproto"), "ipv4", "=="))
	}
	addRule(pre, "incoming", append(incoming,
		nftMatch(map[string]interface{}{"ct": map[string]interface{}{"key": "state"}}, "new", "in"),
		setMark("ct"),
	))
	marked := func() interface{} {
		return nftMatch(map[string]interface{}{"&": []interface{}{markExpr("ct"), spec.mark}}, spec.mark, "==")
	}
	if spec.TunDevice != "" {
		addRule(pre, "tun", []interface{}{nftMatch(meta("iifname"), spec.TunDevice, "=="), marked(), setMark("meta")})
	}
	addRule(output, "local", []interface{}{marked(), setMark("meta")})
	return objects
}

func runNFT(commands []map[string]interface{}) error {
	if len(commands) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string]interface{}{"nftables": commands})
	if err != nil {
		return err
	}
	_, err = runCommandInput(string(data), "nft", "-j", "-f", "-")
	return err
}

func checkedResources(spec resolvedSpec, state *returnRoutingState) (*returnResources, error) {
	resources, err := readReturnResources()
	if err != nil {
		return nil, err
	}
	if err = checkReturnResources(spec, state, resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func matchingResource(expected map[string]interface{}, rows []map[string]interface{}) (map[string]interface{}, error) {
	var found map[string]interface{}
	for _, row := range rows {
		if normalizedResourceEqual(withoutContainerComment(expected), withoutContainerComment(row)) {
			if found != nil {
				return nil, fmt.Errorf("发现重复资源，无法确认唯一归属")
			}
			found = row
		}
	}
	return found, nil
}

func confirmResources(expected, actual []map[string]interface{}) ([]map[string]interface{}, error) {
	if len(expected) != len(actual) {
		return nil, fmt.Errorf("写入后的资源数量与计划不一致")
	}
	confirmed := []map[string]interface{}{}
	for _, row := range expected {
		match, err := matchingResource(row, actual)
		if err != nil {
			return nil, err
		}
		if match == nil {
			return nil, fmt.Errorf("写入后的资源内容与计划不一致")
		}
		confirmed = append(confirmed, match)
	}
	return confirmed, nil
}

func routeArguments(action string, row map[string]interface{}) ([]string, error) {
	text := func(key string) string { value, _ := row[key].(string); return value }
	table, tableOK := resourceNumber(row["table"])
	proto, protoOK := resourceNumber(row["protocol"])
	metric, metricOK := resourceNumber(row["metric"])
	if !tableOK || !protoOK || !metricOK || proto != returnProtocol || text("dst") == "" || text("gateway") == "" || text("dev") == "" {
		return nil, fmt.Errorf("路由归属记录不完整，拒绝修改")
	}
	return []string{"-4", "route", action, text("dst"), "via", text("gateway"), "dev", text("dev"),
		"table", strconv.FormatUint(table, 10), "proto", strconv.FormatUint(proto, 10), "metric", strconv.FormatUint(metric, 10)}, nil
}

func ruleArguments(action string, row map[string]interface{}) ([]string, error) {
	priority, pOK := resourceNumber(row["priority"])
	table, tOK := resourceNumber(row["table"])
	mark, mOK := resourceNumber(row["fwmark"])
	mask, maskOK := resourceNumber(row["fwmask"])
	proto, protoOK := resourceNumber(row["protocol"])
	if !pOK || !tOK || !mOK || !maskOK || !protoOK || proto != returnProtocol || row["src"] != "all" {
		return nil, fmt.Errorf("策略规则归属记录不完整，拒绝修改")
	}
	return []string{"-4", "rule", action, "priority", strconv.FormatUint(priority, 10), "from", "all", "fwmark",
		fmt.Sprintf("0x%x/0x%x", mark, mask), "lookup", strconv.FormatUint(table, 10), "protocol", strconv.FormatUint(proto, 10)}, nil
}

// Removing only recorded handles, then empty chains, is safe even if another
// writer inserts a rule after our read: the kernel rejects deleting a nonempty
// chain and the nft transaction aborts. The shared table is never deleted.
func removeOwnedReturn(cfg *Config, spec resolvedSpec, state *returnRoutingState) error {
	resources, err := checkedResources(spec, state)
	if err != nil {
		return err
	}
	owner := state.Ownership
	commands := []map[string]interface{}{}
	tables := []map[string]interface{}{}
	for _, kind := range []string{"rule", "chain", "table"} {
		for _, object := range owner.NFT {
			body, ok := object[kind].(map[string]interface{})
			if !ok {
				continue
			}
			current, err := matchingResource(object, ownedNftObjects(resources))
			if err != nil {
				return err
			}
			if current == nil {
				continue
			}
			actual := current[kind].(map[string]interface{})
			if kind == "table" {
				tables = append(tables, current)
				continue
			}
			oldHandle, a := resourceNumber(body["handle"])
			handle, b := resourceNumber(actual["handle"])
			if !a || !b || oldHandle != handle {
				return fmt.Errorf("nft 资源标识已变化，拒绝删除")
			}
			selector := map[string]interface{}{"family": "inet", "table": managedReturnTable}
			if kind == "rule" {
				selector["chain"] = body["chain"]
				selector["handle"] = handle
			} else {
				selector["handle"] = handle
			}
			commands = append(commands, map[string]interface{}{"delete": nftObject(kind, selector)})
		}
	}
	if err = runNFT(commands); err != nil {
		return fmt.Errorf("精确移除 nft 规则失败，保留归属记录：%w", err)
	}
	resources, err = checkedResources(spec, state)
	if err != nil {
		return err
	}
	for _, object := range owner.NFT {
		if _, isTable := object["table"]; isTable {
			continue
		}
		remaining, matchErr := matchingResource(object, ownedNftObjects(resources))
		if matchErr != nil {
			return matchErr
		}
		if remaining != nil {
			return fmt.Errorf("nft 删除未通过回读，保留归属记录")
		}
	}
	owner.NFT = tables
	if err = saveReturnState(cfg, state); err != nil {
		return err
	}
	for len(owner.Rules) > 0 {
		resources, err = checkedResources(spec, state)
		if err != nil {
			return err
		}
		row := owner.Rules[len(owner.Rules)-1]
		current, matchErr := matchingResource(row, resources.Rules)
		if matchErr != nil {
			return matchErr
		}
		if current != nil {
			args, argErr := ruleArguments("del", row)
			if argErr != nil {
				return argErr
			}
			if _, err = runCommand("ip", args...); err != nil {
				return err
			}
			after, readErr := checkedResources(spec, state)
			if readErr != nil {
				return readErr
			}
			remaining, readErr := matchingResource(row, after.Rules)
			if readErr != nil {
				return readErr
			}
			if remaining != nil {
				return fmt.Errorf("策略规则删除未通过回读，保留归属记录")
			}
		}
		owner.Rules = owner.Rules[:len(owner.Rules)-1]
		if err = saveReturnState(cfg, state); err != nil {
			return err
		}
	}
	for len(owner.Routes) > 0 {
		resources, err = checkedResources(spec, state)
		if err != nil {
			return err
		}
		row := owner.Routes[len(owner.Routes)-1]
		current, matchErr := matchingResource(row, resources.Routes)
		if matchErr != nil {
			return matchErr
		}
		if current != nil {
			args, argErr := routeArguments("del", row)
			if argErr != nil {
				return argErr
			}
			if _, err = runCommand("ip", args...); err != nil {
				return err
			}
			after, readErr := checkedResources(spec, state)
			if readErr != nil {
				return readErr
			}
			remaining, readErr := matchingResource(row, after.Routes)
			if readErr != nil {
				return readErr
			}
			if remaining != nil {
				return fmt.Errorf("路由删除未通过回读，保留归属记录")
			}
		}
		owner.Routes = owner.Routes[:len(owner.Routes)-1]
		if err = saveReturnState(cfg, state); err != nil {
			return err
		}
	}
	_, err = checkedResources(spec, state)
	return err
}

// ownedRouteExpectation and ownedRuleExpectation are the exact rows written
// for spec; readback and replay comparisons use the same shapes.
func ownedRouteExpectation(spec resolvedSpec, owner *returnOwnership, dst string) map[string]interface{} {
	return map[string]interface{}{"dst": dst, "gateway": spec.RouterIP, "dev": spec.LanInterface,
		"table": spec.Table, "protocol": returnProtocol, "metric": owner.Metric, "flags": []interface{}{}}
}

func ownedRuleExpectation(spec resolvedSpec) map[string]interface{} {
	return map[string]interface{}{"priority": spec.Priority, "src": "all", "fwmark": fmt.Sprintf("0x%x", spec.mark),
		"fwmask": fmt.Sprintf("0x%x", spec.mark), "table": spec.Table, "protocol": returnProtocol}
}

// ownedMatchesSpec reports whether the recorded resources are exactly what
// spec renders today. Interfaces, LAN subnet and router MAC are resolved at
// replay time, so a change there must still trigger a real replay.
func ownedMatchesSpec(spec resolvedSpec, owner *returnOwnership) bool {
	expected := []struct {
		rows []map[string]interface{}
		want []map[string]interface{}
	}{
		{owner.Routes, []map[string]interface{}{ownedRouteExpectation(spec, owner, "default"),
			ownedRouteExpectation(spec, owner, spec.lanCIDR)}},
		{owner.Rules, []map[string]interface{}{ownedRuleExpectation(spec)}},
		{owner.NFT, desiredReturnNFT(spec, owner)},
	}
	for _, group := range expected {
		if len(group.rows) != len(group.want) {
			return false
		}
		for _, want := range group.want {
			if match, err := matchingResource(want, group.rows); err != nil || match == nil {
				return false
			}
		}
	}
	return true
}

func addOwnedReturn(cfg *Config, spec resolvedSpec, state *returnRoutingState) error {
	owner := state.Ownership
	for _, dst := range []string{"default", spec.lanCIDR} {
		if _, err := checkedResources(spec, state); err != nil {
			return err
		}
		expected := ownedRouteExpectation(spec, owner, dst)
		args, err := routeArguments("add", expected)
		if err != nil {
			return err
		}
		if _, err = runCommand("ip", args...); err != nil {
			return err
		}
		resources, err := readReturnResources()
		if err != nil {
			return err
		}
		match, err := matchingResource(expected, resources.Routes)
		if err != nil {
			return err
		}
		if match == nil {
			return fmt.Errorf("路由写入后无法确认归属")
		}
		owner.Routes = append(owner.Routes, match)
		if err = saveReturnState(cfg, state); err != nil {
			return err
		}
		if err = checkReturnResources(spec, state, resources); err != nil {
			return err
		}
	}
	if _, err := checkedResources(spec, state); err != nil {
		return err
	}
	expected := ownedRuleExpectation(spec)
	args, err := ruleArguments("add", expected)
	if err != nil {
		return err
	}
	if _, err = runCommand("ip", args...); err != nil {
		return err
	}
	resources, err := readReturnResources()
	if err != nil {
		return err
	}
	match, err := matchingResource(expected, resources.Rules)
	if err != nil {
		return err
	}
	if match == nil {
		return fmt.Errorf("策略规则写入后无法确认唯一归属")
	}
	owner.Rules = append(owner.Rules, match)
	if err = saveReturnState(cfg, state); err != nil {
		return err
	}
	if err = checkReturnResources(spec, state, resources); err != nil {
		return err
	}
	objects := desiredReturnNFT(spec, owner)
	commands := []map[string]interface{}{}
	for _, object := range objects {
		if _, isTable := object["table"]; isTable && len(owner.NFT) > 0 {
			continue
		}
		verb := "create"
		if _, isRule := object["rule"]; isRule {
			verb = "add"
		}
		commands = append(commands, map[string]interface{}{verb: object})
	}
	if _, err = checkedResources(spec, state); err != nil {
		return err
	}
	if err = runNFT(commands); err != nil {
		return err
	}
	resources, err = readReturnResources()
	if err != nil {
		return err
	}
	confirmed, err := confirmResources(objects, ownedNftObjects(resources))
	if err != nil {
		return err
	}
	owner.NFT = confirmed
	if err = saveReturnState(cfg, state); err != nil {
		return err
	}
	if err = checkReturnResources(spec, state, resources); err != nil {
		return err
	}
	if !ownedManifestComplete(owner, resources) {
		return fmt.Errorf("回流写入后的最终回读不完整，不能确认发布成功")
	}
	return nil
}

func cloneReturnState(state *returnRoutingState) *returnRoutingState {
	if state == nil {
		return nil
	}
	data, _ := json.Marshal(state)
	var clone returnRoutingState
	_ = json.Unmarshal(data, &clone)
	return &clone
}

// ApplyReturnRouting is called under returnRoutingMu by HTTP and boot replay.
func ApplyReturnRouting(cfg *Config, input ReturnRoutingSpec) (map[string]interface{}, error) {
	unlock, err := lockReturnJournal(cfg)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if gateway, err := loadGatewayState(cfg); err != nil {
		return nil, err
	} else if gateway != nil && (gateway.Active || gateway.Pending) {
		return nil, fmt.Errorf("Linux 网关已启用或尚未完成，请先撤销网关转发再配置自动回流")
	}
	spec, err := prepareSpec(cfg, input)
	if err != nil {
		return nil, err
	}
	state, err := loadReturnState(cfg)
	if err != nil {
		return nil, err
	}
	if state != nil && (!validReturnOwner(state) || state.Legacy != nil) {
		return nil, fmt.Errorf("已有回流状态缺少有效归属或事务未完成，保留现有规则，请先人工核对")
	}
	if state != nil && state.Ownership.Pending {
		if err = recoverPendingReturn(cfg, state); err != nil {
			return nil, err
		}
	}
	resources, err := checkedResources(spec, state)
	if err != nil {
		return nil, err
	}
	routing, err := readMihomoRoutingStrict(cfg.ConfigPath)
	if err != nil {
		return nil, err
	}
	allocation, err := selectReturnAllocation(state, resources, routing)
	if err != nil {
		return nil, err
	}
	if allocation.Table != spec.Table || allocation.Priority != spec.Priority || allocation.Mark != fmt.Sprintf("0x%x", spec.mark) {
		return nil, fmt.Errorf("回流资源分配已变化，请重新预览后发布（表 %d，优先级 %d，标记 %s）", allocation.Table, allocation.Priority, allocation.Mark)
	}
	previous := cloneReturnState(state)
	if state == nil {
		owner, ownerErr := newReturnOwner()
		if ownerErr != nil {
			return nil, ownerErr
		}
		state = &returnRoutingState{Spec: spec.ReturnRoutingSpec, Ownership: owner}
	}
	state.Ownership.Pending = true
	if err = saveReturnState(cfg, state); err != nil {
		return nil, err
	}
	oldSpec := spec
	if previous != nil {
		oldSpec, err = validateSpec(previous.Spec)
		if err != nil {
			return nil, err
		}
	}
	if err = removeOwnedReturn(cfg, oldSpec, state); err == nil {
		state.Spec = spec.ReturnRoutingSpec
		err = addOwnedReturn(cfg, spec, state)
	}
	if err != nil {
		cause := err
		if cleanupErr := removeOwnedReturn(cfg, spec, state); cleanupErr != nil {
			return nil, fmt.Errorf("回流应用失败：%v；安全撤销未完成，未删除无法确认归属的资源：%v", cause, cleanupErr)
		}
		if previous != nil && previous.Active {
			restoreSpec, restoreErr := prepareSpec(cfg, previous.Spec)
			if restoreErr == nil {
				state.Spec = previous.Spec
				restoreErr = addOwnedReturn(cfg, restoreSpec, state)
			}
			if restoreErr != nil {
				return nil, fmt.Errorf("回流应用失败：%v；恢复原受管配置失败：%v", cause, restoreErr)
			}
		}
		state.Active = previous != nil && previous.Active
		state.Ownership.Pending = false
		if saveErr := saveReturnState(cfg, state); saveErr != nil {
			return nil, fmt.Errorf("%v；保存撤销状态失败：%v", cause, saveErr)
		}
		return nil, cause
	}
	state.Active = true
	state.Ownership.Pending = false
	state.AppliedAt = time.Now().Format(time.RFC3339)
	disableSendRedirects(state, spec.LanInterface)
	if err = saveReturnState(cfg, state); err != nil {
		return nil, fmt.Errorf("回流状态保存失败，保留事务记录：%w", err)
	}
	return map[string]interface{}{"success": true, "safety_version": spec.SafetyVersion, "lan_interface": spec.LanInterface,
		"lan_cidr": spec.lanCIDR, "router_mac": spec.routerMAC, "tun_device": spec.TunDevice}, nil
}

func RemoveReturnRouting(cfg *Config, restoreLegacy bool) (map[string]interface{}, error) {
	if restoreLegacy {
		return nil, fmt.Errorf("禁止自动恢复或执行手工回流脚本")
	}
	unlock, err := lockReturnJournal(cfg)
	if err != nil {
		return nil, err
	}
	defer unlock()
	state, err := loadReturnState(cfg)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return map[string]interface{}{"success": true, "removed": false}, nil
	}
	if !validReturnOwner(state) || state.Legacy != nil {
		return nil, fmt.Errorf("无法证明旧回流资源的归属，已保留全部现有规则")
	}
	if state.Ownership.Pending {
		if err = recoverPendingReturn(cfg, state); err != nil {
			return nil, err
		}
	}
	spec, err := validateSpec(state.Spec)
	if err != nil {
		return nil, err
	}
	if _, err = checkedResources(spec, state); err != nil {
		return nil, err
	}
	state.Ownership.Pending = true
	if err = saveReturnState(cfg, state); err != nil {
		return nil, err
	}
	if err = removeOwnedReturn(cfg, spec, state); err != nil {
		return nil, err
	}
	state.Active, state.Ownership.Pending = false, false
	restoreSendRedirects(state)
	if err = saveReturnState(cfg, state); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "removed": true, "retained_empty_table": true}, nil
}

func ReapplyReturnRouting(cfg *Config) {
	go reapplyReturnRouting(cfg)
}

func reapplyReturnRouting(cfg *Config) {
	returnRoutingMu.Lock()
	defer returnRoutingMu.Unlock()
	state, err := loadReturnState(cfg)
	if err != nil {
		log.Printf("return routing: %v", err)
		return
	}
	if state == nil {
		return
	}
	if state.Ownership == nil {
		// Familiar names, comments, and matching content cannot establish ownership.
		// Keep legacy resources and the original journal untouched at boot.
		log.Printf("return routing: preserved legacy resources; ownership is missing, automatic migration is disabled")
		return
	}
	if !validReturnOwner(state) || state.Legacy != nil || state.Ownership.Pending {
		log.Printf("return routing: preserved existing resources; ownership is missing or incomplete")
		return
	}
	if !state.Active {
		return
	}
	// An Agent restart (upgrade, crash) usually finds everything still in place;
	// replaying would remove and re-add it, briefly dropping return routing.
	// Only a reboot or drift needs the real replay below.
	if returnResourcesComplete(state) {
		if spec, specErr := prepareSpec(cfg, state.Spec); specErr == nil && ownedMatchesSpec(spec, state.Ownership) {
			log.Printf("return routing: owned resources complete and current, replay skipped")
			ensureSendRedirects(cfg, spec.LanInterface)
			return
		}
	}
	if _, err = ApplyReturnRouting(cfg, state.Spec); err != nil {
		log.Printf("return routing: safe replay refused: %v", err)
	}
}

// Missing owned resources can be recreated, but must never appear healthy in
// preview or readback. Any change to a surviving resource blocks the write.
func returnResourcesComplete(state *returnRoutingState) bool {
	if !state.Active || !validReturnOwner(state) || state.Legacy != nil || state.Ownership.Pending {
		return false
	}
	owner := state.Ownership
	if len(owner.Routes) != 2 || len(owner.Rules) != 1 || len(owner.NFT) < 5 {
		return false
	}
	resources, err := readReturnResources()
	if err != nil {
		return false
	}
	spec, err := validateSpec(state.Spec)
	if err != nil || checkReturnResources(spec, state, resources) != nil {
		return false
	}
	return ownedManifestComplete(owner, resources)
}

func ownedManifestComplete(owner *returnOwnership, resources *returnResources) bool {
	for _, group := range []struct{ expected, actual []map[string]interface{} }{
		{owner.Routes, resources.Routes}, {owner.Rules, resources.Rules}, {owner.NFT, ownedNftObjects(resources)},
	} {
		for _, row := range group.expected {
			found, err := matchingResource(row, group.actual)
			if err != nil || found == nil {
				return false
			}
		}
	}
	return true
}
