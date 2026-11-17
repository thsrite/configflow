package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"reflect"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

var editROSRuntime = stringSet("active", "inactive", "immediate-gw", "belongs-to", "hw-offloaded", "debug.fwp-ptr", "actual-interface", "bytes", "packets", "uptime", "last-updated")
var editROSSpecial = stringSet("connect", "ecmp", "blackhole", "unreachable", "prohibit", "bgp", "ospf", "rip", "dhcp", "vpn", "modem", "local", "invalid")
var editROSFlags = func() map[string]bool {
	v := maps.Clone(editROSSpecial)
	for _, s := range []string{"dynamic", "static", "disabled", "suppress-hw-offload"} {
		v[s] = true
	}
	return v
}()
var editROSDefaults = func() Row {
	v := Row{"routing-table": "main", "disabled": "false", "comment": "", "distance": "1", "scope": "30", "target-scope": "10", "check-gateway": "none", "pref-src": "", "vrf-interface": "", "suppress-hw-offload": "false", "dynamic": "false", "static": "true"}
	for k := range editROSSpecial {
		v[k] = "false"
	}
	return v
}()

func editROSFlag(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(v) {
		case "true", "yes":
			return true, true
		case "false", "no":
			return false, true
		}
	}
	return false, false
}
func editROSSnapshot(row Row) Row {
	out := cloneRow(row)
	for k := range editROSRuntime {
		delete(out, k)
	}
	return out
}
func editROSEntry(row Row, duplicates map[string]bool) *EditableRoute {
	reasons := []string{}
	id, _ := row[".id"].(string)
	if !rosIDPattern.MatchString(id) {
		reasons = append(reasons, messageEditROSID)
	} else if duplicates[id] {
		reasons = append(reasons, messageEditROSDuplicateID)
	}
	cidr := rosText(row["dst-address"])
	if p, ok := netipx.ParseNetwork4(cidr, false); ok {
		cidr = p.String()
	} else {
		reasons = append(reasons, messageEditROSCIDR)
	}
	gateway := rosText(row["gateway"])
	if a, err := netip.ParseAddr(gateway); err == nil && a.Is4() {
		gateway = a.String()
	} else {
		reasons = append(reasons, messageEditROSGateway)
	}
	table := rosTextDefault(row["routing-table"], "main")
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
	if rosText(row["vrf-interface"]) != "" {
		reasons = append(reasons, messageEditROSVRF)
	}
	unknown := []string{}
	badType := false
	for k, v := range row {
		if !rosRouteFields[k] && !editROSRuntime[k] && !editROSFlags[k] && k != ".id" {
			unknown = append(unknown, k)
		}
		if !editROSRuntime[k] {
			switch v.(type) {
			case string, bool, int, int64:
			case json.Number:
				if _, ok := jsonInt(v); !ok {
					badType = true
				}
			default:
				badType = true
			}
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		reasons = append(reasons, fmt.Sprintf(messageEditROSUnknown, strings.Join(unknown, "、")))
	}
	if badType {
		reasons = append(reasons, messageEditROSFieldType)
	}
	comment := ""
	if v, ok := row["comment"]; ok {
		var valid bool
		comment, valid = v.(string)
		if !valid {
			comment = rosText(v)
			reasons = append(reasons, messageEditROSComment)
		}
	}
	disabled, valid := editROSFlag(row["disabled"])
	if _, ok := row["disabled"]; !ok {
		valid = true
	}
	return &EditableRoute{RouteFields: RouteFields{ID: id, CIDR: cidr, Gateway: gateway, Enabled: valid && !disabled, Comment: comment, Table: table, Editable: len(reasons) == 0, Reason: strings.Join(uniqueStrings(reasons), "；"), Managed: IsManaged(comment)}, Snapshot: editROSSnapshot(row)}
}
func editROSEntries(rows []Row) []*EditableRoute {
	seen, duplicates := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		if id, ok := r[".id"].(string); ok {
			if seen[id] {
				duplicates[id] = true
			}
			seen[id] = true
		}
	}
	out := make([]*EditableRoute, 0, len(rows))
	for _, r := range rows {
		out = append(out, editROSEntry(r, duplicates))
	}
	return out
}

