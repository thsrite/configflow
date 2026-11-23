package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"configflow/internal/netipx"
)

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

// natKey is one member or CIDR as RouterOS compares it: 192.0.2.1 and
// 192.0.2.1/32 are one entry. Only a single IPv4 network is normalised; a range
// such as 192.0.2.0-192.0.2.255 keeps its text, because RouterOS stores it as a
// different entry and the earlier per-destination writer never produced one.
func natKey(value string) string {
	value = strings.TrimSpace(value)
	if p, ok := netipx.ParseNetwork4(value, false); ok {
		return p.String()
	}
	return value
}
func natMembers(name string, rows []Row) (ours, foreign []Row, problems []string) {
	ours, foreign, problems = []Row{}, []Row{}, []string{}
	for _, row := range rows {
		switch {
		case row.Flag("dynamic") || row.Set("timeout"):
			problems = append(problems, fmt.Sprintf(messageNATListDynamic, name))
		case !row.Enabled():
			problems = append(problems, fmt.Sprintf(messageNATListDisabled, name, row.Text("address")))
		case row.Is("comment", NATListComment):
			ours = append(ours, row)
		default:
			foreign = append(foreign, row)
		}
	}
	return ours, foreign, device.Unique(problems)
}
func overlapsAny(network netip.Prefix, others []netip.Prefix) bool {
	return slices.ContainsFunc(others, network.Overlaps)
}

// prefixOverlap answers "overlaps any of networks" by bisection over the
// collapsed, sorted networks, like Python's ros_policy.network_overlap: a
// policy migration checks thousands of members against thousands of
// exclusions. R08 may replace it by its exported ROSPolicyNetworkOverlap.
func prefixOverlap(networks []netip.Prefix) func(netip.Prefix) bool {
	collapsed, err := netipx.Collapse(networks)
	if err != nil {
		return func(network netip.Prefix) bool { return overlapsAny(network, networks) }
	}
	return func(network netip.Prefix) bool {
		last := netipx.Broadcast(network)
		// The last collapsed network starting at or before network's end is
		// the only candidate; it overlaps when it ends at or after the start.
		i := sort.Search(len(collapsed), func(i int) bool { return last.Less(collapsed[i].Addr()) })
		return i > 0 && !netipx.Broadcast(collapsed[i-1]).Less(network.Masked().Addr())
	}
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
func StillRoutedNAT(routes RoutesReport, facts *device.State, nat *NatContext, policyPlan *ROSPolicyPlan, gateway string) []netip.Prefix {
	if nat == nil || nat.Routes == nil {
		return nil
	}
	out := []netip.Prefix{}
	deleting := map[string]bool{}
	for _, extra := range routes.Extra {
		deleting[extra.RouteID] = true
	}
	policy := natPolicyEvidence(policyPlan)
	everything := netip.MustParsePrefix("0.0.0.0/0")
	for _, row := range nat.Routes {
		if !row.Enabled() || !viaGateway(gateway, row.Text("gateway")) || deleting[row.ID()] {
			continue
		}
		owned := false
		if policy != nil {
			for _, item := range policy.PreviousResources {
				if item.Path == ros.PathRoutes && item.ID == row.ID() {
					owned = true
					break
				}
			}
		}
		if owned {
			out = append(out, device.Networks(append(slices.Clone(policy.Destinations), policy.PreviousDestinations...))...)
			continue
		}
		networks := device.Networks([]string{row.Text("dst-address")})
		if len(networks) == 0 && !strings.Contains(row.Text("dst-address"), ":") {
			networks = []netip.Prefix{everything}
		}
		out = append(out, networks...)
	}
	var policyRoutes, retiredPolicyRoutes []*PolicyRouteEntry
	if facts != nil {
		policyRoutes, retiredPolicyRoutes = facts.PolicyRoutes, facts.RetiredPolicyRoutes
	}
	for _, entry := range append(slices.Clone(policyRoutes), retiredPolicyRoutes...) {
		if entry.Enabled && viaGateway(gateway, entry.Gateway) {
			if len(entry.Dst) == 0 {
				out = append(out, everything)
			} else {
				out = append(out, device.Networks(entry.Dst)...)
			}
		}
	}
	for _, row := range nat.Mangle {
		if row.Enabled() && row.Is("action", "route") && viaGateway(gateway, row.Text("route-dst")) {
			out = append(out, everything)
		}
	}
	return out
}

func legacyNAT(rows []Row, sources []string, items []NatItem, desired, skipped []string, excluded map[string]bool, egress string, routed []netip.Prefix) (merged []ROSResource, destinations []string, stale, retained []ROSResource) {
	merged, destinations, stale, retained = []ROSResource{}, []string{}, []ROSResource{}, []ROSResource{}
	blocked, proxied := device.Networks(skipped), device.Networks(desired)
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
	fields := device.StringSet("chain", "action", "disabled", "comment", "log", "log-prefix", "dynamic", "src-address", "dst-address", "out-interface")
	for _, row := range rows {
		if !row.Is("comment", PolicyAutoComment) || excluded[row.ID()] || !row.Is("chain", "srcnat") || !row.Is("action", "accept") || !row.Enabled() || row.Flag("dynamic", "invalid", "log") || row.Set("log-prefix") || !row.Set("src-address") || !row.Set("dst-address") || row.Text("out-interface") != "" && row.Text("out-interface") != egress || !sourceKeys[natKey(row.Text("src-address"))] {
			continue
		}
		extra := false
		for key := range ros.ConfigROSRow(row) {
			if !fields[key] {
				extra = true
			}
		}
		if extra {
			continue
		}
		key := natKey(row.Text("dst-address"))
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
			found[natKey(row.Text("src-address"))] = true
			interfaces[row.Text("out-interface")] = true
		}
		if len(found) != len(sourceKeys) || len(interfaces) != 1 {
			continue
		}
		network, ok := netipx.ParseNetwork4(key, true)
		if !ok {
			continue
		}
		if cidr, ok := wanted[key]; ok {
			if !overlapsAny(network, blocked) {
				for _, row := range group {
					selected[row.ID()] = true
				}
				destinations = append(destinations, cidr)
			}
		} else if !overlapsAny(network, proxied) {
			target := kept
			if routed != nil && !overlapsAny(network, routed) {
				target = old
			}
			for _, row := range group {
				target[row.ID()] = true
			}
		}
	}
	for _, row := range rows {
		if selected[row.ID()] {
			merged = append(merged, ros.NewResource(ros.PathNAT, row))
		}
		if old[row.ID()] {
			stale = append(stale, ros.NewResource(ros.PathNAT, row))
		}
		if kept[row.ID()] {
			retained = append(retained, ros.NewResource(ros.PathNAT, row))
		}
	}
	return
}

