package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// SnapshotDNSGuard reads only the bypass list plus managed moved IDs, and the
// three small firewall/health tables. Only Netwatch can degrade to unreadable.
func SnapshotDNSGuard(ctx context.Context, d *RouterOS) (*DNSGuardSnapshot, error) {
	manifest, err := parseDNSGuardManifest(d.DNSGuardManifest())
	if err != nil {
		return nil, err
	}
	live := DNSGuardSnapshot{}
	for _, path := range dnsGuardPaths {
		var rows []Row
		if path == ros.PathAddresses {
			rows, err = d.FilteredAddresses(ctx, DNSGuardBypassList)
			if err == nil && manifest != nil {
				present := ros.RowIDs(rows)
				missing := []string{}
				for _, e := range manifest.Resources {
					if e.Path == path && !present[e.ID] {
						missing = append(missing, e.ID)
					}
				}
				var moved []Row
				moved, err = d.LocateAddresses(ctx, missing)
				rows = append(rows, moved...)
			}
		} else {
			rows, err = d.List(ctx, path)
		}
		if err != nil {
			if path != ros.PathNetwatch || !device.IsRouterError(err) {
				return nil, err
			}
			rows = nil
		}
		live[path] = rows
	}
	return &live, nil
}

func guardComparable(role string, config Row, failover bool) Row {
	out := ros.CloneRow(config)
	if failover && strings.HasPrefix(role, "hijack") {
		delete(out, "disabled")
	}
	return out
}
func guardOwned(previous *DNSGuardManifest, live *DNSGuardSnapshot) ([]DNSGuardResource, error) {
	owned := []DNSGuardResource{}
	if previous == nil {
		return owned, nil
	}
	failover := previous.Spec != nil && previous.Spec.Failover
	indices := map[string]map[string][]Row{}
	for _, path := range dnsGuardPaths {
		indices[path] = map[string][]Row{}
		for _, row := range (*live)[path] {
			indices[path][row.ID()] = append(indices[path][row.ID()], row)
		}
	}
	for _, e := range previous.Resources {
		if (*live)[e.Path] == nil {
			return nil, device.Error(messageGuardWatchOwned)
		}
		matches := indices[e.Path][e.ID]
		if len(matches) > 1 {
			return nil, device.Error(messageGuardDuplicate)
		}
		if len(matches) == 0 {
			continue
		}
		if !ros.SameRow(guardComparable(e.Role, ros.StableRow(e.Path, matches[0]), failover), guardComparable(e.Role, e.Config, failover)) {
			return nil, device.Error(fmt.Sprintf(messageGuardEdited, e.Path, e.ID))
		}
		owned = append(owned, e)
	}
	return owned, nil
}
func guardMatches(item DNSGuardCreate, row Row, failover bool) bool {
	for key, value := range item.Values {
		if failover && strings.HasPrefix(item.Role, "hijack") && key == "disabled" {
			continue
		}
		if ros.Comparable(key, row[key]) != ros.Comparable(key, value) {
			return false
		}
	}
	return true
}
func guardOnTop(id string, nat []Row, owned map[[2]string]bool) bool {
	for _, row := range nat {
		if row.ID() == id {
			return true
		}
		if row.Is("chain", "dstnat") && row.Text("dst-port") == "53" && !row.Flag("disabled") && !owned[[2]string{ros.PathNAT, row.ID()}] {
			return false
		}
	}
	return false
}

func guardForeignHijack(nat []Row, owned map[[2]string]bool) bool {
	for _, row := range nat {
		// Netwatch selects all matching comments, including disabled rules and
		// rules outside dstnat/53. Only the manifest proof establishes ownership.
		if row.Is("comment", dnsGuardHijack) && !owned[[2]string{ros.PathNAT, row.ID()}] {
			return true
		}
	}
	return false
}

