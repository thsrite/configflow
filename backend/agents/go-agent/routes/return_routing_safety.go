package routes

// This file only observes resources and checks ownership. A successful command
// or a familiar table name is never, by itself, evidence of ownership.
import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type returnResources struct {
	Routes []map[string]any
	Rules  []map[string]any
	NFT    []map[string]any
	VRFs   []map[string]any
}

func resourceNumber(value any) (uint64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := strconv.ParseUint(string(v), 10, 64)
		return n, err == nil
	case string:
		switch v {
		case "unspec":
			return 0, true
		case "default":
			return 253, true
		case "main":
			return 254, true
		case "local":
			return 255, true
		}
		base := 10
		if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
			base, v = 16, v[2:]
		}
		n, err := strconv.ParseUint(v, base, 64)
		return n, err == nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= math.Exp2(64) || math.Trunc(v) != v {
			return 0, false
		}
		return uint64(v), true
	case float32:
		return resourceNumber(float64(v))
	}
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return 0, false
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() >= 0 {
			return uint64(v.Int()), true
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), true
	}
	return 0, false
}

func decodeResourceJSON(raw string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON")
	}
	return value, nil
}

func resourceObjects(value any, name string) ([]map[string]any, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s 必须返回 JSON 数组", name)
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || len(object) == 0 {
			return nil, fmt.Errorf("%s 包含不完整记录", name)
		}
		result = append(result, object)
	}
	return result, nil
}

func resourceTable(row map[string]any) (uint64, bool) {
	value, exists := row["table"]
	if !exists {
		return 254, true
	} // iproute2 omits the default main table.
	return resourceNumber(value)
}

