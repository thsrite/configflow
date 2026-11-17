package routers

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// ROSResource identifies one previewed object with its complete stable config.
type ROSResource struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Config Row    `json:"config"`
}

func resource(path string, row Row) ROSResource {
	return ROSResource{path, row.id(), configROSRow(row)}
}

// NATLists is the two NAT lists and outside firewall references. Nil reference
// slices mean that the corresponding table could not be read.
type NATLists struct {
	Src          []Row     `json:"src"`
	Dst          []Row     `json:"dst"`
	MangleRefs   []ListRef `json:"mangle_refs"`
	FirewallRefs []ListRef `json:"firewall_refs"`
}

func (l *NATLists) rows(name string) []Row {
	if name == NATSrcList {
		return l.Src
	}
	return l.Dst
}
func (l *NATLists) refs() []ListRef { return append(slices.Clone(l.MangleRefs), l.FirewallRefs...) }

// NATPolicyEvidence describes the part of a source-policy transition relevant
// to NAT-list planning. R08 supplies this from its typed policy plan.
type NATPolicyEvidence struct {
	Migrations                         []ROSResource
	Replacements                       []ROSReplacement
	PreviousResources                  []ROSResource
	Destinations, PreviousDestinations []string
}

// ROSReplacement is an object a policy transition creates in place of a migration.
type ROSReplacement struct {
	Path   string `json:"path"`
	Values Row    `json:"values"`
}

// NATListSync is the complete list delta decided by a preview.
type NATListSync struct {
	OpHead
	Comment      string        `json:"comment"`
	SrcList      string        `json:"src_list"`
	DstList      string        `json:"dst_list"`
	Rule         *ROSResource  `json:"rule"`
	OutInterface string        `json:"out_interface"`
	ReplaceRule  bool          `json:"replace_rule"`
	Sources      []string      `json:"sources"`
	Destinations []string      `json:"destinations"`
	SrcAdd       []string      `json:"src_add"`
	SrcRemove    []ROSResource `json:"src_remove"`
	DstAdd       []string      `json:"dst_add"`
	DstRemove    []ROSResource `json:"dst_remove"`
	Legacy       []ROSResource `json:"legacy"`
	Stale        []ROSResource `json:"stale"`
	Withdraw     bool          `json:"withdraw"`
}

// NATListMembers is the ConfigFlow-owned member keys remaining after a stage.
type NATListMembers struct {
	Src []string `json:"src"`
	Dst []string `json:"dst"`
}

// NATListCheck proves shape, removed legacy IDs, and exact owned members.
type NATListCheck struct {
	Rule         *string        `json:"rule"`
	OutInterface string         `json:"out_interface"`
	Sources      []string       `json:"sources"`
	Destinations []string       `json:"destinations"`
	Removed      []string       `json:"removed"`
	Members      NATListMembers `json:"members"`
	Withdraw     bool           `json:"withdraw"`
}

// NATListPlan is nil when the existing per-rule plan and reads stay unchanged.
type NATListPlan struct {
	Ops      []Op             `json:"ops"`
	Blockers []string         `json:"blockers"`
	Warnings []string         `json:"warnings"`
	Evidence *NATListEvidence `json:"evidence"`
	Check    *NATListCheck    `json:"check"`
	Exempt   []string         `json:"exempt"`
}

// NATListEvidence retains stable list snapshots in a preview fingerprint.
type NATListEvidence struct {
	Src []Row `json:"src"`
	Dst []Row `json:"dst"`
}

func natKey(value string) string {
	value = strings.TrimSpace(value)
	if p, ok := parsePrefix4(value); ok {
		return p.String()
	}
	return value
}
func parsePrefix4(value string) (netip.Prefix, bool) {
	p := Networks([]string{value})
	if len(p) == 1 {
		return p[0], true
	}
	return netip.Prefix{}, false
}
func natMembers(name string, rows []Row) (ours, foreign []Row, problems []string) {
	ours, foreign, problems = []Row{}, []Row{}, []string{}
	for _, row := range rows {
		switch {
		case row.flag("dynamic") || row.set("timeout"):
			problems = append(problems, fmt.Sprintf(messageNATListDynamic, name))
		case !row.enabled():
			problems = append(problems, fmt.Sprintf(messageNATListDisabled, name, row.text("address")))
		case row.is("comment", NATListComment):
			ours = append(ours, row)
		default:
			foreign = append(foreign, row)
		}
	}
	return ours, foreign, uniqueStrings(problems)
}
func overlapsAny(network netip.Prefix, others []netip.Prefix) bool {
	return slices.ContainsFunc(others, network.Overlaps)
}
func viaGateway(gateway, value string) bool {
	for _, part := range strings.Split(value, ",") {
		part, _, _ = strings.Cut(part, "%")
		part, _, _ = strings.Cut(part, "@")
		if strings.TrimSpace(part) == gateway {
			return true
		}
	}
	return false
}