// ReadRoutes reads manual route snapshots, retaining read-only rows for display.
func (d *RouterOS) ReadRoutes(ctx context.Context) ([]*EditableRoute, error) {
	rows, err := d.list(ctx, rosRoutes)
	if err != nil {
		return nil, err
	}
	return editROSEntries(rows), nil
}
func editROSCanonical(row Row, ignoreID bool) Row {
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
			if p, ok := netipx.ParseNetwork4(rosText(v), false); ok {
				out[k] = p.String()
			}
		} else if k == "distance" || k == "scope" || k == "target-scope" {
			out[k] = rosText(v)
		}
	}
	return out
}
func editROSSame(a, b Row, ignoreID bool) bool {
	return sameRow(editROSCanonical(a, ignoreID), editROSCanonical(b, ignoreID))
}
func editROSFind(rows []Row, id string) (Row, error) {
	var found Row
	for _, row := range rows {
		if row[".id"] == id {
			if found != nil {
				return nil, routerError(messageEditROSFindID)
			}
			found = row
		}
	}
	return found, nil
}
func editROSNoDuplicate(rows []Row, cidr, id string, before bool) error {
	for _, r := range editROSEntries(rows) {
		if r.Table == "main" && r.CIDR == cidr && r.ID != id {
			kind := KindRouter
			if before {
				kind = KindConflict
			}
			return rosError(kind, fmt.Sprintf(messageEditROSDuplicate, cidr))
		}
	}
	return nil
}
func editValues(values *RouteValues, message string) (netip.Prefix, string, error) {
	if values == nil {
		return netip.Prefix{}, "", routerError(messageEditVendorValues)
	}
	p, ok := netipx.ParseNetwork4(values.CIDR, false)
	g, err := netip.ParseAddr(values.Gateway)
	if !ok || err != nil || !g.Is4() {
		return netip.Prefix{}, "", routerError(message)
	}
	return p, g.String(), nil
}
func editResource(id string) string { return rosRoutes + "/" + escapeROSPathID(id) }

