package rospolicy

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func natRefsAt(path string, rows []ros.Row) []ros.ListRef {
	refs := ros.NATListRefs(rows)
	if refs == nil {
		refs = []ros.ListRef{}
	}
	for i := range refs {
		refs[i].Path = path + strings.TrimPrefix(refs[i].Path, ros.PathNAT)
	}
	return refs
}
func (d rosDevice) stateNATLists(ctx context.Context, nats []ros.Row, byName map[string][]ros.Row, mangle []ros.Row) (*NATLists, error) {
	src, dst := byName[ros.NATSrcList], byName[ros.NATDstList]
	if src == nil {
		src = []ros.Row{}
	}
	if dst == nil {
		dst = []ros.Row{}
	}
	d.ExpectAddresses(ros.NATSrcList, src)
	d.ExpectAddresses(ros.NATDstList, dst)
	firewall := []ros.ListRef{}
	if len(src)+len(dst) > 0 || slices.ContainsFunc(nats, ros.IsNATListCandidate) {
		for _, path := range []string{ros.PathFilter, "/ip/firewall/raw"} {
			rows, err := d.List(ctx, path)
			if err != nil {
				firewall = nil
				break
			}
			firewall = append(firewall, natRefsAt(path, rows)...)
		}
	}
	var mangleRefs []ros.ListRef
	if mangle != nil {
		mangleRefs = natRefsAt(ros.PathMangle, mangle)
	}
	return &NATLists{src, dst, mangleRefs, firewall}, nil
}
func (d rosDevice) readNATLists(ctx context.Context) (map[string][]ros.Row, error) {
	rows, err := d.ReadAddresses(ctx, []string{ros.NATSrcList, ros.NATDstList})
	if err != nil {
		return nil, err
	}
	for _, name := range []string{ros.NATSrcList, ros.NATDstList} {
		d.ExpectAddresses(name, rows[name])
	}
	return rows, nil
}
func (d rosDevice) assessedNAT(rows []ros.Row) []ros.Row {
	if len(d.DNSGuardManifest()) == 0 {
		return rows
	}
	ids := ros.OwnedHairpinIDs(d.DNSGuardManifest(), rows)
	return slices.DeleteFunc(slices.Clone(rows), func(row ros.Row) bool { return ids[row.ID()] })
}
func (d rosDevice) foreignNATRefs(ctx context.Context, keep map[string]bool, nats []ros.Row) ([]ros.ListRef, error) {
	refs := []ros.ListRef{}
	for _, path := range []string{ros.PathNAT, ros.PathMangle, ros.PathFilter, "/ip/firewall/raw"} {
		rows := nats
		if path != ros.PathNAT || nats == nil {
			var err error
			rows, err = d.List(ctx, path)
			if err != nil {
				return nil, err
			}
		}
		for _, ref := range natRefsAt(path, rows) {
			if path == ros.PathNAT && keep[strings.TrimPrefix(ref.Path, ros.PathNAT+"/")] {
				continue
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// NATBaseline is the manually enrolled list rule and its owned sources.
type NATBaseline struct {
	Rule ros.Resource   `json:"rule"`
	Src  []ros.Resource `json:"src"`
}

// NATResources reads the baseline for auto-sync, nil when no list rule exists.
func (d rosDevice) NATResources(ctx context.Context) (*NATBaseline, error) {
	rows, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return nil, err
	}
	found := ros.InspectNATListRule(d.assessedNAT(rows), nil)
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
	result := &NATBaseline{Rule: ros.NewResource(ros.PathNAT, found.Rule), Src: []ros.Resource{}}
	for _, row := range lists[ros.NATSrcList] {
		if row.Is("comment", ros.NATListComment) {
			result.Src = append(result.Src, ros.NewResource(ros.PathAddresses, row))
		}
	}
	return result, nil
}

// NATMemberUpdate permits only enrolled destination deltas; all other changes
// require a manual publish. Source identity is additionally checked by R14.
func NATMemberUpdate(stages []device.Stage, enrolled *NATBaseline) *NATListSync {
	ops := []device.Op{}
	for _, stage := range stages {
		if stage.Key == device.StageRoutes {
			for _, op := range stage.Ops {
				if op.OpKind() == "nat_list_sync" {
					ops = append(ops, op)
				}
			}
		}
	}
	for _, stage := range stages {
		if stage.Key == device.StageNATBypass {
			ops = append(ops, stage.Ops...)
		}
	}
	if len(ops) == 0 || enrolled == nil {
		return nil
	}
	var merged NATListSync
	adds, removes := []string{}, []ros.Resource{}
	for _, value := range ops {
		op, ok := value.(*NATListSync)
		if !ok || op.Rule == nil || op.ReplaceRule || op.Withdraw || len(op.Legacy)+len(op.Stale)+len(op.SrcAdd)+len(op.SrcRemove) > 0 || op.Rule.ID != enrolled.Rule.ID || !ros.SameRow(ros.ConfigROSRow(op.Rule.Config), ros.ConfigROSRow(enrolled.Rule.Config)) || op.OutInterface != enrolled.Rule.Config.Text("out-interface") {
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
func NATMigration(stages []device.Stage) (merged, stale int) {
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
func VerifyNATLists(ctx context.Context, check *NATListCheck, nat *ros.NatContext, listsSlot **NATLists, readerSlot *func(context.Context) (*NATLists, error)) error {
	lists := ListsOfNAT(ctx, listsSlot, readerSlot)
	if lists == nil {
		return device.Error(messageNATVerifyLists)
	}
	if lists.MangleRefs == nil {
		return device.Error(messageNATVerifyPrefix + messageNATMangleUnreadable)
	}
	if lists.FirewallRefs == nil {
		return device.Error(messageNATVerifyPrefix + messageNATFirewallUnreadable)
	}
	rows := []ros.Row{}
	if nat != nil {
		rows = nat.Rules
	}
	found := ros.InspectNATListRule(rows, lists.refs())
	if check.Withdraw {
		if found.Rule != nil || slices.ContainsFunc(rows, ros.IsNATListCandidate) {
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
		if removed[row.ID()] {
			return device.Error(messageNATVerifyLegacy)
		}
	}
	for i, name := range []string{ros.NATSrcList, ros.NATDstList} {
		ours, present := map[string]bool{}, map[string]bool{}
		for _, row := range lists.rows(name) {
			if row.Enabled() {
				present[NATKey(row.Text("address"))] = true
				if row.Is("comment", ros.NATListComment) {
					ours[NATKey(row.Text("address"))] = true
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
				if !present[NATKey(value)] {
					return device.Error(fmt.Sprintf(messageNATVerifyMissing, name))
				}
			}
		}
	}
	return nil
}