// StillRoutedNAT returns destinations still reaching the proxy after route
// deletions. Nil means unreadable evidence and prevents stale-rule deletion.
func StillRoutedNAT(report *Report, state *RouterState, gateway string) []netip.Prefix {
	if state.NatContext == nil || state.NatContext.Routes == nil {
		return nil
	}
	out := []netip.Prefix{}
	deleting := map[string]bool{}
	for _, extra := range report.Routes.Extra {
		deleting[extra.RouteID] = true
	}
	policy := state.NATPolicy
	everything := netip.MustParsePrefix("0.0.0.0/0")
	for _, row := range state.NatContext.Routes {
		if !row.enabled() || !viaGateway(gateway, row.text("gateway")) || deleting[row.id()] {
			continue
		}
		owned := false
		if policy != nil {
			for _, item := range policy.PreviousResources {
				if item.Path == rosRoutes && item.ID == row.id() {
					owned = true
					break
				}
			}
		}
		if owned {
			out = append(out, Networks(append(slices.Clone(policy.Destinations), policy.PreviousDestinations...))...)
			continue
		}
		networks := Networks([]string{row.text("dst-address")})
		if len(networks) == 0 && !strings.Contains(row.text("dst-address"), ":") {
			networks = []netip.Prefix{everything}
		}
		out = append(out, networks...)
	}
	for _, entry := range append(slices.Clone(state.PolicyRoutes), state.RetiredPolicyRoutes...) {
		if entry.Enabled && viaGateway(gateway, entry.Gateway) {
			if len(entry.Dst) == 0 {
				out = append(out, everything)
			} else {
				out = append(out, Networks(entry.Dst)...)
			}
		}
	}
	for _, row := range state.NatContext.Mangle {
		if row.enabled() && row.is("action", "route") && viaGateway(gateway, row.text("route-dst")) {
			out = append(out, everything)
		}
	}
	return out
}

