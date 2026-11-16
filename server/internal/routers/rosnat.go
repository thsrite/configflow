package routers

// RouterOS NAT assessment: whether a proxy path needs a source-NAT bypass.
// Deliberately not a firewall simulator: only a proven egress mismatch
// excludes a potentially translating rule; other matchers stay possible.

import (
	"encoding/json"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/netipx"
)

// Row is one RouterOS REST row (decoded JSON; values are strings on real devices).
type Row map[string]any

// NatContext is the RouterOS evidence for NAT assessment. A nil list could not be read.
type NatContext struct {
	Routes           []Row `json:"routes"`
	Rules            []Row `json:"rules"`
	RoutingRules     []Row `json:"routing_rules"`
	Mangle           []Row `json:"mangle"`
	VRFs             []Row `json:"vrfs"`
	InterfaceLists   []Row `json:"interface_lists"`
	InterfaceMembers []Row `json:"interface_members"`
}

// NatAssessment is the NAT status of one proxy CIDR: not_required, missing,
// unknown; for a bypass also synced/adoptable with the accepting rule ids.
type NatAssessment struct {
	Status  string   `json:"status"`
	Detail  string   `json:"detail"`
	RuleIDs []string `json:"rule_ids"`
}

var (
	nonTranslating = []string{"accept", "log", "passthrough", "return"}
	translating    = []string{"masquerade", "src-nat", "netmap", "same", "endpoint-independent-nat"}
	// Mangle actions that only touch packet/connection metadata, logging,
	// headers or control flow, never gateway selection. Unknown actions are
	// unsafe evidence; a jump is checked against the chains that exist.
	nonRoutingMangle = []string{"accept", "log", "passthrough", "return", "add-dst-to-address-list",
		"add-src-to-address-list", "change-dscp", "change-mss", "change-ttl", "clear-df", "fasttrack-connection",
		"mark-connection", "mark-packet", "set-priority", "sniff-pc", "sniff-tzsp", "strip-ipv4-options"}
	// Fields an accept may carry and still prove an unconditional exemption.
	exemptionFields = []string{".id", "chain", "action", "disabled", "comment", "log", "log-prefix", "bytes",
		"packets", "dynamic", "invalid", "src-address", "dst-address", "src-address-list", "dst-address-list",
		"out-interface", "out-interface-list"}
)

// Row values are decoded device data: RouterOS REST sends every value as a
// string ("" or absent when unset, flags "true"/"false" or "yes"/"no");
// iKuai sends numbers for ids and counters.

// text is the field as text: a string, or a JSON number (iKuai ids); "" for
// anything else.
func (r Row) text(key string) string {
	switch v := r[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return ""
}

// set: the field holds a value.
func (r Row) set(key string) bool { return r.text(key) != "" }

func (r Row) is(key, value string) bool     { s, ok := r[key].(string); return ok && s == value }
func (r Row) has(key string) bool           { _, ok := r[key]; return ok }
func (r Row) str(key string) (string, bool) { s, ok := r[key].(string); return s, ok }

// flag: any of keys is a RouterOS true/yes flag (a JSON true counts too).
func (r Row) flag(keys ...string) bool {
	return slices.ContainsFunc(keys, func(key string) bool {
		switch v := r[key].(type) {
		case bool:
			return v
		case string:
			return strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") || v == "1"
		}
		return false
	})
}

func (r Row) enabled() bool { return !r.flag("disabled") }

// number is an integer field (RouterOS: decimal text; nil when absent or not a number).
func (r Row) number(key string) *int64 {
	if s, ok := r.str(key); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return &n
		}
		return nil
	}
	if n, ok := intValue(r[key]); ok {
		return &n
	}
	return nil
}

// id is the row's .id as text.
func (r Row) id() string { return r.text(".id") }

func rowsOK(rows []Row) bool {
	return rows != nil && !slices.ContainsFunc(rows, func(r Row) bool { return r == nil })
}

// rosInterface accepts a single interface name, not an address, list or recursive gateway.
func rosInterface(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ",;%/@") {
		return ""
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return ""
	}
	return value
}

type mainRoute struct {
	row     Row
	network netip.Prefix
}

type egress struct{ iface, problem string }

