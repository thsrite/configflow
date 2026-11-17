package routers

// The ConfigFlow RouterOS NAT list rule (one srcnat accept over two address
// lists). Inspection is shared with NAT assessment; planning, reads and guarded
// writes live in natlist_plan.go, natlist_read.go and natlist_write.go.

import (
	"maps"
	"slices"
	"strings"
)

const (
	NATListComment = "cf-mihomo-nat"
	NATSrcList     = "cf-mihomo-nat-src"
	NATDstList     = "cf-mihomo-nat-dst"
	PathNAT        = "/ip/firewall/nat"
	PathMangle     = "/ip/firewall/mangle"
)

// Fields the list rule may carry: the evidence PreservesSources accepts,
// minus literal addresses and interface lists.
var natListRuleFields = []string{".id", "chain", "action", "disabled", "comment", "log", "log-prefix", "bytes",
	"packets", "dynamic", "invalid", "src-address-list", "dst-address-list", "out-interface"}

// NATListRule is the inspection of a NAT table: the list rule (nil when
// there is none), a problem only a person can resolve, and its out-interface.
type NATListRule struct {
	Rule         Row
	Problem      string
	OutInterface string
}

// ListRef is a row selecting one of the lists: Path is "<path>/<id>".
type ListRef struct{ Path, Name string }

// NATListRefs are the rows selecting either list, negated or not.
func NATListRefs(rows []Row) []ListRef {
	var found []ListRef
	for _, row := range rows {
		for _, key := range []string{"src-address-list", "dst-address-list"} {
			if name := strings.TrimLeft(row.text(key), "!"); name == NATSrcList || name == NATDstList {
				found = append(found, ListRef{PathNAT + "/" + row.text(".id"), name})
				break
			}
		}
	}
	return found
}

// IsNATListCandidate: the comment and a selector of either list. The comment
// alone is no ownership (an adopted hand-made exemption carries it too).
func IsNATListCandidate(row Row) bool {
	return row.is("comment", NATListComment) && len(NATListRefs([]Row{row})) > 0
}

// InspectNATListRule finds the list rule in an ordered NAT table; others are
// Mangle/filter/raw rows selecting the lists.
func InspectNATListRule(rows []Row, others []ListRef) NATListRule {
	var found []int
	for i, row := range rows {
		if IsNATListCandidate(row) {
			found = append(found, i)
		}
	}
	if len(found) > 1 {
		return NATListRule{Problem: "发现多条 ConfigFlow NAT 免转换列表规则（" + NATListComment + "），无法确认归属，请删除多余规则后重新预览"}
	}
	own := ""
	if len(found) == 1 {
		own = PathNAT + "/" + rows[found[0]].text(".id")
	}
	refs := slices.DeleteFunc(NATListRefs(rows), func(r ListRef) bool { return len(found) == 1 && r.Path == own })
	if refs = append(refs, others...); len(refs) > 0 {
		return NATListRule{Problem: "其他规则 " + refs[0].Path + " 正在引用 ConfigFlow 的免转换地址列表 " + refs[0].Name + "，请先解除引用"}
	}
	if len(found) == 0 {
		return NATListRule{}
	}
	row := rows[found[0]]
	id := row.text(".id")
	var changed []string
	for _, key := range slices.Sorted(maps.Keys(row)) {
		if !slices.Contains(natListRuleFields, key) {
			changed = append(changed, key)
		}
	}
	for _, c := range []struct {
		key  string
		fine bool
	}{{"chain", row.is("chain", "srcnat")}, {"action", row.is("action", "accept")},
		{"src-address-list", row.is("src-address-list", NATSrcList)}, {"dst-address-list", row.is("dst-address-list", NATDstList)},
		{"log", !row.flag("log") && !row.set("log-prefix")}} {
		if !c.fine {
			changed = append(changed, c.key)
		}
	}
	egress := row.text("out-interface")
	if egress != "" && (strings.HasPrefix(egress, "!") || rosInterface(egress) == "") {
		changed = append(changed, "out-interface")
	}
	switch {
	case len(changed) > 0:
		return NATListRule{Problem: "ConfigFlow 的 NAT 免转换规则 " + id + " 被修改（" + strings.Join(changed, "、") + "），不会自动覆盖，请恢复或删除后重新预览"}
	case !row.enabled():
		return NATListRule{Problem: "ConfigFlow 的 NAT 免转换规则 " + id + " 已停用，请启用或删除后重新预览"}
	case row.flag("dynamic", "invalid"):
		return NATListRule{Problem: "ConfigFlow 的 NAT 免转换规则 " + id + " 是动态或无效规则，请检查后重新预览"}
	}
	for _, other := range rows[:found[0]] {
		// Not even a return ahead of it: only rules that let packets through.
		chain, set := other["chain"]
		action, isStr := other.str("action")
		if other.enabled() && (!set || chain == nil || chain == "srcnat" || chain == "") &&
			!(isStr && slices.Contains([]string{"accept", "log", "passthrough"}, action)) {
			return NATListRule{Problem: "ConfigFlow 的 NAT 免转换规则 " + id + " 位于源 NAT 转换规则 " + other.text(".id") + " 之后，请移到最前或删除后重新预览"}
		}
	}
	return NATListRule{Rule: row, OutInterface: egress}
}
