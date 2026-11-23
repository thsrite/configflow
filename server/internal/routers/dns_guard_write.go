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

	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
)

// DNSGuardPublication returns whether this driver assigned a publication and its
// current value. A set null means withdrawal; an unset value leaves storage alone.
func (d *RouterOS) DNSGuardPublication() (json.RawMessage, bool) {
	return ros.GuardRaw(d.DNSGuardManifest()), d.DNSGuardManifestSet()
}

func guardValidateSpec(spec *DNSGuardSpec) bool {
	if spec == nil {
		return true
	}
	a, ok := netipx.ParseAddr4(spec.Agent)
	if !ok || a.String() != spec.Agent || ros.Interface(spec.Interface) == "" {
		return false
	}
	n, ok := netipx.ParseNetwork4(spec.Network, true)
	if !ok || !n.Contains(a) {
		return false
	}
	for _, cidr := range spec.Bypass {
		if _, ok := netipx.ParseNetwork4(cidr, true); !ok {
			return false
		}
	}
	if spec.Exempt != nil {
		for _, host := range *spec.Exempt {
			if _, ok := netipx.ParseAddr4(host); !ok {
				return false
			}
		}
	}
	return true
}
func guardValidateOp(op DNSGuardReplace) bool {
	if !guardValidateSpec(op.Spec) {
		return false
	}
	keep := map[string]bool{}
	if op.Keep != nil {
		for _, role := range *op.Keep {
			if keep[role] {
				return false
			}
			keep[role] = true
		}
	}
	creates := []DNSGuardCreate{}
	roles := []string{}
	for _, item := range DNSGuardResources(op.Spec) {
		roles = append(roles, item.Role)
		if !keep[item.Role] {
			creates = append(creates, item)
		}
	}
	if !reflect.DeepEqual(creates, op.Create) {
		return false
	}
	if op.Roles != nil && !slices.Equal(roles, *op.Roles) {
		return false
	}
	for role := range keep {
		if !slices.Contains(roles, role) {
			return false
		}
	}
	return true
}

func guardCheckPreview(op DNSGuardReplace, owned []DNSGuardResource, live *DNSGuardSnapshot) error {
	if len(owned) != len(op.Owned) {
		return device.Error(messageGuardChanged)
	}
	for i, e := range owned {
		want := op.Owned[i]
		if e.Path != want.Path || e.ID != want.ID || e.Role != want.Role || !ros.SameRow(e.Config, want.Config) {
			return device.Error(messageGuardChanged)
		}
	}
	byRole := map[string]DNSGuardResource{}
	ownedIDs := map[[2]string]bool{}
	for _, e := range owned {
		byRole[e.Role] = e
		ownedIDs[[2]string{e.Path, e.ID}] = true
	}
	if op.Spec != nil {
		if guardForeignHijack((*live)[ros.PathNAT], ownedIDs) {
			return device.Error(messageGuardHijackCollision)
		}
		// A collision introduced after preview is still foreign, even if it uses
		// our familiar comment/name. Never claim it by that marker.
		for _, r := range []struct{ path, key, name string }{{ros.PathAddresses, "list", DNSGuardBypassList}, {ros.PathNetwatch, "name", dnsGuardWatch}} {
			for _, row := range (*live)[r.path] {
				if row.Is(r.key, r.name) && !ownedIDs[[2]string{r.path, row.ID()}] {
					return device.Error(messageGuardChanged)
				}
			}
		}
		if op.Spec.Failover && (*live)[ros.PathNetwatch] == nil {
			return device.Error(messageGuardWatchUnsupported)
		}
	}
	if op.Keep != nil {
		keep := device.StringSet((*op.Keep)...)
		for _, item := range DNSGuardResources(op.Spec) {
			if keep[item.Role] {
				e, ok := byRole[item.Role]
				row := ros.FindRow((*live)[item.Path], e.ID)
				if !ok || e.Path != item.Path || row == nil || !guardMatches(item, row, op.Spec.Failover) {
					return device.Error(messageGuardChanged)
				}
				if strings.HasPrefix(item.Role, "hijack") && !guardOnTop(e.ID, (*live)[ros.PathNAT], ownedIDs) {
					return device.Error(messageGuardChanged)
				}
			}
		}
	}
	return nil
}

