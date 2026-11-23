package routers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/devhttp"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
	"configflow/internal/routers/device"
)

// Apply applies one RouterOS release operation and returns its compensation.
// Once started it runs to the end even if the request is cancelled (PORTING
// rule 4b): a write abandoned between the request and its read-back would leave
// a change on the router that no undo record covers. lifecycle.Detach keeps
// only the shutdown cancellation at the drain deadline (O3 2.5; R12 D-R12-3
// then records instead of rolling back). Every device request still has its
// own timeout.
func (d *RouterOS) Apply(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if err := jsondecode.Unmarshal(raw, &head); err != nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	switch head.Kind {
	case "ros_policy_replace":
		var op policyWrite
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := d.applyPolicy(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	case "ros_policy_restore":
		var undo ROSPolicyUndo
		if devhttp.DecodeJSON(raw, &undo) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := d.RestoreROSPolicy(ctx, undo); err != nil {
			return nil, err
		}
		return json.RawMessage(`{}`), nil
	case "nat_list_sync":
		var op NATListSync
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := d.ApplyNATLists(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	case "nat_list_restore":
		var op NATListUndo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := d.RestoreNATLists(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage(`{}`), nil
	case "dns_guard_replace":
		var op DNSGuardReplace
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := d.ApplyDNSGuard(ctx, op)
		if err != nil {
			return nil, err
		}
		out, _ := json.Marshal(undo)
		return out, nil
	case "dns_guard_restore":
		var op DNSGuardUndo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := d.RestoreDNSGuard(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	case "ipv6_fakeip_replace":
		var op IPv6FakeIPReplace
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := d.ApplyIPv6FakeIP(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	case "ipv6_fakeip_restore":
		var op IPv6FakeIPUndo
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := d.RestoreIPv6FakeIP(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	case "ipv6_routes_replace":
		var op IPv6RoutesReplace
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := d.ApplyIPv6Routes(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	case "ipv6_routes_restore":
		var op IPv6RoutesUndo
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := d.RestoreIPv6Routes(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	case "ipv6_dns_update":
		var op IPv6DNSUpdate
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, e := d.ApplyIPv6DNS(ctx, op)
		if e != nil {
			return nil, e
		}
		return json.Marshal(undo)
	case "ipv6_pd_guard_sync":
		var op IPv6PDGuardSync
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, e := d.ApplyIPv6PDGuard(ctx, op)
		if e != nil {
			return nil, e
		}
		return json.Marshal(undo)
	case "ipv6_pd_guard_restore":
		var op IPv6PDGuardUndo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if e := d.RestoreIPv6PDGuard(ctx, op); e != nil {
			return nil, e
		}
		return json.RawMessage(`{}`), nil
	default:
		return d.applyBase(ctx, raw)
	}
}

// ReadState returns routes, NAT exemptions, and only the forwarding evidence
// needed to assess the active source-NAT selectors.
func (d *RouterOS) ReadState(ctx context.Context) (*RouterState, error) {
	d.forwardFilterRows = nil
	state := &RouterState{Routes: []*RouteEntry{}, NatBypass: []*NatBypassEntry{}, AddressSets: map[string][]string{}}
	routes, err := d.list(ctx, rosRoutes)
	if err != nil {
		return nil, err
	}
	nats, err := d.list(ctx, rosNAT)
	if err != nil {
		return nil, err
	}
	mangle, err := d.list(ctx, rosMangle)
	if err != nil && !device.IsRouterError(err) {
		return nil, err
	}
	var mangleRows []Row
	if err == nil {
		mangleRows = mangle
	}
	referencedNames := referencedAddressLists(append(slices.Clone(nats), mangleRows...))
	listNames := slices.Clone(referencedNames)
	if (d.natListTouched || slices.Contains(referencedNames, NATSrcList) || slices.Contains(referencedNames, NATDstList)) && !slices.Contains(listNames, "cf-mihomo-nat-src") {
		listNames = append(listNames, "cf-mihomo-nat-src")
	}
	if (d.natListTouched || slices.Contains(referencedNames, NATSrcList) || slices.Contains(referencedNames, NATDstList)) && !slices.Contains(listNames, "cf-mihomo-nat-dst") {
		listNames = append(listNames, "cf-mihomo-nat-dst")
	}
	slices.Sort(listNames)
	listRows, err := d.ReadAddresses(ctx, listNames)
	if err != nil {
		return nil, err
	}
	for _, name := range listNames {
		for _, row := range listRows[name] {
			if slices.Contains(referencedNames, name) && !rosFlag(row["disabled"]) {
				state.AddressSets[name] = append(state.AddressSets[name], row.text("address"))
			}
		}
	}
	if slices.Contains(listNames, NATSrcList) || slices.Contains(listNames, NATDstList) || d.natListTouched {
		state.NATLists, _ = d.stateNATLists(ctx, nats, listRows, mangleRows)
	} else {
		state.NATListsReader = func(ctx context.Context) (*NATLists, error) {
			rows, err := d.ReadAddresses(ctx, []string{NATSrcList, NATDstList})
			if err != nil {
				return nil, err
			}
			return d.stateNATLists(ctx, nats, rows, mangleRows)
		}
	}

	assessed := nats
	if len(d.dnsGuardManifest) != 0 && string(d.dnsGuardManifest) != "null" {
		ids := ownedHairpinIDs(d.dnsGuardManifest, nats)
		assessed = make([]Row, 0, len(nats))
		for _, row := range nats {
			if !ids[row.id()] {
				assessed = append(assessed, row)
			}
		}
	}
	contextRows := &NatContext{
		Routes: selectFields(routes, rosRouteEvidence), Rules: withoutCounters(assessed),
	}
	contextRows.RoutingRules, contextRows.Mangle, contextRows.VRFs = nil, nil, nil
	contextRows.InterfaceLists, contextRows.InterfaceMembers = nil, nil

	scoped := []Row{}
	for _, row := range assessed {
		if row.is("chain", "srcnat") && !rosFlag(row["disabled"]) &&
			(row.set("out-interface") || row.set("out-interface-list")) {
			scoped = append(scoped, row)
		}
	}
	if len(scoped) > 0 {
		for _, item := range []struct{ field, path string }{
			{"routing_rules", "/routing/rule"}, {"mangle", rosMangle}, {"vrfs", "/ip/vrf"},
		} {
			if item.field == "mangle" && mangleRows != nil {
				contextRows.Mangle = withoutCounters(mangleRows)
				continue
			}
			rows, readErr := d.list(ctx, item.path)
			if readErr != nil {
				if !device.IsRouterError(readErr) {
					return nil, readErr
				}
				continue
			}
			switch item.field {
			case "routing_rules":
				contextRows.RoutingRules = rows
			case "mangle":
				contextRows.Mangle = withoutCounters(rows)
			case "vrfs":
				contextRows.VRFs = rows
			}
		}
		if slices.ContainsFunc(scoped, func(row Row) bool { return row.set("out-interface-list") }) {
			for _, item := range []struct{ field, path string }{
				{"interface_lists", "/interface/list"}, {"interface_members", "/interface/list/member"},
			} {
				rows, readErr := d.list(ctx, item.path)
				if readErr != nil {
					if !device.IsRouterError(readErr) {
						return nil, readErr
					}
					continue
				}
				if item.field == "interface_lists" {
					contextRows.InterfaceLists = rows
				} else {
					contextRows.InterfaceMembers = rows
				}
			}
		}
	}
	state.NatContext = contextRows

	for _, row := range routes {
		dst, ok := netipx.ParseNetwork(row.text("dst-address"), false)
		if !ok {
			continue
		}
		var active *bool
		if row.has("active") {
			active = new(rosFlag(row["active"]))
		} else if row.has("inactive") {
			active = new(!rosFlag(row["inactive"]))
		}
		if rosFlag(row["inactive"]) || rosFlag(row["invalid"]) {
			active = new(false)
		}
		var distance *int64
		if n, parseErr := strconv.ParseInt(row.text("distance"), 10, 64); parseErr == nil {
			distance = &n
		}
		table := device.FirstNonEmpty(row.text("routing-table"), "main")
		state.Routes = append(state.Routes, &RouteEntry{ID: row.id(), Dst: dst.String(), Gateway: row.text("gateway"),
			Enabled: !rosFlag(row["disabled"]), Comment: row.text("comment"), Table: table, Active: active, Distance: distance})
	}
	for _, row := range nats {
		comment := row.text("comment")
		if strings.HasPrefix(comment, "cf-ros-policy-") || strings.HasPrefix(comment, "cf-dnsguard:") ||
			!row.is("chain", "srcnat") || !row.is("action", "accept") {
			continue
		}
		dst, src := []string{}, []string{}
		if row.set("dst-address") {
			dst = append(dst, row.text("dst-address"))
		}
		if row.set("dst-address-list") {
			dst = append(dst, state.AddressSets[row.text("dst-address-list")]...)
		}
		if row.set("src-address") {
			src = append(src, row.text("src-address"))
		}
		if row.set("src-address-list") {
			src = append(src, state.AddressSets[row.text("src-address-list")]...)
		}
		attrs := make(Row)
		for key, value := range row {
			if !rosReadOnly[key] && !strings.HasPrefix(key, ".") {
				attrs[key] = value
			}
		}
		encoded, marshalErr := json.Marshal(attrs)
		if marshalErr != nil {
			return nil, marshalErr
		}
		state.NatBypass = append(state.NatBypass, &NatBypassEntry{ID: row.id(), Dst: dst, Src: src,
			Enabled: !rosFlag(row["disabled"]), Comment: comment, Attributes: encoded})
	}
	if len(d.policyManifest) != 0 {
		if err := d.filterPolicyMangle(contextRows); err != nil && !errors.Is(err, device.ErrNotPorted) {
			return nil, err
		}
	}
	return state, nil
}