func legacyNAT(rows []Row, sources []string, items []NatItem, desired, skipped []string, excluded map[string]bool, egress string, routed []netip.Prefix) (merged []ROSResource, destinations []string, stale, retained []ROSResource) {
	merged, destinations, stale, retained = []ROSResource{}, []string{}, []ROSResource{}, []ROSResource{}
	blocked, proxied := Networks(skipped), Networks(desired)
	wanted := map[string]string{}
	for _, item := range items {
		if item.Status == "synced" || item.Status == "missing" {
			wanted[natKey(item.CIDR)] = item.CIDR
		}
	}
	sourceKeys := map[string]bool{}
	for _, s := range sources {
		sourceKeys[natKey(s)] = true
	}
	groups := map[string][]Row{}
	order := []string{}
	fields := stringSet("chain", "action", "disabled", "comment", "log", "log-prefix", "dynamic", "src-address", "dst-address", "out-interface")
	for _, row := range rows {
		if !row.is("comment", PolicyAutoComment) || excluded[row.id()] || !row.is("chain", "srcnat") || !row.is("action", "accept") || !row.enabled() || row.flag("dynamic", "invalid", "log") || row.set("log-prefix") || !row.set("src-address") || !row.set("dst-address") || row.text("out-interface") != "" && row.text("out-interface") != egress || !sourceKeys[natKey(row.text("src-address"))] {
			continue
		}
		extra := false
		for key := range configROSRow(row) {
			if !fields[key] {
				extra = true
			}
		}
		if extra {
			continue
		}
		key := natKey(row.text("dst-address"))
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	selected, old, kept := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, key := range order {
		group := groups[key]
		found, interfaces := map[string]bool{}, map[string]bool{}
		for _, row := range group {
			found[natKey(row.text("src-address"))] = true
			interfaces[row.text("out-interface")] = true
		}
		if len(found) != len(sourceKeys) || len(interfaces) != 1 {
			continue
		}
		network, ok := parsePrefix4(key)
		if !ok {
			continue
		}
		if cidr, ok := wanted[key]; ok {
			if !overlapsAny(network, blocked) {
				for _, row := range group {
					selected[row.id()] = true
				}
				destinations = append(destinations, cidr)
			}
		} else if !overlapsAny(network, proxied) {
			target := kept
			if routed != nil && !overlapsAny(network, routed) {
				target = old
			}
			for _, row := range group {
				target[row.id()] = true
			}
		}
	}
	for _, row := range rows {
		if selected[row.id()] {
			merged = append(merged, resource(rosNAT, row))
		}
		if old[row.id()] {
			stale = append(stale, resource(rosNAT, row))
		}
		if kept[row.id()] {
			retained = append(retained, resource(rosNAT, row))
		}
	}
	return
}

// ListsOfNAT performs a deferred scoped read at most once for this state.
func ListsOfNAT(ctx context.Context, state *RouterState) *NATLists {
	if state.NATLists == nil && state.NATListsReader != nil {
		read := state.NATListsReader
		state.NATListsReader = nil
		state.NATLists, _ = read(ctx)
	}
	return state.NATLists
}

// PlanNATLists plans member deltas and provable legacy migration without
// changing the legacy read path when no list exemption is needed.
func PlanNATLists(ctx context.Context, report *Report, state *RouterState, gateway string, skipped []string) *NATListPlan {
	if state.NatContext == nil {
		return nil
	}
	rows := state.NatContext.Rules
	items := []NatItem{}
	missing, desired := []string{}, []string{}
	for _, item := range report.NatBypass.Items {
		desired = append(desired, item.CIDR)
		if !slices.Contains(skipped, item.CIDR) {
			items = append(items, item)
			if item.Status == "missing" {
				missing = append(missing, item.CIDR)
			}
		}
	}
	refs := []ListRef{}
	if state.NATLists != nil {
		refs = state.NATLists.refs()
	}
	found := InspectNATListRule(rows, refs)
	moving := map[string]bool{}
	movingAddress := map[string]bool{}
	if p := state.NATPolicy; p != nil {
		for _, item := range p.Migrations {
			if item.Path == rosNAT {
				moving[item.ID] = true
			}
			if item.Path == rosAddresses {
				movingAddress[item.ID] = true
			}
		}
	}
	sources := []string{}
	gatewayAddr, _ := netip.ParseAddr(gateway)
	for _, network := range ClientSourceNetworks(gatewayAddr) {
		sources = append(sources, network.String())
	}
	legacy, legacyDest, stale, retained := legacyNAT(rows, sources, items, desired, skipped, moving, found.OutInterface, StillRoutedNAT(report, state, gateway))
	if found.Rule == nil && found.Problem == "" && len(missing) == 0 && len(legacy) == 0 {
		return nil
	}
	result := &NATListPlan{Ops: []Op{}, Blockers: []string{}, Warnings: []string{}, Exempt: []string{}}
	lists := ListsOfNAT(ctx, state)
	if lists == nil {
		result.Blockers = append(result.Blockers, messageNATListsUnreadable)
		return result
	}
	found = InspectNATListRule(rows, lists.refs())
	evidence := func(rows []Row) []Row {
		out := []Row{}
		for _, row := range rows {
			v := configROSRow(row)
			v[".id"] = row[".id"]
			out = append(out, v)
		}
		return out
	}
	result.Evidence = &NATListEvidence{evidence(lists.Src), evidence(lists.Dst)}
	if lists.MangleRefs == nil {
		result.Blockers = append(result.Blockers, messageNATMangleUnreadable)
	}
	if lists.FirewallRefs == nil {
		result.Blockers = append(result.Blockers, messageNATFirewallUnreadable)
	}
	if found.Problem != "" {
		result.Blockers = append(result.Blockers, found.Problem)
	}
	srcOwn, srcForeign, problems := natMembers(NATSrcList, lists.Src)
	dstOwn, dstForeign, more := natMembers(NATDstList, lists.Dst)
	result.Blockers = append(result.Blockers, append(problems, more...)...)
	for i, foreign := range [][]Row{srcForeign, dstForeign} {
		name := []string{NATSrcList, NATDstList}[i]
		if len(foreign) > 0 {
			if found.Rule == nil {
				result.Blockers = append(result.Blockers, fmt.Sprintf(messageNATListForeign, name))
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf(messageNATListForeignKept, name, len(foreign)))
			}
		}
	}
	if len(result.Blockers) > 0 {
		return result
	}
	if len(retained) > 0 {
		values := []string{}
		for _, item := range retained {
			values = append(values, item.Config.text("dst-address"))
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf(messageNATLegacyKept, len(retained), natSample(uniqueStrings(values))))
	}
	ruleID := ""
	var rule *ROSResource
	if found.Rule != nil {
		v := resource(rosNAT, found.Rule)
		rule = &v
		ruleID = v.ID
	}
	covered := []string{}
	for _, item := range items {
		if item.Status == "synced" && rule != nil && slices.Contains(item.RuleIDs, ruleID) {
			covered = append(covered, item.CIDR)
		}
	}
	targets := []string{}
	targetKeys := map[string]bool{}
	for _, cidr := range append(append(slices.Clone(missing), covered...), legacyDest...) {
		key := natKey(cidr)
		if !targetKeys[key] {
			targetKeys[key] = true
			targets = append(targets, cidr)
		}
	}
	held := Networks(skipped)
	holds := false
	for _, row := range dstOwn {
		for _, network := range Networks([]string{row.text("address")}) {
			holds = holds || overlapsAny(network, held)
		}
	}
	withdraw := rule != nil && len(targets) == 0 && !holds
	if withdraw {
		stale = []ROSResource{}
	}
	split := func(ours, foreign []Row, keep []string) ([]ROSResource, map[string]bool, []string) {
		present, kept := map[string]bool{}, []string{}
		for _, row := range foreign {
			present[natKey(row.text("address"))] = true
		}
		removing := []ROSResource{}
		keepKeys := map[string]bool{}
		for _, value := range keep {
			keepKeys[natKey(value)] = true
		}
		for _, row := range ours {
			if movingAddress[row.id()] {
				continue
			}
			key := natKey(row.text("address"))
			if withdraw || !keepKeys[key] || present[key] {
				removing = append(removing, resource(rosAddresses, row))
			} else {
				present[key] = true
				kept = append(kept, key)
			}
		}
		return removing, present, kept
	}
	srcRemove, srcPresent, srcKept := split(srcOwn, srcForeign, sources)
	dstRemove, dstPresent, dstKept := split(dstOwn, dstForeign, desired)
	srcAdd, dstAdd := []string{}, []string{}
	if !withdraw {
		for _, value := range sources {
			if !srcPresent[natKey(value)] {
				srcAdd = append(srcAdd, value)
			}
		}
	}
	for _, value := range targets {
		if !dstPresent[natKey(value)] {
			dstAdd = append(dstAdd, value)
		}
	}
	after := func(kept, added []string) []string {
		for _, value := range added {
			kept = append(kept, natKey(value))
		}
		slices.Sort(kept)
		return slices.Compact(kept)
	}
	natRules := map[string]*NatBypassEntry{}
	for _, entry := range state.NatBypass {
		if entry.Enabled && !moving[entry.ID] {
			natRules[entry.ID] = entry
		}
	}
	adoptIDs := []string{}
	for _, item := range items {
		if item.Status == "adoptable" {
			for _, id := range item.RuleIDs {
				if entry := natRules[id]; entry != nil && !IsManaged(entry.Comment) && !slices.Contains(adoptIDs, id) {
					adoptIDs = append(adoptIDs, id)
				}
			}
		}
	}
	for _, id := range adoptIDs {
		entry := natRules[id]
		comment := ManagedComment(entry.Comment)
		label := firstNonEmpty(entry.Comment, id)
		result.Ops = append(result.Ops, &NATUpdateOp{OpHead: OpHead{Kind: "nat_update", Summary: fmt.Sprintf(messageNATAdopt, label, comment)}, ID: id, AddDst: []string{}, Comment: comment, Expected: slices.Clone(entry.Attributes)})
	}
	create := rule == nil && len(targets) > 0
	if create || withdraw || len(srcAdd)+len(srcRemove)+len(dstAdd)+len(dstRemove)+len(legacy)+len(stale) > 0 {
		op := NATListSync{OpHead: OpHead{Kind: "nat_list_sync"}, Comment: NATListComment, SrcList: NATSrcList, DstList: NATDstList, Rule: rule, OutInterface: found.OutInterface, Sources: sources, Destinations: targets, SrcAdd: srcAdd, SrcRemove: srcRemove, DstAdd: dstAdd, DstRemove: dstRemove, Legacy: legacy, Stale: stale, Withdraw: withdraw}
		legacyKeys := map[string]bool{}
		for _, cidr := range legacyDest {
			legacyKeys[natKey(cidr)] = true
		}
		fresh := []string{}
		for _, cidr := range targets {
			if !legacyKeys[natKey(cidr)] {
				fresh = append(fresh, cidr)
			}
		}
		op.Summary = NATListSummary(op, gateway, create, fresh)
		result.Ops = append(result.Ops, &op)
	}
	removed := []string{}
	for _, item := range append(slices.Clone(legacy), stale...) {
		removed = append(removed, item.ID)
	}
	var checkID *string
	if rule != nil {
		checkID = &ruleID
	}
	result.Check = &NATListCheck{checkID, found.OutInterface, sources, targets, removed, NATListMembers{after(srcKept, srcAdd), after(dstKept, dstAdd)}, withdraw}
	gone := stringSet(removed...)
	removedKeys := map[string]bool{}
	for _, item := range dstRemove {
		removedKeys[natKey(item.Config.text("address"))] = true
	}
	for _, entry := range state.NatBypass {
		if !entry.Enabled || moving[entry.ID] || gone[entry.ID] || entry.ID == ruleID && withdraw {
			continue
		}
		for _, cidr := range entry.Dst {
			if entry.ID != ruleID || !removedKeys[natKey(cidr)] {
				result.Exempt = append(result.Exempt, cidr)
			}
		}
	}
	result.Exempt = append(result.Exempt, targets...)
	if p := state.NATPolicy; p != nil {
		for _, item := range p.Replacements {
			if item.Path == rosNAT {
				result.Exempt = append(result.Exempt, item.Values.text("dst-address"))
			}
		}
	}
	return result
}

