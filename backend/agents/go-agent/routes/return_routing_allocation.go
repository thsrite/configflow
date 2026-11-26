package routes

import "fmt"

// Allocation only inspects a complete readReturnResources snapshot. It neither
// claims resources nor updates the journal; Apply must recheck before writing.
type returnAllocation struct {
	Version  int    `json:"version"`
	Table    int    `json:"table"`
	Priority int    `json:"priority"`
	Mark     string `json:"mark"`
	Reused   bool   `json:"reused"`
}

func selectReturnAllocation(state *returnRoutingState, resources *returnResources, routing mihomoRouting) (returnAllocation, error) {
	var allocation returnAllocation
	if resources == nil {
		return allocation, fmt.Errorf("缺少完整回流资源读取结果，无法分配")
	}
	if state != nil {
		// Never escape a changed or unfinished owned allocation by selecting
		// another one: that would leave the old resources behind unexamined.
		if state.Legacy != nil || !validReturnOwner(state) {
			return allocation, fmt.Errorf("已有回流状态缺少可验证归属，拒绝重新分配或自动接管")
		}
		if state.Ownership.Pending {
			// Preview only: Apply records the same token-proven objects before
			// it replays the transaction.
			manifest, err := pendingNFTManifest(state.Ownership, resources)
			if err != nil {
				return allocation, fmt.Errorf("上次回流事务未完成，不能重新分配资源：%w", err)
			}
			state = cloneReturnState(state)
			state.Ownership.NFT = manifest
			if !validReturnOwner(state) {
				return allocation, fmt.Errorf("上次回流事务未完成，不能重新分配资源")
			}
		}
		spec, err := validateSpec(state.Spec)
		if err != nil {
			return allocation, fmt.Errorf("已有回流资源记录无效：%w", err)
		}
		if err = checkMihomoConflicts(spec, routing); err != nil {
			return allocation, err
		}
		// Missing owned objects can be rebuilt, but changed or additional
		// objects (including changed recorded nft handles) must block reuse.
		if err = checkReturnResources(spec, state, resources); err != nil {
			return allocation, err
		}
		return returnAllocation{Version: 1, Table: spec.Table, Priority: spec.Priority,
			Mark: fmt.Sprintf("0x%x", spec.mark), Reused: true}, nil
	}
	if len(ownedNftObjects(resources)) != 0 {
		return allocation, fmt.Errorf("nft 表 %s 已存在且没有归属记录，拒绝自动接管", managedReturnTable)
	}

	tables, priorities := map[uint64]bool{}, map[uint64]bool{}
	if routing.tableIndex > 0 {
		tables[uint64(routing.tableIndex)] = true
	}
	var occupiedMarkBits uint32
	for _, mark := range routing.marks {
		occupiedMarkBits |= mark
	}
	for _, route := range resources.Routes {
		if dst, ok := route["dst"].(string); !ok || dst == "" {
			return allocation, fmt.Errorf("路由记录缺少目的地址，无法分配")
		}
		table, ok := resourceTable(route)
		if !ok {
			return allocation, fmt.Errorf("路由表标识无法解析，无法分配")
		}
		tables[table] = true
	}
	for _, vrf := range resources.VRFs {
		table, ok := vrfTable(vrf)
		if !ok {
			return allocation, fmt.Errorf("VRF 占用信息不完整，无法分配")
		}
		tables[table] = true
	}
	for _, rule := range resources.Rules {
		priority, ok := resourceNumber(rule["priority"])
		if !ok {
			return allocation, fmt.Errorf("策略规则 priority 无效，无法分配")
		}
		priorities[priority] = true
		if target, exists, err := ruleGoto(rule); err != nil {
			return allocation, err
		} else if exists {
			priorities[target] = true
		}
		if raw, exists := rule["table"]; exists {
			table, ok := resourceNumber(raw)
			if !ok {
				return allocation, fmt.Errorf("策略规则 table 无法解析，无法分配")
			}
			tables[table] = true
		}
		if _, exists := rule["fwmark"]; exists {
			_, mask, err := ruleMark(rule)
			if err != nil {
				return allocation, err
			}
			// A zero-valued mark still reserves every bit in its selector mask.
			occupiedMarkBits |= mask
		}
	}

	table := firstFreeReturnNumber(252, tables)
	if table == 0 {
		return allocation, fmt.Errorf("没有可安全分配的回流路由表（1-252）")
	}
	priorityLimit := 32765
	if routing.ruleIndex <= priorityLimit {
		priorityLimit = routing.ruleIndex - 1
	}
	priority := firstFreeReturnNumber(priorityLimit, priorities)
	if priority == 0 {
		return allocation, fmt.Errorf("没有位于 mihomo 之前的空闲回流策略优先级")
	}
	var mark uint32
	// Preserve the historical bit first, then try higher bits, then lower.
	for offset := 0; offset < 32; offset++ {
		bit := (22 + offset) % 32
		candidate := uint32(1) << uint(bit)
		if occupiedMarkBits&candidate != 0 {
			continue
		}
		conflict := false
		for _, object := range resources.NFT {
			if nftMarkConflict(object, candidate) {
				conflict = true
				break
			}
		}
		if !conflict {
			mark = candidate
			break
		}
	}
	if mark == 0 {
		return allocation, fmt.Errorf("没有可证明空闲的单 bit 回流 mark，现有规则可能使用全部标记位")
	}
	spec := resolvedSpec{ReturnRoutingSpec: ReturnRoutingSpec{SafetyVersion: 1,
		Table: table, Priority: priority, Mark: fmt.Sprintf("0x%x", mark)}, mark: mark}
	if err := checkMihomoConflicts(spec, routing); err != nil {
		return allocation, err
	}
	if err := checkReturnResources(spec, nil, resources); err != nil {
		return allocation, err
	}
	return returnAllocation{Version: 1, Table: table, Priority: priority,
		Mark: spec.Mark, Reused: false}, nil
}

// Prefer 104, then larger numbers, then 1..103. Each dimension is selected
// independently; no table/priority/mark Cartesian search is necessary.
func firstFreeReturnNumber(limit int, occupied map[uint64]bool) int {
	for candidate := 104; candidate <= limit; candidate++ {
		if !occupied[uint64(candidate)] {
			return candidate
		}
	}
	for candidate := 1; candidate <= 103 && candidate <= limit; candidate++ {
		if !occupied[uint64(candidate)] {
			return candidate
		}
	}
	return 0
}