// RouteEgressIndex is the parsed route evidence of one unchanged device read.
type RouteEgressIndex struct {
	problem   string
	main      []mainRoute
	exact     map[netip.Prefix]bool
	static    *StaticRouteIndex
	raw       map[string]Row
	gateways  map[netip.Addr]egress
	selectors map[string]*addressList // NAT address lists, parsed once per read
	listRule  struct {
		rows []Row
		rule *NATListRule
	}
}

// NewRouteEgressIndex indexes the main-table routes of ctx (nil: no evidence).
func NewRouteEgressIndex(ctx *NatContext) *RouteEgressIndex {
	ix := &RouteEgressIndex{exact: map[netip.Prefix]bool{}, gateways: map[netip.Addr]egress{},
		selectors: map[string]*addressList{}}
	if ctx == nil || !rowsOK(ctx.Routes) {
		ix.problem = "无法读取路由出口信息"
		return ix
	}
	for _, row := range ctx.Routes {
		if table := row.text("routing-table"); table != "" && table != "main" {
			continue
		}
		network, ok := netipx.ParseNetwork4(row.text("dst-address"), false)
		if !ok {
			if row.enabled() {
				ix.problem = "主路由表包含无法识别的目的地址"
			}
			continue
		}
		ix.main = append(ix.main, mainRoute{row, network})
		ix.exact[network] = true
	}
	return ix
}

func (ix *RouteEgressIndex) selectRoutes(cidr, gateway string) StaticRouteSelection {
	if ix.static == nil {
		ix.raw = map[string]Row{}
		entries := make([]*RouteEntry, len(ix.main))
		for i, m := range ix.main {
			key := strconv.Itoa(i)
			e := NewRoute(key, m.network.String(), m.row.text("gateway"), m.row.enabled(), "")
			if m.row.has("active") {
				e.Active = new(m.row.flag("active"))
			} else if m.row.has("inactive") {
				e.Active = new(!m.row.flag("inactive"))
			}
			if m.row.flag("inactive", "invalid") {
				e.Active = new(false)
			}
			e.Distance = m.row.number("distance")
			entries[i] = e
			ix.raw[key] = m.row
		}
		ix.static = NewStaticRouteIndex(entries)
	}
	sel, _ := ix.static.Select(cidr, gateway)
	return sel
}

// GatewayEgress is the interface of the one active connected route to nextHop.
func (ix *RouteEgressIndex) GatewayEgress(nextHop netip.Addr) (iface, problem string) {
	e, ok := ix.gateways[nextHop]
	if !ok {
		e = gatewayEgress(nextHop, ix.main)
		ix.gateways[nextHop] = e
	}
	return e.iface, e.problem
}

// gatewayEgress infers the egress of a route not created yet, only from one
// active directly connected route to the gateway, never from a default or
// recursive route; competing more-specific routes make it ambiguous.
func gatewayEgress(nextHop netip.Addr, main []mainRoute) egress {
	var covering []int
	connected := map[int]bool{}
	prefix := -1
	for i, m := range main {
		if m.row.enabled() && m.network.Contains(nextHop) {
			covering = append(covering, i)
			if m.row.flag("connect") && m.row.flag("active") && !m.row.flag("blackhole", "invalid", "ecmp") {
				connected[i] = true
				prefix = max(prefix, m.network.Bits())
			}
		}
	}
	if len(connected) == 0 {
		return egress{problem: "代理路由尚未创建，且无法找到旁路下一跳的有效直连路由"}
	}
	selected := slices.DeleteFunc(covering, func(i int) bool { return main[i].network.Bits() < prefix })
	if len(selected) != 1 || !connected[selected[0]] {
		return egress{problem: "旁路下一跳存在多条或更细的路由，无法可靠推断新增路由的出口"}
	}
	row := main[selected[0]].row
	iface, immediate := rosInterface(row.text("gateway")), ""
	if row.set("immediate-gw") {
		immediate = rosInterface(row.text("immediate-gw"))
	} else {
		immediate = iface
	}
	if iface == "" || immediate != iface {
		return egress{problem: "旁路下一跳的直连路由未给出一致的接口出口"}
	}
	return egress{iface: iface}
}

