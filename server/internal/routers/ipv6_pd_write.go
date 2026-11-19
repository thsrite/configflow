package routers

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"configflow/internal/ctxx"
)

// SetIPv6PDGuardManifest supplies an independent copy of the stored PD receipt.
func (d *RouterOS) SetIPv6PDGuardManifest(raw json.RawMessage) {
	d.ipv6PDGuardManifest = guardRaw(raw)
	d.ipv6PDGuardManifestSet = true
}
func pdCreateGuard(path string, values Row) func([]Row) error {
	return func(rows []Row) error {
		if path == pdScripts || path == pdSchedulers {
			for _, row := range rows {
				if row.text("name") == values.text("name") {
					return routerError(messagePDPreviewChanged)
				}
			}
		}
		return nil
	}
}
func (d *RouterOS) pdGone(ctx context.Context, path, id string) bool {
	_, e := d.get(ctx, path+"/"+escapeROSPathID(id), rosTimeout)
	return routerErrorKind(e) == KindMissingResource
}
func (d *RouterOS) rollbackPD(ctx context.Context, changes []ROSChange) (map[[2]string]string, map[[3]string]bool, error) {
	remap := map[[2]string]string{}
	completed := map[[3]string]bool{}
	failures := []string{}
	for _, c := range changes {
		if e := ctx.Err(); e != nil {
			return remap, completed, e
		}
		allowed := slices.Contains([]string{pdScripts, pdSchedulers, pdClients, pdLists, pdPrefixes}, c.Path)
		if !allowed || !rosIDPattern.MatchString(c.ID) || len(c.Expected) == 0 || !slices.Contains([]string{"PATCH", "RESTORE", "DELETE"}, c.Method) {
			failures = append(failures, messageOperationFormat)
			continue
		}
		if c.Path == pdClients && (c.Method != "PATCH" || len(c.Values) != 1) {
			failures = append(failures, messageOperationFormat)
			continue
		}
		check, e := d.perform(ctx, c, &[]ROSChange{}, true, pdCreateGuard(c.Path, c.Values))
		// The PUT reply proves the restored ID even if its verification read fails.
		// Keep the original config proof; never bless a changed readback as our write.
		if e == nil && c.Method == "RESTORE" && rosIDPattern.MatchString(check.ID) {
			remap[[2]string{c.Path, c.ID}] = check.ID
		}
		if e == nil {
			e = d.verifyChange(ctx, check)
		}
		// A lost compensation reply is not failure when a fresh GET proves absence.
		if e != nil && c.Method == "DELETE" && d.pdGone(ctx, c.Path, c.ID) {
			e = nil
		}
		if e != nil {
			failures = append(failures, e.Error())
		} else {
			completed[[3]string{c.Method, c.Path, c.ID}] = true
			if c.Method == "RESTORE" {
				remap[[2]string{c.Path, c.ID}] = check.ID
			}
		}
	}
	if len(failures) > 0 {
		return remap, completed, rollbackError(messagePDRollback + strings.Join(failures, "；"))
	}
	return remap, completed, nil
}
func pdRemapped(previous json.RawMessage, remap map[[2]string]string) json.RawMessage {
	// Change only IDs; keep unknown document/resource fields and original proofs.
	var doc map[string]json.RawMessage
	var entries []map[string]json.RawMessage
	if json.Unmarshal(previous, &doc) != nil || json.Unmarshal(doc["resources"], &entries) != nil {
		return guardRaw(previous)
	}
	for _, r := range entries {
		var path, id string
		_ = json.Unmarshal(r["path"], &path)
		_ = json.Unmarshal(r["id"], &id)
		if next := remap[[2]string{path, id}]; next != "" {
			r["id"], _ = json.Marshal(next)
		}
	}
	doc["resources"], _ = json.Marshal(entries)
	raw, _ := json.Marshal(doc)
	return raw
}
func (d *RouterOS) commitPD(ctx context.Context, raw json.RawMessage) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	d.SetIPv6PDGuardManifest(raw)
	return nil
}

