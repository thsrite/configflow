package routeedit

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"fmt"
	"maps"
	"net/netip"
	"reflect"
	"slices"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
)

var editROSRuntime = device.StringSet("active", "inactive", "immediate-gw", "belongs-to", "hw-offloaded", "debug.fwp-ptr", "actual-interface", "bytes", "packets", "uptime", "last-updated")
var editROSSpecial = device.StringSet("connect", "ecmp", "blackhole", "unreachable", "prohibit", "bgp", "ospf", "rip", "dhcp", "vpn", "modem", "local", "invalid")
var editROSFlags = func() map[string]bool {
	v := maps.Clone(editROSSpecial)
	for _, s := range []string{"dynamic", "static", "disabled", "suppress-hw-offload"} {
		v[s] = true
	}
	return v
}()
var editROSDefaults = func() ros.Row {
	v := ros.Row{"routing-table": "main", "disabled": "false", "comment": "", "distance": "1", "scope": "30", "target-scope": "10", "check-gateway": "none", "pref-src": "", "vrf-interface": "", "suppress-hw-offload": "false", "dynamic": "false", "static": "true"}
	for k := range editROSSpecial {
		v[k] = "false"
	}
	return v
}()

func editROSFlag(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "true", "yes":
		return true, true
	case "false", "no":
		return false, true
	}
	return false, false
}
func editROSSnapshot(row ros.Row) ros.Row {
	out := ros.CloneRow(row)
	for k := range editROSRuntime {
		delete(out, k)
	}
	return out
}
func EditROSEntry(row ros.Row, duplicates map[string]bool) *EditableRoute {
	reasons := []string{}
	id := row[".id"]
	if !ros.IDPattern.MatchString(id) {
		reasons = append(reasons, messageEditROSID)
	} else if duplicates[id] {
		reasons = append(reasons, messageEditROSDuplicateID)
	}
	cidr := row["dst-address"]
	if p, ok := netipx.ParseNetwork4(cidr, false); ok {
		cidr = p.String()
	} else {
		reasons = append(reasons, messageEditROSCIDR)
	}
	gateway := row["gateway"]
	if a, err := netip.ParseAddr(gateway); err == nil && a.Is4() {
		gateway = a.String()
	} else {
		reasons = append(reasons, messageEditROSGateway)
	}
	table := row.TextOr("routing-table", "main")
	if table != "main" {
		reasons = append(reasons, messageEditROSTable)
	}
	for _, key := range slices.Sorted(maps.Keys(editROSFlags)) {
		if v, ok := row[key]; ok {
			if _, valid := editROSFlag(v); !valid {
				reasons = append(reasons, fmt.Sprintf(messageEditROSFlag, key))
			}
		}
	}
	flag := func(k string) bool { v, _ := editROSFlag(row[k]); return v }
	if flag("dynamic") {
		reasons = append(reasons, messageEditROSDynamic)
	}
	if v, ok := row["static"]; ok {
		if f, valid := editROSFlag(v); valid && !f {
			reasons = append(reasons, messageEditROSStatic)
		}
	}
	if flag("connect") {
		reasons = append(reasons, messageEditROSConnect)
	}
	for k := range editROSSpecial {
		if k != "connect" && flag(k) {
			reasons = append(reasons, messageEditROSSpecial)
			break
		}
	}
	if row["vrf-interface"] != "" {
		reasons = append(reasons, messageEditROSVRF)
	}
	unknown := []string{}
	badType := false
	for k, v := range row {
		if !ros.RouteFields[k] && !editROSRuntime[k] && !editROSFlags[k] && k != ".id" {
			unknown = append(unknown, k)
		}
		if !editROSRuntime[k] && ros.StructuredText(v) {
			badType = true
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		reasons = append(reasons, fmt.Sprintf(messageEditROSUnknown, strings.Join(unknown, "、")))
	}
	if badType {
		reasons = append(reasons, messageEditROSFieldType)
	}
	comment := row["comment"]
	if ros.StructuredText(comment) {
		comment = ""
		reasons = append(reasons, messageEditROSComment)
	}
	disabled, valid := editROSFlag(row["disabled"])
	if _, ok := row["disabled"]; !ok {
		valid = true
	}
	return &EditableRoute{RouteFields: RouteFields{ID: id, CIDR: cidr, Gateway: gateway, Enabled: valid && !disabled, Comment: comment, Table: table, Editable: len(reasons) == 0, Reason: strings.Join(device.Unique(reasons), "；"), Managed: device.IsManaged(comment)}, Snapshot: RouteSnapshot{ros: editROSSnapshot(row)}}
}
func editROSEntries(rows []ros.Row) []*EditableRoute {
	seen, duplicates := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		if id, ok := r[".id"]; ok {
			if seen[id] {
				duplicates[id] = true
			}
			seen[id] = true
		}
	}
	out := make([]*EditableRoute, 0, len(rows))
	for _, r := range rows {
		out = append(out, EditROSEntry(r, duplicates))
	}
	return out
}