func (d *RouterOS) rollbackDNSGuard(ctx context.Context, changes []ROSChange) (map[[2]string]string, error) {
	remap := map[[2]string]string{}
	failures := []string{}
	for _, change := range changes {
		if !guardPath(change.Path) || (change.Method != "RESTORE" && change.Method != "DELETE") || !ros.IDPattern.MatchString(change.ID) || len(change.Expected) == 0 {
			failures = append(failures, device.MessageOperationFormat)
			continue
		}
		if change.Method == "RESTORE" && change.Values != nil {
			change.Values = ros.CloneRow(change.Values)
			if id, ok := remap[[2]string{change.Path, change.Values.Text("place-before")}]; ok {
				change.Values["place-before"] = id
			}
		}
		if err := ctx.Err(); err != nil {
			return remap, err
		}
		check, err := d.Perform(ctx, change, &[]ROSChange{}, true)
		if err == nil {
			err = d.VerifyChange(ctx, check)
		}
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if change.Method == "RESTORE" {
			remap[[2]string{change.Path, change.ID}] = check.ID
		}
	}
	if len(failures) > 0 {
		return remap, device.RollbackError(messageGuardRollback + strings.Join(failures, "；"))
	}
	return remap, nil
}
func (d *RouterOS) remapDNSGuard(ctx context.Context, raw json.RawMessage, remap map[[2]string]string) error {
	m, err := parseDNSGuardManifest(raw)
	if err != nil {
		return err
	}
	if m == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		d.SetDNSGuardManifest(nil)
		return nil
	}
	for i, e := range m.Resources {
		if id, ok := remap[[2]string{e.Path, e.ID}]; ok {
			row, err := d.Row(ctx, e.Path, id)
			if err != nil {
				return err
			}
			m.Resources[i].ID = id
			m.Resources[i].Config = ros.StableRow(e.Path, row)
		}
	}
	// Preserve unknown stored manifest members while refreshing only resources.
	var document map[string]json.RawMessage
	if json.Unmarshal(raw, &document) != nil {
		return device.Error(messageGuardManifest)
	}
	var resources []map[string]json.RawMessage
	if json.Unmarshal(document["resources"], &resources) != nil || len(resources) != len(m.Resources) {
		return device.Error(messageGuardManifest)
	}
	for i, e := range m.Resources {
		resources[i]["id"], _ = json.Marshal(e.ID)
		resources[i]["config"], _ = json.Marshal(e.Config)
	}
	document["resources"], _ = json.Marshal(resources)
	value, _ := json.Marshal(document)
	if err := ctx.Err(); err != nil {
		return err
	}
	d.SetDNSGuardManifest(value)
	return nil
}

