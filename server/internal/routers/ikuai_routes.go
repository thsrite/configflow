package routers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
)

func editIkuaiEntry(raw ikuaiRow) *EditableRoute {
	snapshot := decodeIkuaiRow(raw)
	delete(snapshot, "status")
	if mask, ok := snapshot["netmask"].(string); ok {
		snapshot["netmask"] = ikuaiRouteMask(mask)
	}
	reasons := []string{}
	missing, unknown := []string{}, []string{}
	for _, k := range IkuaiRouteFields {
		if !snapshot.has(k) {
			missing = append(missing, k)
		}
	}
	for k := range snapshot {
		if !slices.Contains(IkuaiRouteFields, k) {
			unknown = append(unknown, k)
		}
	}
	if len(missing) > 0 || len(unknown) > 0 {
		slices.Sort(missing)
		slices.Sort(unknown)
		m, u := strings.Join(missing, ","), strings.Join(unknown, ",")
		if m == "" {
			m = messageEditNone
		}
		if u == "" {
			u = messageEditNone
		}
		reasons = append(reasons, fmt.Sprintf(messageEditIKFields, m, u))
	}
	for _, k := range IkuaiRouteFields {
		v, exists := snapshot[k]
		if !exists {
			continue
		}
		valid := false
		if k == "id" || k == "prio" {
			n, ok := jsonInt(v)
			valid = ok && n >= 0
			if k == "id" {
				valid = ok && n >= 1
			}
		} else {
			_, valid = v.(string)
		}
		if !valid {
			reasons = append(reasons, fmt.Sprintf(messageEditIKType, k))
		}
	}
	if snapshot["ip_type"] != "4" {
		reasons = append(reasons, messageEditIKIPv4)
	}
	if snapshot["enabled"] != "yes" && snapshot["enabled"] != "no" {
		reasons = append(reasons, messageEditIKEnabled)
	}
	if iface, _ := snapshot["interface"].(string); iface == "" {
		reasons = append(reasons, messageEditIKInterface)
	}
	tag, ok := snapshot["tagname"].(string)
	if !ok || utf8.RuneCountInString(tag) < 1 || utf8.RuneCountInString(tag) > 15 {
		reasons = append(reasons, messageEditIKName)
	}
	cidr := ""
	p, ok := netipx.ParseNetwork(snapshot.text("dst_addr")+"/"+snapshot.text("netmask"), false)
	if !ok {
		reasons = append(reasons, messageEditIKNetwork)
	} else {
		cidr = p.String()
		if !p.Addr().Is4() {
			reasons = append(reasons, messageEditIKIPv4)
		}
	}
	gateway, ok := snapshot["gateway"].(string)
	a, err := netip.ParseAddr(gateway)
	if !ok || err != nil || !a.Is4() {
		reasons = append(reasons, messageEditIKGateway)
	}
	comment, _ := snapshot["comment"].(string)
	return &EditableRoute{RouteFields: RouteFields{ID: snapshot.text("id"), CIDR: cidr, Gateway: gateway, Enabled: snapshot["enabled"] == "yes", Comment: comment, Table: "main", Editable: len(reasons) == 0, Reason: strings.Join(uniqueStrings(reasons), "；"), Managed: IsManaged(comment)}, Snapshot: RouteSnapshot{ikuai: snapshot}}
}

// ReadRoutes reads every static_rt page, retaining unsupported forms as read-only.
func (d *Ikuai) ReadRoutes(ctx context.Context) (result []*EditableRoute, err error) {
	defer func() { err = routeSafeError(err, d.endpoint, d.password) }()
	raw, err := d.ListAll(ctx, "static_rt", "", true, "")
	if err != nil {
		return nil, err
	}
	rows := make([]*EditableRoute, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, editIkuaiEntry(r))
	}
	return rows, nil
}
func editIKNoDuplicate(rows []*EditableRoute, cidr, id string) error {
	for _, r := range rows {
		if r.CIDR == cidr && r.ID != id {
			return rosError(KindConflict, fmt.Sprintf(messageEditIKDuplicate, cidr))
		}
	}
	return nil
}
func editIKNew(rows []*EditableRoute, old map[string]bool, desired ikuaiRow) *EditableRoute {
	tag, ok := desired["tagname"].(string)
	if !ok || tag == "" {
		return nil
	}
	var match *EditableRoute
	for _, r := range rows {
		if r.Snapshot.ikuai["tagname"] == tag {
			if match != nil {
				return nil
			}
			match = r
		}
	}
	if match == nil || old[match.ID] || !match.Editable {
		return nil
	}
	actual := maps.Clone(match.Snapshot.ikuai)
	delete(actual, "id")
	if !jsonEqual(actual, desired) {
		return nil
	}
	return match
}
func editIKIDs(rows []*EditableRoute) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.ID] = true
	}
	return out
}
func editIKSnapshot(entry *EditableRoute, snapshot ikuaiRow) bool {
	return entry != nil && entry.Editable && jsonEqual(entry.Snapshot.ikuai, snapshot)
}
func (d *Ikuai) editIKVerify(ctx context.Context, id string, snapshot ikuaiRow) error {
	rows, err := d.ReadRoutes(ctx)
	if err != nil {
		return err
	}
	if !editIKSnapshot(editableByID(rows, id), snapshot) {
		return routerError(messageEditIKRead)
	}
	return nil
}

