package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func syncStaticResources(ctx context.Context, d *RouterOS, desired *Desired) ([]ROSResource, error) {
	wanted := syncCIDRSet(desired.ProxyCIDRs)
	rows, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return nil, err
	}
	result := []ROSResource{}
	found := map[string]bool{}
	for _, row := range rows {
		if row.TextOr("routing-table", "main") != "main" || !wanted[row.Text("dst-address")] || !row.Is("gateway", desired.ProxyGateway) || !device.IsManaged(row.Text("comment")) {
			continue
		}
		entry := editROSEntry(row, map[string]bool{})
		if !entry.Editable || !entry.Enabled {
			return nil, device.Error(messageSyncStaticSpecial)
		}
		result = append(result, ros.NewResource(ros.PathRoutes, row))
		found[row.Text("dst-address")] = true
	}
	if !maps.Equal(found, wanted) {
		return nil, device.Error(messageSyncStaticMissing)
	}
	if len(result) != len(wanted) {
		return nil, device.Error(messageSyncStaticDuplicate)
	}
	return result, nil
}
func restrictSyncPlan(plan *Plan, baseline *RuleSyncBaseline) ([]Op, error) {
	if len(plan.Blockers) > 0 {
		return nil, device.Error(strings.Join(plan.Blockers, "；"))
	}
	merged, stale := NATMigration(plan.Stages)
	if merged > 0 || stale > 0 {
		details := []string{}
		if merged > 0 {
			details = append(details, fmt.Sprintf(messageSyncMerged, merged))
		}
		if stale > 0 {
			details = append(details, fmt.Sprintf(messageSyncStale, stale))
		}
		return nil, device.Error(fmt.Sprintf(messageSyncMigrate, strings.Join(details, "，")))
	}
	nat := NATMemberUpdate(plan.Stages, baseline.NATList)
	var routes *Stage
	for i := range plan.Stages {
		stage := &plan.Stages[i]
		if stage.Key == StageRoutes {
			routes = stage
		}
		if stage.Key == StageNATBypass && nat != nil || stage.Key == StageIPv6Routes {
			continue
		}
		if stage.Key != StageRoutes && len(stage.Ops) > 0 {
			return nil, device.Error(fmt.Sprintf(messageSyncStage, stage.Title))
		}
	}
	if routes == nil {
		return nil, device.Error(messageSyncConflict)
	}
	ids := map[string]bool{}
	for _, item := range baseline.StaticResources {
		if item.ID == "" {
			return nil, device.Error(device.MessageOperationFormat)
		}
		ids[item.ID] = true
	}
	result := []Op{}
	for _, op := range routes.Ops {
		switch v := syncValueOp(op).(type) {
		case *NATListSync:
			if nat == nil {
				return nil, device.Error(fmt.Sprintf(messageSyncStage, StageTitles[StageNATBypass]))
			}
		case *ROSPolicyReplace:
			if len(v.Migrations) > 0 {
				return nil, device.Error(messageSyncPolicyMigration)
			}
			previous, err := policyManifest(v.Previous)
			if err != nil {
				return nil, err
			}
			if previous == nil || previous.Spec == nil || v.Spec == nil {
				return nil, device.Error(messageSyncPolicyMissing)
			}
			a, b := *previous.Spec, *v.Spec
			a.Destinations = nil
			b.Destinations = nil
			if !device.JSONEqual(a, b) {
				return nil, device.Error(messageSyncPolicyScope)
			}
		case RouteDeleteOp:
			if ids[v.ID] {
				result = append(result, v)
			}
		case RouteAddOp:
			result = append(result, v)
		default:
			return nil, device.Error(messageSyncAdopt)
		}
	}
	return result, nil
}

// RuleSyncDelta is the proven additions/removals and the last twenty changes.
type RuleSyncDelta struct {
	Added   int           `json:"added"`
	Removed int           `json:"removed"`
	Log     []RuleSyncLog `json:"log"`
}