// Egress resolves the interface traffic to cidr leaves through gateway: from
// the static routes to cidr, or (policyRouting, the caller verified the Mangle
// priority and table) from the gateway's connected route. ix may be nil.
func Egress(cidr, gateway string, ctx *NatContext, policyRouting bool, ix *RouteEgressIndex) (iface, problem string) {
	if ctx == nil {
		ctx = &NatContext{}
	}
	target, ok1 := netipx.ParseNetwork4(cidr, false)
	nextHop, ok2 := netipx.ParseAddr4(gateway)
	if !ok1 || !ok2 {
		return "", "代理网段或旁路下一跳不是有效 IPv4 地址"
	}
	for _, part := range []struct {
		rows  []Row
		label string
	}{{ctx.RoutingRules, "策略路由"}, {ctx.Mangle, "路由标记"}, {ctx.VRFs, "VRF"}} {
		if !rowsOK(part.rows) {
			return "", "无法读取 " + part.label + "，不能确定旁路出口"
		}
	}
	if slices.ContainsFunc(ctx.RoutingRules, Row.enabled) {
		return "", "存在启用的策略路由，不能仅凭主路由表确定旁路出口"
	}
	// The whole mangle collection is read, custom chains included. An absent or
	// unrecognized action never proves there is no policy route.
	chains := map[string]bool{}
	for _, row := range ctx.Mangle {
		if chain, ok := row.str("chain"); ok && chain != "" {
			chains[chain] = true
		}
	}
	for _, row := range ctx.Mangle {
		if !row.enabled() {
			continue
		}
		action, ok := row.str("action")
		switch {
		case !ok || action == "":
			return "", "启用的 mangle 规则缺少有效动作，不能确定旁路出口"
		case action == "mark-routing" || action == "route":
			return "", "存在启用的路由标记或路由动作，不能确定旁路出口"
		case action == "jump":
			if jump, ok := row.str("jump-target"); !ok || !chains[jump] {
				return "", "无法确认 mangle 跳转目标链的完整规则，不能确定旁路出口"
			}
		case !slices.Contains(nonRoutingMangle, action):
			return "", "存在无法识别的 mangle 动作，不能确定旁路出口"
		}
	}
	if slices.ContainsFunc(ctx.VRFs, func(r Row) bool { return r.enabled() && !r.is("name", "main") }) {
		return "", "存在非 main VRF，不能确定旁路出口"
	}
	if ix == nil {
		ix = NewRouteEgressIndex(ctx)
	}
	if ix.problem != "" {
		return "", ix.problem
	}
	if policyRouting {
		return ix.GatewayEgress(nextHop)
	}

	sel := ix.selectRoutes(cidr, nextHop.String())
	if sel.Conflict {
		return "", "代理网段存在有效的其他下一跳，不能确定整个网段使用同一旁路出口"
	}
	interfaces := map[string]bool{}
	for _, e := range sel.Selected {
		row := ix.raw[e.ID]
		if !isTrue(e.Active) || row.flag("blackhole", "invalid", "ecmp") {
			return "", "代理路由未生效、无效、属于黑洞路由或包含多个等价出口"
		}
		parts := strings.Split(row.text("immediate-gw"), "%")
		if len(parts) != 2 {
			return "", "无法从代理路由确认唯一的实际出口"
		}
		if _, ok := netipx.ParseAddr4(parts[0]); !ok {
			return "", "代理路由的实际下一跳无法识别"
		}
		name := rosInterface(parts[1])
		if name == "" {
			return "", "代理路由包含多个或无法识别的实际出口"
		}
		interfaces[name] = true
	}
	if !sel.Covered {
		if ix.exact[target] {
			return "", "代理路由未生效，无法确认整个网段的实际出口"
		}
		name, problem := ix.GatewayEgress(nextHop)
		if name == "" {
			return "", problem
		}
		interfaces[name] = true
	}
	if len(interfaces) != 1 {
		return "", "代理网段的有效路由使用不同或无法确认的实际出口"
	}
	for name := range interfaces {
		iface = name
	}
	return iface, ""
}

// match is a selector's answer: no, yes, or not decidable from the evidence.
type match int

const (
	matchNo match = iota
	matchYes
	matchUnsure
)

func matchOf(b bool) match {
	if b {
		return matchYes
	}
	return matchNo
}

