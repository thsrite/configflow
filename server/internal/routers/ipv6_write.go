package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/devhttp"
)

func ipv6ValidSpec(spec *IPv6RouteSpec) bool {
	if spec == nil {
		return true
	}
	p, ok := ipv6Network(spec.CIDR)
	if !ok || p.String() != spec.CIDR {
		return false
	}
	address, zone, ok := strings.Cut(spec.Gateway, "%")
	if !ok || zone == "" {
		return false
	}
	a, ok := ipv6Network(address)
	return ok && a.Bits() == 128
}

// rollbackIPv6 carries only response-proven restored IDs back to the manifest.
// A failed restoration stops later deletion of replacement protection.
func (d *RouterOS) rollbackIPv6(ctx context.Context, changes []ROSChange, fake bool) (map[string]string, error) {
	remap := map[string]string{}
	failures := []string{}
	for index := 0; index < len(changes); {
		change := changes[index]
		if change.Path != rosIPv6Routes || !rosIDPattern.MatchString(change.ID) || len(change.Expected) == 0 || (change.Method != "RESTORE" && change.Method != "DELETE") {
			failures = append(failures, device.MessageOperationFormat)
			index++
			continue
		}
		if err := ctx.Err(); err != nil {
			return remap, err
		}
		if len(failures) > 0 && (fake || change.Method == "DELETE") {
			index++
			continue
		}
		end := index + 1
		if !fake {
			for end < len(changes) && changes[end].Path == rosIPv6Routes && changes[end].Method == change.Method && rosIDPattern.MatchString(changes[end].ID) && len(changes[end].Expected) > 0 {
				end++
			}
		}
		if end-index >= d.batchMin && change.Method == "RESTORE" {
			for _, problem := range d.restoreRemoved(ctx, changes[index:end], rosIPv6Routes, remap) {
				if problem != "" {
					failures = append(failures, problem)
				}
			}
		} else if end-index >= d.batchMin && change.Method == "DELETE" {
			for _, problem := range d.restoreDeleted(ctx, changes[index:end], false, rosIPv6Routes) {
				if problem != "" {
					failures = append(failures, problem)
				}
			}
		} else {
			for _, change := range changes[index:end] {
				if len(failures) > 0 && (fake || change.Method == "DELETE") {
					break
				}
				check, err := d.perform(ctx, change, &[]ROSChange{}, true)
				if err == nil {
					err = d.verifyChange(ctx, check)
				}
				if err != nil && change.Method == "DELETE" && d.ipv6RouteGone(ctx, change.ID) {
					// A lost reply or failed readback after the device removed the new
					// route is not a failed compensation; treating it as one would skip
					// restoring the previous route and leave the destination unrouted.
					err = nil
				}
				if err != nil {
					failures = append(failures, err.Error())
				} else if change.Method == "RESTORE" {
					remap[change.ID] = check.ID
				}
			}
		}
		index = end
	}
	if len(failures) > 0 {
		prefix := messageIPv6RoutesRollback
		if fake {
			prefix = messageIPv6FakeRollback
		}
		return remap, device.RollbackError(prefix + strings.Join(failures, "；"))
	}
	return remap, nil
}

// ipv6RouteGone proves by a fresh read that a route ID no longer exists.
// Read errors are not proof of absence.
func (d *RouterOS) ipv6RouteGone(ctx context.Context, id string) bool {
	row, err := d.find(ctx, rosIPv6Routes, id)
	return err == nil && row == nil
}

// remapIPv6 updates only IDs returned by successful restores. It never searches
// for matching configurations (which may have been created by another writer).
// Unknown top-level and per-resource stored fields survive the update.
func (d *RouterOS) remapIPv6(ctx context.Context, previous json.RawMessage, remap map[string]string, fake bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !device.HasStoredState(previous) {
		d.setIPv6Manifest(fake, nil)
		return nil
	}
	var document map[string]json.RawMessage
	var resources []map[string]json.RawMessage
	if json.Unmarshal(previous, &document) != nil || document == nil || json.Unmarshal(document["resources"], &resources) != nil {
		return device.Error(device.MessageOperationFormat)
	}
	for _, entry := range resources {
		var id string
		if json.Unmarshal(entry["id"], &id) != nil {
			return device.Error(device.MessageOperationFormat)
		}
		if restored, ok := remap[id]; ok {
			// The restore response proved this new identity. Keep the old stable proof:
			// re-reading here and blessing its config would adopt a concurrent edit.
			entry["id"], _ = json.Marshal(restored)
		}
	}
	document["resources"], _ = json.Marshal(resources)
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.setIPv6Manifest(fake, raw)
	return nil
}
func ipv6Changes(undos []json.RawMessage) ([]ROSChange, error) {
	changes := []ROSChange{}
	for _, raw := range undos {
		var undo ROSUndo
		if devhttp.DecodeJSON(raw, &undo) != nil || undo.Kind != "ros_restore" {
			return nil, device.Error(device.MessageOperationFormat)
		}
		changes = append(changes, undo.Changes...)
	}
	return changes, nil
}
func ipv6MergeRollback(cause, rollback, remap error) error {
	if rollback == nil {
		rollback = remap
	} else if remap != nil {
		rollback = device.RollbackError(rollback.Error() + "；" + remap.Error())
	}
	if rollback != nil {
		return device.RollbackError(cause.Error() + "；" + rollback.Error())
	}
	return cause
}