// pdStoreManifest patches known fields while retaining future document and
// resource members, including when an old ID has just been remapped.
func pdStoreManifest(previous json.RawMessage, spec *IPv6PDSpec, resources []IPv6PDResource) json.RawMessage {
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(previous, &doc)
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}
	var old []map[string]json.RawMessage
	_ = json.Unmarshal(doc["resources"], &old)
	next := []map[string]json.RawMessage{}
	for _, entry := range resources {
		fields := map[string]json.RawMessage{}
		for _, candidate := range old {
			var role, path, id string
			_ = json.Unmarshal(candidate["role"], &role)
			_ = json.Unmarshal(candidate["path"], &path)
			_ = json.Unmarshal(candidate["id"], &id)
			if role == entry.Role && path == entry.Path && id == entry.ID {
				for k, v := range candidate {
					fields[k] = v
				}
				break
			}
		}
		fields["role"], _ = json.Marshal(entry.Role)
		fields["path"], _ = json.Marshal(entry.Path)
		fields["id"], _ = json.Marshal(entry.ID)
		if entry.Config != nil {
			fields["config"], _ = json.Marshal(entry.Config)
		}
		next = append(next, fields)
	}
	doc["version"] = json.RawMessage("1")
	if spec == nil {
		doc["spec"] = json.RawMessage("null")
	} else {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(doc["spec"], &fields)
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		known, _ := json.Marshal(spec)
		var updates map[string]json.RawMessage
		_ = json.Unmarshal(known, &updates)
		for k, v := range updates {
			fields[k] = v
		}
		doc["spec"], _ = json.Marshal(fields)
	}
	doc["resources"], _ = json.Marshal(next)
	raw, _ := json.Marshal(doc)
	return raw
}
func (d *RouterOS) recoverPD(ctx context.Context, previous json.RawMessage, remap map[[2]string]string, created map[string]IPv6PDResource, spec *IPv6PDSpec) error {
	raw := pdRemapped(previous, remap)
	var m IPv6PDManifest
	_ = jsondecode.Unmarshal(raw, &m)
	if len(created) > 0 {
		if m.Version == 0 {
			m = IPv6PDManifest{Version: 1, Spec: spec, Resources: []IPv6PDResource{}}
		}
		for _, role := range []string{"script", "scheduler", "hook"} {
			entry, ok := created[role]
			if !ok {
				continue
			}
			exists := false
			for i, old := range m.Resources {
				if old.Role == role {
					exists = true
					// A trusted surviving creation supersedes an older missing ID for this role.
					// Existing configurations are never refreshed from a new device read.
					if old.ID != entry.ID {
						m.Resources[i] = entry
					}
				}
			}
			if !exists {
				m.Resources = append(m.Resources, entry)
			}
		}
		raw = pdStoreManifest(raw, m.Spec, m.Resources)
	}
	return d.commitPD(ctx, raw)
}