func listContains(name, iface string, ctx *NatContext) match {
	switch name {
	case "all":
		return matchYes
	case "none":
		return matchNo
	case "static", "dynamic":
		return matchUnsure
	}
	lists, members := ctx.InterfaceLists, ctx.InterfaceMembers
	if !rowsOK(lists) || !rowsOK(members) {
		return matchUnsure
	}
	if slices.ContainsFunc(members, func(r Row) bool {
		return r.enabled() && (!r.set("list") || !r.set("interface"))
	}) {
		return matchUnsure
	}
	var matching []Row
	for _, r := range lists {
		if r.is("name", name) && r.enabled() {
			matching = append(matching, r)
		}
	}
	if len(matching) != 1 || matching[0].set("include") || matching[0].set("exclude") {
		return matchUnsure
	}
	if slices.ContainsFunc(members, func(r Row) bool {
		return r.enabled() && r.is("list", name) && (r.is("interface", "all") || strings.HasPrefix(r.text("interface"), "all-"))
	}) {
		return matchUnsure
	}
	return matchOf(slices.ContainsFunc(members, func(r Row) bool {
		return r.enabled() && r.is("list", name) && r.is("interface", iface)
	}))
}

// selectorMatches evaluates an out-interface(-list) selector against the
// egress interface ("" when unknown).
func selectorMatches(value, iface string, ctx *NatContext, isList bool) match {
	text := strings.TrimSpace(value)
	if text == "" {
		return matchYes
	}
	name, negate := strings.CutPrefix(text, "!")
	if name == "" || strings.HasPrefix(name, "!") || iface == "" && name != "all" && name != "none" {
		return matchUnsure
	}
	result := matchUnsure
	switch {
	case isList:
		result = listContains(name, iface, ctx)
	case name == "all":
		result = matchYes
	case strings.HasPrefix(name, "all-"):
		// Legacy interface groups (all-ethernet, all-ppp…) are no literal names.
	default:
		result = matchOf(name == iface)
	}
	if negate && result != matchUnsure {
		return matchOf(result == matchNo)
	}
	return result
}

// AssessNAT is not_required, missing or unknown for one proxy CIDR. missing
// means a source-NAT rule may match (matchers other than the egress are not
// simulated); not_required needs no possible translation or a proven egress mismatch.
func AssessNAT(cidr, gateway string, state *RouterState, policyRouting bool, ix *RouteEgressIndex) NatAssessment {
	ctx := state.NatContext
	if ctx == nil {
		return NatAssessment{Status: "unknown", Detail: "缺少 RouterOS NAT 判断依据，无法确认是否需要免 NAT"}
	}
	if !rowsOK(ctx.Rules) {
		return NatAssessment{Status: "unknown", Detail: "无法读取完整 NAT 规则，无法确认是否需要免 NAT"}
	}
	if slices.ContainsFunc(ctx.Rules, func(r Row) bool { return r.enabled() && !r.set("chain") }) {
		return NatAssessment{Status: "unknown", Detail: "启用的 NAT 规则缺少链信息，无法排除源地址转换"}
	}
	var candidates []Row
	for _, r := range ctx.Rules {
		if action, ok := r.str("action"); r.enabled() && r.is("chain", "srcnat") && !(ok && slices.Contains(nonTranslating, action)) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return NatAssessment{Status: "not_required", Detail: "没有启用的潜在源 NAT 转换规则，无需额外免 NAT"}
	}
	iface, problem, evaluated := "", "", false
	var unknown []string
	possible := false
	for _, r := range candidates {
		if r.set("out-interface") || r.set("out-interface-list") {
			if !evaluated {
				iface, problem = Egress(cidr, gateway, ctx, policyRouting, ix)
				evaluated = true
			}
			if iface == "" {
				unknown = append(unknown, problem)
				continue
			}
			// Matchers of one rule are conjunctive: a definite mismatch
			// excludes the rule even when the other selector is undecidable.
			a := selectorMatches(r.text("out-interface"), iface, ctx, false)
			b := selectorMatches(r.text("out-interface-list"), iface, ctx, true)
			if a == matchNo || b == matchNo {
				continue
			}
			if a == matchUnsure || b == matchUnsure {
				unknown = append(unknown, "NAT 出口条件包含无法解析的接口分组或列表，无法排除源地址转换")
				continue
			}
		}
		if action, _ := r.str("action"); !slices.Contains(translating, action) {
			unknown = append(unknown, "存在可能匹配的 NAT 跳转或未知动作，无法确认是否发生源地址转换")
		} else {
			possible = true
		}
	}
	switch {
	case len(unknown) > 0:
		return NatAssessment{Status: "unknown", Detail: unknown[0]}
	case possible:
		return NatAssessment{Status: "missing", Detail: "存在可能匹配旁路流量的源 NAT 转换规则，需要免 NAT 保护"}
	}
	return NatAssessment{Status: "not_required", Detail: "旁路出口为 " + iface + "，与启用的潜在源 NAT 规则均不匹配，无需额外免 NAT"}
}