func natSample(values []string) string {
	value := strings.Join(values[:min(5, len(values))], "、")
	if len(values) > 5 {
		value += fmt.Sprintf(messageNATSampleCount, len(values))
	}
	return value
}

// NATListSummary renders the exact user-facing operation summary.
func NATListSummary(op NATListSync, gateway string, create bool, fresh []string) string {
	if op.Withdraw {
		return messageNATWithdraw
	}
	parts := []string{}
	head := ""
	if create {
		target := fmt.Sprintf(messageNATTarget, gateway, gateway, len(op.Sources), len(op.Destinations))
		if len(op.Legacy) > 0 {
			head = fmt.Sprintf(messageNATMerge, len(op.Legacy), target)
			if len(fresh) > 0 {
				parts = append(parts, fmt.Sprintf(messageNATFresh, len(fresh), natSample(fresh)))
			}
			parts = append(parts, messageNATWhyMerge+messageNATCreateProve)
		} else {
			head = fmt.Sprintf(messageNATCreate, target)
		}
	} else {
		changes := []string{}
		if len(op.DstAdd) > 0 {
			changes = append(changes, fmt.Sprintf(messageNATAdd, len(op.DstAdd), natSample(op.DstAdd)))
		}
		if len(op.DstRemove) > 0 {
			values := []string{}
			for _, item := range op.DstRemove {
				values = append(values, item.Config.text("address"))
			}
			changes = append(changes, fmt.Sprintf(messageNATRemove, len(op.DstRemove), natSample(values)))
		}
		if len(changes) > 0 {
			head = messageNATDestination + strings.Join(changes, "、")
		}
		if len(op.SrcAdd)+len(op.SrcRemove) > 0 {
			parts = append(parts, fmt.Sprintf(messageNATSources, gateway, len(op.SrcAdd), len(op.SrcRemove)))
		}
		if len(op.Legacy) > 0 {
			parts = append(parts, fmt.Sprintf(messageNATMergeExisting, len(op.Legacy), messageNATWhyMerge))
		}
	}
	if create && len(op.DstRemove) > 0 {
		parts = append(parts, fmt.Sprintf(messageNATResidual, len(op.DstRemove)))
	}
	if len(op.Stale) > 0 {
		values := []string{}
		for _, item := range op.Stale {
			values = append(values, item.Config.text("dst-address"))
		}
		parts = append(parts, fmt.Sprintf(messageNATStale, len(op.Stale), natSample(uniqueStrings(values))))
	}
	return strings.Join(nonEmpty(append([]string{head}, parts...)), "；")
}