// ApplyIPv6PDGuard re-plans immediately before the first write, then applies
// only the confirmed change list. Every compensation retains its original proof.
func (d *RouterOS) ApplyIPv6PDGuard(ctx context.Context, op IPv6PDGuardSync) (*IPv6PDGuardUndo, error) {
	ctx = ctxx.Detach(ctx)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	router := &Router{RouterSettings: RouterSettings{Type: "routeros", IPv6PDGuard: &PDGuard{Enabled: op.Enabled}}, Extra: map[string]json.RawMessage{"ipv6_pd_guard_state": guardRaw(op.Previous)}}
	fresh, e := InspectIPv6PDGuard(ctx, router, op.Gateway, d)
	if e != nil {
		return nil, e
	}
	if len(fresh.Blockers) > 0 || len(fresh.Ops) != 1 {
		return nil, routerError(messagePDPreviewChanged)
	}
	next, ok := fresh.Ops[0].(*IPv6PDGuardSync)
	if !ok || !reflect.DeepEqual(next.Changes, op.Changes) || !reflect.DeepEqual(next.Spec, op.Spec) || !slices.Equal(next.Owned, op.Owned) {
		return nil, routerError(messagePDPreviewChanged)
	}
	undo := []ROSChange{}
	created := map[string]IPv6PDResource{}
	expected := map[string]IPv6PDResource{}
	for role, entry := range fresh.proofs {
		expected[role] = entry
	}
	apply := func(c IPv6PDChange) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		check, e := d.perform(ctx, c.ROSChange, &undo, false, pdCreateGuard(c.Path, c.Values))
		if check.ID != "" && c.Method == "PUT" {
			proof := d.ownershipSnapshot(c.Path, mergeRows(c.Values, check.created))
			created[c.Role] = IPv6PDResource{c.Role, c.Path, check.ID, proof}
			expected[c.Role] = created[c.Role]
		}
		if e != nil {
			return e
		}
		if e = d.verifyChange(ctx, check); e != nil {
			return e
		}
		if c.Role != "state" && c.Method == "PATCH" {
			entry := expected[c.Role]
			if entry.ID == c.ID {
				var row Row
				_ = json.Unmarshal(c.Expected, &row)
				entry.Config = d.ownershipSnapshot(c.Path, mergeRows(row, c.Values))
				expected[c.Role] = entry
			}
		}
		return nil
	}
	e = func() error {
		for _, c := range op.Changes {
			if e := apply(c); e != nil {
				return e
			}
		}
		if !op.Enabled && slices.ContainsFunc(op.Changes, func(c IPv6PDChange) bool { return c.Path == pdScripts }) {
			for _, path := range []string{pdLists, pdPrefixes} {
				query := path
				if path == pdLists {
					query += "?list=" + pdLast
				}
				rows, e := pdRows(ctx, d, query)
				if e != nil {
					return e
				}
				for _, row := range rows {
					if path == pdLists || pdDeprecated.MatchString(row.text("comment")) && !policyFlag(row["dynamic"]) {
						if e := apply(pdChange(d, "state", "DELETE", path, row, nil)); e != nil {
							return e
						}
					}
				}
			}
		}
		var raw json.RawMessage
		if op.Enabled && len(op.Owned) > 0 {
			live, e := pdSnapshot(ctx, d, false)
			if e != nil {
				return e
			}
			resources := []IPv6PDResource{}
			for _, role := range []string{"script", "scheduler", "hook"} {
				if !slices.Contains(op.Owned, role) {
					continue
				}
				entry, exists := expected[role]
				if !exists || entry.ID == "" {
					return routerError(messagePDNotFound)
				}
				rows := live.clients
				if role == "script" {
					rows = live.scripts
				} else if role == "scheduler" {
					rows = live.schedulers
				}
				row := findRow(rows, entry.ID)
				if row == nil || !sameRow(d.ownershipSnapshot(entry.Path, row), entry.Config) {
					return routerError(messagePDOwnership)
				}
				resources = append(resources, entry)
			}
			raw = pdStoreManifest(op.Previous, op.Spec, resources)
		}
		return d.commitPD(ctx, raw)
	}()
	slices.Reverse(undo)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		remap, completed, rollbackErr := d.rollbackPD(ctx, undo)
		for role, r := range created {
			if completed[[3]string{"DELETE", r.Path, r.ID}] {
				delete(created, role)
			}
		}
		if rollbackErr == nil {
			created = nil
		}
		if commitErr := d.recoverPD(ctx, op.Previous, remap, created, op.Spec); commitErr != nil {
			return nil, commitErr
		}
		if rollbackErr != nil {
			return nil, rollbackError(e.Error() + "；" + rollbackErr.Error())
		}
		return nil, e
	}
	return &IPv6PDGuardUndo{Kind: "ipv6_pd_guard_restore", Changes: undo, Previous: guardRaw(op.Previous)}, nil
}