// MutateRoute edits one route, proving ownership again before every compensation.
func (d *RouterOS) MutateRoute(ctx context.Context, action string, expected *EditableRoute, values *RouteValues) (*RouteMutation, error) {
	ctx = context.WithoutCancel(ctx)
	if action != "add" && action != "update" && action != "delete" {
		return nil, routerError(messageEditVendorAction)
	}
	var desired Row
	if action != "delete" {
		p, g, err := editValues(values, messageEditROSValues)
		if err != nil {
			return nil, err
		}
		disabled := "false"
		if !values.Enabled {
			disabled = "true"
		}
		desired = Row{"dst-address": p.String(), "gateway": g, "disabled": disabled, "comment": values.Comment}
	}
	previous, err := d.list(ctx, rosRoutes)
	if err != nil {
		return nil, err
	}
	var before *EditableRoute
	var after Row
	id := ""
	if action == "add" {
		if expected != nil {
			return nil, routerError(messageEditROSAddOld)
		}
		if err := editROSNoDuplicate(previous, rosText(desired["dst-address"]), "", true); err != nil {
			return nil, err
		}
		desired["routing-table"] = "main"
	} else {
		if expected == nil || expected.Snapshot == nil || expected.ID == "" {
			return nil, rosError(KindConflict, messageEditROSSnapshot)
		}
		before = editableByID(editROSEntries(previous), expected.ID)
		if !reflect.DeepEqual(before, expected) {
			return nil, rosError(KindConflict, messageEditROSDrift)
		}
		if !before.Editable {
			return nil, rosError(KindConflict, firstNonEmpty(before.Reason, messageEditReadonly))
		}
		id = before.ID
		if action == "update" {
			if err := editROSNoDuplicate(previous, rosText(desired["dst-address"]), id, true); err != nil {
				return nil, err
			}
			after = mergeRows(before.Snapshot, desired)
			if editROSSame(before.Snapshot, after, false) {
				return &RouteMutation{Entry: before}, nil
			}
		}
	}
	result, err := d.editROSWrite(ctx, action, desired, previous, &id, &after, before)
	if err == nil {
		return result, nil
	}
	if recovery := d.editROSCompensate(ctx, action, previous, before, id, after); recovery != nil {
		return nil, rollbackError(fmt.Sprintf(messageEditROSRollback, err, recovery))
	}
	return nil, err
}
func (d *RouterOS) editROSWrite(ctx context.Context, action string, desired Row, previous []Row, id *string, after *Row, before *EditableRoute) (*RouteMutation, error) {
	if action == "add" {
		response, err := d.request(ctx, "PUT", rosRoutes, desired, 0)
		if err != nil {
			return nil, err
		}
		row, ok := response.(map[string]any)
		candidate, _ := row[".id"].(string)
		if !ok || !rosIDPattern.MatchString(candidate) || findRow(previous, candidate) != nil {
			return nil, rosError(KindUncertainWrite, messageEditROSCreateID)
		}
		*id = candidate
		*after = mergeRows(desired, editROSSnapshot(row))
		(*after)[".id"] = candidate
		expected := editROSCanonical(desired, false)
		actual := editROSCanonical(*after, false)
		for k := range desired {
			if !reflect.DeepEqual(actual[k], expected[k]) {
				return nil, routerError(messageEditROSCreateMismatch)
			}
		}
	} else {
		method := "PATCH"
		if action == "delete" {
			method = "DELETE"
		}
		if _, err := d.request(ctx, method, editResource(*id), desired, 0); err != nil {
			return nil, err
		}
	}
	rows, err := d.list(ctx, rosRoutes)
	if err != nil {
		return nil, err
	}
	current, err := editROSFind(rows, *id)
	if err != nil {
		return nil, err
	}
	if action == "delete" {
		if current != nil {
			return nil, routerError(messageEditROSDeleteRead)
		}
		return &RouteMutation{Changed: true}, nil
	}
	if current == nil || !editROSSame(current, *after, false) {
		return nil, routerError(messageEditROSReadMismatch)
	}
	entry := editROSEntry(current, nil)
	if !entry.Editable {
		return nil, routerError(messageEditROSAfterReadonly + entry.Reason)
	}
	if err := editROSNoDuplicate(rows, entry.CIDR, *id, false); err != nil {
		return nil, err
	}
	return &RouteMutation{Changed: true, Entry: entry}, nil
}
func (d *RouterOS) editROSRestore(ctx context.Context, method, path string, values Row, verify func([]Row, any) bool) error {
	response, writeErr := d.request(ctx, method, path, values, 0)
	rows, err := d.list(ctx, rosRoutes)
	if err != nil {
		return err
	}
	if !verify(rows, response) {
		detail := ""
		if writeErr != nil {
			detail = fmt.Sprintf(messageEditROSRestoreDetail, writeErr)
		}
		return routerError(messageEditROSRestoreRead + detail)
	}
	return nil
}
func (d *RouterOS) editROSCompensate(ctx context.Context, action string, previous []Row, before *EditableRoute, id string, after Row) error {
	rows, err := d.list(ctx, rosRoutes)
	if err != nil {
		return err
	}
	if action == "add" {
		if id == "" {
			old, now := []Row{}, []Row{}
			for _, r := range previous {
				old = append(old, editROSCanonical(r, false))
			}
			for _, r := range rows {
				now = append(now, editROSCanonical(r, false))
			}
			if reflect.DeepEqual(old, now) {
				return nil
			}
			return routerError(messageEditROSUnowned)
		}
		current, err := editROSFind(rows, id)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if !editROSSame(current, after, false) {
			return routerError(messageEditROSAddChanged)
		}
		return d.editROSRestore(ctx, "DELETE", editResource(id), nil, func(latest []Row, _ any) bool { r, e := editROSFind(latest, id); return e == nil && r == nil })
	}
	current, err := editROSFind(rows, before.ID)
	if err != nil {
		return err
	}
	if current != nil && editROSSame(current, before.Snapshot, false) {
		return nil
	}
	if action == "update" {
		if current == nil || !editROSSame(current, after, false) {
			return routerError(messageEditROSUpdateChanged)
		}
		if err := editROSNoDuplicate(rows, before.CIDR, before.ID, false); err != nil {
			return err
		}
		original := Row{}
		for _, k := range []string{"dst-address", "gateway", "disabled", "comment"} {
			v, ok := before.Snapshot[k]
			if !ok {
				v = editROSDefaults[k]
				if v == nil {
					v = ""
				}
			}
			original[k] = v
		}
		return d.editROSRestore(ctx, "PATCH", editResource(before.ID), original, func(latest []Row, _ any) bool {
			r, e := editROSFind(latest, before.ID)
			return e == nil && r != nil && editROSSame(r, before.Snapshot, false)
		})
	}
	if current != nil {
		return routerError(messageEditROSDeleteChanged)
	}
	if err := editROSNoDuplicate(rows, before.CIDR, "", false); err != nil {
		return err
	}
	prior := rowIDs(rows)
	return d.editROSRestore(ctx, "PUT", rosRoutes, payload(rosRoutes, before.Snapshot), func(latest []Row, response any) bool {
		candidates := []Row{}
		for _, r := range latest {
			if !prior[r.text(".id")] && editROSSame(r, before.Snapshot, true) {
				candidates = append(candidates, r)
			}
		}
		candidate := ""
		if r, ok := response.(map[string]any); ok {
			candidate, _ = r[".id"].(string)
		}
		return len(candidates) == 1 && (candidate == "" || candidates[0][".id"] == candidate)
	})
}
