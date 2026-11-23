package routers

import (
	"configflow/internal/routers/device"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func natRefsAt(path string, rows []Row) []ListRef {
	refs := NATListRefs(rows)
	if refs == nil {
		refs = []ListRef{}
	}
	for i := range refs {
		refs[i].Path = path + strings.TrimPrefix(refs[i].Path, rosNAT)
	}
	return refs
}
func (d *RouterOS) stateNATLists(ctx context.Context, nats []Row, byName map[string][]Row, mangle []Row) (*NATLists, error) {
	src, dst := byName[NATSrcList], byName[NATDstList]
	if src == nil {
		src = []Row{}
	}
	if dst == nil {
		dst = []Row{}
	}
	d.ExpectAddresses(NATSrcList, src)
	d.ExpectAddresses(NATDstList, dst)
	firewall := []ListRef{}
	if len(src)+len(dst) > 0 || slices.ContainsFunc(nats, IsNATListCandidate) {
		for _, path := range []string{rosFilter, "/ip/firewall/raw"} {
			rows, err := d.list(ctx, path)
			if err != nil {
				firewall = nil
				break
			}
			firewall = append(firewall, natRefsAt(path, rows)...)
		}
	}
	var mangleRefs []ListRef
	if mangle != nil {
		mangleRefs = natRefsAt(rosMangle, mangle)
	}
	return &NATLists{src, dst, mangleRefs, firewall}, nil
}
func (d *RouterOS) readNATLists(ctx context.Context) (map[string][]Row, error) {
	rows, err := d.ReadAddresses(ctx, []string{NATSrcList, NATDstList})
	if err != nil {
		return nil, err
	}
	for _, name := range []string{NATSrcList, NATDstList} {
		d.ExpectAddresses(name, rows[name])
	}
	return rows, nil
}
func (d *RouterOS) assessedNAT(rows []Row) []Row {
	if len(d.dnsGuardManifest) == 0 {
		return rows
	}
	ids := ownedHairpinIDs(d.dnsGuardManifest, rows)
	return slices.DeleteFunc(slices.Clone(rows), func(row Row) bool { return ids[row.id()] })
}
func (d *RouterOS) foreignNATRefs(ctx context.Context, keep map[string]bool, nats []Row) ([]ListRef, error) {
	refs := []ListRef{}
	for _, path := range []string{rosNAT, rosMangle, rosFilter, "/ip/firewall/raw"} {
		rows := nats
		if path != rosNAT || nats == nil {
			var err error
			rows, err = d.list(ctx, path)
			if err != nil {
				return nil, err
			}
		}
		for _, ref := range natRefsAt(path, rows) {
			if path == rosNAT && keep[strings.TrimPrefix(ref.Path, rosNAT+"/")] {
				continue
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// NATBaseline is the manually enrolled list rule and its owned sources.
type NATBaseline struct {
	Rule ROSResource   `json:"rule"`
	Src  []ROSResource `json:"src"`
}

// NATResources reads the baseline for auto-sync, nil when no list rule exists.
func (d *RouterOS) NATResources(ctx context.Context) (*NATBaseline, error) {
	rows, err := d.list(ctx, rosNAT)
	if err != nil {
		return nil, err
	}
	found := InspectNATListRule(d.assessedNAT(rows), nil)
	if found.Problem != "" {
		return nil, device.Error(found.Problem)
	}
	if found.Rule == nil {
		return nil, nil
	}
	lists, err := d.readNATLists(ctx)
	if err != nil {
		return nil, err
	}
	result := &NATBaseline{Rule: resource(rosNAT, found.Rule), Src: []ROSResource{}}
	for _, row := range lists[NATSrcList] {
		if row.is("comment", NATListComment) {
			result.Src = append(result.Src, resource(rosAddresses, row))
		}
	}
	return result, nil
}

// NATMemberUpdate permits only enrolled destination deltas; all other changes
// require a manual publish. Source identity is additionally checked by R14.
func NATMemberUpdate(stages []Stage, enrolled *NATBaseline) *NATListSync {
	ops := []Op{}
	for _, stage := range stages {
		if stage.Key == StageRoutes {
			for _, op := range stage.Ops {
				if op.OpKind() == "nat_list_sync" {
					ops = append(ops, op)
				}
			}
		}
	}
	for _, stage := range stages {
		if stage.Key == StageNATBypass {
			ops = append(ops, stage.Ops...)
		}
	}
	if len(ops) == 0 || enrolled == nil {
		return nil
	}
	var merged NATListSync
	adds, removes := []string{}, []ROSResource{}
	for _, value := range ops {
		op, ok := value.(*NATListSync)
		if !ok || op.Rule == nil || op.ReplaceRule || op.Withdraw || len(op.Legacy)+len(op.Stale)+len(op.SrcAdd)+len(op.SrcRemove) > 0 || op.Rule.ID != enrolled.Rule.ID || !sameRow(configROSRow(op.Rule.Config), configROSRow(enrolled.Rule.Config)) || op.OutInterface != enrolled.Rule.Config.text("out-interface") {
			return nil
		}
		adds = append(adds, op.DstAdd...)
		removes = append(removes, op.DstRemove...)
		merged = *op
	}
	merged.DstAdd, merged.DstRemove = adds, removes
	return &merged
}

// NATMigration counts legacy merges and stale deletions, including static
// preparation nested inside a source-policy operation.
func NATMigration(stages []Stage) (merged, stale int) {
	for _, stage := range stages {
		for _, op := range stage.Ops {
			if sync, ok := op.(*NATListSync); ok {
				merged += len(sync.Legacy)
				stale += len(sync.Stale)
			} else {
				m, s := nestedNATMigration(op)
				merged += m
				stale += s
			}
		}
	}
	return
}

// VerifyNATLists checks exact owned membership and all outside reference tables.
func VerifyNATLists(ctx context.Context, check *NATListCheck, state *RouterState) error {
	lists := ListsOfNAT(ctx, state)
	if lists == nil {
		return device.Error(messageNATVerifyLists)
	}
	if lists.MangleRefs == nil {
		return device.Error(messageNATVerifyPrefix + messageNATMangleUnreadable)
	}
	if lists.FirewallRefs == nil {
		return device.Error(messageNATVerifyPrefix + messageNATFirewallUnreadable)
	}
	rows := []Row{}
	if state.NatContext != nil {
		rows = state.NatContext.Rules
	}
	found := InspectNATListRule(rows, lists.refs())
	if check.Withdraw {
		if found.Rule != nil || slices.ContainsFunc(rows, IsNATListCandidate) {
			return device.Error(messageNATVerifyRulePresent)
		}
	} else {
		if found.Problem != "" {
			return device.Error(messageNATVerifyPrefix + found.Problem)
		}
		if len(check.Destinations) > 0 && found.Rule == nil {
			return device.Error(messageNATVerifyNoRule)
		}
		if found.Rule != nil && found.OutInterface != check.OutInterface {
			return device.Error(messageNATVerifyEgress)
		}
	}
	removed := device.StringSet(check.Removed...)
	for _, row := range rows {
		if removed[row.id()] {
			return device.Error(messageNATVerifyLegacy)
		}
	}
	for i, name := range []string{NATSrcList, NATDstList} {
		ours, present := map[string]bool{}, map[string]bool{}
		for _, row := range lists.rows(name) {
			if row.enabled() {
				present[natKey(row.text("address"))] = true
				if row.is("comment", NATListComment) {
					ours[natKey(row.text("address"))] = true
				}
			}
		}
		expected := check.Members.Src
		wanted := check.Sources
		if i == 1 {
			expected, wanted = check.Members.Dst, check.Destinations
		}
		if check.Withdraw {
			expected = nil
		}
		if !maps.Equal(ours, device.StringSet(expected...)) {
			return device.Error(fmt.Sprintf(messageNATVerifyMembers, name))
		}
		if !check.Withdraw {
			for _, value := range wanted {
				if !present[natKey(value)] {
					return device.Error(fmt.Sprintf(messageNATVerifyMissing, name))
				}
			}
		}
	}
	return nil
}