// ListsOfNAT performs a deferred scoped read at most once for this state.
func ListsOfNAT(ctx context.Context, lists **NATLists, reader *func(context.Context) (*NATLists, error)) *NATLists {
	if lists == nil {
		return nil
	}
	if *lists == nil && reader != nil && *reader != nil {
		read := *reader
		*reader = nil
		*lists, _ = read(ctx)
	}
	return *lists
}

// PlanNATLists plans member deltas and provable legacy migration without
// changing the legacy read path when no list exemption is needed.
func PlanNATLists(ctx context.Context, routes RoutesReport, natReport NatReport, facts *device.State, nat *NatContext, listsSlot **NATLists, readerSlot *func(context.Context) (*NATLists, error), policyPlan *ROSPolicyPlan, gateway string, skipped []string) *NATListPlan {
	if nat == nil {
		return nil
	}
	rows := nat.Rules
	items := []NatItem{}
	missing, desired := []string{}, []string{}
	for _, item := range natReport.Items {
		desired = append(desired, item.CIDR)
		if !slices.Contains(skipped, item.CIDR) {
			items = append(items, item)
			if item.Status == "missing" {
				missing = append(missing, item.CIDR)
			}
		}
	}
	refs := []ListRef{}
	if listsSlot != nil && *listsSlot != nil {
		refs = (*listsSlot).refs()
	}
	found := ros.InspectNATListRule(rows, refs)
	moving := map[string]bool{}
	movingAddress := map[string]bool{}
	if p := natPolicyEvidence(policyPlan); p != nil {
		for _, item := range p.Migrations {
			if item.Path == ros.PathNAT {
				moving[item.ID] = true
			}
			if item.Path == ros.PathAddresses {
				movingAddress[item.ID] = true
			}
		}
	}
	sources := []string{}
	gatewayAddr, _ := netip.ParseAddr(gateway)
	for _, network := range ros.ClientSourceNetworks(gatewayAddr) {
		sources = append(sources, network.String())
	}
	legacy, legacyDest, stale, retained := legacyNAT(rows, sources, items, desired, skipped, moving, found.OutInterface, StillRoutedNAT(routes, facts, nat, policyPlan, gateway))
	if found.Rule == nil && found.Problem == "" && len(missing) == 0 && len(legacy) == 0 {
		return nil
	}
	result := &NATListPlan{Ops: []Op{}, Blockers: []string{}, Warnings: []string{}, Exempt: []string{}}
	lists := ListsOfNAT(ctx, listsSlot, readerSlot)
	if lists == nil {
		result.Blockers = append(result.Blockers, messageNATListsUnreadable)
		return result
	}
	found = ros.InspectNATListRule(rows, lists.refs())
	evidence := func(rows []Row) []Row {
		out := []Row{}
		for _, row := range rows {
			v := ros.ConfigROSRow(row)
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
			values = append(values, item.Config.Text("dst-address"))
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf(messageNATLegacyKept, len(retained), natSample(device.Unique(values))))
	}
	ruleID := ""
	var rule *ROSResource
	if found.Rule != nil {
		v := ros.NewResource(ros.PathNAT, found.Rule)
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
	held := device.Networks(skipped)
	holds := false
	for _, row := range dstOwn {
		for _, network := range device.Networks([]string{row.Text("address")}) {
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
			present[natKey(row.Text("address"))] = true
		}
		removing := []ROSResource{}
		keepKeys := map[string]bool{}
		for _, value := range keep {
			keepKeys[natKey(value)] = true
		}
		for _, row := range ours {
			if movingAddress[row.ID()] {
				continue
			}
			key := natKey(row.Text("address"))
			if withdraw || !keepKeys[key] || present[key] {
				removing = append(removing, ros.NewResource(ros.PathAddresses, row))
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
	var natBypass []*NatBypassEntry
	if facts != nil {
		natBypass = facts.NatBypass
	}
	for _, entry := range natBypass {
		if entry.Enabled && !moving[entry.ID] {
			natRules[entry.ID] = entry
		}
	}
	adoptIDs := []string{}
	for _, item := range items {
		if item.Status == "adoptable" {
			for _, id := range item.RuleIDs {
				if entry := natRules[id]; entry != nil && !device.IsManaged(entry.Comment) && !slices.Contains(adoptIDs, id) {
					adoptIDs = append(adoptIDs, id)
				}
			}
		}
	}
	for _, id := range adoptIDs {
		entry := natRules[id]
		comment := device.ManagedComment(entry.Comment)
		label := device.FirstNonEmpty(entry.Comment, id)
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
	gone := device.StringSet(removed...)
	removedKeys := map[string]bool{}
	for _, item := range dstRemove {
		removedKeys[natKey(item.Config.Text("address"))] = true
	}
	for _, entry := range natBypass {
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
	if p := natPolicyEvidence(policyPlan); p != nil {
		for _, item := range p.Replacements {
			if item.Path == ros.PathNAT {
				result.Exempt = append(result.Exempt, item.Values.Text("dst-address"))
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
				values = append(values, item.Config.Text("address"))
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
			values = append(values, item.Config.Text("dst-address"))
		}
		parts = append(parts, fmt.Sprintf(messageNATStale, len(op.Stale), natSample(device.Unique(values))))
	}
	return strings.Join(device.NonEmpty(append([]string{head}, parts...)), "；")
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
			merged := map[string]bool{}
			for _, item := range op.Legacy {
				merged[natKey(item.Config.Text("dst-address"))] = true
			}
			fresh := []string{}
			for _, cidr := range op.Destinations {
				if !merged[natKey(cidr)] {
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
	overlaps := prefixOverlap(device.Networks(exclusions))
	for _, item := range addresses {
		if item.Text("list") != rule.Text("dst-address-list") || !item.Enabled() {
			continue
		}
		networks := device.Networks([]string{item.Text("address")})
		if len(networks) == 0 {
			return nil, device.Error(fmt.Sprintf(messageNATSplitInvalid, item.ID()))
		}
		if !slices.ContainsFunc(networks, overlaps) {
			continue
		}
		if !item.Is("comment", NATListComment) || item.Flag("dynamic") {
			return nil, device.Error(fmt.Sprintf(messageNATSplitForeign, item.ID()))
		}
		result = append(result, ros.NewResource(ros.PathAddresses, item))
	}
	return result, nil
}