func readReturnResources() (*returnResources, error) {
	if err := returnRoutingTools(); err != nil {
		return nil, err
	}
	result := &returnResources{}
	reads := []struct {
		name string
		args []string
		dest *[]map[string]any
	}{
		{"ip", []string{"-j", "-N", "-4", "route", "show", "table", "all"}, &result.Routes},
		{"ip", []string{"-j", "-N", "-4", "rule", "show"}, &result.Rules},
		{"nft", []string{"-j", "list", "ruleset"}, &result.NFT},
		{"ip", []string{"-j", "-d", "link", "show"}, &result.VRFs},
	}
	for _, read := range reads {
		label := read.name + " " + strings.Join(read.args, " ")
		raw, err := runCommand(read.name, read.args...)
		if err != nil {
			return nil, fmt.Errorf("无法确认回流资源占用（%s）：%w", label, err)
		}
		value, err := decodeResourceJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("回流资源读取格式无效（%s）：%w", label, err)
		}
		if read.name == "nft" {
			doc, ok := value.(map[string]any)
			if !ok || len(doc) != 1 {
				return nil, fmt.Errorf("nft ruleset 返回结构不完整")
			}
			value, ok = doc["nftables"]
			if !ok {
				return nil, fmt.Errorf("nft ruleset 缺少 nftables")
			}
		}
		objects, err := resourceObjects(value, label)
		if err != nil {
			return nil, err
		}
		*read.dest = objects
	}
	for _, route := range result.Routes {
		if dst, ok := route["dst"].(string); !ok || dst == "" {
			return nil, fmt.Errorf("路由记录缺少目的地址")
		}
		if _, ok := resourceTable(route); !ok {
			return nil, fmt.Errorf("路由表标识无法解析")
		}
	}
	for _, rule := range result.Rules {
		if _, ok := resourceNumber(rule["priority"]); !ok {
			return nil, fmt.Errorf("策略规则缺少有效 priority")
		}
		_, hasGoto, gotoErr := ruleGoto(rule)
		if gotoErr != nil {
			return nil, gotoErr
		}
		nop, hasNop := rule["nop"]
		if hasNop && nop != nil {
			return nil, fmt.Errorf("策略规则 nop 格式无效")
		}
		if table, exists := rule["table"]; exists {
			if _, ok := resourceNumber(table); !ok {
				return nil, fmt.Errorf("策略规则 table 无法解析")
			}
		} else {
			action, _ := rule["action"].(string)
			if action != "blackhole" && action != "unreachable" && action != "prohibit" && action != "nop" && !hasGoto && !hasNop {
				return nil, fmt.Errorf("策略规则缺少可解释的 table/action")
			}
		}
		if _, exists := rule["fwmark"]; exists {
			if _, _, err := ruleMark(rule); err != nil {
				return nil, err
			}
		}
	}
	filtered := make([]map[string]any, 0, len(result.NFT))
	for _, object := range result.NFT {
		if len(object) != 1 {
			return nil, fmt.Errorf("nft 对象结构不完整")
		}
		for kind, raw := range object {
			body, ok := raw.(map[string]any)
			if !ok || len(body) == 0 {
				return nil, fmt.Errorf("nft %s 对象无效", kind)
			}
			if kind == "metainfo" {
				continue
			}
			family, _ := body["family"].(string)
			if family == "" {
				return nil, fmt.Errorf("nft %s 缺少 family", kind)
			}
			if kind == "table" {
				if name, _ := body["name"].(string); name == "" {
					return nil, fmt.Errorf("nft table 缺少 name")
				}
			} else if table, _ := body["table"].(string); table == "" {
				return nil, fmt.Errorf("nft %s 缺少 table", kind)
			}
			if kind == "rule" {
				if chain, _ := body["chain"].(string); chain == "" {
					return nil, fmt.Errorf("nft rule 缺少 chain")
				}
				expressions, ok := body["expr"].([]any)
				if !ok {
					return nil, fmt.Errorf("nft rule 缺少 expr")
				}
				if err := validateNFTExpressions(expressions); err != nil {
					return nil, err
				}
			} else if kind != "table" {
				if name, _ := body["name"].(string); name == "" {
					return nil, fmt.Errorf("nft %s 缺少 name", kind)
				}
			}
			filtered = append(filtered, object)
		}
	}
	result.NFT = filtered
	// Some iproute2 releases emit empty JSON objects for interfaces rejected
	// by "type vrf". Read the full list and filter after validating identity.
	vrfs := []map[string]any{}
	for _, link := range result.VRFs {
		index, valid := resourceNumber(link["ifindex"])
		name, _ := link["ifname"].(string)
		if !valid || index == 0 || name == "" {
			return nil, fmt.Errorf("接口身份信息不完整，无法确认 VRF 占用")
		}
		raw, present := link["linkinfo"]
		if !present {
			continue
		}
		info, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("接口类型信息无效，无法确认 VRF 占用")
		}
		kind, hasKind := info["info_kind"]
		if !hasKind {
			// A physical VRF slave may have only info_slave_kind/data; the
			// master VRF device remains visible elsewhere in this full list.
			if slave, ok := info["info_slave_kind"].(string); ok && slave != "" {
				continue
			}
			return nil, fmt.Errorf("接口类型信息不完整，无法确认 VRF 占用")
		}
		kindName, ok := kind.(string)
		if !ok || kindName == "" {
			return nil, fmt.Errorf("接口类型信息无效，无法确认 VRF 占用")
		}
		if kindName != "vrf" {
			continue
		}
		if _, ok := vrfTable(link); !ok {
			return nil, fmt.Errorf("VRF 路由表信息不完整")
		}
		vrfs = append(vrfs, link)
	}
	result.VRFs = vrfs
	return result, nil
}

// iproute2's print_rule emits numeric "goto", not action:"goto", and
// "unresolved":null when its target does not exist. Source: ip/iprule.c,
// print_rule(), FR_ACT_GOTO / FR_ACT_NOP, in the upstream iproute2 tree.
func ruleGoto(rule map[string]any) (uint64, bool, error) {
	value, exists := rule["goto"]
	if !exists {
		return 0, false, nil
	}
	if _, text := value.(string); text {
		return 0, true, fmt.Errorf("策略规则 goto 必须是数值目标")
	}
	target, ok := resourceNumber(value)
	if !ok || target > math.MaxUint32 {
		return 0, true, fmt.Errorf("策略规则 goto 目标无法解析")
	}
	if _, unresolved := rule["unresolved"]; unresolved {
		return 0, true, fmt.Errorf("策略规则 goto 目标尚未解析，无法安全申请优先级")
	}
	if action, present := rule["action"]; present && action != "goto" {
		return 0, true, fmt.Errorf("策略规则 goto/action 不一致")
	}
	if _, nop := rule["nop"]; nop {
		return 0, true, fmt.Errorf("策略规则 goto/nop 不一致")
	}
	return target, true, nil
}