// ApplyDNSGuard executes only the previewed delta. Accepted writes and readback
// finish after browser cancellation; device calls retain their 10-second bound.
func (d *RouterOS) ApplyDNSGuard(ctx context.Context, op DNSGuardReplace) (*DNSGuardUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !guardValidateOp(op) {
		return nil, device.Error(device.MessageOperationFormat)
	}
	previous, err := parseDNSGuardManifest(op.Previous)
	if err != nil {
		return nil, err
	}
	d.InvalidateAddressReads()
	live, err := SnapshotDNSGuard(ctx, d)
	if err != nil {
		return nil, err
	}
	owned, err := guardOwned(previous, live)
	if err != nil {
		return nil, err
	}
	if err = guardCheckPreview(op, owned, live); err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	if op.Keep != nil {
		keep = device.StringSet((*op.Keep)...)
	}
	undo := []ROSChange{}
	created := map[string]DNSGuardResource{}
	expectedCreated := map[string]json.RawMessage{}
	run := func() error {
		for i := len(owned) - 1; i >= 0; i-- {
			e := owned[i]
			if keep[e.Role] {
				created[e.Role] = e
				continue
			}
			row, err := d.Row(ctx, e.Path, e.ID)
			if err != nil {
				return err
			}
			failover := previous != nil && previous.Spec != nil && previous.Spec.Failover
			if !ros.SameRow(guardComparable(e.Role, ros.StableRow(e.Path, row), failover), guardComparable(e.Role, e.Config, failover)) {
				return device.Error(fmt.Sprintf(messageGuardEdited, e.Path, e.ID))
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			check, err := d.Perform(ctx, ROSChange{Method: "DELETE", Path: e.Path, ID: e.ID, Expected: ros.MarshalRow(d.OwnershipSnapshot(e.Path, row))}, &undo, false)
			if err != nil {
				return err
			}
			if err = d.VerifyChange(ctx, check); err != nil {
				return err
			}
		}
		for _, item := range op.Create {
			values := ros.CloneRow(item.Values)
			if item.Top {
				rows, err := d.List(ctx, item.Path)
				if err != nil {
					return err
				}
				if len(rows) > 0 {
					values["place-before"] = rows[0].ID()
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			check, err := d.Perform(ctx, ROSChange{Method: "PUT", Path: item.Path, Values: values}, &undo, false)
			if err != nil {
				return err
			}
			if err = d.VerifyChange(ctx, check); err != nil {
				return err
			}
			created[item.Role] = DNSGuardResource{Role: item.Role, Path: item.Path, ID: check.ID}
			expectedCreated[item.Role] = undo[len(undo)-1].Expected
		}
		roles := []string{}
		if op.Roles != nil {
			roles = *op.Roles
		} else {
			for _, e := range op.Create {
				roles = append(roles, e.Role)
			}
		}
		resources := []DNSGuardResource{}
		for _, role := range roles {
			e, ok := created[role]
			if !ok {
				return device.Error(device.MessageOperationFormat)
			}
			row, err := d.Row(ctx, e.Path, e.ID)
			if err != nil {
				return err
			}
			if expected, ok := expectedCreated[role]; ok {
				if !ros.RowsEqual(d.OwnershipSnapshot(e.Path, row), expected) {
					return device.Error(messageGuardChanged)
				}
			} else if !ros.SameRow(guardComparable(role, ros.StableRow(e.Path, row), op.Spec != nil && op.Spec.Failover), guardComparable(role, e.Config, op.Spec != nil && op.Spec.Failover)) {
				return device.Error(messageGuardChanged)
			}
			e.Config = ros.StableRow(e.Path, row)
			resources = append(resources, e)
		}
		if op.Spec == nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			d.SetDNSGuardManifest(nil)
		} else {
			raw, _ := json.Marshal(DNSGuardManifest{Version: 1, Spec: op.Spec, Resources: resources})
			if err := ctx.Err(); err != nil {
				return err
			}
			d.SetDNSGuardManifest(raw)
		}
		return nil
	}
	err = run()
	slices.Reverse(undo)
	if err != nil {
		remap, rollback := d.rollbackDNSGuard(ctx, undo)
		// Objects the rollback did restore are ours again under new IDs, even
		// when another step failed. Without the remap they carry the reserved
		// list name / hijack comment with no proof and block every later preview,
		// while the restored Netwatch keeps toggling by comment (Python bug).
		if rollback == nil || len(remap) > 0 {
			if remapErr := d.remapDNSGuard(ctx, op.Previous, remap); rollback == nil {
				rollback = remapErr
			} else if remapErr != nil {
				rollback = device.RollbackError(rollback.Error() + "；" + remapErr.Error())
			}
		}
		if rollback != nil {
			return nil, device.RollbackError(err.Error() + "；" + rollback.Error())
		}
		return nil, err
	}
	return &DNSGuardUndo{Kind: "dns_guard_restore", Changes: undo, Previous: ros.GuardRaw(op.Previous)}, nil
}

// RestoreDNSGuard reverses a completed guard delta and refreshes restored IDs.
func (d *RouterOS) RestoreDNSGuard(ctx context.Context, op DNSGuardUndo) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	d.InvalidateAddressReads()
	remap, err := d.rollbackDNSGuard(ctx, op.Changes)
	if err != nil {
		return err
	}
	return d.remapDNSGuard(ctx, op.Previous, remap)
}

// VerifyDNSGuard proves all expected roles, their ownership, and hijack priority.
func VerifyDNSGuard(ctx context.Context, d *RouterOS, check *DNSGuardCheck) error {
	if check == nil {
		return device.Error(device.MessageOperationFormat)
	}
	m, err := parseDNSGuardManifest(d.DNSGuardManifest())
	if err != nil {
		return err
	}
	live, err := SnapshotDNSGuard(ctx, d)
	if err != nil {
		return err
	}
	owned, err := guardOwned(m, live)
	if err != nil {
		return err
	}
	roles := []string{}
	hijacks := map[[2]string]bool{}
	for _, e := range owned {
		roles = append(roles, e.Role)
		if strings.HasPrefix(e.Role, "hijack") {
			hijacks[[2]string{e.Path, e.ID}] = true
		}
	}
	if !slices.Equal(roles, check.Roles) || (len(check.Roles) == 0 && m != nil) {
		return device.Error(messageGuardVerify)
	}
	for _, e := range owned {
		if strings.HasPrefix(e.Role, "hijack") && !guardOnTop(e.ID, (*live)[ros.PathNAT], hijacks) {
			return device.Error(messageGuardVerifyOrder)
		}
	}
	return nil
}