// ApplyIPv6FakeIP executes one immutable preview. Browser disconnects cannot
// abandon it; Runtime shutdown cancellation is checked at every write boundary.
func (d *RouterOS) ApplyIPv6FakeIP(ctx context.Context, op IPv6FakeIPReplace) (*IPv6FakeIPUndo, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ipv6ValidSpec(op.Spec) || op.Expected == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	if op.Spec != nil {
		p, _ := ipv6Network(op.Spec.CIDR)
		if !p.Addr().IsPrivate() || p.Bits() < 7 || op.Values == nil || op.Values.text("dst-address") != op.Spec.CIDR || op.Values.text("gateway") != op.Spec.Gateway || op.Values.text("routing-table") != "main" || op.Values.text("distance") != "1" || op.Values.text("disabled") != "false" || !strings.HasPrefix(op.Values.text("comment"), "cf-fakeip6:") || len(op.Values) != 6 {
			return nil, device.Error(device.MessageOperationFormat)
		}
	} else if op.Values != nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	live, err := SnapshotIPv6(ctx, d, op.Spec != nil)
	if err != nil {
		return nil, err
	}
	if !jsonValuesEqual(device.JSON(IPv6Evidence(live)), device.JSON(op.Expected)) {
		return nil, device.Error(messageIPv6PreviewChanged)
	}
	owned, err := ipv6Owned(op.Previous, live, true)
	if err != nil {
		return nil, err
	}
	if !ipv6ResourcesEqual(owned, op.Owned) {
		return nil, device.Error(messageIPv6FakeOwnership)
	}
	undo := []ROSChange{}
	created := map[string]Row{}
	resources := []IPv6Resource{}
	run := func() error {
		for _, entry := range owned {
			row, err := d.row(ctx, rosIPv6Routes, entry.ID)
			if err != nil {
				return err
			}
			if !sameRow(StableIPv6Row(row), entry.Config) {
				return device.Error(messageIPv6FakeDuring)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			check, err := d.perform(ctx, ROSChange{Method: "DELETE", Path: rosIPv6Routes, ID: entry.ID, Expected: marshalRow(d.ownershipSnapshot(rosIPv6Routes, row))}, &undo, false)
			if err != nil {
				return err
			}
			if err = d.verifyChange(ctx, check); err != nil {
				return err
			}
		}
		if op.Spec != nil {
			rows, err := d.list(ctx, rosIPv6Routes)
			if err != nil {
				return err
			}
			if len(ipv6Targets(IPv6Snapshot{"routes": rows}, op.Spec.CIDR, nil)) > 0 {
				return device.Error(messageIPv6FakeAppeared)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			check, err := d.perform(ctx, ROSChange{Method: "PUT", Path: rosIPv6Routes, Values: op.Values}, &undo, false, func(rows []Row) error {
				if len(ipv6Targets(IPv6Snapshot{"routes": rows}, op.Spec.CIDR, nil)) > 0 {
					return device.Error(messageIPv6FakeAppeared)
				}
				return nil
			})
			if err != nil {
				return err
			}
			created[check.ID] = StableIPv6Row(check.created)
			if err = d.verifyChange(ctx, check); err != nil {
				return err
			}
			row, err := d.row(ctx, rosIPv6Routes, check.ID)
			if err != nil {
				return err
			}
			if !ipv6Correct(row, *op.Spec) {
				return device.Error(messageIPv6FakeInactive)
			}
			// Confirm complete transaction proof before recording this stable config.
			if !rowsEqual(d.ownershipSnapshot(rosIPv6Routes, row), undo[len(undo)-1].Expected) {
				return device.Error(messageIPv6FakeDuring)
			}
			resources = append(resources, IPv6Resource{rosIPv6Routes, check.ID, StableIPv6Row(row)})
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.Spec == nil {
			d.SetIPv6FakeIPManifest(nil)
		} else {
			raw, _ := json.Marshal(IPv6FakeIPManifest{1, op.Spec, resources})
			d.SetIPv6FakeIPManifest(raw)
		}
		return nil
	}
	err = run()
	slices.Reverse(undo)
	if err != nil {
		remap, rollback := d.rollbackIPv6(ctx, undo, true)
		// Keep partial successful restores enrolled even if another compensation failed.
		mapped := d.remapIPv6(ctx, op.Previous, remap, true)
		if rollback != nil && len(created) > 0 && len(remap) == 0 {
			if recovery := d.recoverIPv6Created(ctx, created, op.Spec, "", true); recovery != nil {
				mapped = recovery
			}
		}
		return nil, ipv6MergeRollback(err, rollback, mapped)
	}
	return &IPv6FakeIPUndo{"ipv6_fakeip_restore", undo, guardRaw(op.Previous)}, nil
}

// RestoreIPv6FakeIP reverses a completed transaction and refreshes trusted IDs.
func (d *RouterOS) RestoreIPv6FakeIP(ctx context.Context, op IPv6FakeIPUndo) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	remap, err := d.rollbackIPv6(ctx, op.Changes, true)
	if err != nil && len(remap) == 0 {
		return err
	}
	mapped := d.remapIPv6(ctx, op.Previous, remap, true)
	if err != nil {
		if mapped != nil {
			return device.RollbackError(err.Error() + "；" + mapped.Error())
		}
		return err
	}
	return mapped
}

func (d *RouterOS) writeIPv6Routes(ctx context.Context, raw []json.RawMessage, proofs map[string]Row, created map[string]Row, undos *[]json.RawMessage) error {
	record := func(_ json.RawMessage, undo json.RawMessage) { *undos = append(*undos, undo) }
	for index := 0; index < len(raw); {
		if err := ctx.Err(); err != nil {
			return err
		}
		var first rosOperation
		if jsondecode.Unmarshal(raw[index], &first) != nil {
			return device.Error(device.MessageOperationFormat)
		}
		batch := d.routeBatches(raw, index, first.Kind)
		left := batch
		if len(batch) >= d.batchMin {
			var err error
			if first.Kind == "route_add" {
				left, err = d.addRoutes(ctx, raw, batch, record, rosIPv6Routes, routeCreateProof{guard: ipv6AddGuard(proofs), created: created})
			} else {
				left, err = d.deleteRoutes(ctx, raw, batch, record, rosIPv6Routes, proofs)
			}
			if err != nil {
				return err
			}
		}
		for _, i := range left {
			var op rosOperation
			_ = jsondecode.Unmarshal(raw[i], &op)
			change := ROSChange{Method: "PUT", Path: rosIPv6Routes, Values: routeAddValues(op)}
			if op.Kind == "route_delete" {
				row, err := d.row(ctx, rosIPv6Routes, op.ID)
				if err != nil {
					return err
				}
				if !sameRow(StableIPv6Row(row), proofs[op.ID]) || policyFlag(row["dynamic"]) || policyFlag(row["connect"]) {
					return device.Error(messageIPv6RoutesOwnership)
				}
				change = ROSChange{Method: "DELETE", Path: rosIPv6Routes, ID: op.ID, Expected: marshalRow(d.ownershipSnapshot(rosIPv6Routes, row))}
			}
			// Record our own transaction journal before the write. Unlike _transaction,
			// no inner compensation can hide the IDs needed by the outer manifest owner.
			journal := []ROSChange{}
			guards := []func([]Row) error{}
			if change.Method == "PUT" {
				guards = append(guards, ipv6AddGuard(proofs))
			}
			check, err := d.perform(ctx, change, &journal, false, guards...)
			if err == nil && change.Method == "PUT" {
				created[check.ID] = StableIPv6Row(check.created)
			}
			if err == nil {
				err = d.verifyChange(ctx, check)
			}
			slices.Reverse(journal)
			if len(journal) > 0 {
				encoded, _ := json.Marshal(ROSUndo{Kind: "ros_restore", Changes: journal})
				*undos = append(*undos, encoded)
			}
			if err != nil {
				return err
			}
		}
		index += len(batch)
	}
	return nil
}

// ApplyIPv6Routes adds before removing, guarding complete manifests even on the
// console batching path. It never rebuilds ownership by searching for comments.
func (d *RouterOS) ApplyIPv6Routes(ctx context.Context, op IPv6RoutesReplace) (*IPv6RoutesUndo, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.Expected == nil || op.Owned == nil || op.Adds == nil || op.Deletes == nil || op.Retained == nil || op.Scope == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	live, err := SnapshotIPv6(ctx, d, true)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	proofs := map[string]Row{}
	for _, entry := range op.Owned {
		ids[entry.ID] = true
		proofs[entry.ID] = entry.Config
	}
	if !jsonValuesEqual(device.JSON(IPv6Evidence(ipv6Scoped(live, device.StringSet(op.Scope...), ids))), device.JSON(op.Expected)) {
		return nil, device.Error(messageIPv6PreviewChanged)
	}
	owned, err := ipv6Owned(op.Previous, live, false)
	if err != nil {
		return nil, err
	}
	if !ipv6ResourcesEqual(owned, op.Owned) {
		return nil, device.Error(messageIPv6RoutesOwnership)
	}
	seen := map[string]bool{}
	adds, deletes := []json.RawMessage{}, []json.RawMessage{}
	for _, add := range op.Adds {
		if add.Kind != "route_add" || !ipv6ValidSpec(&IPv6RouteSpec{add.CIDR, add.Gateway}) || add.Gateway != op.Gateway || !slices.Contains(op.Scope, add.CIDR) || seen[add.CIDR] || !strings.HasPrefix(add.Comment, "cf-rule6:") {
			return nil, device.Error(device.MessageOperationFormat)
		}
		seen[add.CIDR] = true
		raw, _ := json.Marshal(add)
		adds = append(adds, raw)
	}
	partition := map[string]bool{}
	for _, del := range op.Deletes {
		if del.Kind != "route_delete" || !ids[del.ID] || partition[del.ID] || proofs[del.ID].text("dst-address") != del.CIDR {
			return nil, device.Error(device.MessageOperationFormat)
		}
		partition[del.ID] = true
		raw, _ := json.Marshal(del)
		deletes = append(deletes, raw)
	}
	for _, id := range op.Retained {
		if !ids[id] || partition[id] {
			return nil, device.Error(device.MessageOperationFormat)
		}
		partition[id] = true
	}
	if len(partition) != len(owned) {
		return nil, device.Error(device.MessageOperationFormat)
	}
	undos := []json.RawMessage{}
	created := map[string]Row{}
	run := func() error {
		if err := d.writeIPv6Routes(ctx, adds, proofs, created, &undos); err != nil {
			return err
		}
		if err := d.writeIPv6Routes(ctx, deletes, proofs, created, &undos); err != nil {
			return err
		}
		changes, err := ipv6Changes(undos)
		if err != nil {
			return err
		}
		added := map[string]ROSChange{}
		for _, change := range changes {
			if change.Method == "DELETE" {
				added[change.ID] = change
			}
		}
		after, err := d.list(ctx, rosIPv6Routes)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, row := range after {
			counts[row.id()]++
		}
		resources := []IPv6Resource{}
		keep := device.StringSet(op.Retained...)
		byID := map[string]Row{}
		for _, row := range after {
			byID[row.id()] = row
		}
		for _, entry := range owned {
			if keep[entry.ID] {
				row := byID[entry.ID]
				if counts[entry.ID] != 1 || !sameRow(StableIPv6Row(row), entry.Config) {
					return device.Error(messageIPv6RoutesOwnership)
				}
				resources = append(resources, entry)
			}
		}
		for _, row := range after {
			if change, ok := added[row.id()]; ok {
				if counts[row.id()] != 1 || !rowsEqual(d.ownershipSnapshot(rosIPv6Routes, row), change.Expected) {
					return device.Error(messageIPv6RoutesOwnership)
				}
				if !ipv6Correct(row, IPv6RouteSpec{ipv6Key(row), op.Gateway}) {
					return device.Error(fmt.Sprintf(messageIPv6RoutesNewInactive, row.text("dst-address")))
				}
				resources = append(resources, IPv6Resource{rosIPv6Routes, row.id(), StableIPv6Row(row)})
			}
		}
		if len(resources) != len(op.Retained)+len(op.Adds) {
			return device.Error(messageIPv6RoutesCount)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(resources) == 0 {
			d.SetIPv6RoutesManifest(nil)
		} else {
			raw, _ := json.Marshal(IPv6RoutesManifest{1, op.Gateway, resources})
			d.SetIPv6RoutesManifest(raw)
		}
		return nil
	}
	err = run()
	slices.Reverse(undos)
	if err != nil {
		changes, decodeErr := ipv6Changes(undos)
		if decodeErr != nil {
			return nil, decodeErr
		}
		remap, rollback := d.rollbackIPv6(ctx, changes, false)
		mapped := d.remapIPv6(ctx, op.Previous, remap, false)
		if rollback != nil && len(created) > 0 {
			if recovery := d.recoverIPv6Created(ctx, created, nil, op.Gateway, false); recovery != nil {
				mapped = recovery
			}
		}
		return nil, ipv6MergeRollback(err, rollback, mapped)
	}
	return &IPv6RoutesUndo{"ipv6_routes_restore", undos, guardRaw(op.Previous)}, nil
}

// RestoreIPv6Routes reverses a completed rule-set delta, including partial remaps.
func (d *RouterOS) RestoreIPv6Routes(ctx context.Context, op IPv6RoutesUndo) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	changes, err := ipv6Changes(op.Undos)
	if err != nil {
		return err
	}
	remap, rollback := d.rollbackIPv6(ctx, changes, false)
	previous := op.Previous
	if rollback != nil {
		previous = mergeIPv6Recovery(previous, d.ipv6RoutesManifest)
	}
	mapped := d.remapIPv6(ctx, previous, remap, false)
	if rollback != nil {
		if mapped != nil {
			return device.RollbackError(rollback.Error() + "；" + mapped.Error())
		}
		return rollback
	}
	return mapped
}

// VerifyIPv6FakeIP proves removal, borrowed identity or the exact managed route.
func VerifyIPv6FakeIP(ctx context.Context, d *RouterOS, check *IPv6FakeIPCheck) error {
	if check == nil {
		return device.Error(device.MessageOperationFormat)
	}
	live, err := SnapshotIPv6(ctx, d, false)
	if err != nil {
		return err
	}
	owned, err := ipv6Owned(d.ipv6FakeIPManifest, live, true)
	if err != nil {
		return err
	}
	if check.Spec == nil {
		if device.HasStoredState(d.ipv6FakeIPManifest) || len(owned) > 0 {
			return device.Error(messageIPv6FakeVerifyRemove)
		}
		if check.Removed != nil {
			for _, row := range live["routes"] {
				if slices.Contains(*check.Removed, row.id()) {
					return device.Error(messageIPv6FakeVerifyRemove)
				}
			}
		}
		return nil
	}
	rows := ipv6Targets(live, check.Spec.CIDR, nil)
	if len(rows) != 1 || !ipv6Correct(rows[0], *check.Spec) {
		return device.Error(messageIPv6FakeVerifyConflict)
	}
	if check.Borrowed != nil {
		if rows[0].id() != check.Borrowed.ID || !sameRow(StableIPv6Row(rows[0]), check.Borrowed.Config) {
			return device.Error(messageIPv6FakeVerifyBorrowed)
		}
	} else {
		var manifest IPv6FakeIPManifest
		_ = jsondecode.Unmarshal(d.ipv6FakeIPManifest, &manifest)
		if len(owned) != 1 || rows[0].id() != owned[0].ID || manifest.Spec == nil || *manifest.Spec != *check.Spec {
			return device.Error(messageIPv6FakeVerifyManifest)
		}
	}
	return nil
}

// VerifyIPv6Routes verifies exact destination sets, retained proofs and withdrawals.
func VerifyIPv6Routes(ctx context.Context, d *RouterOS, check *IPv6RoutesCheck) error {
	if check == nil {
		return device.Error(device.MessageOperationFormat)
	}
	live, err := SnapshotIPv6(ctx, d, false)
	if err != nil {
		return err
	}
	owned, err := ipv6Owned(d.ipv6RoutesManifest, live, false)
	if err != nil {
		return err
	}
	var manifest IPv6RoutesManifest
	_ = jsondecode.Unmarshal(d.ipv6RoutesManifest, &manifest)
	if len(owned) != len(manifest.Resources) {
		return device.Error(messageIPv6RoutesVerifyMissing)
	}
	byCIDR := map[string][]Row{}
	rowsByID := map[string]Row{}
	for _, row := range live["routes"] {
		rowsByID[row.id()] = row
	}
	for _, entry := range owned {
		row := rowsByID[entry.ID]
		byCIDR[ipv6Key(entry.Config)] = append(byCIDR[ipv6Key(entry.Config)], row)
	}
	for _, cidr := range check.Active {
		rows := byCIDR[cidr]
		if len(rows) != 1 || !ipv6Correct(rows[0], IPv6RouteSpec{cidr, check.Gateway}) {
			return device.Error(fmt.Sprintf(messageIPv6RoutesVerifyActive, cidr, check.Gateway))
		}
	}
	for _, entry := range check.Borrowed {
		row := rowsByID[entry.ID]
		if row == nil || !sameRow(StableIPv6Row(row), entry.Config) || !ipv6Correct(row, IPv6RouteSpec{ipv6Key(entry.Config), check.Gateway}) {
			return device.Error(messageIPv6RoutesVerifyBorrowed)
		}
	}
	for _, row := range live["routes"] {
		if slices.Contains(check.Removed, row.id()) {
			return device.Error(messageIPv6RoutesVerifyRemove)
		}
	}
	return nil
}

func ipv6AddGuard(proofs map[string]Row) func([]Row) error {
	return func(rows []Row) error {
		seen := map[string]bool{}
		for _, row := range rows {
			if row.textOr("routing-table", "main") != "main" {
				continue
			}
			expected, ok := proofs[row.id()]
			if !ok || seen[row.id()] || !sameRow(StableIPv6Row(row), expected) || policyFlag(row["dynamic"]) || policyFlag(row["connect"]) {
				return device.Error(messageIPv6RoutesOwnership)
			}
			seen[row.id()] = true
		}
		return nil
	}
}

// mergeIPv6Recovery retains both generations' original proofs after a partial
// whole-stage rollback. Missing IDs stay stale, never become clear commands or
// configuration matches that could adopt an object created by another writer.
func mergeIPv6Recovery(previous, current json.RawMessage) json.RawMessage {
	if !device.HasStoredState(previous) {
		return guardRaw(current)
	}
	if !device.HasStoredState(current) {
		return guardRaw(previous)
	}
	var oldDoc, newDoc map[string]json.RawMessage
	var oldEntries, newEntries []map[string]json.RawMessage
	if json.Unmarshal(previous, &oldDoc) != nil || json.Unmarshal(current, &newDoc) != nil || json.Unmarshal(oldDoc["resources"], &oldEntries) != nil || json.Unmarshal(newDoc["resources"], &newEntries) != nil {
		return guardRaw(previous)
	}
	ids := map[string]bool{}
	for _, entry := range oldEntries {
		var id string
		_ = json.Unmarshal(entry["id"], &id)
		ids[id] = true
	}
	for _, entry := range newEntries {
		var id string
		_ = json.Unmarshal(entry["id"], &id)
		if !ids[id] {
			ids[id] = true
			oldEntries = append(oldEntries, entry)
		}
	}
	oldDoc["resources"], _ = json.Marshal(oldEntries)
	raw, _ := json.Marshal(oldDoc)
	return raw
}

// recoverIPv6Created saves birth proofs even when compensation or its reads
// failed. It never refreshes them from current rows: later external edits and
// ID reuse therefore remain detectable by the next preview. Gone IDs are stale
// receipts, not permission to delete or to adopt an equivalent route.
func (d *RouterOS) recoverIPv6Created(ctx context.Context, created map[string]Row, spec *IPv6RouteSpec, gateway string, fake bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resources := []IPv6Resource{}
	ids := []string{}
	for id := range created {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		resources = append(resources, IPv6Resource{rosIPv6Routes, id, cloneRow(created[id])})
	}
	if fake {
		if len(resources) != 1 || spec == nil {
			return device.Error(device.MessageOperationFormat)
		}
		// Preserve the previous record's unknown top-level members while changing
		// its explicitly known publication fields to this proven new generation.
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(d.ipv6FakeIPManifest, &fields)
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		fields["version"] = json.RawMessage("1")
		fields["spec"], _ = json.Marshal(spec)
		fields["resources"], _ = json.Marshal(resources)
		raw, _ := json.Marshal(fields)
		if err := ctx.Err(); err != nil {
			return err
		}
		d.SetIPv6FakeIPManifest(raw)
	} else {
		raw, _ := json.Marshal(IPv6RoutesManifest{1, gateway, resources})
		combined := mergeIPv6Recovery(d.ipv6RoutesManifest, raw)
		if err := ctx.Err(); err != nil {
			return err
		}
		d.SetIPv6RoutesManifest(combined)
	}
	return nil
}