// BuildDNSGuard plans a safe incremental publish from a single device snapshot.
func BuildDNSGuard(desired *Desired, live *DNSGuardSnapshot, nat *NatContext) *DNSGuardPlan {
	p := &DNSGuardPlan{DNSGuardReport: DNSGuardReport{Status: "disabled", Items: []DNSGuardItem{}, Blockers: []string{}, Warnings: []string{}}, Spec: json.RawMessage("null"), Ops: []Op{}, Roles: []string{}}
	if desired == nil || desired.DNSGuard == nil {
		return p
	}
	request := desired.DNSGuard
	settings := request.Settings
	p.Enabled = settings.Enabled
	fail := func(err error) *DNSGuardPlan {
		p.Status = "unknown"
		p.Message = err.Error()
		p.Blockers = append(p.Blockers, err.Error())
		return p
	}
	if live == nil {
		return fail(device.Error(messageGuardUnreadable))
	}
	previous, err := parseDNSGuardManifest(request.Previous)
	if err != nil {
		return fail(err)
	}
	owned, err := guardOwned(previous, live)
	if err != nil {
		return fail(err)
	}
	var spec *DNSGuardSpec
	if settings.Enabled {
		spec, err = guardDesiredSpec(settings, desired, nat)
		if err != nil {
			return fail(err)
		}
	}
	p.Spec, _ = json.Marshal(spec)
	items := DNSGuardResources(spec)
	ownedIDs := map[[2]string]bool{}
	byRole := map[string]DNSGuardResource{}
	rows := map[[2]string]Row{}
	for _, e := range owned {
		ownedIDs[[2]string{e.Path, e.ID}] = true
		byRole[e.Role] = e
	}
	for _, path := range dnsGuardPaths {
		for _, row := range (*live)[path] {
			rows[[2]string{path, row.ID()}] = row
		}
	}
	if spec != nil {
		if guardForeignHijack((*live)[ros.PathNAT], ownedIDs) {
			return fail(device.Error(messageGuardHijackCollision))
		}
		if spec.Failover && (*live)[ros.PathNetwatch] == nil {
			p.Blockers = append(p.Blockers, messageGuardWatchUnsupported)
		}
		for _, reserved := range []struct{ path, key, name, label string }{{ros.PathAddresses, "list", DNSGuardBypassList, messageGuardAddressList}, {ros.PathNetwatch, "name", dnsGuardWatch, "Netwatch"}} {
			for _, row := range (*live)[reserved.path] {
				if row.Is(reserved.key, reserved.name) && !ownedIDs[[2]string{reserved.path, row.ID()}] {
					p.Blockers = append(p.Blockers, fmt.Sprintf(messageGuardNameCollision, reserved.label, reserved.name))
					break
				}
			}
		}
		others := []string{}
		for _, row := range (*live)[ros.PathNAT] {
			if row.Is("chain", "dstnat") && row.Text("dst-port") == "53" && (row.Is("action", "dst-nat") || row.Is("action", "redirect")) && !row.Flag("disabled") && !ownedIDs[[2]string{ros.PathNAT, row.ID()}] {
				others = append(others, device.FirstNonEmpty(row.Text("comment"), row.ID()))
			}
		}
		if len(others) > 0 {
			p.Warnings = append(p.Warnings, fmt.Sprintf(messageGuardForeignDNS, strings.Join(others, "、")))
		}
	}
	kept := map[string]bool{}
	keptRoles := []string{}
	created := []DNSGuardCreate{}
	removed := []DNSGuardResource{}
	failover := spec != nil && spec.Failover
	for _, item := range items {
		p.Roles = append(p.Roles, item.Role)
		e := byRole[item.Role]
		row := rows[[2]string{item.Path, e.ID}]
		keep := row != nil && e.Path == item.Path && guardMatches(item, row, failover)
		if keep && strings.HasPrefix(item.Role, "hijack") {
			keep = guardOnTop(e.ID, (*live)[ros.PathNAT], ownedIDs)
		}
		status := "add"
		if row != nil || len(owned) == 0 {
			status = "pending"
		}
		if keep {
			kept[item.Role] = true
			keptRoles = append(keptRoles, item.Role)
			status = "synced"
		} else {
			created = append(created, item)
		}
		p.Items = append(p.Items, DNSGuardItem{Role: item.Role, Object: guardObjectName(item.Path), Detail: guardDescribe(item.Role, item.Values), Status: status, Paused: strings.HasPrefix(item.Role, "hijack") && row != nil && row.Flag("disabled")})
	}
	roleSet := device.StringSet(p.Roles...)
	for _, e := range owned {
		if !kept[e.Role] {
			removed = append(removed, e)
			if !roleSet[e.Role] {
				p.Items = append(p.Items, DNSGuardItem{Role: e.Role, Object: guardObjectName(e.Path), Detail: guardDescribe(e.Role, e.Config), Status: "remove"})
			}
		}
	}
	op := &DNSGuardReplace{OpHead: OpHead{Kind: "dns_guard_replace", Summary: guardSummary(spec, previous)}, Spec: spec, Previous: ros.GuardRaw(request.Previous), Owned: owned, Create: created}
	if spec == nil && len(owned) == 0 {
		p.Message = messageGuardDisabled
		if previous != nil {
			p.Message = messageGuardDisabledGone
			op.Summary = messageGuardClear
			p.Ops = append(p.Ops, op)
		}
		return p
	}
	ownedRoles := []string{}
	for _, e := range owned {
		ownedRoles = append(ownedRoles, e.Role)
	}
	if previous != nil && reflect.DeepEqual(previous.Spec, spec) && len(kept) == len(items) && slices.Equal(ownedRoles, p.Roles) {
		p.Status = "synced"
		p.Message = messageGuardSynced
		if slices.ContainsFunc(p.Items, func(item DNSGuardItem) bool { return item.Paused }) {
			p.Message = messageGuardPaused
		}
		return p
	}
	p.Status = "pending"
	p.Message = messageGuardPending
	if spec == nil {
		p.Message = messageGuardWithdraw
	}
	if len(owned) > 0 {
		op.Keep = &keptRoles
		op.Delete = &removed
		roles := slices.Clone(p.Roles)
		op.Roles = &roles
	}
	p.Ops = append(p.Ops, op)
	return p
}

func dnsGuardReportPart(p *DNSGuardPlan) ReportPart {
	statuses := []string{}
	if p.Status != "disabled" {
		statuses = append(statuses, p.Status)
	}
	return ReportPart{
		Key:      "dns_guard",
		Section:  p.DNSGuardReport,
		Statuses: statuses,
		InSync:   (p.Status == "disabled" || p.Status == "synced") && len(p.Blockers) == 0 && len(p.Ops) == 0,
	}
}