// addressList is a parsed address list: collapsed networks with their bounds.
type addressList struct {
	networks []netip.Prefix
	ok       bool
}

func parseAddressList(values []string) *addressList {
	var networks []netip.Prefix
	for _, value := range values {
		if first, last, isRange := strings.Cut(value, "-"); isRange {
			a, ok1 := netipx.ParseAddr4(first)
			b, ok2 := netipx.ParseAddr4(last)
			if !ok1 || !ok2 || b.Less(a) {
				return &addressList{}
			}
			networks = append(networks, netipx.Summarize(a, b)...)
		} else if n, ok := netipx.ParseNetwork4(value, false); ok {
			networks = append(networks, n)
		} else {
			return &addressList{}
		}
	}
	collapsed, _ := netipx.Collapse(networks)
	return &addressList{networks: collapsed, ok: true}
}

// addressHits intersects a packet range with a row's literal and list
// selectors of field (both must match); ok false when undecidable.
func addressHits(row Row, field string, target netip.Prefix, sets map[string][]string, cache map[string]*addressList) (hits []netip.Prefix, ok bool) {
	literal := netip.MustParsePrefix("0.0.0.0/0")
	if row.set(field) {
		var valid bool
		if literal, valid = netipx.ParseNetwork4(row.text(field), false); !valid {
			return nil, false
		}
	}
	if !target.Overlaps(literal) {
		return []netip.Prefix{}, true
	}
	if !subnetOf(target, literal) {
		target = literal
	}
	if !row.set(field + "-list") {
		return []netip.Prefix{target}, true
	}
	listName, isStr := row.str(field + "-list")
	values, known := sets[listName]
	if !isStr || strings.HasPrefix(listName, "!") || !known {
		return nil, false
	}
	list := cache[listName]
	if list == nil {
		list = parseAddressList(values)
		cache[listName] = list
	}
	if !list.ok {
		return nil, false
	}
	hits = []netip.Prefix{}
	for _, n := range list.networks {
		if n.Overlaps(target) && subnetOf(target, n) {
			hits = append(hits, target)
		} else if n.Overlaps(target) {
			hits = append(hits, n)
		}
	}
	return hits, true
}

func subnetOf(a, b netip.Prefix) bool { return b.Bits() <= a.Bits() && b.Contains(a.Addr()) }

// addressExclude is a.address_exclude(b) in Python's order (b inside a).
func addressExclude(a, b netip.Prefix) []netip.Prefix {
	var out []netip.Prefix
	for a != b {
		low := netip.PrefixFrom(a.Addr(), a.Bits()+1)
		high := netip.PrefixFrom(netipx.Broadcast(low).Next(), a.Bits()+1)
		if subnetOf(b, low) {
			out, a = append(out, high), low
		} else {
			out, a = append(out, low), high
		}
	}
	return out
}

type flow [2]netip.Prefix // source, destination