func vrfTable(object map[string]any) (uint64, bool) {
	info, _ := object["linkinfo"].(map[string]any)
	if info["info_kind"] != "vrf" {
		return 0, false
	}
	data, _ := info["info_data"].(map[string]any)
	value, ok := resourceNumber(data["table"])
	return value, ok && value > 0
}

func validateNFTExpressions(expressions []any) error {
	for _, raw := range expressions {
		expression, ok := raw.(map[string]any)
		if !ok || len(expression) != 1 {
			return fmt.Errorf("nft rule 表达式不完整")
		}
		for kind, rawBody := range expression {
			if kind != "mangle" && kind != "match" {
				continue
			}
			body, ok := rawBody.(map[string]any)
			if !ok {
				return fmt.Errorf("nft %s 表达式不完整", kind)
			}
			if kind == "mangle" {
				key, valid := body["key"].(map[string]any)
				_, hasValue := body["value"]
				if len(body) != 2 || !valid || len(key) != 1 || !hasValue || body["value"] == nil {
					return fmt.Errorf("nft mangle 缺少有效赋值目标或值")
				}
				for _, reference := range []string{"meta", "ct"} {
					if value, exists := key[reference]; exists {
						target, ok := value.(map[string]any)
						field, _ := target["key"].(string)
						if !ok || field == "" {
							return fmt.Errorf("nft mangle 赋值目标不完整")
						}
					}
				}
			} else {
				op, _ := body["op"].(string)
				if len(body) != 3 || op == "" || body["left"] == nil || body["right"] == nil {
					return fmt.Errorf("nft match 缺少操作或参数")
				}
			}
		}
	}
	return nil
}

func nftObjectIsManaged(object map[string]any) bool {
	for kind, raw := range object {
		body, _ := raw.(map[string]any)
		if body["family"] != "inet" {
			continue
		}
		if kind == "table" && body["name"] == managedReturnTable {
			return true
		}
		if kind != "table" && body["table"] == managedReturnTable {
			return true
		}
	}
	return false
}

func ownedNftObjects(resources *returnResources) []map[string]any {
	result := []map[string]any{}
	if resources != nil {
		for _, object := range resources.NFT {
			if nftObjectIsManaged(object) {
				result = append(result, object)
			}
		}
	}
	return result
}

func normalizeResource(value any, parent string) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, child := range v {
			if key == "handle" || key == "index" || key == "position" {
				continue
			}
			if parent == "counter" && (key == "packets" || key == "bytes") {
				continue
			}
			if key == "table" || key == "priority" || key == "metric" || key == "fwmark" || key == "fwmask" || key == "protocol" {
				if n, ok := resourceNumber(child); ok {
					result[key] = json.Number(strconv.FormatUint(n, 10))
					continue
				}
			}
			result[key] = normalizeResource(child, key)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, child := range v {
			result[i] = normalizeResource(child, parent)
		}
		// Anonymous nft sets are unordered. Kernel/compiler output may reorder
		// CIDRs; rule expressions remain ordered and must never use this path.
		if parent == "set" {
			if intervals, ok := ipv4SetIntervals(result); ok {
				return intervals
			}
			sort.SliceStable(result, func(i, j int) bool {
				left, _ := json.Marshal(result[i])
				right, _ := json.Marshal(result[j])
				return string(left) < string(right)
			})
		}
		return result
	case json.Number:
		return v
	}
	if n, ok := resourceNumber(value); ok {
		if _, isString := value.(string); !isString {
			return json.Number(strconv.FormatUint(n, 10))
		}
	}
	return value
}