// RestoreIPv6PDGuard compensates and remaps successful restorations even when
// another step fails. IDs are keyed by table; equal IDs never cross tables.
func (d *RouterOS) RestoreIPv6PDGuard(ctx context.Context, op IPv6PDGuardUndo) error {
	ctx = ctxx.Detach(ctx)
	if e := ctx.Err(); e != nil {
		return e
	}
	remap, completed, e := d.rollbackPD(ctx, op.Changes)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var current IPv6PDManifest
		_ = jsondecode.Unmarshal(d.ipv6PDGuardManifest, &current)
		retained := map[string]IPv6PDResource{}
		for _, r := range current.Resources {
			if r.Config != nil && !completed[[3]string{"DELETE", r.Path, r.ID}] && !completed[[3]string{"PATCH", r.Path, r.ID}] {
				retained[r.Role] = r
			}
		}
		if err := d.recoverPD(ctx, op.Previous, remap, retained, current.Spec); err != nil {
			return err
		}
		return e
	}
	return d.commitPD(ctx, pdRemapped(op.Previous, remap))
}

// VerifyIPv6PDGuard verifies a no-op or changed installation and its manifest.
func VerifyIPv6PDGuard(ctx context.Context, d *RouterOS, check *IPv6PDGuardCheck) error {
	if check == nil {
		return nil
	}
	live, e := pdSnapshot(ctx, d, check.Withdraw)
	if e != nil {
		return e
	}
	if check.Withdraw {
		// A pre-delete hook run can create a new ID after the sweep. The old-ID
		// removal check alone must not report that live runtime state as withdrawn.
		swept := slices.ContainsFunc(check.Removed, func(item [2]string) bool { return item[0] == pdScripts })
		if swept && (len(live.last) > 0 || slices.ContainsFunc(live.prefixes, func(row Row) bool {
			return pdDeprecated.MatchString(row.text("comment")) && !policyFlag(row["dynamic"])
		})) {
			return routerError(messagePDVerifyWithdraw)
		}
		if hasStoredState(d.ipv6PDGuardManifest) {
			return routerError(messagePDVerifyWithdraw)
		}
		for _, v := range []struct {
			path string
			rows []Row
		}{{pdScripts, live.scripts}, {pdSchedulers, live.schedulers}, {pdLists, live.last}, {pdPrefixes, live.prefixes}} {
			for _, r := range v.rows {
				if slices.Contains(check.Removed, [2]string{v.path, r.id()}) {
					return routerError(messagePDVerifyWithdraw)
				}
			}
		}
		return nil
	}
	spec, client, _, e := pdDerive(ctx, d, &live, check.Gateway)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(spec, check.Spec) {
		return routerError(messagePDVerifyLAN)
	}
	script, scheduler := pdNamed(live.scripts, pdWatch), pdNamed(live.schedulers, pdPoll)
	if pdScriptState(script, spec).state != "ok" || pdSchedulerState(scheduler).state != "ok" || !slices.Contains([]string{"ok", "shared"}, pdHookState(client).state) {
		return routerError(messagePDVerifyTemplate)
	}
	owned, e := pdOwned(d, d.ipv6PDGuardManifest, live)
	if e != nil {
		return e
	}
	roles := []string{}
	current := map[string]Row{"script": script, "scheduler": scheduler, "hook": client}
	for role, row := range owned {
		roles = append(roles, role)
		if current[role] == nil || current[role].id() != row.id() {
			return routerError(messagePDVerifyManifest)
		}
	}
	slices.Sort(roles)
	wanted := slices.Clone(check.Owned)
	slices.Sort(wanted)
	if !slices.Equal(roles, wanted) {
		return routerError(messagePDVerifyManifest)
	}
	return nil
}
func init() {
	RegisterRouterOSHandler("ipv6_dns_update", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6DNSUpdate
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		undo, e := d.ApplyIPv6DNS(ctx, op)
		if e != nil {
			return nil, e
		}
		return json.Marshal(undo)
	})
	RegisterRouterOSHandler("ipv6_pd_guard_sync", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6PDGuardSync
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		undo, e := d.ApplyIPv6PDGuard(ctx, op)
		if e != nil {
			return nil, e
		}
		return json.Marshal(undo)
	})
	RegisterRouterOSHandler("ipv6_pd_guard_restore", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op IPv6PDGuardUndo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		if e := d.RestoreIPv6PDGuard(ctx, op); e != nil {
			return nil, e
		}
		return json.RawMessage(`{}`), nil
	})
}
