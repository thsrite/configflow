package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/devhttp"
	"configflow/internal/lifecycle"
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
			failures = append(failures, messageOperationFormat)
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
		return remap, rollbackError(prefix + strings.Join(failures, "；"))
	}
	return remap, nil
}

// remapIPv6 updates only IDs returned by successful restores. It never searches
// for matching configurations (which may have been created by another writer).
// Unknown top-level and per-resource stored fields survive the update.
func (d *RouterOS) remapIPv6(ctx context.Context, previous json.RawMessage, remap map[string]string, fake bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !truthy(previous) {
		d.setIPv6Manifest(fake, nil)
		return nil
	}
	var document map[string]json.RawMessage
	var resources []map[string]json.RawMessage
	if json.Unmarshal(previous, &document) != nil || document == nil || json.Unmarshal(document["resources"], &resources) != nil {
		return routerError(messageOperationFormat)
	}
	for _, entry := range resources {
		var id string
		if json.Unmarshal(entry["id"], &id) != nil {
			return routerError(messageOperationFormat)
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
			return nil, routerError(messageOperationFormat)
		}
		changes = append(changes, undo.Changes...)
	}
	return changes, nil
}
func ipv6MergeRollback(cause, rollback, remap error) error {
	if rollback == nil {
		rollback = remap
	} else if remap != nil {
		rollback = rollbackError(rollback.Error() + "；" + remap.Error())
	}
	if rollback != nil {
		return rollbackError(cause.Error() + "；" + rollback.Error())
	}
	return cause
}