// ipv4SetIntervals compares an anonymous IPv4 set by the addresses it matches.
// nft merges adjacent elements of interval sets, so 91.108.4.0/22,
// 91.108.8.0/21 and 91.108.16.0/21 read back as one 91.108.4.0-91.108.23.255.
func ipv4SetIntervals(items []any) ([]any, bool) {
	type interval struct{ start, end uint32 }
	address := func(value any) (uint32, bool) {
		text, ok := value.(string)
		if !ok {
			return 0, false
		}
		ip := net.ParseIP(text).To4()
		if ip == nil {
			return 0, false
		}
		return binary.BigEndian.Uint32(ip), true
	}
	intervals := make([]interval, 0, len(items))
	for _, item := range items {
		if start, ok := address(item); ok {
			intervals = append(intervals, interval{start, start})
			continue
		}
		element, ok := item.(map[string]any)
		if !ok || len(element) != 1 {
			return nil, false
		}
		if prefix, ok := element["prefix"].(map[string]any); ok {
			start, okAddr := address(prefix["addr"])
			length, okLen := resourceNumber(prefix["len"])
			if !okAddr || !okLen || length > 32 {
				return nil, false
			}
			size := uint32(math.MaxUint32)
			if length > 0 {
				size = uint32(1)<<(32-length) - 1
			}
			if start&size != 0 {
				return nil, false
			}
			intervals = append(intervals, interval{start, start | size})
			continue
		}
		bounds, ok := element["range"].([]any)
		if !ok || len(bounds) != 2 {
			return nil, false
		}
		start, okStart := address(bounds[0])
		end, okEnd := address(bounds[1])
		if !okStart || !okEnd || start > end {
			return nil, false
		}
		intervals = append(intervals, interval{start, end})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
	merged := []interval{}
	for _, next := range intervals {
		last := len(merged) - 1
		if last >= 0 && (merged[last].end == math.MaxUint32 || next.start <= merged[last].end+1) {
			if next.end > merged[last].end {
				merged[last].end = next.end
			}
			continue
		}
		merged = append(merged, next)
	}
	result := make([]any, len(merged))
	text := func(value uint32) string {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, value)
		return ip.String()
	}
	for i, span := range merged {
		result[i] = map[string]any{"range": []any{text(span.start), text(span.end)}}
	}
	return result, true
}

func normalizedResourceEqual(a, b map[string]any) bool {
	left, err := json.Marshal(normalizeResource(a, ""))
	if err != nil {
		return false
	}
	right, err := json.Marshal(normalizeResource(b, ""))
	return err == nil && string(left) == string(right)
}

// withoutContainerComment drops the comment of an nft table or chain object
// for return-routing comparisons. Whether it is listed depends on the nft
// release (1.0.6 omits it), so it cannot prove or disprove return-routing
// ownership; chain names and rule comments (listed by every release) do. The
// Linux gateway pins the kernel representation instead and keeps comparing it.
func withoutContainerComment(object map[string]any) map[string]any {
	if len(object) != 1 {
		return object
	}
	for kind, raw := range object {
		body, ok := raw.(map[string]any)
		if (kind != "table" && kind != "chain") || !ok {
			return object
		}
		if _, tagged := body["comment"]; !tagged {
			return object
		}
		copy := make(map[string]any, len(body))
		for key, value := range body {
			if key != "comment" {
				copy[key] = value
			}
		}
		return map[string]any{kind: copy}
	}
	return object
}

func consumeOwned(row map[string]any, expected []map[string]any, used []bool) bool {
	for i, candidate := range expected {
		if !used[i] && ownedResourceEqual(withoutContainerComment(candidate), withoutContainerComment(row)) {
			used[i] = true
			return true
		}
	}
	return false
}

// Creation plans do not yet know kernel handles. Persisted ownership does:
// replacing an object with identical content does not preserve its identity.
func ownedResourceEqual(expected, actual map[string]any) bool {
	if !normalizedResourceEqual(expected, actual) {
		return false
	}
	var sameHandles func(any, any) bool
	sameHandles = func(before, now any) bool {
		switch value := before.(type) {
		case map[string]any:
			current, ok := now.(map[string]any)
			if !ok {
				return false
			}
			if handle, exists := value["handle"]; exists {
				oldID, oldOK := resourceNumber(handle)
				newID, newOK := resourceNumber(current["handle"])
				if !oldOK || !newOK || oldID != newID {
					return false
				}
			}
			for key, child := range value {
				if !sameHandles(child, current[key]) {
					return false
				}
			}
		case []any:
			current, ok := now.([]any)
			if !ok || len(value) != len(current) {
				return false
			}
			for i, child := range value {
				if !sameHandles(child, current[i]) {
					return false
				}
			}
		}
		return true
	}
	return sameHandles(expected, actual)
}