// PreservesSources proves, in rule order, that no rule translates any
// source/destination pair. Literal addresses, address lists and proven egress
// selectors can discharge coverage; other matchers cannot. accepted (may be
// nil) collects the ids of accepts that exempted something; cache (may be
// nil) holds parsed address lists.
func PreservesSources(rows []Row, sources, destinations []netip.Prefix, iface string, ctx *NatContext,
	sets map[string][]string, accepted *[]string, cache map[string]*addressList) bool {
	var pending []flow
	for _, s := range sources {
		for _, d := range destinations {
			pending = append(pending, flow{s, d})
		}
	}
	if cache == nil {
		cache = map[string]*addressList{}
	}
	type hit struct {
		pair       flow
		srcs, dsts []netip.Prefix
		ok         bool
	}
	for _, row := range rows {
		if len(pending) == 0 {
			return true
		}
		if chain, set := row["chain"]; !row.enabled() || set && chain != nil && chain != "srcnat" {
			continue
		}
		egressMatch := selectorMatches(row.text("out-interface"), iface, ctx, false)
		listMatch := selectorMatches(row.text("out-interface-list"), iface, ctx, true)
		if egressMatch == matchNo || listMatch == matchNo {
			continue
		}
		var affected []hit
		for _, pair := range pending {
			srcs, ok1 := addressHits(row, "src-address", pair[0], sets, cache)
			dsts, ok2 := addressHits(row, "dst-address", pair[1], sets, cache)
			if (!ok1 || len(srcs) > 0) && (!ok2 || len(dsts) > 0) {
				affected = append(affected, hit{pair, srcs, dsts, ok1 && ok2})
			}
		}
		if len(affected) == 0 {
			continue
		}
		action := row["action"]
		if action == "log" || action == "passthrough" {
			continue
		}
		if action != "accept" {
			return false
		}
		if !row.set("chain") || slices.ContainsFunc(slices.Sorted(maps.Keys(row)), func(k string) bool { return !slices.Contains(exemptionFields, k) }) ||
			row.flag("invalid", "dynamic") || egressMatch != matchYes || listMatch != matchYes {
			continue
		}
		before := slices.Clone(pending)
		for _, h := range affected {
			if !h.ok {
				continue
			}
			pieces := []flow{h.pair}
			for _, source := range h.srcs {
				for _, destination := range h.dsts {
					var remainder []flow
					for _, p := range pieces {
						a, b := p[0], p[1]
						if !a.Overlaps(source) || !b.Overlaps(destination) {
							remainder = append(remainder, p)
							continue
						}
						hitA, hitB := a, b
						if !subnetOf(a, source) {
							hitA = source
						}
						if !subnetOf(b, destination) {
							hitB = destination
						}
						for _, part := range addressExclude(a, hitA) {
							remainder = append(remainder, flow{part, b})
						}
						for _, part := range addressExclude(b, hitB) {
							remainder = append(remainder, flow{hitA, part})
						}
					}
					pieces = remainder
				}
			}
			if i := slices.Index(pending, h.pair); i >= 0 {
				pending = slices.Delete(pending, i, i+1)
			}
			pending = append(pending, pieces...)
		}
		if accepted != nil && !slices.Equal(pending, before) {
			*accepted = append(*accepted, row.id())
		}
	}
	return true
}

// ClientSourceNetworks is the gateway's /24 without the network, broadcast
// and gateway addresses: the clients a new exemption covers.
func ClientSourceNetworks(gateway netip.Addr) []netip.Prefix {
	network := netip.PrefixFrom(gateway, 24).Masked()
	first, last := network.Addr().Next(), netipx.Broadcast(network).Prev()
	var out []netip.Prefix
	if first.Less(gateway) {
		out = append(out, netipx.Summarize(first, gateway.Prev())...)
	}
	if gateway.Less(last) {
		out = append(out, netipx.Summarize(gateway.Next(), last)...)
	}
	return out
}