// NATRemovalsFirst moves pruning and stale exemptions before route deletions.
func NATRemovalsFirst(natOps, routeOps []Op, gateway string) ([]Op, []Op) {
	hasDelete := slices.ContainsFunc(routeOps, func(op Op) bool { return op.OpKind() == "route_delete" })
	if !hasDelete {
		return natOps, routeOps
	}
	early, rest := []Op{}, []Op{}
	for _, original := range natOps {
		sync, ok := original.(*NATListSync)
		if !ok || sync.ReplaceRule {
			rest = append(rest, original)
			continue
		}
		op := *sync
		if op.Withdraw {
			early = append(early, original)
			continue
		}
		pruning := []ROSResource{}
		if op.Rule != nil {
			pruning = op.DstRemove
		}
		if len(pruning)+len(op.Stale) == 0 {
			rest = append(rest, original)
			continue
		}
		prune := op
		prune.SrcAdd, prune.SrcRemove, prune.DstAdd, prune.Legacy, prune.Destinations, prune.DstRemove = []string{}, []ROSResource{}, []string{}, []ROSResource{}, []string{}, pruning
		prune.Summary = NATListSummary(prune, gateway, false, nil) + messageNATBeforeRoutes
		early = append(early, &prune)
		if len(pruning) > 0 {
			op.DstRemove = []ROSResource{}
		}
		op.Stale = []ROSResource{}
		if op.Rule == nil || len(op.SrcAdd)+len(op.SrcRemove)+len(op.DstAdd)+len(op.DstRemove)+len(op.Legacy) > 0 {
			fresh := []string{}
			for _, cidr := range op.Destinations {
				if !slices.ContainsFunc(op.Legacy, func(item ROSResource) bool { return natKey(item.Config.text("dst-address")) == natKey(cidr) }) {
					fresh = append(fresh, cidr)
				}
			}
			op.Summary = NATListSummary(op, gateway, op.Rule == nil, fresh)
			rest = append(rest, &op)
		}
	}
	return rest, append(early, routeOps...)
}

