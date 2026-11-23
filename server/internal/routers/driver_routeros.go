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
	"configflow/internal/routerdoc"
	"configflow/internal/routers/clientlist"
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/fixedip"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routeedit"
	"time"
)

// RouterOS is the composed RouterOS 7 driver: it embeds the low-level REST
// client and adds ReadState and feature-operation Apply dispatch.
type RouterOS struct {
	*ros.Client
}

// NewRouterOS constructs a RouterOS driver with requests-compatible device HTTP.
func NewRouterOS(endpoint, username, password string, verifyTLS bool) *RouterOS {
	return &RouterOS{Client: ros.NewClient(endpoint, username, password, verifyTLS)}
}

// NewRouterOSWithHTTP creates a RouterOS driver with an injected device client.
func NewRouterOSWithHTTP(endpoint string, client DeviceHTTP) *RouterOS {
	return &RouterOS{Client: ros.NewClientWithHTTP(endpoint, client)}
}

// ApplyMany delegates batched route operations to ros.Client while routing any
// non-batched operation (such as nat_list_sync) through the full RouterOS Apply.
func (d *RouterOS) ApplyMany(ctx context.Context, ops []json.RawMessage, record func(json.RawMessage, json.RawMessage)) error {
	return d.Client.ApplyManyWith(ctx, ops, record, d.Apply)
}

// RestoreMany delegates batched route rollbacks to ros.Client while routing any
// non-batched undo record through the full RouterOS Apply.
func (d *RouterOS) RestoreMany(ctx context.Context, undos []json.RawMessage, stopOnFailure bool) []string {
	return d.Client.RestoreManyWith(ctx, undos, stopOnFailure, d.Apply)
}

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
		var op dnsguard.Replace
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := dnsguard.Apply(ctx, d.Client, op)
		if err != nil {
			return nil, err
		}
		out, _ := json.Marshal(undo)
		return out, nil
	case "dns_guard_restore":
		var op dnsguard.Undo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := dnsguard.Restore(ctx, d.Client, op); err != nil {
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
		return d.ApplyBase(ctx, raw)
	}
}

// ReadState returns routes, NAT exemptions, and only the forwarding evidence
// needed to assess the active source-NAT selectors.
func (d *RouterOS) ReadState(ctx context.Context) (*RouterState, error) {
	d.ResetForwardFilterRows()
	state := &RouterState{Routes: []*RouteEntry{}, NatBypass: []*NatBypassEntry{}, AddressSets: map[string][]string{}}
	routes, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return nil, err
	}
	nats, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return nil, err
	}
	mangle, err := d.List(ctx, ros.PathMangle)
	if err != nil && !device.IsRouterError(err) {
		return nil, err
	}
	var mangleRows []Row
	if err == nil {
		mangleRows = mangle
	}
	referencedNames := ros.ReferencedAddressLists(append(slices.Clone(nats), mangleRows...))
	listNames := slices.Clone(referencedNames)
	if (d.NATListTouched() || slices.Contains(referencedNames, NATSrcList) || slices.Contains(referencedNames, NATDstList)) && !slices.Contains(listNames, "cf-mihomo-nat-src") {
		listNames = append(listNames, "cf-mihomo-nat-src")
	}
	if (d.NATListTouched() || slices.Contains(referencedNames, NATSrcList) || slices.Contains(referencedNames, NATDstList)) && !slices.Contains(listNames, "cf-mihomo-nat-dst") {
		listNames = append(listNames, "cf-mihomo-nat-dst")
	}
	slices.Sort(listNames)
	listRows, err := d.ReadAddresses(ctx, listNames)
	if err != nil {
		return nil, err
	}
	for _, name := range listNames {
		for _, row := range listRows[name] {
			if slices.Contains(referencedNames, name) && !ros.Flag(row["disabled"]) {
				state.AddressSets[name] = append(state.AddressSets[name], row.Text("address"))
			}
		}
	}
	if slices.Contains(listNames, NATSrcList) || slices.Contains(listNames, NATDstList) || d.NATListTouched() {
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
	if len(d.DNSGuardManifest()) != 0 && string(d.DNSGuardManifest()) != "null" {
		ids := ros.OwnedHairpinIDs(d.DNSGuardManifest(), nats)
		assessed = make([]Row, 0, len(nats))
		for _, row := range nats {
			if !ids[row.ID()] {
				assessed = append(assessed, row)
			}
		}
	}
	contextRows := &NatContext{
		Routes: ros.SelectFields(routes, ros.RouteEvidence), Rules: ros.WithoutCounters(assessed),
	}
	contextRows.RoutingRules, contextRows.Mangle, contextRows.VRFs = nil, nil, nil
	contextRows.InterfaceLists, contextRows.InterfaceMembers = nil, nil

	scoped := []Row{}
	for _, row := range assessed {
		if row.Is("chain", "srcnat") && !ros.Flag(row["disabled"]) &&
			(row.Set("out-interface") || row.Set("out-interface-list")) {
			scoped = append(scoped, row)
		}
	}
	if len(scoped) > 0 {
		for _, item := range []struct{ field, path string }{
			{"routing_rules", "/routing/rule"}, {"mangle", ros.PathMangle}, {"vrfs", "/ip/vrf"},
		} {
			if item.field == "mangle" && mangleRows != nil {
				contextRows.Mangle = ros.WithoutCounters(mangleRows)
				continue
			}
			rows, readErr := d.List(ctx, item.path)
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
				contextRows.Mangle = ros.WithoutCounters(rows)
			case "vrfs":
				contextRows.VRFs = rows
			}
		}
		if slices.ContainsFunc(scoped, func(row Row) bool { return row.Set("out-interface-list") }) {
			for _, item := range []struct{ field, path string }{
				{"interface_lists", "/interface/list"}, {"interface_members", "/interface/list/member"},
			} {
				rows, readErr := d.List(ctx, item.path)
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
		dst, ok := netipx.ParseNetwork(row.Text("dst-address"), false)
		if !ok {
			continue
		}
		var active *bool
		if row.Has("active") {
			active = new(ros.Flag(row["active"]))
		} else if row.Has("inactive") {
			active = new(!ros.Flag(row["inactive"]))
		}
		if ros.Flag(row["inactive"]) || ros.Flag(row["invalid"]) {
			active = new(false)
		}
		var distance *int64
		if n, parseErr := strconv.ParseInt(row.Text("distance"), 10, 64); parseErr == nil {
			distance = &n
		}
		table := device.FirstNonEmpty(row.Text("routing-table"), "main")
		state.Routes = append(state.Routes, &RouteEntry{ID: row.ID(), Dst: dst.String(), Gateway: row.Text("gateway"),
			Enabled: !ros.Flag(row["disabled"]), Comment: row.Text("comment"), Table: table, Active: active, Distance: distance})
	}
	for _, row := range nats {
		comment := row.Text("comment")
		if strings.HasPrefix(comment, "cf-ros-policy-") || strings.HasPrefix(comment, "cf-dnsguard:") ||
			!row.Is("chain", "srcnat") || !row.Is("action", "accept") {
			continue
		}
		dst, src := []string{}, []string{}
		if row.Set("dst-address") {
			dst = append(dst, row.Text("dst-address"))
		}
		if row.Set("dst-address-list") {
			dst = append(dst, state.AddressSets[row.Text("dst-address-list")]...)
		}
		if row.Set("src-address") {
			src = append(src, row.Text("src-address"))
		}
		if row.Set("src-address-list") {
			src = append(src, state.AddressSets[row.Text("src-address-list")]...)
		}
		attrs := make(Row)
		for key, value := range row {
			if !ros.ReadOnlyFields[key] && !strings.HasPrefix(key, ".") {
				attrs[key] = value
			}
		}
		encoded, marshalErr := json.Marshal(attrs)
		if marshalErr != nil {
			return nil, marshalErr
		}
		state.NatBypass = append(state.NatBypass, &NatBypassEntry{ID: row.ID(), Dst: dst, Src: src,
			Enabled: !ros.Flag(row["disabled"]), Comment: comment, Attributes: encoded})
	}
	if len(d.PolicyManifest()) != 0 {
		if err := d.FilterPolicyMangle(contextRows); err != nil && !errors.Is(err, device.ErrNotPorted) {
			return nil, err
		}
	}
	return state, nil
}

// ReadRoutes implements routeedit.RouteEditor for RouterOS.
func (d *RouterOS) ReadRoutes(ctx context.Context) ([]*EditableRoute, error) {
	return routeedit.ReadROSRoutes(ctx, d.Client)
}

// MutateRoute implements routeedit.RouteEditor for RouterOS.
func (d *RouterOS) MutateRoute(ctx context.Context, action string, expected *EditableRoute, values *RouteValues) (*RouteMutation, error) {
	return routeedit.MutateROSRoute(ctx, d.Client, action, expected, values)
}

// ReadFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) ReadFixedIP(ctx context.Context) (*FixedIPSnapshot, error) {
	return fixedip.NewROS(d.Client).ReadFixedIP(ctx)
}

// PreviewFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) PreviewFixedIP(ctx context.Context, ip, mac, name string) (*FixedIPOperation, error) {
	return fixedip.NewROS(d.Client).PreviewFixedIP(ctx, ip, mac, name)
}

// ApplyFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) ApplyFixedIP(ctx context.Context, op *FixedIPOperation) (*FixedIPBinding, error) {
	return fixedip.NewROS(d.Client).ApplyFixedIP(ctx, op)
}

// RollbackFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) RollbackFixedIP(ctx context.Context, b *FixedIPBinding) error {
	return fixedip.NewROS(d.Client).RollbackFixedIP(ctx, b)
}

// PreviewDeleteFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) PreviewDeleteFixedIP(ctx context.Context, b *FixedIPBinding) (*FixedIPOperation, error) {
	return fixedip.NewROS(d.Client).PreviewDeleteFixedIP(ctx, b)
}

// DeleteFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) DeleteFixedIP(ctx context.Context, op *FixedIPOperation) (*FixedIPBinding, error) {
	return fixedip.NewROS(d.Client).DeleteFixedIP(ctx, op)
}

// RestoreDeletedFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) RestoreDeletedFixedIP(ctx context.Context, b *FixedIPBinding) (*FixedIPBinding, error) {
	return fixedip.NewROS(d.Client).RestoreDeletedFixedIP(ctx, b)
}

// SeenFixedIP implements fixedip.Device for RouterOS.
func (d *RouterOS) SeenFixedIP(ctx context.Context, ip, mac string) (bool, error) {
	return fixedip.NewROS(d.Client).SeenFixedIP(ctx, ip, mac)
}

// ReadClients implements clientlist.Reader for RouterOS.
func (d *RouterOS) ReadClients(ctx context.Context, opts ClientReadOptions) (*routerdoc.Reading, error) {
	return clientlist.NewROS(d.Client).ReadClients(ctx, opts)
}

// ReadConnections delegates RouterOS firewall connection reads to clientlist.
func (d *RouterOS) ReadConnections(ctx context.Context, q ConnectionQuery, timeout time.Duration) ([]Row, bool, error) {
	return clientlist.ReadConnections(ctx, d.Client, q, timeout)
}
