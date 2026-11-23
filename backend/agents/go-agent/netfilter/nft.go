package netfilter

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

type nftChain struct {
	family, table, name string
	hook, policy        string
	base                bool
	rules               [][]any
}

type nftRuleset struct {
	tables map[string]bool
	chains map[string]*nftChain
	order  []*nftChain
	sets   map[string][]any // named sets and maps -> elements
}

func nftKey(parts ...string) string { return strings.Join(parts, "|") }

func (r *nftRuleset) hasTable(family, name string) bool { return r.tables[nftKey(family, name)] }

// set returns the elements of a named set or map of the chain's table.
func (r *nftRuleset) set(chain *nftChain, name string) ([]any, bool) {
	elements, ok := r.sets[nftKey(chain.family, chain.table, name)]
	return elements, ok
}

func parseNFT(output string) (*nftRuleset, error) {
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		return nil, err
	}
	if doc.Nftables == nil {
		return nil, fmt.Errorf("nft ruleset 返回格式无效")
	}
	ruleset := &nftRuleset{tables: map[string]bool{}, chains: map[string]*nftChain{}, sets: map[string][]any{}}
	type ruleObject struct {
		Family string `json:"family"`
		Table  string `json:"table"`
		Chain  string `json:"chain"`
		Expr   []any  `json:"expr"`
	}
	var rules []ruleObject
	for _, object := range doc.Nftables {
		if raw, ok := object["table"]; ok {
			var table struct {
				Family string `json:"family"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal(raw, &table); err != nil {
				return nil, err
			}
			ruleset.tables[nftKey(table.Family, table.Name)] = true
		}
		if raw, ok := object["chain"]; ok {
			var chain struct {
				Family string `json:"family"`
				Table  string `json:"table"`
				Name   string `json:"name"`
				Type   string `json:"type"`
				Hook   string `json:"hook"`
				Policy string `json:"policy"`
			}
			if err := json.Unmarshal(raw, &chain); err != nil {
				return nil, err
			}
			value := &nftChain{family: chain.Family, table: chain.Table, name: chain.Name, hook: chain.Hook, policy: chain.Policy, base: chain.Hook != ""}
			ruleset.chains[nftKey(chain.Family, chain.Table, chain.Name)] = value
			ruleset.order = append(ruleset.order, value)
		}
		for _, kind := range []string{"set", "map"} {
			raw, ok := object[kind]
			if !ok {
				continue
			}
			var set struct {
				Family string `json:"family"`
				Table  string `json:"table"`
				Name   string `json:"name"`
				Elem   []any  `json:"elem"`
			}
			if err := json.Unmarshal(raw, &set); err != nil {
				return nil, err
			}
			if set.Elem == nil {
				set.Elem = []any{}
			}
			ruleset.sets[nftKey(set.Family, set.Table, set.Name)] = set.Elem
		}
		if raw, ok := object["rule"]; ok {
			var rule ruleObject
			if err := json.Unmarshal(raw, &rule); err != nil {
				return nil, err
			}
			rules = append(rules, rule)
		}
	}
	for _, rule := range rules {
		chain := ruleset.chains[nftKey(rule.Family, rule.Table, rule.Chain)]
		if chain == nil {
			return nil, fmt.Errorf("nft 规则引用了不存在的链")
		}
		chain.rules = append(chain.rules, rule.Expr)
	}
	return ruleset, nil
}

type nftEvaluation struct {
	in      *inspection
	ruleset *nftRuleset
	base    *nftChain
	found   []nftFinding
}

type nftFinding struct {
	chain   string
	policy  bool
	unknown bool
	flow    string
	proto   string
}

// record keeps a possible drop; strict flows only count certain ones (see
// iptEvaluation.record).
func (e *nftEvaluation) record(item nftFinding, f flow, m tri, certain bool) {
	if f.strict && !(certain && m == yes) {
		if e.in != nil {
			e.in.skipped = true
		}
		return
	}
	item.flow, item.proto = f.name, f.proto
	e.found = append(e.found, item)
}

// evaluateNFT evaluates every IPv4 forward and input base chain independently:
// accept only ends its own chain, while a drop in any of them drops the packet.
func (in *inspection) evaluateNFT(ruleset *nftRuleset, iptablesNFTRead, firewalldSkipped bool) {
	for _, chain := range ruleset.order {
		if !chain.base || (chain.hook != "forward" && chain.hook != "input") || (chain.family != "ip" && chain.family != "inet") {
			continue
		}
		switch {
		case iptablesNFTRead && chain.family == "ip" && chain.table == "filter":
			continue // already evaluated through iptables-save
		case firewalldSkipped && chain.table == "firewalld":
			continue // firewalld's state is unknown; reported as unknown
		case chain.family == "ip" && chain.table == "configflow_gateway":
			continue // selective gateway resources are judged by the gateway itself
		}
		flows := in.flows(chain.hook)
		if len(flows) == 0 {
			continue
		}
		in.hooks++
		in.notePolicy(chain.hook, chain.policy)
		e := &nftEvaluation{in: in, ruleset: ruleset, base: chain}
		for _, f := range flows {
			e.chain(chain, f, 0, true)
		}
		if len(e.found) == 0 {
			continue
		}
		firewalld := in.firewalld && chain.family == "inet" && chain.table == "firewalld"
		name := "nftables " + chain.family + " " + chain.table + " " + chain.name
		var plan *remedyPlan
		kind := "nftables"
		if firewalld {
			kind = "firewalld"
			plan = in.plan("firewalld", "", "", "", "", "firewalld")
			if plan.lanZones == nil {
				plan.lanZones = firewalldZones(ruleset, in.lan)
			}
		} else {
			plan = in.plan("nftables", "", chain.family, chain.table, chain.name, name)
		}
		for _, item := range e.found {
			plan.flows[item.flow] = true
			detail := ""
			switch {
			case item.policy:
				detail = name + " 链默认策略为 drop"
			case item.unknown:
				detail = name + " 链有无法判断的规则（" + item.chain + "）"
			default:
				detail = name + " 链有 drop 或 reject 规则（" + item.chain + "）"
			}
			if firewalld {
				if chain.hook == "input" {
					detail = "firewalld 正在运行，会拒绝从 " + displayDevice(in.target.Device) + " 进入本机的连接（" + chain.name + "）"
				} else {
					detail = "firewalld 正在运行，会拒绝转发到 " + displayDevice(in.target.Device) + " 的流量（" + chain.name + "）"
				}
				switch item.flow {
				case FlowRedirectTCP:
					detail = "firewalld 正在运行，会拒绝被重定向到本机端口的 TCP（" + chain.name + "）"
				case FlowLocalDNS:
					detail = "firewalld 正在运行，会拒绝局域网发到本机 53 端口的 DNS 查询（" + chain.name + "）"
				}
			}
			policy := ""
			if item.policy {
				policy = "drop"
			}
			in.record(finding{source: Source{Kind: kind, Hook: chain.hook, Family: chain.family, Table: chain.table, Chain: chain.name, Policy: policy, Detail: clip(detail)}, policy: item.policy, flow: item.flow, proto: item.proto})
		}
	}
}

// nftChainState tracks, within one chain, whether a rule may already have
// accepted the packet or returned from the chain; later drops are then not
// certain (see iptEvaluation.chain).
type nftChainState struct{ mayAccept, mayReturn bool }

func (st *nftChainState) sure(certain bool) bool { return certain && !st.mayAccept && !st.mayReturn }

// chain also reports whether a rule on the way may have accepted the packet.
func (e *nftEvaluation) chain(chain *nftChain, f flow, depth int, certain bool) (verdict, bool) {
	st := &nftChainState{}
	for _, expressions := range chain.rules {
		result := e.rule(chain, expressions, f, depth, certain, st)
		if result == returned {
			break // a base chain then applies its policy
		}
		if result != fallThrough {
			return result, st.mayAccept
		}
	}
	if chain.base {
		if chain.policy == "drop" {
			e.record(nftFinding{chain: chain.name, policy: true}, f, yes, certain && !st.mayAccept)
			return dropped, st.mayAccept
		}
		return accepted, st.mayAccept
	}
	return fallThrough, st.mayAccept
}

// rule returns fallThrough to continue with the next rule of the chain.
func (e *nftEvaluation) rule(chain *nftChain, expressions []any, f flow, depth int, certain bool, st *nftChainState) verdict {
	m := yes
	invalidOnly := false
	for _, raw := range expressions {
		expression, ok := raw.(map[string]any)
		if !ok || len(expression) != 1 {
			m = and(m, maybe)
			continue
		}
		for name, body := range expression {
			switch name {
			case "match":
				value, invalid := nftMatch(body, f, func(name string) ([]any, bool) { return e.ruleset.set(chain, name) })
				m = and(m, value)
				invalidOnly = invalidOnly || invalid
			case "counter", "log", "mangle", "notrack", "comment":
			case "xt":
				spec, _ := body.(map[string]any)
				if spec["type"] == "target" {
					if target, _ := spec["name"].(string); !nonTerminatingTargets[target] {
						return e.verdict(chain, "unknown", "", m, invalidOnly, f, depth, certain, st)
					}
					continue
				}
				m = and(m, maybe)
			case "accept", "drop", "return", "continue":
				return e.verdict(chain, name, "", m, invalidOnly, f, depth, certain, st)
			case "reject":
				return e.verdict(chain, "drop", "", m, invalidOnly, f, depth, certain, st)
			case "jump", "goto":
				spec, _ := body.(map[string]any)
				target, _ := spec["target"].(string)
				return e.verdict(chain, name, target, m, invalidOnly, f, depth, certain, st)
			case "vmap":
				return e.vmap(chain, body, m, invalidOnly, f, depth, certain, st)
			case "queue":
				return e.verdict(chain, "unknown", "", m, invalidOnly, f, depth, certain, st)
			default:
				m = and(m, maybe)
			}
		}
		if m == no {
			return fallThrough
		}
	}
	return fallThrough
}

func (e *nftEvaluation) verdict(chain *nftChain, kind, target string, m tri, invalidOnly bool, f flow, depth int, certain bool, st *nftChainState) verdict {
	if m == no {
		return fallThrough
	}
	sure := st.sure(certain)
	switch kind {
	case "accept":
		if m == yes {
			return accepted
		}
		st.mayAccept = true
	case "drop":
		if invalidOnly {
			return fallThrough
		}
		e.record(nftFinding{chain: chain.name}, f, m, sure)
		if m == yes {
			return dropped
		}
	case "return":
		if m == yes {
			return returned
		}
		st.mayReturn = true
	case "jump", "goto":
		next := e.ruleset.chains[nftKey(chain.family, chain.table, target)]
		if next == nil || next.base || depth >= 16 {
			e.record(nftFinding{chain: chain.name, unknown: true}, f, m, sure)
			st.mayAccept = true
			return fallThrough
		}
		result, inner := e.chain(next, f, depth+1, sure && m == yes)
		if inner || (m != yes && result == accepted) {
			st.mayAccept = true
		}
		if m != yes {
			return fallThrough
		}
		switch result {
		case accepted, dropped:
			return result
		}
		if kind == "goto" {
			return returned
		}
	case "continue":
	default:
		// An unknown verdict may drop or accept.
		e.record(nftFinding{chain: chain.name, unknown: true}, f, m, sure)
		st.mayAccept = true
	}
	return fallThrough
}

// vmap resolves elements keyed by an interface name or a conntrack state per
// flow; any other key may match each element. Jump and goto targets are
// followed like those of an ordinary rule.
func (e *nftEvaluation) vmap(chain *nftChain, body any, m tri, invalidOnly bool, f flow, depth int, certain bool, st *nftChainState) verdict {
	spec, _ := body.(map[string]any)
	elements, ok := e.elements(chain, spec["data"])
	if !ok {
		e.record(nftFinding{chain: chain.name, unknown: true}, f, m, st.sure(certain))
		st.mayAccept = true
		return fallThrough
	}
	key := nftMetaKey(spec["key"])
	ctState := nftCTKey(spec["key"]) == "state"
	for _, raw := range elements {
		pair, _ := raw.([]any)
		if len(pair) != 2 {
			e.record(nftFinding{chain: chain.name, unknown: true}, f, m, st.sure(certain))
			st.mayAccept = true
			continue
		}
		element, elementInvalid := maybe, false
		value := nftElementValue(pair[0])
		switch {
		case key == "iifname" || key == "iif" || key == "oifname" || key == "oif":
			s := f.out
			if key == "iifname" || key == "iif" {
				s = f.in
			}
			if name, ok := value.(string); ok && !strings.HasPrefix(name, "@") {
				element = matchInterface(name, s, '*')
			}
		case key == "l4proto":
			if values, ok := nftStrings(value); ok {
				element = protoMatch(values, f)
			}
		case ctState:
			if values, ok := nftStrings(value); ok {
				if len(values) == 1 && values[0] == "invalid" {
					elementInvalid = true
				} else {
					element = stateMatch(values, false, f)
				}
			}
		}
		kind, target := nftVerdictOf(pair[1])
		if result := e.verdict(chain, kind, target, and(m, element), invalidOnly || elementInvalid, f, depth, certain, st); result != fallThrough {
			return result
		}
	}
	return fallThrough
}

// elements reads an anonymous verdict map or a named map reference (@name).
func (e *nftEvaluation) elements(chain *nftChain, value any) ([]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		elements, ok := v["set"].([]any)
		return elements, ok
	case string:
		if strings.HasPrefix(v, "@") {
			return e.ruleset.set(chain, v[1:])
		}
	}
	return nil, false
}

// nftElementValue unwraps an element carrying options ({"elem": {"val": ...}}).
func nftElementValue(value any) any {
	if wrapped, ok := value.(map[string]any); ok {
		if elem, ok := wrapped["elem"].(map[string]any); ok {
			return elem["val"]
		}
	}
	return value
}

func nftVerdictOf(value any) (string, string) {
	verdict, _ := value.(map[string]any)
	for name, body := range verdict {
		switch name {
		case "accept", "drop", "return", "continue":
			return name, ""
		case "reject":
			return "drop", ""
		case "jump", "goto":
			spec, _ := body.(map[string]any)
			target, _ := spec["target"].(string)
			return name, target
		}
	}
	return "unknown", ""
}

func nftMetaKey(value any) string {
	expression, _ := value.(map[string]any)
	meta, _ := expression["meta"].(map[string]any)
	key, _ := meta["key"].(string)
	return key
}

func nftCTKey(value any) string {
	expression, _ := value.(map[string]any)
	ct, _ := expression["ct"].(map[string]any)
	key, _ := ct["key"].(string)
	return key
}

func nftStrings(value any) ([]string, bool) {
	switch v := value.(type) {
	case string:
		return []string{v}, true
	case []any:
		result := []string{}
		for _, item := range v {
			text, ok := nftElementValue(item).(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	case map[string]any:
		if set, ok := v["set"]; ok {
			return nftStrings(set)
		}
	}
	return nil, false
}

// nftResolve reads the right-hand values of a match. A named set (@name) is
// read from the ruleset; one that cannot be resolved to names is not usable.
func nftResolve(value any, lookup func(string) ([]any, bool)) ([]string, bool) {
	if name, ok := value.(string); ok && strings.HasPrefix(name, "@") {
		elements, found := lookup(name[1:])
		if !found {
			return nil, false
		}
		return nftStrings(elements)
	}
	values, ok := nftStrings(value)
	if !ok {
		return nil, false
	}
	for _, item := range values {
		if strings.HasPrefix(item, "@") {
			return nil, false
		}
	}
	return values, true
}

// nftMatch evaluates interface, protocol, address type and conntrack
// matches; any other match is conditional. The second result marks a match on
// ct state invalid only.
func nftMatch(body any, f flow, lookup func(string) ([]any, bool)) (tri, bool) {
	spec, _ := body.(map[string]any)
	op, _ := spec["op"].(string)
	negated := op == "!="
	left, _ := spec["left"].(map[string]any)
	if masked, ok := left["&"].([]any); ok && len(masked) == 2 {
		// tcp flags & (fin | syn | rst | ack) == syn
		if nftTCPFlagsPayload(masked[0]) && (op == "==" || op == "!=") {
			if f.proto != "" && f.proto != "tcp" {
				return no, false
			}
			mask, okMask := nftTCPFlagBits(masked[1])
			comp, okComp := nftTCPFlagBits(spec["right"])
			if okMask && okComp {
				return negate(tcpFlags{mask: mask, comp: comp}.match(f), negated), false
			}
		}
		return maybe, false
	}
	// A header match carries an implicit protocol dependency: on a packet of
	// another family or layer 4 protocol it never matches, negated or not.
	if payload, ok := left["payload"].(map[string]any); ok {
		protocol, _ := payload["protocol"].(string)
		field, _ := payload["field"].(string)
		switch protocol {
		case "ip6", "icmpv6":
			return no, false
		case "tcp", "udp", "udplite", "sctp", "dccp", "icmp":
			if f.proto != "" && f.proto != protocol {
				return no, false
			}
		}
		switch {
		case field == "dport" && (protocol == "tcp" || protocol == "udp" || protocol == "th") && (op == "==" || op == "!=" || op == "in"):
			if spans, ok := nftPorts(spec["right"], lookup); ok {
				return negate(portMatch(spans, f), negated), false
			}
		case field == "flags" && protocol == "tcp":
			if flags, ok := nftTCPFlagBits(spec["right"]); ok {
				switch op {
				case "in": // tcp flags syn: any of these bits is set
					if !f.ct.known || !f.ct.isNew {
						return maybe, false
					}
					switch {
					case flags&tcpFlagSYN != 0:
						return yes, false
					case flags&ecnTCPFlags != 0:
						return maybe, false
					}
					return no, false
				case "==", "!=":
					return negate(tcpFlags{mask: everyTCPFlag, comp: flags}.match(f), negated), false
				}
			}
		}
		switch protocol {
		case "icmp":
			if field == "type" && (op == "==" || op == "!=" || op == "in") {
				if values, ok := nftResolve(spec["right"], lookup); ok {
					return negate(icmpTypeMatch(values, f), negated), false
				}
			}
		case "ip":
			if field == "saddr" && (op == "==" || op == "!=" || op == "in") {
				if prefixes, ok := nftPrefixes(spec["right"], lookup); ok {
					return negate(sourceMatch(prefixes, f), negated), false
				}
			}
			if field == "protocol" && (op == "==" || op == "!=" || op == "in") {
				if values, ok := nftResolve(spec["right"], lookup); ok {
					return negate(protoMatch(values, f), negated), false
				}
			}
		}
		return maybe, false
	}
	if op != "==" && op != "!=" && op != "in" {
		return maybe, false
	}
	if key := nftMetaKey(left); key != "" {
		values, ok := nftResolve(spec["right"], lookup)
		if !ok {
			return maybe, false
		}
		switch key {
		case "iifname", "iif":
			return negate(matchAnyInterface(values, f.in, '*'), negated), false
		case "oifname", "oif":
			return negate(matchAnyInterface(values, f.out, '*'), negated), false
		case "l4proto":
			return negate(protoMatch(values, f), negated), false
		case "nfproto", "protocol":
			// IPv4 flows only: ipv4 (nfproto) or ip (ether protocol).
			result := no
			for _, value := range values {
				if value == "ipv4" || value == "ip" {
					result = yes
				}
			}
			return negate(result, negated), false
		}
		return maybe, false
	}
	if fib, ok := left["fib"].(map[string]any); ok {
		flags, _ := fib["flags"].([]any)
		if result, _ := fib["result"].(string); result == "type" && len(flags) == 1 && flags[0] == "daddr" {
			if values, ok := nftResolve(spec["right"], lookup); ok {
				return negate(addressTypeMatch(values, f), negated), false
			}
		}
		return maybe, false
	}
	if ct, ok := left["ct"].(map[string]any); ok && ct["key"] == "state" {
		values, ok := nftResolve(spec["right"], lookup)
		if !ok {
			return maybe, false
		}
		if !negated && len(values) == 1 && values[0] == "invalid" {
			if f.ct.known {
				return no, false
			}
			return maybe, true
		}
		return stateMatch(values, negated, f), false
	}
	if ct, ok := left["ct"].(map[string]any); ok && ct["key"] == "status" {
		values, ok := nftResolve(spec["right"], lookup)
		if !ok {
			return maybe, false
		}
		// Status flags: "ct status dnat" tests the DNAT bit.
		if len(values) == 1 && values[0] == "dnat" {
			return negate(f.dnat, negated), false
		}
		return maybe, false
	}
	return maybe, false
}

func nftTCPFlagsPayload(value any) bool {
	expression, _ := value.(map[string]any)
	payload, _ := expression["payload"].(map[string]any)
	return payload["protocol"] == "tcp" && payload["field"] == "flags"
}

// nftTCPFlagBits reads "syn", ["syn", "ack"] or {"|": ["fin", "syn"]}.
func nftTCPFlagBits(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		bit, ok := tcpFlagNames[v]
		return bit, ok && v != "all" && v != "none"
	case []any:
		flags := 0
		for _, item := range v {
			bit, ok := nftTCPFlagBits(item)
			if !ok {
				return 0, false
			}
			flags |= bit
		}
		return flags, true
	case map[string]any:
		if items, ok := v["|"].([]any); ok && len(v) == 1 {
			return nftTCPFlagBits(items)
		}
	}
	return 0, false
}

// nftPorts reads a port, a range, an anonymous set of those or a named set.
func nftPorts(value any, lookup func(string) ([]any, bool)) ([]portSpan, bool) {
	switch v := value.(type) {
	case float64:
		if v < 0 || v > 65535 || v != float64(int(v)) {
			return nil, false
		}
		return []portSpan{{int(v), int(v)}}, true
	case string:
		if strings.HasPrefix(v, "@") {
			elements, ok := lookup(v[1:])
			if !ok {
				return nil, false
			}
			return nftPorts(elements, lookup)
		}
	case []any:
		var spans []portSpan
		for _, item := range v {
			if name, ok := item.(string); ok && strings.HasPrefix(name, "@") {
				return nil, false // no nested references
			}
			part, ok := nftPorts(nftElementValue(item), lookup)
			if !ok {
				return nil, false
			}
			spans = append(spans, part...)
		}
		return spans, len(spans) > 0
	case map[string]any:
		if set, ok := v["set"]; ok && len(v) == 1 {
			return nftPorts(set, lookup)
		}
		if bounds, ok := v["range"].([]any); ok && len(bounds) == 2 && len(v) == 1 {
			lo, okLo := nftPorts(bounds[0], lookup)
			hi, okHi := nftPorts(bounds[1], lookup)
			if okLo && okHi && len(lo) == 1 && len(hi) == 1 && lo[0].lo <= hi[0].lo {
				return []portSpan{{lo[0].lo, hi[0].lo}}, true
			}
		}
	}
	return nil, false
}

// nftPrefixes reads IPv4 addresses, prefixes, ranges, anonymous sets of those
// and named sets.
func nftPrefixes(value any, lookup func(string) ([]any, bool)) ([]netip.Prefix, bool) {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "@") {
			elements, ok := lookup(v[1:])
			if !ok {
				return nil, false
			}
			return nftPrefixes(elements, lookup)
		}
		prefix, ok := parsePrefix(v)
		return []netip.Prefix{prefix}, ok
	case []any:
		var result []netip.Prefix
		for _, item := range v {
			if name, ok := item.(string); ok && strings.HasPrefix(name, "@") {
				return nil, false
			}
			part, ok := nftPrefixes(nftElementValue(item), lookup)
			if !ok {
				return nil, false
			}
			result = append(result, part...)
		}
		return result, len(result) > 0
	case map[string]any:
		if set, ok := v["set"]; ok && len(v) == 1 {
			return nftPrefixes(set, lookup)
		}
		if spec, ok := v["prefix"].(map[string]any); ok && len(v) == 1 {
			addr, _ := spec["addr"].(string)
			length, ok := spec["len"].(float64)
			if !ok {
				return nil, false
			}
			prefix, ok := parsePrefix(fmt.Sprintf("%s/%d", addr, int(length)))
			return []netip.Prefix{prefix}, ok
		}
		if bounds, ok := v["range"].([]any); ok && len(bounds) == 2 && len(v) == 1 {
			lo, _ := bounds[0].(string)
			hi, _ := bounds[1].(string)
			from, errLo := netip.ParseAddr(lo)
			to, errHi := netip.ParseAddr(hi)
			if errLo != nil || errHi != nil || !from.Is4() || !to.Is4() || to.Less(from) {
				return nil, false
			}
			// A range is kept exact as the prefixes that cover it.
			var result []netip.Prefix
			for addr := from; ; {
				bits := 32
				for bits > 0 {
					candidate := netip.PrefixFrom(addr, bits-1).Masked()
					last := lastAddr(candidate)
					if candidate.Addr() != addr || to.Less(last) {
						break
					}
					bits--
				}
				prefix := netip.PrefixFrom(addr, bits)
				result = append(result, prefix)
				last := lastAddr(prefix)
				if last == to || len(result) > 64 {
					return result, last == to
				}
				addr = last.Next()
			}
		}
	}
	return nil, false
}

func lastAddr(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Masked().Addr().As4()
	value := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	value |= ^uint32(0) >> uint(prefix.Bits()) // shifting by 32 gives 0
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}

// firewalldZones finds the zones of the LAN interfaces in firewalld's input
// dispatch (filter_INPUT_POLICIES since 1.0, filter_INPUT_ZONES before): the
// first jump or goto to a zone chain that applies to the interface by name
// or unconditionally (the default zone). Rules with any other match (source
// zones) are skipped. Unknown LAN interfaces give none.
func firewalldZones(ruleset *nftRuleset, lan []string) []string {
	var zones []string
	seen := map[string]bool{}
	for _, iface := range lan {
		zone := ""
		for _, name := range []string{"filter_INPUT_POLICIES", "filter_INPUT_ZONES"} {
			chain := ruleset.chains[nftKey("inet", "firewalld", name)]
			if chain == nil {
				continue
			}
			for _, expressions := range chain.rules {
				if zone = firewalldZoneRule(expressions, iface); zone != "" {
					break
				}
			}
			if zone != "" {
				break
			}
		}
		if zone != "" && !seen[zone] {
			seen[zone] = true
			zones = append(zones, zone)
		}
	}
	return zones
}

func firewalldZoneRule(expressions []any, iface string) string {
	for _, raw := range expressions {
		expression, _ := raw.(map[string]any)
		if body, ok := expression["match"].(map[string]any); ok {
			left, _ := body["left"].(map[string]any)
			value, _ := body["right"].(string)
			if body["op"] != "==" || nftMetaKey(left) != "iifname" || value != iface {
				return ""
			}
			continue
		}
		for _, verdict := range []string{"jump", "goto"} {
			spec, ok := expression[verdict].(map[string]any)
			if !ok {
				continue
			}
			target, _ := spec["target"].(string)
			zone := strings.TrimPrefix(target, "filter_IN_")
			if zone == target || strings.HasPrefix(zone, "policy_") || !nftNamePattern.MatchString(zone) {
				return ""
			}
			return zone
		}
		return ""
	}
	return ""
}