// ApplyIPv6FakeIP executes one immutable preview. Browser disconnects cannot
// abandon it; Runtime shutdown cancellation is checked at every write boundary.
func (d *RouterOS) ApplyIPv6FakeIP(ctx context.Context, op IPv6FakeIPReplace) (*IPv6FakeIPUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ipv6ValidSpec(op.Spec) || op.Expected == nil {
		return nil, routerError(messageOperationFormat)
	}
	if op.Spec != nil {
		p, _ := ipv6Network(op.Spec.CIDR)
		if !p.Addr().IsPrivate() || p.Bits() < 7 || op.Values == nil || op.Values.text("dst-address") != op.Spec.CIDR || op.Values.text("gateway") != op.Spec.Gateway || op.Values.text("routing-table") != "main" || op.Values.text("distance") != "1" || op.Values.text("disabled") != "false" || !strings.HasPrefix(op.Values.text("comment"), "cf-fakeip6:") || len(op.Values) != 6 {
			return nil, routerError(messageOperationFormat)
		}
	} else if op.Values != nil {
		return nil, routerError(messageOperationFormat)
	}
	live, err := SnapshotIPv6(ctx, d, op.Spec != nil)
	if err != nil {
		return nil, err
	}
	if !jsonValuesEqual(mustPolicyJSON(IPv6Evidence(live)), mustPolicyJSON(op.Expected)) {
		return nil, routerError(messageIPv6PreviewChanged)
	}
	owned, err := ipv6Owned(op.Previous, live, true)
	if err != nil {
		return nil, err
	}
	if !ipv6ResourcesEqual(owned, op.Owned) {
		return nil, routerError(messageIPv6FakeOwnership)
	}
	undo := []ROSChange{}
	resources := []IPv6Resource{}
	run := func() error {
		for _, entry := range owned {
			row, err := d.row(ctx, rosIPv6Routes, entry.ID)
			if err != nil {
				return err
			}
			if !sameRow(StableIPv6Row(row), entry.Config) {
				return routerError(messageIPv6FakeDuring)
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
				return routerError(messageIPv6FakeAppeared)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			check, err := d.perform(ctx, ROSChange{Method: "PUT", Path: rosIPv6Routes, Values: op.Values}, &undo, false, func(rows []Row) error {
				if len(ipv6Targets(IPv6Snapshot{"routes": rows}, op.Spec.CIDR, nil)) > 0 {
					return routerError(messageIPv6FakeAppeared)
				}
				return nil
			})
			if err != nil {
				return err
			}
			if err = d.verifyChange(ctx, check); err != nil {
				return err
			}
			row, err := d.row(ctx, rosIPv6Routes, check.ID)
			if err != nil {
				return err
			}
			if !ipv6Correct(row, *op.Spec) {
				return routerError(messageIPv6FakeInactive)
			}
			// Confirm complete transaction proof before recording this stable config.
			if !rowsEqual(d.ownershipSnapshot(rosIPv6Routes, row), undo[len(undo)-1].Expected) {
				return routerError(messageIPv6FakeDuring)
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
		return nil, ipv6MergeRollback(err, rollback, mapped)
	}
	return &IPv6FakeIPUndo{"ipv6_fakeip_restore", undo, guardRaw(op.Previous)}, nil
}

// RestoreIPv6FakeIP reverses a completed transaction and refreshes trusted IDs.
func (d *RouterOS) RestoreIPv6FakeIP(ctx context.Context, op IPv6FakeIPUndo) error {
	ctx = lifecycle.Detach(ctx)
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
			return rollbackError(err.Error() + "；" + mapped.Error())
		}
		return err
	}
	return mapped
}

func (d *RouterOS) writeIPv6Routes(ctx context.Context, raw []json.RawMessage, proofs map[string]Row, undos *[]json.RawMessage) error {
	record := func(_ json.RawMessage, undo json.RawMessage) { *undos = append(*undos, undo) }
	for index := 0; index < len(raw); {
		if err := ctx.Err(); err != nil {
			return err
		}
		var first rosOperation
		if json.Unmarshal(raw[index], &first) != nil {
			return routerError(messageOperationFormat)
		}
		batch := d.routeBatches(raw, index, first.Kind)
		left := batch
		if len(batch) >= d.batchMin {
			var err error
			if first.Kind == "route_add" {
				left, err = d.addRoutes(ctx, raw, batch, record, rosIPv6Routes, ipv6AddGuard(proofs))
			} else {
				left, err = d.deleteRoutes(ctx, raw, batch, record, rosIPv6Routes, proofs)
			}
			if err != nil {
				return err
			}
		}
		for _, i := range left {
			var op rosOperation
			_ = json.Unmarshal(raw[i], &op)
			change := ROSChange{Method: "PUT", Path: rosIPv6Routes, Values: routeAddValues(op)}
			if op.Kind == "route_delete" {
				row, err := d.row(ctx, rosIPv6Routes, op.ID)
				if err != nil {
					return err
				}
				if !sameRow(StableIPv6Row(row), proofs[op.ID]) || policyFlag(row["dynamic"]) || policyFlag(row["connect"]) {
					return routerError(messageIPv6RoutesOwnership)
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
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.Expected == nil || op.Owned == nil || op.Adds == nil || op.Deletes == nil || op.Retained == nil || op.Scope == nil {
		return nil, routerError(messageOperationFormat)
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
	if !jsonValuesEqual(mustPolicyJSON(IPv6Evidence(ipv6Scoped(live, stringSet(op.Scope...), ids))), mustPolicyJSON(op.Expected)) {
		return nil, routerError(messageIPv6PreviewChanged)
	}
	owned, err := ipv6Owned(op.Previous, live, false)
	if err != nil {
		return nil, err
	}
	if !ipv6ResourcesEqual(owned, op.Owned) {
		return nil, routerError(messageIPv6RoutesOwnership)
	}
	seen := map[string]bool{}
	adds, deletes := []json.RawMessage{}, []json.RawMessage{}
	for _, add := range op.Adds {
		if add.Kind != "route_add" || !ipv6ValidSpec(&IPv6RouteSpec{add.CIDR, add.Gateway}) || add.Gateway != op.Gateway || !slices.Contains(op.Scope, add.CIDR) || seen[add.CIDR] || !strings.HasPrefix(add.Comment, "cf-rule6:") {
			return nil, routerError(messageOperationFormat)
		}
		seen[add.CIDR] = true
		raw, _ := json.Marshal(add)
		adds = append(adds, raw)
	}
	partition := map[string]bool{}
	for _, del := range op.Deletes {
		if del.Kind != "route_delete" || !ids[del.ID] || partition[del.ID] || proofs[del.ID].text("dst-address") != del.CIDR {
			return nil, routerError(messageOperationFormat)
		}
		partition[del.ID] = true
		raw, _ := json.Marshal(del)
		deletes = append(deletes, raw)
	}
	for _, id := range op.Retained {
		if !ids[id] || partition[id] {
			return nil, routerError(messageOperationFormat)
		}
		partition[id] = true
	}
	if len(partition) != len(owned) {
		return nil, routerError(messageOperationFormat)
	}
	undos := []json.RawMessage{}
	run := func() error {
		if err := d.writeIPv6Routes(ctx, adds, proofs, &undos); err != nil {
			return err
		}
		if err := d.writeIPv6Routes(ctx, deletes, proofs, &undos); err != nil {
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
		keep := stringSet(op.Retained...)
		byID := map[string]Row{}
		for _, row := range after {
			byID[row.id()] = row
		}
		for _, entry := range owned {
			if keep[entry.ID] {
				row := byID[entry.ID]
				if counts[entry.ID] != 1 || !sameRow(StableIPv6Row(row), entry.Config) {
					return routerError(messageIPv6RoutesOwnership)
				}
				resources = append(resources, entry)
			}
		}
		for _, row := range after {
			if change, ok := added[row.id()]; ok {
				if counts[row.id()] != 1 || !rowsEqual(d.ownershipSnapshot(rosIPv6Routes, row), change.Expected) {
					return routerError(messageIPv6RoutesOwnership)
				}
				if !ipv6Correct(row, IPv6RouteSpec{ipv6Key(row), op.Gateway}) {
					return routerError(fmt.Sprintf(messageIPv6RoutesNewInactive, row.text("dst-address")))
				}
				resources = append(resources, IPv6Resource{rosIPv6Routes, row.id(), StableIPv6Row(row)})
			}
		}
		if len(resources) != len(op.Retained)+len(op.Adds) {
			return routerError(messageIPv6RoutesCount)
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
		return nil, ipv6MergeRollback(err, rollback, mapped)
	}
	return &IPv6RoutesUndo{"ipv6_routes_restore", undos, guardRaw(op.Previous)}, nil
}

// RestoreIPv6Routes reverses a completed rule-set delta, including partial remaps.
func (d *RouterOS) RestoreIPv6Routes(ctx context.Context, op IPv6RoutesUndo) error {
	ctx = lifecycle.Detach(ctx)
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
			return rollbackError(rollback.Error() + "；" + mapped.Error())
		}
		return rollback
	}
	return mapped
}

// VerifyIPv6FakeIP proves removal, borrowed identity or the exact managed route.
func VerifyIPv6FakeIP(ctx context.Context, d *RouterOS, check *IPv6FakeIPCheck) error {
	if check == nil {
		return routerError(messageOperationFormat)
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
		if truthy(d.ipv6FakeIPManifest) || len(owned) > 0 {
			return routerError(messageIPv6FakeVerifyRemove)
		}
		if check.Removed != nil {
			for _, row := range live["routes"] {
				if slices.Contains(*check.Removed, row.id()) {
					return routerError(messageIPv6FakeVerifyRemove)
				}
			}
		}
		return nil
	}
	rows := ipv6Targets(live, check.Spec.CIDR, nil)
	if len(rows) != 1 || !ipv6Correct(rows[0], *check.Spec) {
		return routerError(messageIPv6FakeVerifyConflict)
	}
	if check.Borrowed != nil {
		if rows[0].id() != check.Borrowed.ID || !sameRow(StableIPv6Row(rows[0]), check.Borrowed.Config) {
			return routerError(messageIPv6FakeVerifyBorrowed)
		}
	} else {
		var manifest IPv6FakeIPManifest
		_ = json.Unmarshal(d.ipv6FakeIPManifest, &manifest)
		if len(owned) != 1 || rows[0].id() != owned[0].ID || manifest.Spec == nil || *manifest.Spec != *check.Spec {
			return routerError(messageIPv6FakeVerifyManifest)
		}
	}
	return nil
}

// VerifyIPv6Routes verifies exact destination sets, retained proofs and withdrawals.
func VerifyIPv6Routes(ctx context.Context, d *RouterOS, check *IPv6RoutesCheck) error {
	if check == nil {
		return routerError(messageOperationFormat)
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
	_ = json.Unmarshal(d.ipv6RoutesManifest, &manifest)
	if len(owned) != len(manifest.Resources) {
		return routerError(messageIPv6RoutesVerifyMissing)
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
			return routerError(fmt.Sprintf(messageIPv6RoutesVerifyActive, cidr, check.Gateway))
		}
	}
	for _, entry := range check.Borrowed {
		row := rowsByID[entry.ID]
		if row == nil || !sameRow(StableIPv6Row(row), entry.Config) || !ipv6Correct(row, IPv6RouteSpec{ipv6Key(entry.Config), check.Gateway}) {
			return routerError(messageIPv6RoutesVerifyBorrowed)
		}
	}
	for _, row := range live["routes"] {
		if slices.Contains(check.Removed, row.id()) {
			return routerError(messageIPv6RoutesVerifyRemove)
		}
	}
	return nil
}

func init() {
	RegisterRouterOSHandler("ipv6_fakeip_replace", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6FakeIPReplace
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		undo, err := d.ApplyIPv6FakeIP(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	})
	RegisterRouterOSHandler("ipv6_fakeip_restore", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6FakeIPUndo
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		if err := d.RestoreIPv6FakeIP(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	})
	RegisterRouterOSHandler("ipv6_routes_replace", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6RoutesReplace
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		undo, err := d.ApplyIPv6Routes(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	})
	RegisterRouterOSHandler("ipv6_routes_restore", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6RoutesUndo
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		if err := d.RestoreIPv6Routes(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	})
}

func ipv6AddGuard(proofs map[string]Row) func([]Row) error {
	return func(rows []Row) error {
		seen := map[string]bool{}
		for _, row := range rows {
			if rosTextDefault(row["routing-table"], "main") != "main" {
				continue
			}
			expected, ok := proofs[row.id()]
			if !ok || seen[row.id()] || !sameRow(StableIPv6Row(row), expected) || policyFlag(row["dynamic"]) || policyFlag(row["connect"]) {
				return routerError(messageIPv6RoutesOwnership)
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
	if !truthy(previous) {
		return guardRaw(current)
	}
	if !truthy(current) {
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