func ruleMark(rule map[string]any) (uint32, uint32, error) {
	value := rule["fwmark"]
	var inlineMask any
	if text, ok := value.(string); ok && strings.Contains(text, "/") {
		parts := strings.Split(text, "/")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("fwmark 格式无效")
		}
		value, inlineMask = parts[0], parts[1]
	}
	mark, ok := resourceNumber(value)
	if !ok || mark > math.MaxUint32 {
		return 0, 0, fmt.Errorf("fwmark 无法解析")
	}
	mask := uint64(math.MaxUint32)
	if v, exists := rule["fwmask"]; exists {
		inlineMask = v
	}
	if inlineMask != nil {
		var valid bool
		mask, valid = resourceNumber(inlineMask)
		if !valid || mask > math.MaxUint32 {
			return 0, 0, fmt.Errorf("fwmask 无法解析")
		}
	}
	return uint32(mark), uint32(mask), nil
}

func markReference(value any) (string, bool) {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return "", false
	}
	for _, kind := range []string{"meta", "ct"} {
		if body, ok := object[kind].(map[string]any); ok && body["key"] == "mark" {
			return kind, true
		}
	}
	return "", false
}

func containsMark(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, kind := range []string{"meta", "ct"} {
			if body, ok := v[kind].(map[string]any); ok && body["key"] == "mark" {
				return true
			}
		}
		for _, child := range v {
			if containsMark(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsMark(child) {
				return true
			}
		}
	}
	return false
}

// markReadBits only proves simple, explicit masks. Dynamic maps and all other
// expressions remain unknown; guessing their bit use would grant unsafe access.
func markReadBits(value any) (uint32, bool) {
	if _, ok := markReference(value); ok {
		return math.MaxUint32, true
	}
	if !containsMark(value) {
		return 0, true
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return 0, false
	}
	for op, raw := range object {
		parts, ok := raw.([]any)
		if !ok || len(parts) != 2 {
			return 0, false
		}
		if op != "&" {
			return 0, false
		}
		for i := 0; i < 2; i++ {
			if mask, ok := resourceNumber(parts[1-i]); ok && mask <= math.MaxUint32 {
				bits, known := markReadBits(parts[i])
				return bits & uint32(mask), known
			}
		}
	}
	return 0, false
}

func nftMarkConflict(value any, wanted uint32) bool {
	if !containsMark(value) {
		return false
	}
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			if nftMarkConflict(child, wanted) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, kind := range []string{"meta", "ct"} {
			if body, ok := v[kind].(map[string]any); ok && body["key"] == "mark" {
				return true
			}
		}
		if raw, ok := v["mangle"]; ok {
			body, ok := raw.(map[string]any)
			if !ok {
				return true
			}
			kind, writesMark := markReference(body["key"])
			if !writesMark {
				bits, known := markReadBits(body["value"])
				return !known || bits&wanted != 0
			}
			if other, ok := markReference(body["value"]); ok {
				return other != kind
			}
			expression, ok := body["value"].(map[string]any)
			if !ok || len(expression) != 1 {
				return true
			}
			for op, rawParts := range expression {
				parts, ok := rawParts.([]any)
				if !ok || len(parts) != 2 {
					return true
				}
				for i := 0; i < 2; i++ {
					other, isRef := markReference(parts[i])
					mask, isNumber := resourceNumber(parts[1-i])
					if !isRef || other != kind || !isNumber || mask > math.MaxUint32 {
						continue
					}
					switch op {
					case "|", "^":
						return uint32(mask)&wanted != 0
					case "&":
						return ^uint32(mask)&wanted != 0
					}
				}
			}
			return true
		}
		if raw, ok := v["match"]; ok {
			body, ok := raw.(map[string]any)
			if !ok {
				return true
			}
			for _, side := range []string{"left", "right"} {
				bits, known := markReadBits(body[side])
				if !known || bits&wanted != 0 {
					return true
				}
			}
			return false
		}
		if _, ok := markReference(v); ok {
			return true
		}
		for _, child := range v {
			if nftMarkConflict(child, wanted) {
				return true
			}
		}
		return false
	}
	return true
}