// ReadRoutes reads manual route snapshots, retaining read-only rows for display.
func (d *rosDevice) ReadRoutes(ctx context.Context) (result []*EditableRoute, err error) {
	defer func() { err = SafeError(err, d.Endpoint(), d.Password()) }()
	rows, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return nil, err
	}
	return editROSEntries(rows), nil
}
func editROSCanonical(row ros.Row, ignoreID bool) ros.Row {
	out := maps.Clone(editROSDefaults)
	maps.Copy(out, editROSSnapshot(row))
	if ignoreID {
		delete(out, ".id")
	}
	for k, v := range out {
		if editROSFlags[k] {
			if f, ok := editROSFlag(v); ok {
				out[k] = "false"
				if f {
					out[k] = "true"
				}
			}
		} else if k == "dst-address" {
			if p, ok := netipx.ParseNetwork4(v, false); ok {
				out[k] = p.String()
			}
		} else if k == "distance" || k == "scope" || k == "target-scope" {
			out[k] = v
		}
	}
	return out
}
func editROSSame(a, b ros.Row, ignoreID bool) bool {
	return ros.SameRow(editROSCanonical(a, ignoreID), editROSCanonical(b, ignoreID))
}
func editROSFind(rows []ros.Row, id string) (ros.Row, error) {
	var found ros.Row
	for _, row := range rows {
		if row[".id"] == id {
			if found != nil {
				return nil, device.Error(messageEditROSFindID)
			}
			found = row
		}
	}
	return found, nil
}
func editROSNoDuplicate(rows []ros.Row, cidr, id string, before bool) error {
	for _, r := range editROSEntries(rows) {
		if r.Table == "main" && r.CIDR == cidr && r.ID != id {
			kind := device.KindRouter
			if before {
				kind = device.KindConflict
			}
			return device.KindError(kind, fmt.Sprintf(messageEditROSDuplicate, cidr))
		}
	}
	return nil
}
func editValues(values *RouteValues, message string) (netip.Prefix, string, error) {
	if values == nil {
		return netip.Prefix{}, "", device.Error(messageEditVendorValues)
	}
	p, ok := netipx.ParseNetwork4(values.CIDR, false)
	g, err := netip.ParseAddr(values.Gateway)
	if !ok || err != nil || !g.Is4() {
		return netip.Prefix{}, "", device.Error(message)
	}
	return p, g.String(), nil
}
func editResource(id string) string { return ros.PathRoutes + "/" + ros.EscapePathID(id) }