// ApplyRuleSyncDelta changes only destinations enrolled by a manual release.
// Every failure, including final verification, compensates the shared journal.
func ApplyRuleSyncDelta(ctx context.Context, d *RouterOS, desired *Desired, plan *Plan, baseline *RuleSyncBaseline, ensure func(bool) error) (out RuleSyncDelta, err error) {
	ctx = lifecycle.Detach(ctx)
	if err = ctx.Err(); err != nil {
		return
	}
	if baseline == nil || desired == nil || plan == nil || plan.Checks == nil {
		return out, device.Error(device.MessageOperationFormat)
	}
	operations, err := restrictSyncPlan(plan, baseline)
	if err != nil {
		return out, err
	}
	defer d.ReuseAddressReads()()
	previous := slices.Clone(d.PolicyManifest())
	livePtr, err := d.snapshotPolicy(ctx, false)
	if err != nil {
		return out, err
	}
	live := *livePtr
	if err = syncGuardResources(live, baseline.StaticResources); err != nil {
		return out, err
	}
	nat := NATMemberUpdate(plan.Stages, baseline.NATList)
	retainedNAT := []ROSResource{}
	if nat != nil {
		enrolled := baseline.NATList
		for _, item := range append([]ROSResource{enrolled.Rule}, enrolled.Src...) {
			if err = guardPolicyIn(live, item); err != nil {
				return out, err
			}
		}
		for _, path := range []string{ros.PathNAT, ros.PathMangle, ros.PathFilter, "/ip/firewall/raw"} {
			for _, row := range live[path] {
				if path == ros.PathNAT && row.ID() == enrolled.Rule.ID {
					continue
				}
				for _, key := range []string{"src-address-list", "dst-address-list"} {
					name := strings.TrimLeft(row.Text(key), "!")
					if name == NATSrcList || name == NATDstList {
						return out, device.Error(fmt.Sprintf(messageSyncForeignReference, path, row.ID(), name))
					}
				}
			}
		}
		sources, expected := map[string]bool{}, map[string]bool{}
		for _, row := range live[ros.PathAddresses] {
			if row.Is("list", NATSrcList) && row.Is("comment", NATListComment) {
				sources[row.ID()] = true
			}
			if row.Is("list", NATDstList) {
				retainedNAT = append(retainedNAT, ros.NewResource(ros.PathAddresses, row))
			}
		}
		for _, item := range enrolled.Src {
			expected[item.ID] = true
		}
		if !maps.Equal(sources, expected) {
			return out, device.Error(messageSyncNATSources)
		}
		retainedNAT = append([]ROSResource{enrolled.Rule}, retainedNAT...)
	}
	wanted, known := syncCIDRSet(desired.ProxyCIDRs), map[string]ROSResource{}
	for _, item := range baseline.StaticResources {
		known[item.ID] = item
	}
	for _, row := range live[ros.PathRoutes] {
		_, ok := known[row.ID()]
		if row.TextOr("routing-table", "main") == "main" && wanted[row.Text("dst-address")] && !ok {
			return out, device.Error(messageSyncForeignRoute)
		}
	}
	manifest, err := policyManifest(previous)
	if err != nil {
		return out, err
	}
	if manifest != nil {
		owned, e := OwnedROSPolicyRows(previous, livePtr)
		if e != nil {
			return out, e
		}
		if len(owned) != len(manifest.Resources) {
			return out, device.Error(messageSyncPolicyMissingObjects)
		}
		if e = checkPolicyReferences(owned, live); e != nil {
			return out, e
		}
		if e = VerifyROSPolicy(ctx, d, &ROSPolicyCheck{Spec: manifest.Spec, Previous: previous}); e != nil {
			return out, e
		}
	}
	additions := []ROSResource{}
	removals := []ROSResource{}
	for _, op := range operations {
		switch v := syncValueOp(op).(type) {
		case RouteAddOp:
			additions = append(additions, ROSResource{Path: ros.PathRoutes, Config: Row{"dst-address": v.CIDR, "gateway": v.Gateway, "comment": v.Comment, "disabled": "false"}})
		case RouteDeleteOp:
			removals = append(removals, known[v.ID])
		}
	}
	var next *ROSPolicyManifest
	if manifest != nil {
		var v ROSPolicyManifest
		_ = jsondecode.Unmarshal(previous, &v)
		next = &v
	}
	if check := plan.Checks.ROSPolicy; check != nil && manifest != nil {
		var entry *ROSResource
		for i := range manifest.Resources {
			item := &manifest.Resources[i]
			if item.Path == ros.PathMangle && item.Config.Is("chain", "prerouting") && item.Config.Is("action", "jump") {
				entry = item
				break
			}
		}
		if entry == nil || entry.Config.Text("dst-address-list") == "" {
			return out, device.Error(messageSyncPolicyEntry)
		}
		name := entry.Config.Text("dst-address-list")
		old := map[string]ROSResource{}
		for _, item := range manifest.Resources {
			if item.Path == ros.PathAddresses && item.Config.Is("list", name) {
				old[syncNetworkKey(item.Config.Text("address"))] = item
			}
		}
		newSet := device.StringSet(check.Spec.Destinations...)
		for _, cidr := range sortedSyncKeys(newSet) {
			if _, ok := old[cidr]; !ok {
				additions = append(additions, ROSResource{Path: ros.PathAddresses, Config: Row{"list": name, "address": cidr, "comment": entry.Config.Text("comment"), "disabled": "false"}})
			}
		}
		for _, cidr := range sortedSyncKeys(old) {
			if !newSet[cidr] {
				removals = append(removals, old[cidr])
			}
		}
		next.Spec = check.Spec
	}
	if next != nil {
		removed := policyKeys(removals)
		next.Resources = slices.DeleteFunc(next.Resources, func(item ROSResource) bool { return removed[[2]string{item.Path, item.ID}] })
	}
	ids, journal := policyIDs(live), []NATStep{}
	var undo6 *IPv6RoutesUndo
	out.Log = []RuleSyncLog{}
	if ensure == nil {
		ensure = func(bool) error { return ctx.Err() }
	}
	checkCurrent := func(force bool) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return ensure(force)
	}
	defer func() {
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		} // Shutdown stops device I/O; receipt remains running for restart recovery.
		problems := []string{}
		if undo6 != nil {
			if e := d.RestoreIPv6Routes(ctx, *undo6); e != nil {
				problems = append(problems, e.Error())
			}
		}
		steps := slices.Clone(journal)
		slices.Reverse(steps)
		retained := slices.Clone(retainedNAT)
		if manifest != nil {
			retained = append(slices.Clone(manifest.Resources), retained...)
		}
		if e := d.RestoreROSPolicy(ctx, ROSPolicyUndo{Steps: steps, Previous: previous, Retained: retained}); e != nil {
			problems = append(problems, e.Error())
		}
		if len(problems) > 0 {
			err = device.RollbackError(err.Error() + "；" + strings.Join(problems, "；"))
		}
	}()
	if err = checkCurrent(true); err != nil {
		return
	}
	for _, item := range additions {
		if item.Path == ros.PathAddresses {
			continue
		}
		if err = checkCurrent(false); err != nil {
			return
		}
		var rows []Row
		rows, err = d.Scoped(ctx, ros.PathRoutes, item.Config)
		if err != nil {
			return
		}
		for _, row := range rows {
			if row.TextOr("routing-table", "main") == "main" && row.Is("dst-address", item.Config.Text("dst-address")) {
				err = device.Error(messageSyncBeforeWrite)
				return
			}
		}
		if _, err = d.createResource(ctx, item.Path, item.Config, &journal, ids, false); err != nil {
			return
		}
		out.Log = append(out.Log, RuleSyncLog{"ok", fmt.Sprintf(messageSyncAdded, item.Config.Text("dst-address"))})
	}
	members := []Row{}
	for _, item := range additions {
		if item.Path == ros.PathAddresses {
			members = append(members, item.Config)
		}
	}
	if len(members) > 0 {
		if err = checkCurrent(false); err != nil {
			return
		}
		var resources []ROSResource
		resources, err = d.CreateAddresses(ctx, members, &journal, ids)
		if err != nil {
			return
		}
		for _, item := range resources {
			if next != nil {
				position := len(next.Resources)
				for i, v := range next.Resources {
					if v.Path != ros.PathAddresses {
						position = i
						break
					}
				}
				next.Resources = slices.Insert(next.Resources, position, item)
			}
			out.Log = append(out.Log, RuleSyncLog{"ok", fmt.Sprintf(messageSyncAdded, item.Config.Text("address"))})
		}
	}
	if nat != nil && len(nat.DstAdd) > 0 {
		if err = checkCurrent(false); err != nil {
			return
		}
		if _, err = d.CreateAddresses(ctx, syncNATEntries(NATDstList, nat.DstAdd), &journal, ids); err != nil {
			return
		}
		for _, cidr := range nat.DstAdd {
			out.Log = append(out.Log, RuleSyncLog{"ok", fmt.Sprintf(messageSyncNATAdded, cidr)})
		}
	}
	if nat != nil && len(nat.DstRemove) > 0 {
		if err = checkCurrent(false); err != nil {
			return
		}
		if err = d.DeleteAddresses(ctx, nat.DstRemove, &journal); err != nil {
			return
		}
		for _, item := range nat.DstRemove {
			out.Log = append(out.Log, RuleSyncLog{"ok", fmt.Sprintf(messageSyncNATRemoved, item.Config.Text("address"))})
		}
	}
	for _, item := range removals {
		if item.Path == ros.PathAddresses {
			continue
		}
		if err = checkCurrent(false); err != nil {
			return
		}
		if err = d.deleteResource(ctx, item, &journal); err != nil {
			return
		}
		out.Log = append(out.Log, RuleSyncLog{"ok", fmt.Sprintf(messageSyncDeleted, item.Config.Text("dst-address"))})
	}
	removedMembers := []ROSResource{}
	for _, item := range removals {
		if item.Path == ros.PathAddresses {
			removedMembers = append(removedMembers, item)
		}
	}
	if len(removedMembers) > 0 {
		if err = checkCurrent(false); err != nil {
			return
		}
		if err = d.DeleteAddresses(ctx, removedMembers, &journal); err != nil {
			return
		}
		for _, item := range removedMembers {
			out.Log = append(out.Log, RuleSyncLog{"ok", fmt.Sprintf(messageSyncDeleted, item.Config.Text("address"))})
		}
	}
	var routes6 *IPv6RoutesReplace
	for _, stage := range plan.Stages {
		if stage.Key == StageIPv6Routes {
			for _, op := range stage.Ops {
				if v, ok := op.(*IPv6RoutesReplace); ok {
					routes6 = v
					break
				}
			}
		}
	}
	if routes6 != nil {
		if err = checkCurrent(false); err != nil {
			return
		}
		undo6, err = d.ApplyIPv6Routes(ctx, *routes6)
		if err != nil {
			return
		}
		out.Log = append(out.Log, RuleSyncLog{"ok", routes6.Summary})
	}
	if next == nil {
		d.SetPolicyManifest(nil)
	} else { // Keep opaque manifest members when destinations/resources change.
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(previous, &fields)
		var spec map[string]json.RawMessage
		_ = json.Unmarshal(fields["spec"], &spec)
		spec["destinations"] = device.JSON(next.Spec.Destinations)
		fields["spec"] = device.JSON(spec)
		fields["resources"] = device.JSON(next.Resources)
		d.SetPolicyManifest(device.JSON(fields))
	}
	checks := *plan.Checks
	checks.RemovedRoutes = []string{}
	for _, item := range removals {
		if item.Path == ros.PathRoutes {
			checks.RemovedRoutes = append(checks.RemovedRoutes, item.ID)
		}
	}
	d.InvalidateAddressReads()
	var state *RouterState
	state, err = Verify(ctx, StageRoutes, &checks, d, nil, nil)
	if err != nil {
		return
	}
	_, err = Verify(ctx, StageNATBypass, &checks, d, nil, state)
	if err != nil {
		return
	}
	for _, stage := range []string{StageIPv6FakeIP, StageIPv6DNS, StageIPv6Routes} {
		if stage == StageIPv6FakeIP && checks.IPv6FakeIP == nil || stage == StageIPv6DNS && checks.IPv6DNS == nil || stage == StageIPv6Routes && checks.IPv6Routes == nil {
			continue
		}
		if _, err = Verify(ctx, stage, &checks, d, nil, nil); err != nil {
			return
		}
	}
	if err = checkCurrent(true); err != nil {
		return
	}
	out.Added = len(additions)
	out.Removed = len(removals)
	if nat != nil {
		out.Added += len(nat.DstAdd)
		out.Removed += len(nat.DstRemove)
	}
	if routes6 != nil {
		out.Added += len(routes6.Adds)
		out.Removed += len(routes6.Deletes)
	}
	out.Log = out.Log[max(0, len(out.Log)-20):]
	return
}

func syncNATEntries(name string, values []string) []Row {
	rows := []Row{}
	for _, value := range values {
		rows = append(rows, Row{"list": name, "address": value, "comment": NATListComment, "disabled": "false"})
	}
	return rows
}

func syncValueOp(op Op) Op {
	switch v := op.(type) {
	case *RouteAddOp:
		return *v
	case *RouteDeleteOp:
		return *v
	}
	return op
}

// syncGuardResources indexes once even when a receipt contains 20,000 routes.
func syncGuardResources(live ROSPolicySnapshot, items []ROSResource) error {
	rows := map[[2]string][]Row{}
	for path, entries := range live {
		for _, row := range entries {
			key := [2]string{path, row.ID()}
			rows[key] = append(rows[key], row)
		}
	}
	seen := map[[2]string]bool{}
	for _, item := range items {
		key := [2]string{item.Path, item.ID}
		if seen[key] {
			return device.Error(device.MessageOperationFormat)
		}
		seen[key] = true
		if err := guardPolicyIn(ROSPolicySnapshot{item.Path: rows[key]}, item); err != nil {
			return err
		}
	}
	return nil
}