// MutateRoute completes a manual edit and identity-checked compensation independently of request cancellation.
func (d *Ikuai) MutateRoute(ctx context.Context, action string, expected *EditableRoute, values *RouteValues) (*RouteMutation, error) {
	return d.mutateRoute(ctx, action, expected, values, "")
}

// name is a deterministic per-call seam for the Python boundary corpus, never a global hook.
func (d *Ikuai) mutateRoute(ctx context.Context, action string, expected *EditableRoute, values *RouteValues, name string) (result *RouteMutation, err error) {
	defer func() { err = routeSafeError(err, d.endpoint, d.password) }()
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if action != "add" && action != "update" && action != "delete" {
		return nil, routerError(messageEditVendorAction)
	}
	var p netip.Prefix
	var gateway string
	if action != "delete" {
		var err error
		p, gateway, err = editValues(values, messageEditIKValues)
		if err != nil {
			return nil, err
		}
		normalized := *values
		normalized.CIDR = p.String()
		normalized.Gateway = gateway
		normalized.Comment = strings.TrimSpace(normalized.Comment)
		values = &normalized
	}
	if action == "add" && expected != nil {
		return nil, routerError(messageEditIKAddOld)
	}
	if action != "add" && (expected == nil || expected.ID == "" || expected.Snapshot.ikuai == nil) {
		return nil, routerError(messageEditIKSnapshot)
	}
	previous, err := d.ReadRoutes(ctx)
	if err != nil {
		return nil, err
	}
	var before *EditableRoute
	if action != "add" {
		before = editableByID(previous, expected.ID)
		if !routeIdentityEqual(before, expected) {
			return nil, rosError(KindConflict, messageEditIKDrift)
		}
		if !before.Editable {
			return nil, rosError(KindConflict, firstNonEmpty(before.Reason, messageEditIKReadonly))
		}
		if err := editIKUniqueName(previous, before); err != nil {
			return nil, err
		}
	}
	id := ""
	if before != nil {
		id = before.ID
	}
	if action != "delete" {
		if err := editIKNoDuplicate(previous, values.CIDR, id); err != nil {
			return nil, err
		}
	}
	if action == "update" && routeValuesEqual(before, values) {
		return &RouteMutation{Entry: before}, nil
	}
	var desired ikuaiRow
	if action == "add" {
		if name == "" {
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				return nil, routerError(messageEditIKRandom)
			}
			name = "RT" + hex.EncodeToString(random[:])[:13]
		}
		for _, r := range previous {
			if r.Snapshot.ikuai["tagname"] == name {
				return nil, rosError(KindConflict, messageEditIKNameUsed)
			}
		}
		desired = ikuaiRow{"enabled": "yes", "tagname": name, "ip_type": "4", "interface": "auto", "dst_addr": "", "gateway": "", "netmask": "", "prio": 1, "comment": ""}
	} else {
		desired = IkuaiRoutePayload(before.Snapshot.ikuai, nil)
	}
	if action != "delete" {
		desired["dst_addr"] = p.Addr().String()
		desired["netmask"] = ipv4Mask(p.Bits())
		desired["gateway"] = gateway
		desired["enabled"] = "no"
		if values.Enabled {
			desired["enabled"] = "yes"
		}
		desired["comment"] = values.Comment
	}
	result, err = d.editIKWrite(ctx, action, previous, before, desired, values)
	if err == nil {
		return result, nil
	}
	var recovery error
	switch action {
	case "add":
		recovery = d.editIKRestoreAdd(ctx, previous, desired)
	case "update":
		recovery = d.editIKRestoreUpdate(ctx, before, desired)
	case "delete":
		recovery = d.editIKRestoreDelete(ctx, before)
	}
	if recovery != nil {
		return nil, rollbackError(fmt.Sprintf(messageEditIKRollback, err, recovery))
	}
	return nil, routerError(fmt.Sprintf(messageEditIKRecovered, err))
}
func (d *Ikuai) editIKWrite(ctx context.Context, action string, previous []*EditableRoute, before *EditableRoute, desired ikuaiRow, values *RouteValues) (*RouteMutation, error) {
	apiAction := "edit"
	param := desired
	if action == "add" {
		apiAction = "add"
	}
	if action == "delete" {
		apiAction = "del"
		param = ikuaiRow{"id": before.Snapshot.ikuai["id"]}
	}
	if _, err := d.Write(ctx, "static_rt", apiAction, param, 0); err != nil {
		return nil, err
	}
	rows, err := d.ReadRoutes(ctx)
	if err != nil {
		return nil, err
	}
	if action == "delete" {
		if editableByID(rows, before.ID) != nil {
			return nil, routerError(messageEditIKDeleteRead)
		}
		return &RouteMutation{Changed: true}, nil
	}
	var entry *EditableRoute
	if action == "add" {
		entry = editIKNew(rows, editIKIDs(previous), desired)
	} else {
		entry = editableByID(rows, before.ID)
	}
	if entry == nil || (action == "update" && !editIKSnapshot(entry, desired)) {
		return nil, routerError(messageEditIKRead)
	}
	if err := editIKNoDuplicate(rows, values.CIDR, entry.ID); err != nil {
		return nil, err
	}
	return &RouteMutation{Changed: true, Entry: entry}, nil
}
func (d *Ikuai) editIKRestoreUpdate(ctx context.Context, before *EditableRoute, desired ikuaiRow) error {
	rows, err := d.ReadRoutes(ctx)
	if err != nil {
		return err
	}
	current := editableByID(rows, before.ID)
	if editIKSnapshot(current, before.Snapshot.ikuai) {
		return nil
	}
	if !editIKSnapshot(current, desired) {
		return routerError(messageEditIKUpdateChanged)
	}
	if err := editIKUniqueName(rows, current); err != nil {
		return err
	}
	if err := editIKNoDuplicate(rows, before.CIDR, before.ID); err != nil {
		return err
	}
	_, _ = d.Write(ctx, "static_rt", "edit", IkuaiRoutePayload(before.Snapshot.ikuai, nil), 0)
	return d.editIKVerify(ctx, before.ID, before.Snapshot.ikuai)
}
func (d *Ikuai) editIKRestoreDelete(ctx context.Context, before *EditableRoute) error {
	rows, err := d.ReadRoutes(ctx)
	if err != nil {
		return err
	}
	current := editableByID(rows, before.ID)
	if editIKSnapshot(current, before.Snapshot.ikuai) {
		return nil
	}
	if current != nil {
		return routerError(messageEditIKDeleteChanged)
	}
	if err := editIKNoDuplicate(rows, before.CIDR, ""); err != nil {
		return err
	}
	desired := IkuaiRoutePayload(before.Snapshot.ikuai, nil)
	delete(desired, "id")
	name, ok := desired["tagname"].(string)
	if !ok || name == "" {
		return routerError(messageEditIKName)
	}
	for _, r := range rows {
		if r.Snapshot.ikuai["tagname"] == name {
			return routerError(messageEditIKRestoreName)
		}
	}
	old := editIKIDs(rows)
	_, writeErr := d.Write(ctx, "static_rt", "add", desired, 0)
	latest, err := d.ReadRoutes(ctx)
	if err != nil {
		return err
	}
	if editIKNew(latest, old, desired) == nil {
		if writeErr != nil {
			return writeErr
		}
		return routerError(messageEditIKRestoreRead)
	}
	return nil
}
func (d *Ikuai) editIKRestoreAdd(ctx context.Context, previous []*EditableRoute, desired ikuaiRow) error {
	rows, err := d.ReadRoutes(ctx)
	if err != nil {
		return err
	}
	entry := editIKNew(rows, editIKIDs(previous), desired)
	if entry == nil {
		old, now := map[string]ikuaiRow{}, map[string]ikuaiRow{}
		for _, r := range previous {
			old[r.ID] = r.Snapshot.ikuai
		}
		for _, r := range rows {
			now[r.ID] = r.Snapshot.ikuai
		}
		if jsonEqual(old, now) {
			return nil
		}
		return routerError(messageEditIKUnowned)
	}
	_, writeErr := d.Write(ctx, "static_rt", "del", ikuaiRow{"id": entry.Snapshot.ikuai["id"]}, 0)
	latest, err := d.ReadRoutes(ctx)
	if err != nil {
		return err
	}
	if editableByID(latest, entry.ID) != nil {
		if writeErr != nil {
			return writeErr
		}
		return routerError(messageEditIKUndoRead)
	}
	return nil
}

func editIKUniqueName(rows []*EditableRoute, entry *EditableRoute) error {
	name, ok := entry.Snapshot.ikuai["tagname"].(string)
	if !ok || name == "" {
		return rosError(KindConflict, messageEditIKName)
	}
	count := 0
	for _, r := range rows {
		if r.Snapshot.ikuai["tagname"] == name {
			count++
		}
	}
	if count != 1 {
		return rosError(KindConflict, messageEditIKUniqueName)
	}
	return nil
}