// MutateRoute edits one route, proving ownership again before every compensation.
func (d *rosDevice) MutateRoute(ctx context.Context, action string, expected *EditableRoute, values *RouteValues) (result *RouteMutation, err error) {
	defer func() { err = SafeError(err, d.Endpoint(), d.Password()) }()
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if action != "add" && action != "update" && action != "delete" {
		return nil, device.Error(messageEditVendorAction)
	}
	var desired ros.Row
	if action != "delete" {
		p, g, err := editValues(values, messageEditROSValues)
		if err != nil {
			return nil, err
		}
		disabled := "false"
		if !values.Enabled {
			disabled = "true"
		}
		desired = ros.Row{"dst-address": p.String(), "gateway": g, "disabled": disabled, "comment": values.Comment}
	}
	previous, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return nil, err
	}
	var before *EditableRoute
	var after ros.Row
	id := ""
	if action == "add" {
		if expected != nil {
			return nil, device.Error(messageEditROSAddOld)
		}
		if err := editROSNoDuplicate(previous, desired["dst-address"], "", true); err != nil {
			return nil, err
		}
		desired["routing-table"] = "main"
	} else {
		if expected == nil || expected.Snapshot.ros == nil || expected.ID == "" {
			return nil, device.KindError(device.KindConflict, messageEditROSSnapshot)
		}
		before = editableByID(editROSEntries(previous), expected.ID)
		if !reflect.DeepEqual(before, expected) {
			return nil, device.KindError(device.KindConflict, messageEditROSDrift)
		}
		if !before.Editable {
			return nil, device.KindError(device.KindConflict, device.FirstNonEmpty(before.Reason, messageEditReadonly))
		}
		id = before.ID
		if action == "update" {
			if err := editROSNoDuplicate(previous, desired["dst-address"], id, true); err != nil {
				return nil, err
			}
			after = ros.MergeRows(before.Snapshot.ros, desired)
			if editROSSame(before.Snapshot.ros, after, false) {
				return &RouteMutation{Entry: before}, nil
			}
		}
	}
	result, err = d.editROSWrite(ctx, action, desired, previous, &id, &after)
	if err == nil {
		return result, nil
	}
	if recovery := d.editROSCompensate(ctx, action, previous, before, id, after); recovery != nil {
		return nil, device.RollbackError(fmt.Sprintf(messageEditROSRollback, err, recovery))
	}
	return nil, err
}
func (d *rosDevice) editROSWrite(ctx context.Context, action string, desired ros.Row, previous []ros.Row, id *string, after *ros.Row) (*RouteMutation, error) {
	if action == "add" {
		response, err := d.Request(ctx, "PUT", ros.PathRoutes, desired, ros.Timeout)
		if err != nil {
			return nil, err
		}
		row := ros.AsRow(response)
		candidate := row[".id"]
		if !ros.IDPattern.MatchString(candidate) || ros.FindRow(previous, candidate) != nil {
			return nil, device.KindError(device.KindUncertainWrite, messageEditROSCreateID)
		}
		*id = candidate
		*after = ros.MergeRows(desired, editROSSnapshot(row))
		(*after)[".id"] = candidate
		expected := editROSCanonical(desired, false)
		actual := editROSCanonical(*after, false)
		for k := range desired {
			if !reflect.DeepEqual(actual[k], expected[k]) {
				return nil, device.Error(messageEditROSCreateMismatch)
			}
		}
	} else {
		method := "PATCH"
		if action == "delete" {
			method = "DELETE"
		}
		if _, err := d.Request(ctx, method, editResource(*id), desired, ros.Timeout); err != nil {
			return nil, err
		}
	}
	rows, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return nil, err
	}
	current, err := editROSFind(rows, *id)
	if err != nil {
		return nil, err
	}
	if action == "delete" {
		if current != nil {
			return nil, device.Error(messageEditROSDeleteRead)
		}
		return &RouteMutation{Changed: true}, nil
	}
	if current == nil || !editROSSame(current, *after, false) {
		return nil, device.Error(messageEditROSReadMismatch)
	}
	entry := EditROSEntry(current, nil)
	if !entry.Editable {
		return nil, device.Error(messageEditROSAfterReadonly + entry.Reason)
	}
	if err := editROSNoDuplicate(rows, entry.CIDR, *id, false); err != nil {
		return nil, err
	}
	return &RouteMutation{Changed: true, Entry: entry}, nil
}
func (d *rosDevice) editROSRestore(ctx context.Context, method, path string, values ros.Row, verify func([]ros.Row, any) bool) error {
	response, writeErr := d.Request(ctx, method, path, values, ros.Timeout)
	rows, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return err
	}
	if !verify(rows, response) {
		detail := ""
		if writeErr != nil {
			detail = fmt.Sprintf(messageEditROSRestoreDetail, writeErr)
		}
		return device.Error(messageEditROSRestoreRead + detail)
	}
	return nil
}
func (d *rosDevice) editROSCompensate(ctx context.Context, action string, previous []ros.Row, before *EditableRoute, id string, after ros.Row) error {
	rows, err := d.List(ctx, ros.PathRoutes)
	if err != nil {
		return err
	}
	if action == "add" {
		if id == "" {
			old, now := []ros.Row{}, []ros.Row{}
			for _, r := range previous {
				old = append(old, editROSCanonical(r, false))
			}
			for _, r := range rows {
				now = append(now, editROSCanonical(r, false))
			}
			if reflect.DeepEqual(old, now) {
				return nil
			}
			return device.Error(messageEditROSUnowned)
		}
		current, err := editROSFind(rows, id)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if !editROSSame(current, after, false) {
			return device.Error(messageEditROSAddChanged)
		}
		return d.editROSRestore(ctx, "DELETE", editResource(id), nil, func(latest []ros.Row, _ any) bool { r, e := editROSFind(latest, id); return e == nil && r == nil })
	}
	current, err := editROSFind(rows, before.ID)
	if err != nil {
		return err
	}
	if current != nil && editROSSame(current, before.Snapshot.ros, false) {
		return nil
	}
	if action == "update" {
		if current == nil || !editROSSame(current, after, false) {
			return device.Error(messageEditROSUpdateChanged)
		}
		if err := editROSNoDuplicate(rows, before.CIDR, before.ID, false); err != nil {
			return err
		}
		original := ros.Row{}
		for _, k := range []string{"dst-address", "gateway", "disabled", "comment"} {
			v, ok := before.Snapshot.ros[k]
			if !ok {
				v = editROSDefaults[k]
			}
			original[k] = v
		}
		return d.editROSRestore(ctx, "PATCH", editResource(before.ID), original, func(latest []ros.Row, _ any) bool {
			r, e := editROSFind(latest, before.ID)
			return e == nil && r != nil && editROSSame(r, before.Snapshot.ros, false)
		})
	}
	if current != nil {
		return device.Error(messageEditROSDeleteChanged)
	}
	if err := editROSNoDuplicate(rows, before.CIDR, "", false); err != nil {
		return err
	}
	prior := ros.RowIDs(rows)
	return d.editROSRestore(ctx, "PUT", ros.PathRoutes, ros.Payload(ros.PathRoutes, before.Snapshot.ros), func(latest []ros.Row, response any) bool {
		candidates := []ros.Row{}
		for _, r := range latest {
			if ros.IDPattern.MatchString(r.Text(".id")) && !prior[r.Text(".id")] && editROSSame(r, before.Snapshot.ros, true) {
				candidates = append(candidates, r)
			}
		}
		candidate := ""
		if r, ok := response.(map[string]any); ok {
			if raw, exists := r[".id"]; exists && raw != nil && raw != "" {
				var valid bool
				candidate, valid = raw.(string)
				if !valid || !ros.IDPattern.MatchString(candidate) {
					return false
				}
			}
		}
		return len(candidates) == 1 && (candidate == "" || candidates[0][".id"] == candidate)
	})
}

type rosDevice struct{ *ros.Client }

// ReadROSRoutes reads editable static routes from a RouterOS client.
func ReadROSRoutes(ctx context.Context, c *ros.Client) ([]*EditableRoute, error) {
	return (&rosDevice{c}).ReadRoutes(ctx)
}

// MutateROSRoute applies one conservative static-route edit on a RouterOS client.
func MutateROSRoute(ctx context.Context, c *ros.Client, action string, expected *EditableRoute, values *RouteValues) (*RouteMutation, error) {
	return (&rosDevice{c}).MutateRoute(ctx, action, expected, values)
}