func checkReturnResources(spec resolvedSpec, state *returnRoutingState, resources *returnResources) error {
	if resources == nil {
		return fmt.Errorf("缺少完整回流资源读取结果")
	}
	mark := spec.mark
	if mark == 0 {
		parsed, err := parseMark(spec.Mark)
		if err != nil {
			return err
		}
		mark = parsed
	}
	var ownership *returnOwnership
	if state != nil {
		ownership = state.Ownership
		if ownership == nil || ownership.Version != 1 || ownership.Token == "" {
			return fmt.Errorf("旧回流状态缺少可验证归属，拒绝自动接管或修改")
		}
	}
	tables := map[uint64]bool{uint64(spec.Table): true}
	priorities := map[uint64]bool{uint64(spec.Priority): true}
	var ownRoutes, ownRules, ownNFT []map[string]any
	if ownership != nil {
		ownRoutes, ownRules, ownNFT = ownership.Routes, ownership.Rules, ownership.NFT
		if state.Spec.Table > 0 {
			tables[uint64(state.Spec.Table)] = true
		}
		if state.Spec.Priority > 0 {
			priorities[uint64(state.Spec.Priority)] = true
		}
		for _, object := range ownNFT {
			if !nftObjectIsManaged(object) {
				return fmt.Errorf("持久 nft 归属清单无效")
			}
		}
	}
	for _, vrf := range resources.VRFs {
		table, ok := vrfTable(vrf)
		if !ok {
			return fmt.Errorf("VRF 占用信息不完整")
		}
		if tables[table] {
			return fmt.Errorf("路由表 %d 已被 VRF 使用", table)
		}
	}
	usedRoutes, usedRules, usedNFT := make([]bool, len(ownRoutes)), make([]bool, len(ownRules)), make([]bool, len(ownNFT))
	for _, route := range resources.Routes {
		if consumeOwned(route, ownRoutes, usedRoutes) {
			continue
		}
		table, ok := resourceTable(route)
		if !ok {
			return fmt.Errorf("无法解析现有路由表")
		}
		if tables[table] {
			return fmt.Errorf("路由表 %d 存在不属于本次管理的路由", table)
		}
	}
	for _, rule := range resources.Rules {
		if consumeOwned(rule, ownRules, usedRules) {
			continue
		}
		priority, ok := resourceNumber(rule["priority"])
		if !ok {
			return fmt.Errorf("策略规则 priority 无效")
		}
		if priorities[priority] {
			return fmt.Errorf("策略优先级 %d 已被其他规则占用", priority)
		}
		if target, exists, err := ruleGoto(rule); err != nil {
			return err
		} else if exists && priorities[target] {
			return fmt.Errorf("策略优先级 %d 已被其他 goto 规则引用", target)
		}
		if raw, exists := rule["table"]; exists {
			table, ok := resourceNumber(raw)
			if !ok {
				return fmt.Errorf("策略规则 table 无法解析")
			}
			if tables[table] {
				return fmt.Errorf("路由表 %d 已被其他策略规则引用", table)
			}
		}
		if _, exists := rule["fwmark"]; exists {
			_, mask, err := ruleMark(rule)
			if err != nil {
				return err
			}
			if mask&mark != 0 {
				return fmt.Errorf("目标 mark 位已被其他策略规则引用")
			}
		}
	}
	for _, object := range resources.NFT {
		if nftObjectIsManaged(object) {
			if !consumeOwned(object, ownNFT, usedNFT) {
				return fmt.Errorf("nft 表 %s 存在未知或发生变化的对象", managedReturnTable)
			}
			continue
		}
		if nftMarkConflict(object, mark) {
			return fmt.Errorf("其他 nft 规则使用目标 mark 位或无法安全解析其 mark 操作")
		}
	}
	return nil
}