// AssessBypass reads the ordered NAT chain before claiming an exemption is
// effective. An accept with matchers it cannot evaluate is kept intact and
// reported unknown instead of silently broadened.
func AssessBypass(cidr, gateway string, state *RouterState, ix *RouteEgressIndex) NatAssessment {
	assessment := AssessNAT(cidr, gateway, state, false, ix)
	assessment.RuleIDs = []string{}
	ctx := state.NatContext
	if assessment.Status == "not_required" || ctx == nil || !rowsOK(ctx.Rules) {
		return assessment
	}
	rows := ctx.Rules
	iface := ""
	if slices.ContainsFunc(rows, func(r Row) bool { return r.set("out-interface") || r.set("out-interface-list") }) {
		iface, _ = Egress(cidr, gateway, ctx, false, ix)
	}
	gw, ok1 := netipx.ParseAddr4(gateway)
	destination, ok2 := netipx.ParseNetwork4(cidr, true)
	if !ok1 || !ok2 {
		return NatAssessment{Status: "unknown", Detail: "无法确认 NAT 免转换的客户端范围", RuleIDs: []string{}}
	}
	cache := map[string]*addressList{}
	if ix != nil {
		cache = ix.selectors
	}
	accepted := []string{}
	if PreservesSources(rows, ClientSourceNetworks(gw), []netip.Prefix{destination}, iface, ctx, state.AddressSets, &accepted, cache) {
		managed, found := true, false
		for _, r := range rows {
			if slices.Contains(accepted, r.id()) {
				found = true
				comment, _ := r.str("comment")
				managed = managed && IsManaged(comment)
			}
		}
		switch {
		case !found:
			return NatAssessment{Status: "not_required", Detail: "客户端流量不会命中源 NAT 转换规则，无需额外免 NAT", RuleIDs: []string{}}
		case managed:
			return NatAssessment{Status: "synced", RuleIDs: accepted}
		}
		return NatAssessment{Status: "adoptable", RuleIDs: accepted, Detail: "现有免转换规则可保护客户端流量，将直接接管"}
	}
	retiring := slices.Clone(state.PolicyNATRetiring)
	// ConfigFlow's list rule, intact and first, is maintained rather than
	// preserved: a stale source list or a narrower member makes it missing,
	// and the plan rewrites the members.
	if own := listRule(rows, ix); own != nil && (own.OutInterface == "" || own.OutInterface == iface) {
		retiring = append(retiring, own.Rule.id())
	}
	// An existing exemption with any potentially overlapping destination is
	// preserved: never compensate for its restrictions with a broader rule.
	for _, r := range rows {
		if !r.enabled() || !r.is("chain", "srcnat") || !r.is("action", "accept") || slices.Contains(retiring, r.id()) {
			continue
		}
		selectors := r
		if r.set("dst-address") {
			// A literal target with a narrower list still restricts this target.
			selectors = Row{}
			for k, v := range r {
				if k != "dst-address-list" {
					selectors[k] = v
				}
			}
		}
		if hits, ok := addressHits(selectors, "dst-address", destination, state.AddressSets, cache); !ok || len(hits) > 0 {
			return NatAssessment{Status: "unknown", RuleIDs: []string{},
				Detail: "已有免转换规则的顺序或来源、地址列表、接口、协议等条件不能保证客户端流量均免转换；请先检查现有规则"}
		}
	}
	return assessment
}

// listRule is the valid ConfigFlow list rule of this read, once per index.
func listRule(rows []Row, ix *RouteEgressIndex) *NATListRule {
	if ix != nil && len(rows) > 0 && len(ix.listRule.rows) == len(rows) && &ix.listRule.rows[0] == &rows[0] {
		return ix.listRule.rule
	}
	var own *NATListRule
	if found := InspectNATListRule(rows, nil); found.Rule != nil && found.Problem == "" {
		own = &found
	}
	if ix != nil {
		ix.listRule.rows, ix.listRule.rule = rows, own
	}
	return own
}

// BypassExpansion is a planned update adding destinations to a copy of an existing exemption.
type BypassExpansion struct {
	ID      string
	AddDst  []string
	Comment string
}

// PlannedBypassConflicts checks the expanded copies without changing or
// broadening their matchers; one message per CIDR that stays unproven.
func PlannedBypassConflicts(expansions []BypassExpansion, state *RouterState, gateway string) []string {
	if len(expansions) == 0 || state.NatContext == nil {
		return []string{}
	}
	ctx := *state.NatContext
	ctx.Rules = slices.Clone(ctx.Rules)
	targets := map[string]bool{}
	for _, op := range expansions {
		at := slices.IndexFunc(ctx.Rules, func(r Row) bool { return r.id() == op.ID })
		if at < 0 {
			continue
		}
		original := ctx.Rules[at]
		for _, cidr := range op.AddDst {
			clone := Row{}
			for k, v := range original {
				clone[k] = v
			}
			clone[".id"], clone["dst-address"], clone["comment"] = "preview:"+op.ID+":"+cidr, cidr, op.Comment
			ctx.Rules = slices.Insert(ctx.Rules, at, clone)
			at++
			targets[cidr] = true
		}
	}
	virtual := &RouterState{NatContext: &ctx, AddressSets: state.AddressSets}
	ix := NewRouteEgressIndex(&ctx)
	out := []string{}
	for _, cidr := range slices.Sorted(maps.Keys(targets)) {
		switch AssessBypass(cidr, gateway, virtual, ix).Status {
		case "synced", "adoptable", "not_required":
		default:
			out = append(out, cidr+"：扩充后仍无法确认 NAT 免转换覆盖，已保留原规则的限制，请先检查现有规则")
		}
	}
	return out
}