// RestrictNATEgress narrows a list rule before a source policy is withdrawn.
func RestrictNATEgress(op *NATListSync, check *NATListCheck, iface string) string {
	if op.Withdraw || op.OutInterface == iface {
		return ""
	}
	if op.Rule != nil && op.OutInterface != "" {
		return fmt.Sprintf(messageNATEgressConflict, op.OutInterface, iface)
	}
	if op.Rule != nil {
		op.ReplaceRule = true
		check.Removed = append(check.Removed, op.Rule.ID)
	}
	op.OutInterface, check.OutInterface = iface, iface
	op.Summary += fmt.Sprintf(messageNATEgressNarrow, iface)
	return ""
}

// NATPolicyMigrations selects only provably owned overlapping destination members.
func NATPolicyMigrations(rule Row, addresses []Row, exclusions []string) ([]ROSResource, error) {
	result := []ROSResource{}
	for _, item := range addresses {
		if item.text("list") != rule.text("dst-address-list") || !item.enabled() {
			continue
		}
		networks := Networks([]string{item.text("address")})
		if len(networks) == 0 {
			return nil, routerError(fmt.Sprintf(messageNATSplitInvalid, item.id()))
		}
		overlap := false
		for _, n := range networks {
			overlap = overlap || overlapsAny(n, Networks(exclusions))
		}
		if !overlap {
			continue
		}
		if !item.is("comment", NATListComment) || item.flag("dynamic") {
			return nil, routerError(fmt.Sprintf(messageNATSplitForeign, item.id()))
		}
		result = append(result, resource(rosAddresses, item))
	}
	return result, nil
}
